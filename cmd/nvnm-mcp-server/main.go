// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// pgBootTimeout bounds a Postgres connect + migrate at startup so an
// unreachable database cannot hang boot indefinitely. Shared by the key-store
// and keyless-bundle pools.
const pgBootTimeout = 10 * time.Second

// errKeylessDSNInvalid / errKeyStoreDSNInvalid are returned (never wrapped)
// when the respective DSN fails to parse: pgx's parse error can echo the raw
// connection string, including the password, so it must not reach a log or a
// returned error.
var (
	errKeylessDSNInvalid = errors.New(
		"invalid MCP_KEYLESS_PG_DSN: check the DSN format (value withheld from logs)")
	errKeyStoreDSNInvalid = errors.New(
		"invalid KEY_STORE_DSN: check the DSN format (value withheld from logs)")
)

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

	// --- Write-audit store + Admin API Server ---
	writeAudit, adminCleanup, err := startAuditAndAdmin(cfg, managedKeys, pendingStore, emailSender, logger)
	if err != nil {
		return err
	}
	defer adminCleanup()

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
		middleware, writeAudit, tel.Metrics, logger,
	)

	// --- Phase 11 L3: public self-serve key-request endpoint (opt-in) ---
	keyRequestHandler, stopKeyRequest, err := newKeyRequestHandler(cfg, logger)
	if err != nil {
		return err
	}
	defer stopKeyRequest()

	return runTransport(ctx, srv, cfg, validator,
		mcpLimiter, anonLimiter, failLimiter,
		tel.Metrics, keyRequestHandler, cfg.KeyRenewalURL)
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
	renewalURL string,
) error {
	switch cfg.Transport {
	case "stdio":
		return srv.RunStdio(ctx)
	case "http":
		return srv.RunHTTP(
			ctx, cfg.HTTPAddr, validator,
			mcpLimiter, anonLimiter, failLimiter,
			buildOriginAllowlist(cfg), metrics,
			keyRequestHandler, renewalURL,
			cfg.TrustProxyHeaders,
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
		cfg.AnonRateLimit, cfg.AnonRateBurst, cfg.TrustProxyHeaders, cfg.TrustedProxyHops,
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
		cfg.TrustedProxyHops,
	)
	failLimiter.Start()
	logger.Info("MCP pre-auth IP failure-rate limiter enabled",
		slog.Float64("rps", mcpserver.DefaultFailRatePerSec),
		slog.Int("burst", mcpserver.DefaultFailBurst),
		slog.Bool("trust_proxy_headers", cfg.TrustProxyHeaders),
		slog.Int("trusted_proxy_hops", cfg.TrustedProxyHops),
	)
	if _, set := os.LookupEnv("NVNM_TRUSTED_PROXY_HOPS"); set && !cfg.TrustProxyHeaders {
		logger.Warn("NVNM_TRUSTED_PROXY_HOPS set but NVNM_TRUST_PROXY_HEADERS is false; hop count ignored")
	}

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
		Store:            pendingStore,
		RateLimiter:      krLimiter,
		MaxBodyBytes:     cfg.KeyRequestMaxBodyBytes,
		TrustProxy:       cfg.TrustProxyHeaders,
		TrustedProxyHops: cfg.TrustedProxyHops,
		Logger:           logger,
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
// Returns the validator, the key store backend (only for apikey provider, nil otherwise),
// a cleanup function, and any error.
func loadAuth(
	cfg *config.Config,
	logger *slog.Logger,
) (auth.TokenValidator, mcpserver.KeyStoreBackend, func(), error) {
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
) (auth.TokenValidator, mcpserver.KeyStoreBackend, func(), error) {
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

// loadPostgresKeyStore stands up the Postgres-backed API-key store with a
// DSN-safe, bounded boot (mirrors loadWriteAudit). Extracted from loadAPIKeys
// to keep that function within its cyclomatic-complexity budget.
func loadPostgresKeyStore(
	cfg *config.Config, hasher *auth.KeyHasher, logger *slog.Logger,
) (auth.TokenValidator, mcpserver.KeyStoreBackend, func(), error) {
	// Parse before connecting: a malformed DSN produces a pgx error that can
	// echo the connection string (password included), so it must never be
	// wrapped or logged -- return a fixed, credential-free error. The
	// connect/migrate errors below are pgx connection errors (password
	// redacted) and are safe to wrap.
	poolCfg, err := pgxpool.ParseConfig(cfg.KeyStoreDSN)
	if err != nil {
		return nil, nil, nil, errKeyStoreDSNInvalid
	}
	// Bound the boot-time dial + migrate so an unreachable Postgres cannot hang
	// startup indefinitely (CODING_STANDARDS: never allow unbounded waits).
	ctx, cancel := context.WithTimeout(context.Background(), pgBootTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect postgres key store: %w", err)
	}
	// pgxpool is lazy; Ping forces the initial dial under the bounded context
	// so a down database fails boot fast instead of at first lookup.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("ping postgres key store: %w", err)
	}
	if err := mcpserver.RunMigrations(ctx, pool, logger); err != nil {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("migrate postgres key store: %w", err)
	}
	if cfg.KeyHMACPepper == "" {
		pool.Close()
		return nil, nil, nil, config.ErrPepperRequired
	}
	pgStore := mcpserver.NewPostgresKeyStore(pool, hasher)
	if pgStore.Empty() && cfg.Transport == "http" {
		pool.Close()
		return nil, nil, nil, fmt.Errorf("%w: postgres key store has no enabled keys",
			config.ErrHTTPAuthRequired)
	}
	logger.Info("api-key store backend: postgres",
		slog.Int("total", pgStore.TotalCount()),
		slog.Int("enabled", pgStore.ActiveCount()))
	adapter := mcpserver.NewKeyLookupAdapter(pgStore)
	validator := auth.NewAPIKeyValidatorWithHasher(adapter, hasher)
	cleanup := func() { pool.Close() }
	var v auth.TokenValidator
	if validator != nil {
		v = validator
	}
	return v, pgStore, cleanup, nil
}

func loadAPIKeys(
	cfg *config.Config,
	logger *slog.Logger,
) (auth.TokenValidator, mcpserver.KeyStoreBackend, func(), error) {
	hasher := auth.NewKeyHasher([]byte(cfg.KeyHMACPepper), []byte(cfg.KeyHMACPepperPrevious))
	logger.Info("api-key hashing",
		slog.Bool("peppered", cfg.KeyHMACPepper != ""),
		slog.Bool("rotation_window", cfg.KeyHMACPepperPrevious != ""))

	if cfg.KeyStoreBackend == "postgres" {
		return loadPostgresKeyStore(cfg, hasher, logger)
	}

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
		// Static MCP_API_KEY is intentionally non-expiring: ExpiresAt is left
		// as zero (no expiry). KEY_DEFAULT_TTL applies only to admin-issued
		// keys, never to the single-key environment variable path.
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

// startAuditAndAdmin combines loadWriteAudit and startAdminServer into a
// single call so run() does not accumulate their error and nil-check
// branches against its cyclomatic-complexity budget.
func startAuditAndAdmin(
	cfg *config.Config,
	keys mcpserver.KeyStoreBackend,
	pendingStore *mcpserver.PendingKeyStore,
	email mcpserver.EmailSender,
	logger *slog.Logger,
) (mcpserver.WriteAuditStore, func(), error) {
	writeAudit, writeAuditCleanup, err := loadWriteAudit(cfg, logger)
	if err != nil {
		return nil, func() {}, err
	}
	adminShutdown, err := startAdminServer(cfg, keys, pendingStore, email, writeAudit, logger)
	if err != nil {
		writeAuditCleanup()
		return nil, func() {}, err
	}
	return writeAudit, func() {
		if adminShutdown != nil {
			adminShutdown()
		}
		writeAuditCleanup()
	}, nil
}

// loadWriteAudit stands up the dedicated authless-bundle Postgres pool +
// write-audit store. Returns (nil, noop, nil) when persistence is not
// configured (logs-only). Gated on keyless writes + a DSN.
func loadWriteAudit(
	cfg *config.Config, logger *slog.Logger,
) (mcpserver.WriteAuditStore, func(), error) {
	if !cfg.KeylessWrites || cfg.KeylessPGDSN == "" {
		return nil, func() {}, nil
	}
	// Parse the DSN before connecting: a malformed DSN produces a pgx error
	// that can echo the connection string (password included), so it must
	// never be wrapped or logged -- return a fixed, credential-free error.
	// The connect/migrate errors below are pgx connection errors, which
	// redact the password, and are safe to wrap.
	poolCfg, err := pgxpool.ParseConfig(cfg.KeylessPGDSN)
	if err != nil {
		return nil, nil, errKeylessDSNInvalid
	}
	// Bound the boot-time dial + migrate so an unreachable Postgres cannot
	// hang startup indefinitely (CODING_STANDARDS: never allow unbounded waits).
	ctx, cancel := context.WithTimeout(context.Background(), pgBootTimeout)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("connect keyless pg: %w", err)
	}
	// pgxpool is lazy; Ping forces the initial dial under the bounded context
	// so a down database fails boot fast instead of at first write.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("ping keyless pg: %w", err)
	}
	if err := mcpserver.RunMigrations(ctx, pool, logger); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("migrate keyless pg: %w", err)
	}
	logger.Info("write-audit backend: postgres (keyless bundle)")
	return mcpserver.NewPostgresWriteAuditStore(pool), func() { pool.Close() }, nil
}

func startAdminServer(
	cfg *config.Config,
	keys mcpserver.KeyStoreBackend,
	pendingStore *mcpserver.PendingKeyStore,
	email mcpserver.EmailSender,
	writeAudit mcpserver.WriteAuditStore,
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
		cfg.AdminAPIAddr, cfg.AdminAPIKey, keys, cfg.KeyDefaultTTL, logger,
	).WithPendingKeyStore(pendingStore, email).WithWriteAuditStore(writeAudit)
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
