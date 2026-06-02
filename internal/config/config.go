// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sentinel validation errors.
var (
	ErrMissingRPCURL  = errors.New("NVNM_EVM_RPC_URL is required")
	ErrInvalidRPCURL  = errors.New("NVNM_EVM_RPC_URL must start with http:// or https://")
	ErrInvalidChainID = errors.New("NVNM_CHAIN_ID must be a positive integer")
	ErrLegacyEnvVars  = errors.New(
		"legacy INVENIAM_* env vars detected; rename to NVNM_* per docs/RUNBOOK.md#env-var-migration",
	)
	ErrInvalidTimeout         = errors.New("REQUEST_TIMEOUT must be a positive duration")
	ErrInvalidTransport       = errors.New("MCP_TRANSPORT must be \"stdio\" or \"http\"")
	ErrInvalidRetries         = errors.New("RPC_MAX_RETRIES must be non-negative")
	ErrInvalidBackoff         = errors.New("RPC_INITIAL_BACKOFF and RPC_MAX_BACKOFF must be positive durations")
	ErrInvalidRateLimit       = errors.New("RPC_RATE_LIMIT must be positive")
	ErrInvalidRateBurst       = errors.New("RPC_RATE_BURST must be positive")
	ErrInvalidBreakerSettings = errors.New("CIRCUIT_BREAKER_THRESHOLD and CIRCUIT_BREAKER_TIMEOUT must be positive")
	ErrInvalidSampleRatio     = errors.New("OTEL_TRACE_SAMPLE_RATIO must be between 0.0 and 1.0 inclusive")
	ErrInvalidWriteApproval   = errors.New("WRITE_APPROVAL_DEFAULT must be \"required\" or \"auto\"")
	ErrInvalidMCPRateLimit    = errors.New("MCP_RATE_LIMIT must be positive")
	ErrInvalidMCPRateBurst    = errors.New("MCP_RATE_BURST must be positive")
	ErrInvalidAnonRateLimit   = errors.New("MCP_ANON_RATE_LIMIT must be positive")
	ErrInvalidAnonRateBurst   = errors.New("MCP_ANON_RATE_BURST must be positive")
	ErrMissingKeyPendingFile  = errors.New(
		"NVNM_KEY_PENDING_FILE is required when NVNM_KEY_REQUEST_ENABLED is true",
	)
	ErrInvalidKeyRequestRateLimit = errors.New("NVNM_KEY_REQUEST_RATE_LIMIT must be positive")
	ErrInvalidKeyRequestRateBurst = errors.New("NVNM_KEY_REQUEST_RATE_BURST must be positive")
	ErrInvalidKeyRequestMaxBody   = errors.New("NVNM_KEY_REQUEST_MAX_BODY_BYTES must be positive")
	ErrMissingSMTPPort            = errors.New("NVNM_SMTP_PORT is required when NVNM_SMTP_HOST is set")
	ErrMissingSMTPFrom            = errors.New("NVNM_SMTP_FROM is required when NVNM_SMTP_HOST is set")
	ErrAdminKeyWithoutFile        = errors.New("ADMIN_API_KEY requires MCP_API_KEYS_FILE")
	ErrHTTPAuthRequired           = errors.New(
		"HTTP transport requires an authentication provider; " +
			"set MCP_API_KEYS_FILE, MCP_API_KEY, or AUTH_PROVIDER=fusionauth",
	)
	ErrInvalidAuthProvider     = errors.New("AUTH_PROVIDER must be \"apikey\" or \"fusionauth\"")
	ErrMissingFusionAuthURL    = errors.New("FUSIONAUTH_URL is required when AUTH_PROVIDER is \"fusionauth\"")
	ErrMissingFusionAuthAppID  = errors.New("FUSIONAUTH_APPLICATION_ID is required when AUTH_PROVIDER is \"fusionauth\"")
	ErrMissingClientIDHMACKey  = errors.New("MCP_CLIENT_ID_HMAC_KEY is required when AUTH_PROVIDER is \"fusionauth\"")
	ErrInvalidFusionAuthURL    = errors.New("FUSIONAUTH_URL must start with http:// or https://")
	ErrInvalidChainEnvironment = errors.New(`NVNM_CHAIN_ENVIRONMENT must be "testnet" or "mainnet" when set`)
)

// Config holds all server configuration, loaded from environment variables.
// Note: there are no private key fields. The MCP server never holds signing keys;
// write transactions use prepare-sign-submit where signing is external.
type Config struct {
	EVMRPCURL        string
	EVMArchiveRPCURL string
	ChainID          int64
	AnchorAddress    string
	AnchorABIPath    string
	RequestTimeout   time.Duration
	LogLevel         string
	EnableWriteTools bool
	Transport        string
	HTTPAddr         string
	APIKey           string
	APIKeysFile      string
	AdminAPIKey      string
	AdminAPIAddr     string

	// Telemetry
	OTELEndpoint     string
	OTELServiceName  string
	EnablePrometheus bool
	EnableStdoutTel  bool
	OTLPInsecure     bool
	MetricsAddr      string

	// Resilience
	RPCMaxRetries           int
	RPCInitialBackoff       time.Duration
	RPCMaxBackoff           time.Duration
	RPCRateLimit            float64
	RPCRateBurst            int
	CircuitBreakerThreshold uint32
	CircuitBreakerTimeout   time.Duration

	// Trace sampling
	OTELTraceSampleRatio float64

	// Write approval: "required" (default) or "auto"
	WriteApprovalDefault string

	// Per-client MCP rate limiting
	MCPRateLimit float64 // MCP_RATE_LIMIT: requests/second per client (default 60)
	MCPRateBurst int     // MCP_RATE_BURST: burst capacity per client (default 10)

	// Keyless reads (HTTP only). When true, read tools may be called
	// without authentication; write tools keep the auth chain. See
	// docs/superpowers/specs/2026-05-21-keyless-read-auth-middleware-design.md.
	KeylessReads bool // MCP_KEYLESS_READS: allow unauthenticated read tools (default false)

	// Per-IP rate limit for anonymous reads. Must be tighter than the
	// per-client limits above (documented invariant; not enforced here).
	AnonRateLimit float64 // MCP_ANON_RATE_LIMIT: requests/second per source IP (default 5)
	AnonRateBurst int     // MCP_ANON_RATE_BURST: burst capacity per source IP (default 5)

	// Authentication provider: "apikey" (default) or "fusionauth"
	AuthProvider string

	// FusionAuth settings (required when AuthProvider == "fusionauth")
	FusionAuthURL     string // FUSIONAUTH_URL: base URL of the FusionAuth instance
	FusionAuthAppID   string // FUSIONAUTH_APPLICATION_ID: application UUID
	FusionAuthIssuer  string // FUSIONAUTH_ISSUER: expected JWT issuer (defaults to FusionAuthURL)
	FusionAuthJWKSURL string // FUSIONAUTH_JWKS_URL: JWKS endpoint (defaults to BaseURL/jwks.json)
	// FusionAuthClientIDHMACKey (MCP_CLIENT_ID_HMAC_KEY) keys the one-way
	// transform applied to the JWT sub before it becomes the logged
	// client_id. Required when AuthProvider == "fusionauth".
	FusionAuthClientIDHMACKey string
	JWTClockSkew              time.Duration // JWT_CLOCK_SKEW: leeway for token expiry checks (default 60s)
	JWTRolesClaim             string        // JWT_ROLES_CLAIM: name of the roles claim in JWT (default "roles")

	// Chain environment (NVNM-prefixed; introduced in Phase 8).
	// ChainEnvironment selects between testnet and mainnet token naming
	// and operator-facing URL defaults. When NVNM_CHAIN_ENVIRONMENT is
	// unset, the value is inferred from ChainID; chain IDs the server
	// does not recognize fall through to EnvTestnet.
	ChainEnvironment ChainEnvironment

	// DocsURL, ExplorerURL, BridgeURL are operator-facing URLs surfaced
	// in onboarding-tool responses. Optional; empty strings are valid
	// (consumers handle the empty case gracefully). Set via NVNM_DOCS_URL,
	// NVNM_EXPLORER_URL, NVNM_BRIDGE_URL.
	DocsURL     string
	ExplorerURL string
	BridgeURL   string

	// WalletGeneratorURL points the setup wizard's needs_wallet response
	// at the browser-hosted wallet generator page (a sibling repo at
	// NVNM-Chain/nvnm-wallet-page; canonical URL https://wallet.nvnmchain.io).
	// Operators self-hosting the wallet page can override; the default
	// is the canonical Inveniam-hosted instance. Phase 11 D-L8-2.
	WalletGeneratorURL string

	// AllowedOrigins is the comma-separated NVNM_ALLOWED_ORIGINS list
	// parsed into a slice. Empty -> the server falls back to a
	// localhost-only default suitable for local development. Production
	// deployments must set this; the value flows through to the Origin
	// guard middleware on the HTTP transport.
	AllowedOrigins []string

	// TrustProxyHeaders controls whether the pre-auth failure-rate
	// limiter derives the source IP from X-Forwarded-For. Enable only
	// when the server sits behind a reverse proxy that strips
	// client-supplied XFF entries; otherwise an attacker can spoof the
	// header to dodge the limiter. NVNM_TRUST_PROXY_HEADERS env var,
	// default false.
	TrustProxyHeaders bool

	// Phase 11 L3: self-serve API-key request endpoint (POST
	// /api/v1/keys/request). Opt-in; the endpoint is not registered
	// unless KeyRequestEnabled is true. When enabled, KeyPendingFile
	// is required (validated at Load).
	KeyRequestEnabled bool

	// KeyPendingFile is the on-disk path to the pending key-request
	// JSON store. Required when KeyRequestEnabled is true.
	// NVNM_KEY_PENDING_FILE env var.
	KeyPendingFile string

	// KeyRequestRateLimit / KeyRequestRateBurst control the per-source-
	// IP token-bucket on POST /api/v1/keys/request. Defaults are
	// deliberately tight (0.5 rps, burst 3) -- the public endpoint
	// produces durable side effects (a pending row + a reviewer ping)
	// and is not a hot path. NVNM_KEY_REQUEST_RATE_LIMIT (float64,
	// requests per second) and NVNM_KEY_REQUEST_RATE_BURST (int).
	KeyRequestRateLimit float64
	KeyRequestRateBurst int

	// KeyRequestMaxBodyBytes caps the JSON body size for the public
	// key-request endpoint. The outer limitRequestBody middleware caps
	// at MaxRequestBodyBytes (10 MB); this is a tighter, endpoint-
	// scoped cap (default 16 KiB) reflecting the small free-text
	// PII schema in RD1. NVNM_KEY_REQUEST_MAX_BODY_BYTES env var.
	KeyRequestMaxBodyBytes int64

	// Phase 11 RD2: SMTP relay for approval / rejection email delivery.
	// SMTPHost being empty is a valid configuration -- the admin
	// approval flow falls back to a log-only sender so operators
	// without SMTP can still close the loop manually. When SMTPHost
	// is set, SMTPPort and SMTPFrom are required and validated at
	// Load. Username + Password are optional (PlainAuth is attempted
	// only when both are non-empty). FromName is a display-name
	// for the From header.
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string
}

// detectLegacyEnvVars returns the names of any pre-Phase-8.9
// INVENIAM_* env vars currently set in the process environment.
// The legacy set is hard-coded -- only the three vars that the
// server ever read under the old prefix qualify; arbitrary
// INVENIAM_* keys in the environment (from unrelated tooling)
// are ignored. Strict policy: if any are present, we fail loud
// regardless of whether the matching NVNM_* var is also set, so
// operators cannot leave stale config silently in place.
func detectLegacyEnvVars() []string {
	legacyKeys := []string{
		"INVENIAM_EVM_RPC_URL",
		"INVENIAM_EVM_ARCHIVE_RPC_URL",
		"INVENIAM_CHAIN_ID",
	}
	var found []string
	for _, k := range legacyKeys {
		if _, ok := os.LookupEnv(k); ok {
			found = append(found, k)
		}
	}
	return found
}

// Load reads configuration from environment variables and returns a validated Config.
func Load() (*Config, error) {
	if legacy := detectLegacyEnvVars(); len(legacy) > 0 {
		return nil, fmt.Errorf(
			"%w: found %s. Migration table: docs/RUNBOOK.md#env-var-migration",
			ErrLegacyEnvVars, strings.Join(legacy, ", "),
		)
	}
	cfg := &Config{
		EVMRPCURL:        os.Getenv("NVNM_EVM_RPC_URL"),
		EVMArchiveRPCURL: os.Getenv("NVNM_EVM_ARCHIVE_RPC_URL"),
		AnchorAddress:    envOrDefault("ANCHOR_ADDRESS", "0x0000000000000000000000000000000000000A00"),
		AnchorABIPath:    os.Getenv("ANCHOR_ABI_PATH"),
		LogLevel:         envOrDefault("LOG_LEVEL", "info"),
		Transport:        envOrDefault("MCP_TRANSPORT", "stdio"),
		HTTPAddr:         envOrDefault("MCP_HTTP_ADDR", ":8080"),
	}

	chainIDStr := os.Getenv("NVNM_CHAIN_ID")
	if chainIDStr != "" {
		id, err := strconv.ParseInt(chainIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid NVNM_CHAIN_ID %q: %w", chainIDStr, err)
		}
		cfg.ChainID = id
	}

	timeoutStr := envOrDefault("REQUEST_TIMEOUT", "15s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid REQUEST_TIMEOUT %q: %w", timeoutStr, err)
	}
	cfg.RequestTimeout = timeout

	cfg.EnableWriteTools = envOrDefault("ENABLE_WRITE_TOOLS", "false") == "true"
	cfg.APIKey = os.Getenv("MCP_API_KEY")
	cfg.APIKeysFile = os.Getenv("MCP_API_KEYS_FILE")
	cfg.AdminAPIKey = os.Getenv("ADMIN_API_KEY")
	// Default to loopback-only. The admin key is the master key (creates
	// API keys, sets WriteApproval=auto, assigns admin roles), so
	// exposing :8081 cluster-wide is a privilege-escalation foot-gun.
	// Operators that need cluster-internal access set ADMIN_API_ADDR
	// explicitly (e.g. ":8081") and pair it with a NetworkPolicy.
	cfg.AdminAPIAddr = envOrDefault("ADMIN_API_ADDR", "127.0.0.1:8081")

	cfg.OTELEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	cfg.OTELServiceName = envOrDefault("OTEL_SERVICE_NAME", "nvnm-mcp-server")
	cfg.EnablePrometheus = envOrDefault("ENABLE_PROMETHEUS", "true") == "true"
	cfg.EnableStdoutTel = envOrDefault("ENABLE_STDOUT_TELEMETRY", "false") == "true"
	// Secure-by-default: OTLP gRPC connects with TLS unless the
	// operator explicitly opts into insecure (typical for a sidecar
	// collector on localhost). Spans carry pre-sanitization error text
	// and tool-call patterns; an insecure default leaks them on any
	// non-loopback collector path.
	cfg.OTLPInsecure = envOrDefault("OTLP_INSECURE", "false") == "true"
	cfg.MetricsAddr = envOrDefault("METRICS_ADDR", ":9090")

	if loadErr := cfg.loadResilienceConfig(); loadErr != nil {
		return nil, loadErr
	}

	sampleRatioStr := envOrDefault("OTEL_TRACE_SAMPLE_RATIO", "1.0")
	sampleRatio, err := strconv.ParseFloat(sampleRatioStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid OTEL_TRACE_SAMPLE_RATIO %q: %w", sampleRatioStr, err)
	}
	cfg.OTELTraceSampleRatio = sampleRatio

	cfg.WriteApprovalDefault = envOrDefault("WRITE_APPROVAL_DEFAULT", "required")

	if loadErr := cfg.loadMCPRateConfig(); loadErr != nil {
		return nil, loadErr
	}

	if loadErr := cfg.loadKeylessConfig(); loadErr != nil {
		return nil, loadErr
	}

	cfg.AuthProvider = envOrDefault("AUTH_PROVIDER", "apikey")
	cfg.FusionAuthURL = os.Getenv("FUSIONAUTH_URL")
	cfg.FusionAuthAppID = os.Getenv("FUSIONAUTH_APPLICATION_ID")
	cfg.FusionAuthIssuer = os.Getenv("FUSIONAUTH_ISSUER")
	cfg.FusionAuthJWKSURL = os.Getenv("FUSIONAUTH_JWKS_URL")
	cfg.FusionAuthClientIDHMACKey = os.Getenv("MCP_CLIENT_ID_HMAC_KEY")
	cfg.JWTRolesClaim = envOrDefault("JWT_ROLES_CLAIM", "roles")

	clockSkewStr := envOrDefault("JWT_CLOCK_SKEW", "60s")
	clockSkew, err := time.ParseDuration(clockSkewStr)
	if err != nil {
		return nil, fmt.Errorf("invalid JWT_CLOCK_SKEW %q: %w", clockSkewStr, err)
	}
	cfg.JWTClockSkew = clockSkew

	cfg.loadChainEnvironment()
	cfg.DocsURL = os.Getenv("NVNM_DOCS_URL")
	cfg.ExplorerURL = os.Getenv("NVNM_EXPLORER_URL")
	cfg.BridgeURL = os.Getenv("NVNM_BRIDGE_URL")
	cfg.WalletGeneratorURL = envOrDefault("NVNM_WALLET_GENERATOR_URL", "https://wallet.nvnmchain.io")

	if err := cfg.loadKeyRequestConfig(); err != nil {
		return nil, err
	}

	if err := cfg.loadSMTPConfig(); err != nil {
		return nil, err
	}
	cfg.AllowedOrigins = parseCommaSeparated(os.Getenv("NVNM_ALLOWED_ORIGINS"))
	cfg.TrustProxyHeaders = os.Getenv("NVNM_TRUST_PROXY_HEADERS") == "true"

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadResilienceConfig parses the RPC retry / backoff / rate-limit /
// circuit-breaker env vars into c. Extracted from Load() to keep
// Load's cyclomatic complexity below the project's gocyclo threshold;
// each parse adds a branch and these knobs cluster naturally.
func (c *Config) loadResilienceConfig() error {
	retryStr := envOrDefault("RPC_MAX_RETRIES", "3")
	retries, err := strconv.Atoi(retryStr)
	if err != nil {
		return fmt.Errorf("invalid RPC_MAX_RETRIES %q: %w", retryStr, err)
	}
	c.RPCMaxRetries = retries

	initialBackoffStr := envOrDefault("RPC_INITIAL_BACKOFF", "500ms")
	initialBackoff, err := time.ParseDuration(initialBackoffStr)
	if err != nil {
		return fmt.Errorf("invalid RPC_INITIAL_BACKOFF %q: %w", initialBackoffStr, err)
	}
	c.RPCInitialBackoff = initialBackoff

	maxBackoffStr := envOrDefault("RPC_MAX_BACKOFF", "10s")
	maxBackoff, err := time.ParseDuration(maxBackoffStr)
	if err != nil {
		return fmt.Errorf("invalid RPC_MAX_BACKOFF %q: %w", maxBackoffStr, err)
	}
	c.RPCMaxBackoff = maxBackoff

	rateLimitStr := envOrDefault("RPC_RATE_LIMIT", "100")
	rateLimit, err := strconv.ParseFloat(rateLimitStr, 64)
	if err != nil {
		return fmt.Errorf("invalid RPC_RATE_LIMIT %q: %w", rateLimitStr, err)
	}
	c.RPCRateLimit = rateLimit

	rateBurstStr := envOrDefault("RPC_RATE_BURST", "20")
	rateBurst, err := strconv.Atoi(rateBurstStr)
	if err != nil {
		return fmt.Errorf("invalid RPC_RATE_BURST %q: %w", rateBurstStr, err)
	}
	c.RPCRateBurst = rateBurst

	breakerThresholdStr := envOrDefault("CIRCUIT_BREAKER_THRESHOLD", "5")
	breakerThreshold, err := strconv.ParseUint(breakerThresholdStr, 10, 32)
	if err != nil {
		return fmt.Errorf("invalid CIRCUIT_BREAKER_THRESHOLD %q: %w", breakerThresholdStr, err)
	}
	c.CircuitBreakerThreshold = uint32(breakerThreshold)

	breakerTimeoutStr := envOrDefault("CIRCUIT_BREAKER_TIMEOUT", "30s")
	breakerTimeout, err := time.ParseDuration(breakerTimeoutStr)
	if err != nil {
		return fmt.Errorf("invalid CIRCUIT_BREAKER_TIMEOUT %q: %w", breakerTimeoutStr, err)
	}
	c.CircuitBreakerTimeout = breakerTimeout

	return nil
}

// loadChainEnvironment resolves c.ChainEnvironment from
// NVNM_CHAIN_ENVIRONMENT (when set) or by inference from c.ChainID.
// Validate() rejects explicit values that are not "testnet" or
// "mainnet". When the env var is unset and the chain ID is one we
// recognize, the inferred value wins; when neither path resolves an
// environment (operator running against a private fork without
// explicit config), Validate() refuses to start so the operator does
// not unknowingly run against a misclassified chain. Set
// NVNM_CHAIN_ENVIRONMENT explicitly for forks.
func (c *Config) loadChainEnvironment() {
	raw := os.Getenv("NVNM_CHAIN_ENVIRONMENT")
	if raw != "" {
		c.ChainEnvironment = ChainEnvironment(raw)
		return
	}
	if inferred := InferEnvironmentFromChainID(c.ChainID); inferred != "" {
		c.ChainEnvironment = inferred
		return
	}
	// Leave ChainEnvironment as the zero value -- Validate() will
	// surface the missing config as an explicit, fail-fast error.
}

// Validate checks that all required configuration is present and consistent.
func (c *Config) Validate() error {
	if c.EVMRPCURL == "" {
		return ErrMissingRPCURL
	}
	if !strings.HasPrefix(c.EVMRPCURL, "http://") && !strings.HasPrefix(c.EVMRPCURL, "https://") {
		return fmt.Errorf("%w: got %q", ErrInvalidRPCURL, c.EVMRPCURL)
	}
	if c.ChainID <= 0 {
		return ErrInvalidChainID
	}
	if c.RequestTimeout <= 0 {
		return ErrInvalidTimeout
	}
	if c.Transport != "stdio" && c.Transport != "http" {
		return fmt.Errorf("%w: got %q", ErrInvalidTransport, c.Transport)
	}
	if c.WriteApprovalDefault != "required" && c.WriteApprovalDefault != "auto" {
		return fmt.Errorf("%w: got %q", ErrInvalidWriteApproval, c.WriteApprovalDefault)
	}
	if !c.ChainEnvironment.IsValid() {
		return fmt.Errorf("%w: got %q", ErrInvalidChainEnvironment, c.ChainEnvironment)
	}
	if err := c.validateAuth(); err != nil {
		return err
	}
	return c.validateResilience()
}

func (c *Config) validateAuth() error {
	if c.AuthProvider != "apikey" && c.AuthProvider != "fusionauth" {
		return fmt.Errorf("%w: got %q", ErrInvalidAuthProvider, c.AuthProvider)
	}
	if c.AuthProvider == "fusionauth" {
		if c.FusionAuthURL == "" {
			return ErrMissingFusionAuthURL
		}
		if !strings.HasPrefix(c.FusionAuthURL, "http://") && !strings.HasPrefix(c.FusionAuthURL, "https://") {
			return fmt.Errorf("%w: got %q", ErrInvalidFusionAuthURL, c.FusionAuthURL)
		}
		if c.FusionAuthAppID == "" {
			return ErrMissingFusionAuthAppID
		}
		if c.FusionAuthClientIDHMACKey == "" {
			return ErrMissingClientIDHMACKey
		}
	}
	return nil
}

func (c *Config) validateResilience() error {
	if c.RPCMaxRetries < 0 {
		return ErrInvalidRetries
	}
	if c.RPCInitialBackoff <= 0 || c.RPCMaxBackoff <= 0 {
		return ErrInvalidBackoff
	}
	if c.RPCRateLimit <= 0 {
		return ErrInvalidRateLimit
	}
	if c.RPCRateBurst <= 0 {
		return ErrInvalidRateBurst
	}
	if c.CircuitBreakerThreshold == 0 || c.CircuitBreakerTimeout <= 0 {
		return ErrInvalidBreakerSettings
	}
	if c.OTELTraceSampleRatio < 0 || c.OTELTraceSampleRatio > 1 {
		return ErrInvalidSampleRatio
	}
	if c.MCPRateLimit <= 0 {
		return ErrInvalidMCPRateLimit
	}
	if c.MCPRateBurst <= 0 {
		return ErrInvalidMCPRateBurst
	}
	if c.AnonRateLimit <= 0 {
		return ErrInvalidAnonRateLimit
	}
	if c.AnonRateBurst <= 0 {
		return ErrInvalidAnonRateBurst
	}
	return nil
}

func (c *Config) loadMCPRateConfig() error {
	rateLimitStr := envOrDefault("MCP_RATE_LIMIT", "60")
	rateLimit, err := strconv.ParseFloat(rateLimitStr, 64)
	if err != nil {
		return fmt.Errorf("invalid MCP_RATE_LIMIT %q: %w", rateLimitStr, err)
	}
	c.MCPRateLimit = rateLimit

	rateBurstStr := envOrDefault("MCP_RATE_BURST", "10")
	rateBurst, err := strconv.Atoi(rateBurstStr)
	if err != nil {
		return fmt.Errorf("invalid MCP_RATE_BURST %q: %w", rateBurstStr, err)
	}
	c.MCPRateBurst = rateBurst
	return nil
}

// loadKeylessConfig parses the keyless-reads flag and the anonymous
// per-IP rate-limit knobs. Anon defaults are deliberately tighter than
// the per-client defaults.
func (c *Config) loadKeylessConfig() error {
	c.KeylessReads = envOrDefault("MCP_KEYLESS_READS", "false") == "true"

	anonLimitStr := envOrDefault("MCP_ANON_RATE_LIMIT", "5")
	anonLimit, err := strconv.ParseFloat(anonLimitStr, 64)
	if err != nil {
		return fmt.Errorf("invalid MCP_ANON_RATE_LIMIT %q: %w", anonLimitStr, err)
	}
	c.AnonRateLimit = anonLimit

	anonBurstStr := envOrDefault("MCP_ANON_RATE_BURST", "5")
	anonBurst, err := strconv.Atoi(anonBurstStr)
	if err != nil {
		return fmt.Errorf("invalid MCP_ANON_RATE_BURST %q: %w", anonBurstStr, err)
	}
	c.AnonRateBurst = anonBurst
	return nil
}

// loadKeyRequestConfig parses NVNM_KEY_REQUEST_* env vars and validates
// them. KeyRequestEnabled is opt-in; everything else is only consulted
// when enabled. When enabled, KeyPendingFile is required (the on-disk
// store has nowhere to persist without it) and the rate-limit knobs
// must be positive.
func (c *Config) loadKeyRequestConfig() error {
	c.KeyRequestEnabled = envOrDefault("NVNM_KEY_REQUEST_ENABLED", "false") == "true"
	c.KeyPendingFile = os.Getenv("NVNM_KEY_PENDING_FILE")

	limitStr := envOrDefault("NVNM_KEY_REQUEST_RATE_LIMIT", "0.5")
	limit, err := strconv.ParseFloat(limitStr, 64)
	if err != nil {
		return fmt.Errorf("invalid NVNM_KEY_REQUEST_RATE_LIMIT %q: %w", limitStr, err)
	}
	c.KeyRequestRateLimit = limit

	burstStr := envOrDefault("NVNM_KEY_REQUEST_RATE_BURST", "3")
	burst, err := strconv.Atoi(burstStr)
	if err != nil {
		return fmt.Errorf("invalid NVNM_KEY_REQUEST_RATE_BURST %q: %w", burstStr, err)
	}
	c.KeyRequestRateBurst = burst

	maxBodyStr := envOrDefault("NVNM_KEY_REQUEST_MAX_BODY_BYTES", "16384")
	maxBody, err := strconv.ParseInt(maxBodyStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid NVNM_KEY_REQUEST_MAX_BODY_BYTES %q: %w", maxBodyStr, err)
	}
	c.KeyRequestMaxBodyBytes = maxBody

	if !c.KeyRequestEnabled {
		return nil
	}
	if c.KeyPendingFile == "" {
		return ErrMissingKeyPendingFile
	}
	if c.KeyRequestRateLimit <= 0 {
		return ErrInvalidKeyRequestRateLimit
	}
	if c.KeyRequestRateBurst <= 0 {
		return ErrInvalidKeyRequestRateBurst
	}
	if c.KeyRequestMaxBodyBytes <= 0 {
		return ErrInvalidKeyRequestMaxBody
	}
	return nil
}

// loadSMTPConfig parses NVNM_SMTP_* env vars and validates them. Empty
// SMTPHost is a valid configuration (the admin approval flow falls back
// to a log-only sender so operators without SMTP can still complete
// reviews manually). When SMTPHost is set, SMTPPort and SMTPFrom are
// required; missing values fail loud with named sentinel errors.
func (c *Config) loadSMTPConfig() error {
	c.SMTPHost = os.Getenv("NVNM_SMTP_HOST")
	c.SMTPUsername = os.Getenv("NVNM_SMTP_USERNAME")
	c.SMTPPassword = os.Getenv("NVNM_SMTP_PASSWORD")
	c.SMTPFrom = os.Getenv("NVNM_SMTP_FROM")
	c.SMTPFromName = os.Getenv("NVNM_SMTP_FROM_NAME")

	portStr := os.Getenv("NVNM_SMTP_PORT")
	if portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("invalid NVNM_SMTP_PORT %q: %w", portStr, err)
		}
		c.SMTPPort = port
	}

	if c.SMTPHost == "" {
		return nil
	}
	if c.SMTPPort == 0 {
		return ErrMissingSMTPPort
	}
	if c.SMTPFrom == "" {
		return ErrMissingSMTPFrom
	}
	return nil
}

// GetFusionAuthIssuer returns the expected JWT issuer. Falls back to FusionAuthURL.
func (c *Config) GetFusionAuthIssuer() string {
	if c.FusionAuthIssuer != "" {
		return c.FusionAuthIssuer
	}
	return c.FusionAuthURL
}

// GetFusionAuthJWKSURL returns the JWKS endpoint. Falls back to FusionAuthURL + /.well-known/jwks.json.
func (c *Config) GetFusionAuthJWKSURL() string {
	if c.FusionAuthJWKSURL != "" {
		return c.FusionAuthJWKSURL
	}
	return strings.TrimRight(c.FusionAuthURL, "/") + "/.well-known/jwks.json"
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseCommaSeparated splits a comma-separated string, trimming
// whitespace and dropping empty entries. Returns nil for an empty
// input so callers can branch on len() == 0.
func parseCommaSeparated(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
