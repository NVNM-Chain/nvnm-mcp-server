package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	"github.com/inveniam/nvnm-mcp-server/internal/config"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
	"github.com/inveniam/nvnm-mcp-server/internal/logging"
	mcpserver "github.com/inveniam/nvnm-mcp-server/internal/mcp"
	"github.com/inveniam/nvnm-mcp-server/internal/telemetry"
	"github.com/inveniam/nvnm-mcp-server/internal/version"
)

const shutdownTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	transport := flag.String("transport", "",
		"MCP transport: stdio or http (overrides MCP_TRANSPORT env var)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	if *transport != "" {
		cfg.Transport = *transport
		if vErr := cfg.Validate(); vErr != nil {
			return fmt.Errorf("configuration error after flag override: %w", vErr)
		}
	}

	logger := logging.New(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(
		context.Background(), syscall.SIGINT, syscall.SIGTERM,
	)
	defer cancel()

	// --- Telemetry ---
	tel, err := telemetry.New(ctx, telemetry.Config{
		ServiceName:      cfg.OTELServiceName,
		ServiceVersion:   version.Version,
		OTLPEndpoint:     cfg.OTELEndpoint,
		EnablePrometheus: cfg.EnablePrometheus,
		EnableStdout:     cfg.EnableStdoutTel,
		TraceSampleRatio: cfg.OTELTraceSampleRatio,
		OTLPInsecure:     cfg.OTLPInsecure,
	}, logger)
	if err != nil {
		return fmt.Errorf("telemetry init: %w", err)
	}
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutCancel()
		if shutErr := tel.Shutdown(shutCtx); shutErr != nil {
			logger.Error("telemetry shutdown", slog.String("error", shutErr.Error()))
		}
	}()

	logger.Info("starting inveniam-mcp-server",
		slog.String("transport", cfg.Transport),
		slog.Int64("chain_id", cfg.ChainID),
		logging.SafeURL("rpc_url", cfg.EVMRPCURL),
		slog.String("anchor_address", cfg.AnchorAddress),
		slog.String("auth_provider", cfg.AuthProvider),
	)

	// --- Authentication ---
	validator, managedKeys, authCleanup, err := loadAuth(cfg, logger)
	if err != nil {
		return err
	}
	if authCleanup != nil {
		defer authCleanup()
	}

	// --- EVM Client (with tracing wrapper) ---
	rawEVMClient, err := evm.NewClient(ctx, cfg.EVMRPCURL, cfg.RequestTimeout)
	if err != nil {
		return fmt.Errorf("failed to create EVM client: %w", err)
	}
	defer rawEVMClient.Close()

	rpcHost := extractHost(cfg.EVMRPCURL)
	tracingClient := evm.NewTracingClient(rawEVMClient, rpcHost, &evm.TracingMetrics{
		RPCDuration: tel.Metrics.RPCDuration,
		RPCErrors:   tel.Metrics.RPCErrorCount,
	})
	evmClient := evm.NewResilientClient(tracingClient, evm.ResilientConfig{
		MaxRetries:       cfg.RPCMaxRetries,
		InitialBackoff:   cfg.RPCInitialBackoff,
		MaxBackoff:       cfg.RPCMaxBackoff,
		RateLimit:        cfg.RPCRateLimit,
		RateBurst:        cfg.RPCRateBurst,
		BreakerThreshold: cfg.CircuitBreakerThreshold,
		BreakerTimeout:   cfg.CircuitBreakerTimeout,
	}, tel.Metrics, logger)

	// --- Anchor Client ---
	anchorClient := anchor.NewClient(
		evmClient,
		cfg.AnchorAddress,
		cfg.ChainID,
		cfg.AnchorABIPath,
		logger,
	)

	// --- Health Server ---
	healthSrv := telemetry.NewHealthServer(
		cfg.MetricsAddr,
		tel.PrometheusHandler(),
		evmClient,
		anchorClient.Available(),
		logger,
	)
	go func() {
		if hErr := healthSrv.Start(); hErr != nil {
			logger.Error("health server error", slog.String("error", hErr.Error()))
		}
	}()
	defer func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutCancel()
		_ = healthSrv.Close(shutCtx)
	}()

	// --- Admin API Server (API key mode only) ---
	adminShutdown, err := startAdminServer(cfg, managedKeys, logger)
	if err != nil {
		return err
	}
	if adminShutdown != nil {
		defer adminShutdown()
	}

	// --- Per-client MCP rate limiter (HTTP only) ---
	var mcpLimiter *mcpserver.ClientRateLimiter
	var failLimiter *mcpserver.IPFailRateLimiter
	if cfg.Transport == "http" {
		mcpLimiter = mcpserver.NewClientRateLimiter(cfg.MCPRateLimit, cfg.MCPRateBurst)
		mcpLimiter.Start()
		defer mcpLimiter.Stop()
		logger.Info("MCP per-client rate limiter enabled",
			slog.Float64("rps", cfg.MCPRateLimit),
			slog.Int("burst", cfg.MCPRateBurst),
		)

		failLimiter = mcpserver.NewIPFailRateLimiter(
			mcpserver.DefaultFailRatePerSec,
			mcpserver.DefaultFailBurst,
			cfg.TrustProxyHeaders,
		)
		failLimiter.Start()
		defer failLimiter.Stop()
		logger.Info("MCP pre-auth IP failure-rate limiter enabled",
			slog.Float64("rps", mcpserver.DefaultFailRatePerSec),
			slog.Int("burst", mcpserver.DefaultFailBurst),
			slog.Bool("trust_proxy_headers", cfg.TrustProxyHeaders),
		)
	}

	// --- MCP Server ---
	middleware := []mcp.Middleware{
		telemetry.NewMCPMiddleware(tel.Metrics, logger),
	}

	srv := mcpserver.NewServer(
		evmClient, anchorClient,
		cfg.EnableWriteTools, cfg.WriteApprovalDefault,
		string(cfg.ChainEnvironment),
		middleware, logger,
	)

	switch cfg.Transport {
	case "stdio":
		return srv.RunStdio(ctx)
	case "http":
		return srv.RunHTTP(ctx, cfg.HTTPAddr, validator, mcpLimiter, failLimiter, buildOriginAllowlist(cfg))
	default:
		return fmt.Errorf("unknown transport %q: %w",
			cfg.Transport, config.ErrInvalidTransport)
	}
}

// buildOriginAllowlist returns the Origin allowlist for the HTTP
// transport. Nil falls through to DefaultOriginAllowlist inside
// RunHTTP (localhost-only); operators must set NVNM_ALLOWED_ORIGINS
// to expose the server beyond localhost.
func buildOriginAllowlist(cfg *config.Config) *mcpserver.OriginAllowlist {
	if len(cfg.AllowedOrigins) == 0 {
		return nil
	}
	return mcpserver.NewOriginAllowlist(cfg.AllowedOrigins)
}

// loadAuth creates the appropriate TokenValidator based on AUTH_PROVIDER config.
// Returns the validator, the managed key store (only for apikey provider, nil otherwise),
// a cleanup function, and any error.
func loadAuth(
	cfg *config.Config,
	logger *slog.Logger,
) (auth.TokenValidator, *mcpserver.ManagedKeyStore, func(), error) {
	switch cfg.AuthProvider {
	case "fusionauth":
		return loadFusionAuth(cfg, logger)
	default:
		return loadAPIKeys(cfg, logger)
	}
}

func loadFusionAuth(
	cfg *config.Config,
	logger *slog.Logger,
) (auth.TokenValidator, *mcpserver.ManagedKeyStore, func(), error) {
	validator, err := auth.NewFusionAuthValidator(&auth.FusionAuthConfig{
		BaseURL:       cfg.FusionAuthURL,
		ApplicationID: cfg.FusionAuthAppID,
		Issuer:        cfg.GetFusionAuthIssuer(),
		JWKSURL:       cfg.GetFusionAuthJWKSURL(),
		ClockSkew:     cfg.JWTClockSkew,
		RolesClaim:    cfg.JWTRolesClaim,
	}, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("FusionAuth init: %w", err)
	}

	logger.Info("authentication provider: FusionAuth",
		slog.String("fusionauth_url", cfg.FusionAuthURL),
		slog.String("application_id", cfg.FusionAuthAppID),
	)

	cleanup := func() {
		if cErr := validator.Close(); cErr != nil {
			logger.Error("FusionAuth validator close", slog.String("error", cErr.Error()))
		}
	}

	return validator, nil, cleanup, nil
}

func loadAPIKeys(
	cfg *config.Config,
	logger *slog.Logger,
) (auth.TokenValidator, *mcpserver.ManagedKeyStore, func(), error) {
	var managedKeys *mcpserver.ManagedKeyStore

	switch {
	case cfg.APIKeysFile != "":
		mks, err := mcpserver.NewManagedKeyStore(cfg.APIKeysFile)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load API keys file: %w", err)
		}
		if mks.Empty() && cfg.Transport == "http" {
			return nil, nil, nil, fmt.Errorf("%w: file %q has no enabled keys",
				config.ErrHTTPAuthRequired, cfg.APIKeysFile)
		}
		logger.Info("loaded API keys",
			slog.String("file", cfg.APIKeysFile),
			slog.Int("total", mks.TotalCount()),
			slog.Int("enabled", mks.ActiveCount()),
		)
		managedKeys = mks
	case cfg.APIKey != "":
		logger.Info("using single API key from MCP_API_KEY")
		entry := mcpserver.KeyEntry{
			ID:      "static-key",
			Key:     cfg.APIKey,
			Enabled: true,
		}
		managedKeys = mcpserver.NewManagedKeyStoreFromEntries("", []mcpserver.KeyEntry{entry})
	default:
		if cfg.Transport == "http" {
			return nil, nil, nil, config.ErrHTTPAuthRequired
		}
		// stdio transport: unauthenticated is fine -- the transport itself
		// is local-only and operator-controlled.
		return nil, nil, nil, nil
	}

	adapter := mcpserver.NewKeyLookupAdapter(managedKeys)
	validator := auth.NewAPIKeyValidator(adapter)

	var v auth.TokenValidator
	if validator != nil {
		v = validator
	}

	return v, managedKeys, nil, nil
}

func startAdminServer(
	cfg *config.Config,
	keys *mcpserver.ManagedKeyStore,
	logger *slog.Logger,
) (shutdown func(), err error) {
	if cfg.AdminAPIKey == "" {
		return nil, nil
	}
	if cfg.Transport != "http" {
		logger.Warn("ADMIN_API_KEY is set but transport is not HTTP; admin API not started")
		return nil, nil
	}
	if cfg.AuthProvider != "apikey" {
		logger.Info("admin key management API not started (FusionAuth manages users externally)")
		return nil, nil
	}
	if keys == nil {
		return nil, config.ErrAdminKeyWithoutFile
	}

	adminSrv := mcpserver.NewAdminServer(
		cfg.AdminAPIAddr, cfg.AdminAPIKey, keys, logger,
	)
	go func() {
		if aErr := adminSrv.Start(); aErr != nil {
			logger.Error("admin API server error", slog.String("error", aErr.Error()))
		}
	}()

	return func() {
		shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutCancel()
		if shutErr := adminSrv.Close(shutCtx); shutErr != nil {
			logger.Error("admin API shutdown error", slog.String("error", shutErr.Error()))
		}
	}, nil
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	return u.Host
}
