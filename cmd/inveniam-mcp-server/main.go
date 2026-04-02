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
	)

	// --- API Key Auth ---
	managedKeys, err := loadAPIKeys(cfg, logger)
	if err != nil {
		return err
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

	// --- Admin API Server ---
	adminShutdown, err := startAdminServer(cfg, managedKeys, logger)
	if err != nil {
		return err
	}
	if adminShutdown != nil {
		defer adminShutdown()
	}

	// --- MCP Server ---
	middleware := []mcp.Middleware{
		telemetry.NewMCPMiddleware(tel.Metrics, logger),
	}

	srv := mcpserver.NewServer(
		evmClient, anchorClient,
		cfg.EnableWriteTools, cfg.WriteApprovalDefault,
		middleware, logger,
	)

	switch cfg.Transport {
	case "stdio":
		return srv.RunStdio(ctx)
	case "http":
		return srv.RunHTTP(ctx, cfg.HTTPAddr, managedKeys)
	default:
		return fmt.Errorf("unknown transport %q: %w",
			cfg.Transport, config.ErrInvalidTransport)
	}
}

func loadAPIKeys(cfg *config.Config, logger *slog.Logger) (*mcpserver.ManagedKeyStore, error) {
	switch {
	case cfg.APIKeysFile != "":
		mks, err := mcpserver.NewManagedKeyStore(cfg.APIKeysFile)
		if err != nil {
			return nil, fmt.Errorf("load API keys file: %w", err)
		}
		if mks.Empty() && cfg.Transport == "http" {
			logger.Warn("API keys file has no enabled keys; HTTP transport has no authentication",
				slog.String("file", cfg.APIKeysFile),
			)
		} else {
			logger.Info("loaded API keys",
				slog.String("file", cfg.APIKeysFile),
				slog.Int("total", mks.TotalCount()),
				slog.Int("enabled", mks.ActiveCount()),
			)
		}
		return mks, nil
	case cfg.APIKey != "":
		logger.Info("using single API key from MCP_API_KEY")
		entry := mcpserver.KeyEntry{
			ID:      "static-key",
			Key:     cfg.APIKey,
			Enabled: true,
		}
		return mcpserver.NewManagedKeyStoreFromEntries("", []mcpserver.KeyEntry{entry}), nil
	default:
		if cfg.Transport == "http" {
			logger.Warn("no API keys configured; HTTP transport has no authentication")
		}
		return nil, nil
	}
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
