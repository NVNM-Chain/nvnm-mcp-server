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

	defitypes "github.com/defiweb/go-eth/types"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// Sentinel validation errors.
var (
	ErrMissingRPCURL  = errors.New("NVNM_EVM_RPC_URL is required")
	ErrInvalidRPCURL  = errors.New("NVNM_EVM_RPC_URL must start with http:// or https://")
	ErrInvalidChainID = errors.New("NVNM_CHAIN_ID must be a positive integer")
	ErrLegacyEnvVars  = errors.New(
		"legacy INVENIAM_* env vars detected; rename to NVNM_* per docs/RUNBOOK.md#env-var-migration",
	)
	// ErrLegacyWriteApproval is returned when the retired WRITE_APPROVAL_DEFAULT
	// env var is detected at startup. Server-side write approval was removed in
	// Option 0; writes now gate on RBAC + ENABLE_WRITE_TOOLS only.
	ErrLegacyWriteApproval = errors.New(
		"legacy WRITE_APPROVAL_DEFAULT env var detected; server-side write " +
			"approval was removed in Option 0 (writes gate on RBAC + " +
			"ENABLE_WRITE_TOOLS). Remove it per docs/RUNBOOK.md#write-approval-removal")
	ErrInvalidTimeout         = errors.New("REQUEST_TIMEOUT must be a positive duration")
	ErrInvalidTransport       = errors.New("MCP_TRANSPORT must be \"stdio\" or \"http\"")
	ErrInvalidRetries         = errors.New("RPC_MAX_RETRIES must be non-negative")
	ErrInvalidBackoff         = errors.New("RPC_INITIAL_BACKOFF and RPC_MAX_BACKOFF must be positive durations")
	ErrInvalidRateLimit       = errors.New("RPC_RATE_LIMIT must be positive")
	ErrInvalidRateBurst       = errors.New("RPC_RATE_BURST must be positive")
	ErrInvalidBreakerSettings = errors.New("CIRCUIT_BREAKER_THRESHOLD and CIRCUIT_BREAKER_TIMEOUT must be positive")
	ErrInvalidSampleRatio     = errors.New("OTEL_TRACE_SAMPLE_RATIO must be between 0.0 and 1.0 inclusive")
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
	// ErrKeyInLogsNotAllowed (F4) is returned when the self-serve key-request
	// flow is enabled without SMTP and without the explicit
	// NVNM_ALLOW_KEY_IN_LOGS acknowledgement: the approval path would fall
	// back to writing minted API keys to logs, which must be a deliberate
	// operator choice, not a silent default.
	ErrKeyInLogsNotAllowed = errors.New(
		"NVNM_KEY_REQUEST_ENABLED without NVNM_SMTP_HOST would log minted API " +
			"keys via the log-only email sender; configure SMTP or set " +
			"NVNM_ALLOW_KEY_IN_LOGS=true to accept logging keys")
	ErrAdminKeyWithoutFile = errors.New("ADMIN_API_KEY requires MCP_API_KEYS_FILE")
	ErrHTTPAuthRequired    = errors.New(
		"HTTP transport requires an authentication provider; " +
			"set MCP_API_KEYS_FILE, MCP_API_KEY, or AUTH_PROVIDER=fusionauth",
	)
	ErrInvalidAuthProvider     = errors.New("AUTH_PROVIDER must be \"apikey\" or \"fusionauth\"")
	ErrMissingFusionAuthURL    = errors.New("FUSIONAUTH_URL is required when AUTH_PROVIDER is \"fusionauth\"")
	ErrMissingFusionAuthAppID  = errors.New("FUSIONAUTH_APPLICATION_ID is required when AUTH_PROVIDER is \"fusionauth\"")
	ErrMissingClientIDHMACKey  = errors.New("MCP_CLIENT_ID_HMAC_KEY is required when AUTH_PROVIDER is \"fusionauth\"")
	ErrInvalidFusionAuthURL    = errors.New("FUSIONAUTH_URL must start with http:// or https://")
	ErrInvalidChainEnvironment = errors.New(`NVNM_CHAIN_ENVIRONMENT must be "testnet" or "mainnet" when set`)
	ErrStaticKeyRolesRequired  = errors.New(
		"MCP_API_KEY_ROLES is required when MCP_API_KEY is set (set it to a comma list of reader, writer, admin, automation)")
	ErrInvalidRole = errors.New(
		"MCP_API_KEY_ROLES contains an unknown role; valid roles are reader, writer, admin, automation")
	// ErrPepperPreviousWithoutActive is returned when KEY_HMAC_PEPPER_PREVIOUS
	// is set but KEY_HMAC_PEPPER is empty. A previous pepper without an active
	// pepper is a misconfiguration: key verification would never succeed against
	// freshly-peppered keys. Unset the previous pepper or supply the active one.
	ErrPepperPreviousWithoutActive = errors.New(
		"KEY_HMAC_PEPPER_PREVIOUS is set without KEY_HMAC_PEPPER; " +
			"set the active pepper or unset the previous one")
	ErrInvalidKeyStoreBackend = errors.New(
		`KEY_STORE_BACKEND must be "file" or "postgres"`)
	ErrKeyStoreDSNRequired = errors.New(
		"KEY_STORE_DSN is required when KEY_STORE_BACKEND is \"postgres\"")
	ErrPepperRequired = errors.New(
		"KEY_HMAC_PEPPER is required when KEY_STORE_BACKEND is \"postgres\" and " +
			"AUTH_PROVIDER is \"apikey\" (a peppered Postgres store must not run unpeppered)")
	ErrKeylessWritesRequiresDSN = errors.New(
		"MCP_KEYLESS_PG_DSN is required when MCP_KEYLESS_WRITES is true " +
			"(keyless writes without a shared-state audit backend is not a supported mode; " +
			"the persisted write-audit is a security control, not optional)")
	// ErrKeylessWritesRequiresReads is returned when MCP_KEYLESS_WRITES is
	// true but MCP_KEYLESS_READS is not: anonymous writes need the
	// anonymous HTTP path enabled to be reachable at all.
	ErrKeylessWritesRequiresReads = errors.New(
		"MCP_KEYLESS_WRITES=true requires MCP_KEYLESS_READS=true " +
			"(anonymous writes need the anonymous HTTP path enabled)")
	// ErrAnchorAddressInvalid is returned when ANCHOR_ADDRESS does not parse
	// as a valid address while MCP_KEYLESS_WRITES is true. Parsed with the
	// same defitypes.AddressFromHex the runtime relay handler uses, so this
	// boot guard is exactly as strict as the runtime parse.
	ErrAnchorAddressInvalid = errors.New(
		"ANCHOR_ADDRESS is not a valid address (required when MCP_KEYLESS_WRITES=true)")
	ErrSignerWriteRateInvalid   = errors.New("MCP_SIGNER_WRITE_RATE must be >= 1")
	ErrSignerWriteWindowInvalid = errors.New("MCP_SIGNER_WRITE_WINDOW must be > 0")
	ErrInvalidTrustedProxyHops  = errors.New("NVNM_TRUSTED_PROXY_HOPS must be >= 1")
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
	APIKeyRoles      []string
	// KeyHMACPepper is the active HMAC pepper applied to stored key hashes
	// (KEY_HMAC_PEPPER). Optional; when unset, key hashes are unpeppered.
	KeyHMACPepper string
	// KeyHMACPepperPrevious is the pepper used for key hashes that were
	// written before the last pepper rotation (KEY_HMAC_PEPPER_PREVIOUS).
	// Requires KeyHMACPepper to be set; a previous pepper without an active
	// pepper is always a misconfiguration.
	KeyHMACPepperPrevious string
	KeyStoreBackend       string
	KeyStoreDSN           string
	// KeyDefaultTTL is the default lifetime applied to newly issued API keys
	// (KEY_DEFAULT_TTL, default 8760h ≈ 1 year). Applied by the issuing
	// caller; a zero per-key override means no expiry.
	KeyDefaultTTL time.Duration
	// KeyRenewalURL, when set (KEY_RENEWAL_URL), is appended to the
	// expired-key reject message so the holder learns where to renew.
	KeyRenewalURL string
	APIKeysFile   string
	AdminAPIKey   string
	AdminAPIAddr  string

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

	// Per-client MCP rate limiting
	MCPRateLimit float64 // MCP_RATE_LIMIT: requests/second per client (default 60)
	MCPRateBurst int     // MCP_RATE_BURST: burst capacity per client (default 10)

	// Keyless reads (HTTP only). When true, read tools may be called
	// without authentication; write tools keep the auth chain. See the
	// keyless-read auth-middleware design (Phase 9.16).
	KeylessReads bool // MCP_KEYLESS_READS: allow unauthenticated read tools (default false)

	// Keyless writes (HTTP only, authless connector). When true, the
	// evm_send_raw_transaction relay is restricted to the anchor precompile
	// (precompile-only scope) and broadcasts the canonical re-serialization.
	// Default false: authed/self-host keeps the general-purpose relay (D9).
	KeylessWrites bool // MCP_KEYLESS_WRITES: precompile-only authless write relay (default false)

	// KeylessPGDSN is the dedicated Postgres DSN for the authless-bundle
	// shared state (write_audit now; per-signer quota/blacklist later).
	// Separate from KEY_STORE_DSN: hosted authless runs no key store.
	// Empty => logs-only audit, no persistence. MCP_KEYLESS_PG_DSN.
	KeylessPGDSN string

	// SignerWriteRate is the max number of anonymous-write transactions a
	// single signer may submit within SignerWriteWindow. MCP_SIGNER_WRITE_RATE
	// env var, default 500.
	SignerWriteRate int
	// SignerWriteWindow is the fixed window SignerWriteRate is measured
	// over. It is a discrete, boundary-aligned bucket (the quota counter
	// truncates now to this window via WindowStart), not a sliding window.
	// MCP_SIGNER_WRITE_WINDOW env var, default 24h.
	SignerWriteWindow time.Duration
	// SignerQuotaFailOpen controls what happens when the per-signer quota
	// check itself fails (e.g. the keyless Postgres pool is unreachable).
	// Default false: fail closed (reject the write) rather than silently
	// admitting unbounded writes. MCP_SIGNER_QUOTA_FAIL_OPEN env var.
	SignerQuotaFailOpen bool
	// SignerBlacklistFailOpen controls what happens when the per-signer
	// blacklist check itself fails. Default false: fail closed (reject the
	// write) rather than silently admitting a blacklisted signer.
	// MCP_SIGNER_BLACKLIST_FAIL_OPEN env var.
	SignerBlacklistFailOpen bool

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

	// TrustProxyHeaders is the master gate for two defense-in-depth
	// controls, both meaningless unless the server sits behind a
	// reverse proxy that strips client-supplied header values: (1) the
	// pre-auth failure-rate limiter and anon-read limiter derive the
	// source IP from X-Forwarded-For (hop-count-aware; see
	// TrustedProxyHops) instead of RemoteAddr; (2) the C5
	// requireForwardedHTTPS middleware enforces X-Forwarded-Proto,
	// rejecting an explicit non-https value. Enable only behind a
	// proxy that overwrites/strips inbound XFF and sets XFP;
	// otherwise an attacker can spoof either header to dodge the
	// limiter or mask a plaintext downgrade. NVNM_TRUST_PROXY_HEADERS
	// env var, default false.
	TrustProxyHeaders bool

	// TrustedProxyHops is the number of trusted proxy hops in front of
	// the server (including the direct socket peer). Only meaningful when
	// TrustProxyHeaders is true. clientIP walks this many hops in from the
	// right of (X-Forwarded-For ++ RemoteAddr) to find the real client,
	// so a forged left-prefix cannot mint its own rate-limit bucket. Set
	// to the real chain depth (1 = single ingress; 2 = CDN + ingress).
	// NVNM_TRUSTED_PROXY_HOPS env var, default 1, must be >= 1.
	TrustedProxyHops int

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

	// AllowKeyInLogs is the explicit operator acknowledgement (F4) that,
	// when the self-serve key-request flow is enabled without SMTP, the
	// approval path may write freshly-minted API keys to structured logs
	// (the log-only email sender). It is deliberately opt-in: without it,
	// KeyRequestEnabled+no-SMTP fails Validate() rather than silently
	// turning the log pipeline into a secret store. NVNM_ALLOW_KEY_IN_LOGS
	// env var (default false).
	AllowKeyInLogs bool
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
	if os.Getenv("WRITE_APPROVAL_DEFAULT") != "" {
		return nil, ErrLegacyWriteApproval
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

	if loadErr := cfg.loadCoreConfig(); loadErr != nil {
		return nil, loadErr
	}

	if loadErr := cfg.loadFeatureFlags(); loadErr != nil {
		return nil, loadErr
	}

	if loadErr := cfg.loadTrustedProxyHops(); loadErr != nil {
		return nil, loadErr
	}

	cfg.APIKey = os.Getenv("MCP_API_KEY")
	cfg.APIKeyRoles = parseRoleList(os.Getenv("MCP_API_KEY_ROLES"))
	if loadErr := cfg.loadKeyStoreConfig(); loadErr != nil {
		return nil, loadErr
	}
	cfg.APIKeysFile = os.Getenv("MCP_API_KEYS_FILE")
	cfg.AdminAPIKey = os.Getenv("ADMIN_API_KEY")
	// Default to loopback-only. The admin key is the master key (creates
	// API keys, assigns admin roles), so exposing :8081 cluster-wide is
	// a privilege-escalation foot-gun.
	// Operators that need cluster-internal access set ADMIN_API_ADDR
	// explicitly (e.g. ":8081") and pair it with a NetworkPolicy.
	cfg.AdminAPIAddr = envOrDefault("ADMIN_API_ADDR", "127.0.0.1:8081")

	if loadErr := cfg.loadTelemetryConfig(); loadErr != nil {
		return nil, loadErr
	}

	if loadErr := cfg.loadResilienceConfig(); loadErr != nil {
		return nil, loadErr
	}

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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// loadCoreConfig parses the NVNM_CHAIN_ID and REQUEST_TIMEOUT env vars into c.
// Extracted from Load() to reduce its cyclomatic complexity; both fields
// require string-to-typed-value parsing that adds decision branches.
func (c *Config) loadCoreConfig() error {
	chainIDStr := os.Getenv("NVNM_CHAIN_ID")
	if chainIDStr != "" {
		id, err := strconv.ParseInt(chainIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid NVNM_CHAIN_ID %q: %w", chainIDStr, err)
		}
		c.ChainID = id
	}
	timeoutStr := envOrDefault("REQUEST_TIMEOUT", "15s")
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return fmt.Errorf("invalid REQUEST_TIMEOUT %q: %w", timeoutStr, err)
	}
	c.RequestTimeout = timeout
	return nil
}

// loadKeyStoreConfig reads the key-store, pepper, TTL, and renewal-URL env
// vars into c. Extracted from Load() to keep Load's cyclomatic complexity
// below the gocyclo threshold; KEY_DEFAULT_TTL parsing adds a branch and
// all five fields are cohesively about the key-store surface.
func (c *Config) loadKeyStoreConfig() error {
	c.KeyHMACPepper = os.Getenv("KEY_HMAC_PEPPER")
	c.KeyHMACPepperPrevious = os.Getenv("KEY_HMAC_PEPPER_PREVIOUS")
	c.KeyStoreBackend = os.Getenv("KEY_STORE_BACKEND")
	c.KeyStoreDSN = os.Getenv("KEY_STORE_DSN")
	c.KeyRenewalURL = os.Getenv("KEY_RENEWAL_URL")
	ttlStr := envOrDefault("KEY_DEFAULT_TTL", "8760h")
	keyTTL, err := time.ParseDuration(ttlStr)
	if err != nil {
		return fmt.Errorf("invalid KEY_DEFAULT_TTL %q: %w", ttlStr, err)
	}
	c.KeyDefaultTTL = keyTTL
	return nil
}

// loadTelemetryConfig reads the OTEL/metrics env vars into c. Extracted from
// Load() to keep Load's cyclomatic complexity below the gocyclo threshold;
// OTEL_TRACE_SAMPLE_RATIO parsing adds a branch and all three fields are
// cohesively about the telemetry surface.
func (c *Config) loadTelemetryConfig() error {
	c.OTELEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	c.OTELServiceName = envOrDefault("OTEL_SERVICE_NAME", "nvnm-mcp-server")
	c.MetricsAddr = envOrDefault("METRICS_ADDR", ":9090")
	sampleRatioStr := envOrDefault("OTEL_TRACE_SAMPLE_RATIO", "1.0")
	sampleRatio, err := strconv.ParseFloat(sampleRatioStr, 64)
	if err != nil {
		return fmt.Errorf("invalid OTEL_TRACE_SAMPLE_RATIO %q: %w", sampleRatioStr, err)
	}
	c.OTELTraceSampleRatio = sampleRatio
	return nil
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
	if !c.ChainEnvironment.IsValid() {
		return fmt.Errorf("%w: got %q", ErrInvalidChainEnvironment, c.ChainEnvironment)
	}
	if err := c.validateAuth(); err != nil {
		return err
	}
	if err := c.validateKeyStore(); err != nil {
		return err
	}
	if err := c.validateKeyless(); err != nil {
		return err
	}
	if err := c.validateKeyRequestEmail(); err != nil {
		return err
	}
	return c.validateResilience()
}

// validateKeyRequestEmail enforces the F4 fail-closed guard on the
// self-serve key-request approval flow. When KeyRequestEnabled is on but
// no SMTP relay is configured, the approval path falls back to the
// log-only email sender, which writes freshly-minted API keys to
// structured logs. That exposure must be an explicit operator opt-in
// (AllowKeyInLogs), never a silent default. The guard is scoped to
// KeyRequestEnabled: with the feature off, the log-only sender is never
// constructed to mint or log a key, so there is nothing to guard.
func (c *Config) validateKeyRequestEmail() error {
	if c.KeyRequestEnabled && c.SMTPHost == "" && !c.AllowKeyInLogs {
		return ErrKeyInLogsNotAllowed
	}
	return nil
}

// validateKeyless enforces the authless-write bundle's prerequisites.
// Keyless writes ship a mandatory shared-state audit trail (the persisted
// write-audit is a security control, per the authless-writes design); running
// keyless writes without MCP_KEYLESS_PG_DSN would silently degrade to
// logs-only, which is not a supported posture -- fail fast instead.
func (c *Config) validateKeyless() error {
	if c.KeylessWrites && c.KeylessPGDSN == "" {
		return ErrKeylessWritesRequiresDSN
	}
	return c.validateKeylessWrites()
}

// validateKeylessWrites enforces the remaining anonymous-write
// prerequisites once KeylessWrites is on: the anonymous HTTP read path
// must also be enabled (an anonymous write with no anonymous read path is
// unreachable), the anchor address must parse with the same
// defitypes.AddressFromHex the runtime relay handler uses (so this boot
// guard is exactly as strict as the runtime parse -- "anchor_misconfig
// should never fire" genuinely holds), and the signer-quota knobs must be
// sane. Extracted from validateKeyless to keep Validate's cyclomatic
// complexity below the gocyclo threshold.
func (c *Config) validateKeylessWrites() error {
	if !c.KeylessWrites {
		return nil
	}
	if !c.KeylessReads {
		return ErrKeylessWritesRequiresReads
	}
	if _, err := defitypes.AddressFromHex(c.AnchorAddress); err != nil {
		return ErrAnchorAddressInvalid
	}
	if c.SignerWriteRate < 1 {
		return ErrSignerWriteRateInvalid
	}
	if c.SignerWriteWindow <= 0 {
		return ErrSignerWriteWindowInvalid
	}
	return nil
}

// validateKeyStore checks the key-store backend selection and its
// fail-loud prerequisites. The default (empty or "file") imposes none.
func (c *Config) validateKeyStore() error {
	switch c.KeyStoreBackend {
	case "", "file":
		return nil
	case "postgres":
		if c.KeyStoreDSN == "" {
			return ErrKeyStoreDSNRequired
		}
		// FusionAuth never touches the key store, so the pepper gate
		// applies only to the apikey provider.
		if c.AuthProvider == "apikey" && c.KeyHMACPepper == "" {
			return ErrPepperRequired
		}
		return nil
	default:
		return fmt.Errorf("%w: got %q", ErrInvalidKeyStoreBackend, c.KeyStoreBackend)
	}
}

func (c *Config) validateAuth() error {
	if c.KeyHMACPepperPrevious != "" && c.KeyHMACPepper == "" {
		return ErrPepperPreviousWithoutActive
	}
	if c.APIKey != "" {
		if len(c.APIKeyRoles) == 0 {
			return ErrStaticKeyRolesRequired
		}
		for _, r := range c.APIKeyRoles {
			if !auth.IsValidRole(r) {
				return fmt.Errorf("%w: got %q", ErrInvalidRole, r)
			}
		}
	}
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
	keyless, err := envBool("MCP_KEYLESS_READS", false)
	if err != nil {
		return err
	}
	c.KeylessReads = keyless

	keylessW, err := envBool("MCP_KEYLESS_WRITES", false)
	if err != nil {
		return err
	}
	c.KeylessWrites = keylessW
	c.KeylessPGDSN = os.Getenv("MCP_KEYLESS_PG_DSN")

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

	return c.loadSignerQuotaConfig()
}

// loadSignerQuotaConfig parses the per-signer write-quota and blacklist
// fail-mode env vars. Extracted from loadKeylessConfig to keep it (and by
// extension Load's call graph) below the gocyclo threshold; these four
// vars cluster naturally as the Phase 5 signer-quota/blacklist knobs.
func (c *Config) loadSignerQuotaConfig() error {
	rateStr := envOrDefault("MCP_SIGNER_WRITE_RATE", "500")
	rate, err := strconv.Atoi(rateStr)
	if err != nil {
		return fmt.Errorf("invalid MCP_SIGNER_WRITE_RATE %q: %w", rateStr, err)
	}
	c.SignerWriteRate = rate

	windowStr := envOrDefault("MCP_SIGNER_WRITE_WINDOW", "24h")
	window, err := time.ParseDuration(windowStr)
	if err != nil {
		return fmt.Errorf("invalid MCP_SIGNER_WRITE_WINDOW %q: %w", windowStr, err)
	}
	c.SignerWriteWindow = window

	quotaFailOpen, err := envBool("MCP_SIGNER_QUOTA_FAIL_OPEN", false)
	if err != nil {
		return err
	}
	c.SignerQuotaFailOpen = quotaFailOpen

	blacklistFailOpen, err := envBool("MCP_SIGNER_BLACKLIST_FAIL_OPEN", false)
	if err != nil {
		return err
	}
	c.SignerBlacklistFailOpen = blacklistFailOpen
	return nil
}

// loadKeyRequestConfig parses NVNM_KEY_REQUEST_* env vars and validates
// them. KeyRequestEnabled is opt-in; everything else is only consulted
// when enabled. When enabled, KeyPendingFile is required (the on-disk
// store has nowhere to persist without it) and the rate-limit knobs
// must be positive.
func (c *Config) loadKeyRequestConfig() error {
	enabled, err := envBool("NVNM_KEY_REQUEST_ENABLED", false)
	if err != nil {
		return err
	}
	c.KeyRequestEnabled = enabled
	c.KeyPendingFile = os.Getenv("NVNM_KEY_PENDING_FILE")

	allowKeyInLogs, err := envBool("NVNM_ALLOW_KEY_IN_LOGS", false)
	if err != nil {
		return err
	}
	c.AllowKeyInLogs = allowKeyInLogs

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

// loadFeatureFlags parses the boolean feature and telemetry flags into c.
// Extracted from Load() to keep Load's cyclomatic complexity below the
// gocyclo threshold, and to route every boolean env var through envBool's
// fail-loud parsing in one place -- a bare `== "true"` compare silently
// coerced ENABLE_WRITE_TOOLS=1 or =True to read-only with no error.
func (c *Config) loadFeatureFlags() error {
	flags := []struct {
		key      string
		fallback bool
		dst      *bool
	}{
		{"ENABLE_WRITE_TOOLS", false, &c.EnableWriteTools},
		{"ENABLE_PROMETHEUS", true, &c.EnablePrometheus},
		{"ENABLE_STDOUT_TELEMETRY", false, &c.EnableStdoutTel},
		// Secure-by-default: OTLP gRPC connects with TLS unless the operator
		// explicitly opts into insecure (typical for a sidecar collector on
		// localhost). Spans carry pre-sanitization error text and tool-call
		// patterns; an insecure default leaks them on any non-loopback path.
		{"OTLP_INSECURE", false, &c.OTLPInsecure},
		{"NVNM_TRUST_PROXY_HEADERS", false, &c.TrustProxyHeaders},
	}
	for _, f := range flags {
		v, err := envBool(f.key, f.fallback)
		if err != nil {
			return err
		}
		*f.dst = v
	}
	return nil
}

// loadTrustedProxyHops parses NVNM_TRUSTED_PROXY_HOPS (default 1). A value
// < 1 is rejected loudly: 0 (or negative) trusted hops is a meaningless
// configuration when proxy-header trust is enabled -- there is always at
// least the one proxy that set the headers -- so it is rejected at boot.
func (c *Config) loadTrustedProxyHops() error {
	s := envOrDefault("NVNM_TRUSTED_PROXY_HOPS", "1")
	hops, err := strconv.Atoi(s)
	if err != nil {
		return fmt.Errorf("invalid NVNM_TRUSTED_PROXY_HOPS %q: %w", s, err)
	}
	if hops < 1 {
		return ErrInvalidTrustedProxyHops
	}
	c.TrustedProxyHops = hops
	return nil
}

// parseRoleList splits a comma-separated role list, trimming whitespace and
// dropping empty entries. Returns nil for an empty/whitespace input so the
// "no roles configured" case is a clean nil.
func parseRoleList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool parses a boolean env var, returning fallback when unset or empty.
// Unlike a bare `== "true"` compare, it accepts every strconv.ParseBool
// spelling (1/t/T/TRUE/true/True and 0/f/F/FALSE/false/False) and fails loud
// on an unrecognized value rather than silently coercing it to the fallback --
// matching the fail-loud contract of the numeric/duration parsers above. A
// silent coercion is the trap where ENABLE_WRITE_TOOLS=1 or =True yields a
// read-only server with no error.
func envBool(key string, fallback bool) (bool, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("invalid %s %q: want a boolean (true/false): %w", key, v, err)
	}
	return b, nil
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
