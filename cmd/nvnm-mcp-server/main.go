// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
	mcpserver "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/version"
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

	logger.Info("starting nvnm-mcp-server",
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

	// --- Pending key-request store + email sender (Phase 11 L3) ---
	pendingStore, emailSender, err := newPendingAndEmail(cfg, logger)
	if err != nil {
		return err
	}

	// --- Admin API Server (API key mode only) ---
	adminShutdown, err := startAdminServer(cfg, managedKeys, pendingStore, emailSender, logger)
	if err != nil {
		return err
	}
	if adminShutdown != nil {
		defer adminShutdown()
	}

	// --- Per-client / anonymous / fail-rate limiters (HTTP only) ---
	var (
		mcpLimiter  *mcpserver.ClientRateLimiter
		anonLimiter *mcpserver.AnonReadRateLimiter
		failLimiter *mcpserver.IPFailRateLimiter
	)
	if cfg.Transport == "http" {
		var stopLimiters func()
		mcpLimiter, anonLimiter, failLimiter, stopLimiters = newHTTPLimiters(cfg, logger)
		defer stopLimiters()
	}

	// --- MCP Server ---
	middleware := []mcp.Middleware{
		telemetry.NewMCPMiddleware(tel.Metrics, logger),
	}

	srv := mcpserver.NewServer(
		evmClient, anchorClient,
		cfg,
		middleware, logger,
	)

	// --- Phase 11 L3: public self-serve key-request endpoint (opt-in) ---
	keyRequestHandler, stopKeyRequest, err := newKeyRequestHandler(cfg, logger)
	if err != nil {
		return err
	}
	defer stopKeyRequest()

	return runTransport(ctx, srv, cfg, validator,
		mcpLimiter, anonLimiter, failLimiter,
		tel.Metrics, keyRequestHandler)
}

// runTransport dispatches to the configured transport. Extracted from
// run() so the dispatch's switch + error path do not consume run's
// cyclomatic-complexity budget.
func runTransport(
	ctx context.Context,
	srv *mcpserver.Server,
	cfg *config.Config,
	validator auth.TokenValidator,
	mcpLimiter *mcpserver.ClientRateLimiter,
	anonLimiter *mcpserver.AnonReadRateLimiter,
	failLimiter *mcpserver.IPFailRateLimiter,
	metrics *telemetry.Metrics,
	keyRequestHandler http.Handler,
) error {
	switch cfg.Transport {
	case "stdio":
		return srv.RunStdio(ctx)
	case "http":
		return srv.RunHTTP(
			ctx, cfg.HTTPAddr, validator,
			mcpLimiter, anonLimiter, failLimiter,
			buildOriginAllowlist(cfg), metrics,
			keyRequestHandler,
		)
	default:
		return fmt.Errorf("unknown transport %q: %w",
			cfg.Transport, config.ErrInvalidTransport)
	}
}

// newHTTPLimiters constructs the three HTTP-transport limiters
// (per-client, anonymous per-IP, pre-auth IP failure rate) and returns
// a single stop function that drains all three on shutdown. Extracted
// from run() so the cyclomatic-complexity budget there is not consumed
// by limiter wiring.
//
// All three constructors are infallible today, so the helper returns no
// error. If a future limiter gains a fallible Start, this signature must
// change to (..., error) AND the caller must take ownership of stopping
// any already-started limiter on the failure path -- the current
// implementation would leak janitor goroutines.
func newHTTPLimiters(cfg *config.Config, logger *slog.Logger) (
	*mcpserver.ClientRateLimiter,
	*mcpserver.AnonReadRateLimiter,
	*mcpserver.IPFailRateLimiter,
	func(),
) {
	mcpLimiter := mcpserver.NewClientRateLimiter(cfg.MCPRateLimit, cfg.MCPRateBurst)
	mcpLimiter.Start()
	logger.Info("MCP per-client rate limiter enabled",
		slog.Float64("rps", cfg.MCPRateLimit),
		slog.Int("burst", cfg.MCPRateBurst),
	)

	// The anon limiter is built unconditionally so request-path checks
	// stay branch-free. When KeylessReads is off, AuthMiddleware rejects
	// anonymous traffic upstream and the limiter never sees a request;
	// the goroutine is idle but observable in the startup log below.
	anonLimiter := mcpserver.NewAnonReadRateLimiter(
		cfg.AnonRateLimit, cfg.AnonRateBurst, cfg.TrustProxyHeaders,
	)
	anonLimiter.Start()
	logger.Info("MCP anonymous per-IP read limiter started",
		slog.Float64("anon_rps", cfg.AnonRateLimit),
		slog.Int("anon_burst", cfg.AnonRateBurst),
		slog.Bool("keyless_reads", cfg.KeylessReads),
	)

	failLimiter := mcpserver.NewIPFailRateLimiter(
		mcpserver.DefaultFailRatePerSec,
		mcpserver.DefaultFailBurst,
		cfg.TrustProxyHeaders,
	)
	failLimiter.Start()
	logger.Info("MCP pre-auth IP failure-rate limiter enabled",
		slog.Float64("rps", mcpserver.DefaultFailRatePerSec),
		slog.Int("burst", mcpserver.DefaultFailBurst),
		slog.Bool("trust_proxy_headers", cfg.TrustProxyHeaders),
	)

	stop := func() {
		mcpLimiter.Stop()
		anonLimiter.Stop()
		failLimiter.Stop()
	}
	return mcpLimiter, anonLimiter, failLimiter, stop
}

// newKeyRequestHandler builds the Phase 11 L3 public self-serve key-
// request endpoint when cfg.KeyRequestEnabled is true and the transport
// is HTTP. Returns the handler (nil = endpoint disabled), a stop
// function (always non-nil, no-op when disabled so callers can defer
// unconditionally), and any error. Extracted from run() so that
// function's cyclomatic-complexity budget is not consumed by the
// wiring.
func newKeyRequestHandler(cfg *config.Config, logger *slog.Logger) (http.Handler, func(), error) {
	noop := func() {}
	if !cfg.KeyRequestEnabled || cfg.Transport != "http" {
		return nil, noop, nil
	}
	pendingStore, err := mcpserver.NewPendingKeyStore(cfg.KeyPendingFile)
	if err != nil {
		return nil, noop, fmt.Errorf("init pending key store: %w", err)
	}
	krLimiter := mcpserver.NewKeyRequestRateLimiter(
		cfg.KeyRequestRateLimit,
		cfg.KeyRequestRateBurst,
		cfg.TrustProxyHeaders,
	)
	krLimiter.Start()
	handler := mcpserver.NewKeyRequestHandler(mcpserver.KeyRequestHandlerConfig{
		Store:        pendingStore,
		RateLimiter:  krLimiter,
		MaxBodyBytes: cfg.KeyRequestMaxBodyBytes,
		TrustProxy:   cfg.TrustProxyHeaders,
		Logger:       logger,
	})
	logger.Info("self-serve key-request endpoint enabled",
		slog.String("path", mcpserver.KeyRequestPath),
		slog.String("pending_file", cfg.KeyPendingFile),
		slog.Float64("rate_per_sec", cfg.KeyRequestRateLimit),
		slog.Int("burst", cfg.KeyRequestRateBurst),
	)
	return handler, krLimiter.Stop, nil
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
		BaseURL:         cfg.FusionAuthURL,
		ApplicationID:   cfg.FusionAuthAppID,
		Issuer:          cfg.GetFusionAuthIssuer(),
		JWKSURL:         cfg.GetFusionAuthJWKSURL(),
		ClockSkew:       cfg.JWTClockSkew,
		RolesClaim:      cfg.JWTRolesClaim,
		ClientIDHMACKey: []byte(cfg.FusionAuthClientIDHMACKey),
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
	hasher := auth.NewKeyHasher([]byte(cfg.KeyHMACPepper), []byte(cfg.KeyHMACPepperPrevious))
	logger.Info("api-key hashing",
		slog.Bool("peppered", cfg.KeyHMACPepper != ""),
		slog.Bool("rotation_window", cfg.KeyHMACPepperPrevious != ""))

	var managedKeys *mcpserver.ManagedKeyStore

	switch {
	case cfg.APIKeysFile != "":
		mks, err := mcpserver.NewManagedKeyStoreWithHasher(cfg.APIKeysFile, hasher, logger)
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
		logger.Info("using single API key from MCP_API_KEY",
			slog.Any("roles", cfg.APIKeyRoles))
		entry := mcpserver.NewKeyEntryWithHasher("static-key", cfg.APIKey, cfg.APIKeyRoles, hasher)
		managedKeys = mcpserver.NewManagedKeyStoreFromEntriesWithHasher("", []mcpserver.KeyEntry{entry}, hasher)
	default:
		if cfg.Transport == "http" {
			return nil, nil, nil, config.ErrHTTPAuthRequired
		}
		// stdio transport: unauthenticated is fine -- the transport itself
		// is local-only and operator-controlled.
		return nil, nil, nil, nil
	}

	adapter := mcpserver.NewKeyLookupAdapter(managedKeys)
	validator := auth.NewAPIKeyValidatorWithHasher(adapter, hasher)

	var v auth.TokenValidator
	if validator != nil {
		v = validator
	}

	return v, managedKeys, nil, nil
}

func startAdminServer(
	cfg *config.Config,
	keys *mcpserver.ManagedKeyStore,
	pendingStore *mcpserver.PendingKeyStore,
	email mcpserver.EmailSender,
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
	).WithPendingKeyStore(pendingStore, email)
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

// newPendingAndEmail constructs the Phase 11 L3 pending-key store
// (when KeyRequestEnabled) and the email sender (always; falls back
// to log-only when SMTP is not configured). Returns nil for the store
// when the feature is opt-out so callers stay branch-free. Extracted
// from run() so its complexity budget is not consumed by the wiring.
func newPendingAndEmail(cfg *config.Config, logger *slog.Logger) (
	*mcpserver.PendingKeyStore, mcpserver.EmailSender, error,
) {
	var store *mcpserver.PendingKeyStore
	if cfg.KeyRequestEnabled {
		s, err := mcpserver.NewPendingKeyStore(cfg.KeyPendingFile)
		if err != nil {
			return nil, nil, fmt.Errorf("init pending key store: %w", err)
		}
		store = s
	}
	return store, buildEmailSender(cfg, logger), nil
}

// buildEmailSender returns the EmailSender used by the admin pending-
// request approval/rejection flow. When NVNM_SMTP_HOST is set, builds
// an SMTPEmailSender against the configured relay; otherwise falls
// back to a log-only sender so the approval flow still completes (the
// operator copies the freshly-minted key out of structured logs).
// Config-validation already failed loud if SMTP_HOST was set but
// SMTP_PORT or SMTP_FROM were missing, so the SMTPEmailSender
// constructor here cannot return ErrEmailNotConfigured in practice.
func buildEmailSender(cfg *config.Config, logger *slog.Logger) mcpserver.EmailSender {
	if cfg.SMTPHost == "" {
		logger.Info("SMTP not configured; admin approvals will use log-only email sender",
			slog.String("hint", "set NVNM_SMTP_HOST / NVNM_SMTP_PORT / NVNM_SMTP_FROM to enable delivery"),
		)
		return mcpserver.NewLogOnlyEmailSender(logger)
	}
	sender, err := mcpserver.NewSMTPEmailSender(&mcpserver.SMTPConfig{
		Host:     cfg.SMTPHost,
		Port:     cfg.SMTPPort,
		Username: cfg.SMTPUsername,
		Password: cfg.SMTPPassword,
		From:     cfg.SMTPFrom,
		FromName: cfg.SMTPFromName,
	}, logger)
	if err != nil {
		// Should be unreachable -- config.Load already gates on the
		// required fields. Fall back to log-only so the server still
		// starts and the operator can fix the config.
		logger.Error("SMTP sender construction failed; falling back to log-only",
			slog.String("error", err.Error()),
		)
		return mcpserver.NewLogOnlyEmailSender(logger)
	}
	logger.Info("SMTP email sender configured",
		slog.String("host", cfg.SMTPHost),
		slog.Int("port", cfg.SMTPPort),
		slog.String("from", cfg.SMTPFrom),
		slog.Bool("auth", cfg.SMTPUsername != ""),
	)
	return sender
}

func extractHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	return u.Host
}
