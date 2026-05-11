package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"INVENIAM_EVM_RPC_URL",
		"INVENIAM_EVM_ARCHIVE_RPC_URL",
		"INVENIAM_CHAIN_ID",
		"ANCHOR_ADDRESS",
		"ANCHOR_ABI_PATH",
		"REQUEST_TIMEOUT",
		"LOG_LEVEL",
		"MCP_TRANSPORT",
		"MCP_HTTP_ADDR",
		"ENABLE_WRITE_TOOLS",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_SERVICE_NAME",
		"ENABLE_PROMETHEUS",
		"ENABLE_STDOUT_TELEMETRY",
		"METRICS_ADDR",
		"RPC_MAX_RETRIES",
		"RPC_INITIAL_BACKOFF",
		"RPC_MAX_BACKOFF",
		"RPC_RATE_LIMIT",
		"RPC_RATE_BURST",
		"CIRCUIT_BREAKER_THRESHOLD",
		"CIRCUIT_BREAKER_TIMEOUT",
		"OTEL_TRACE_SAMPLE_RATIO",
		"WRITE_APPROVAL_DEFAULT",
		"AUTH_PROVIDER",
		"FUSIONAUTH_URL",
		"FUSIONAUTH_APPLICATION_ID",
		"FUSIONAUTH_ISSUER",
		"FUSIONAUTH_JWKS_URL",
		"JWT_CLOCK_SKEW",
		"JWT_ROLES_CLAIM",
		"NVNM_CHAIN_ENVIRONMENT",
		"NVNM_DOCS_URL",
		"NVNM_EXPLORER_URL",
		"NVNM_BRIDGE_URL",
	} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}
}

func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://evm.inveniam.mantrachain.io")
	t.Setenv("INVENIAM_CHAIN_ID", "58887")
}

func TestLoad_Minimal(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.EVMRPCURL != "https://evm.inveniam.mantrachain.io" {
		t.Errorf("EVMRPCURL = %q, want %q", cfg.EVMRPCURL, "https://evm.inveniam.mantrachain.io")
	}
	if cfg.ChainID != 58887 {
		t.Errorf("ChainID = %d, want 58887", cfg.ChainID)
	}
	if cfg.AnchorAddress != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("AnchorAddress = %q, want default precompile address", cfg.AnchorAddress)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.Transport != "stdio" {
		t.Errorf("Transport = %q, want %q", cfg.Transport, "stdio")
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, ":8080")
	}
	if cfg.RequestTimeout != 15*time.Second {
		t.Errorf("RequestTimeout = %v, want 15s", cfg.RequestTimeout)
	}
	if cfg.EnableWriteTools {
		t.Error("EnableWriteTools should default to false")
	}
}

func TestLoad_AllFields(t *testing.T) {
	clearEnv(t)
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://rpc.example.com")
	t.Setenv("INVENIAM_EVM_ARCHIVE_RPC_URL", "https://archive.example.com")
	t.Setenv("INVENIAM_CHAIN_ID", "1")
	t.Setenv("ANCHOR_ADDRESS", "0x1234567890abcdef1234567890abcdef12345678")
	t.Setenv("ANCHOR_ABI_PATH", "/tmp/test.json")
	t.Setenv("REQUEST_TIMEOUT", "30s")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("MCP_TRANSPORT", "http")
	t.Setenv("MCP_HTTP_ADDR", ":9090")
	t.Setenv("ENABLE_WRITE_TOOLS", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.EVMArchiveRPCURL != "https://archive.example.com" {
		t.Errorf("EVMArchiveRPCURL = %q", cfg.EVMArchiveRPCURL)
	}
	if cfg.ChainID != 1 {
		t.Errorf("ChainID = %d, want 1", cfg.ChainID)
	}
	if cfg.AnchorAddress != "0x1234567890abcdef1234567890abcdef12345678" {
		t.Errorf("AnchorAddress = %q", cfg.AnchorAddress)
	}
	if cfg.AnchorABIPath != "/tmp/test.json" {
		t.Errorf("AnchorABIPath = %q", cfg.AnchorABIPath)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %v, want 30s", cfg.RequestTimeout)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.Transport != "http" {
		t.Errorf("Transport = %q", cfg.Transport)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q", cfg.HTTPAddr)
	}
	if !cfg.EnableWriteTools {
		t.Error("EnableWriteTools should be true")
	}
}

func TestLoad_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T)
		wantErr error
	}{
		{
			name: "missing RPC URL",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("INVENIAM_CHAIN_ID", "1")
			},
			wantErr: ErrMissingRPCURL,
		},
		{
			name: "invalid RPC URL scheme",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("INVENIAM_EVM_RPC_URL", "ws://rpc.example.com")
				t.Setenv("INVENIAM_CHAIN_ID", "1")
			},
			wantErr: ErrInvalidRPCURL,
		},
		{
			name: "missing chain ID",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("INVENIAM_EVM_RPC_URL", "https://rpc.example.com")
			},
			wantErr: ErrInvalidChainID,
		},
		{
			name: "invalid transport",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("INVENIAM_EVM_RPC_URL", "https://rpc.example.com")
				t.Setenv("INVENIAM_CHAIN_ID", "1")
				t.Setenv("MCP_TRANSPORT", "grpc")
			},
			wantErr: ErrInvalidTransport,
		},
		{
			name: "invalid sample ratio too high",
			setup: func(t *testing.T) {
				t.Helper()
				setMinimalEnv(t)
				t.Setenv("OTEL_TRACE_SAMPLE_RATIO", "1.5")
			},
			wantErr: ErrInvalidSampleRatio,
		},
		{
			name: "invalid rate limit",
			setup: func(t *testing.T) {
				t.Helper()
				setMinimalEnv(t)
				t.Setenv("RPC_RATE_LIMIT", "0")
			},
			wantErr: ErrInvalidRateLimit,
		},
		{
			name: "invalid circuit breaker threshold",
			setup: func(t *testing.T) {
				t.Helper()
				setMinimalEnv(t)
				t.Setenv("CIRCUIT_BREAKER_THRESHOLD", "0")
			},
			wantErr: ErrInvalidBreakerSettings,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			tc.setup(t)

			_, err := Load()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %q, want %v", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestLoad_InvalidChainID(t *testing.T) {
	clearEnv(t)
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://rpc.example.com")
	t.Setenv("INVENIAM_CHAIN_ID", "not-a-number")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid INVENIAM_CHAIN_ID") {
		t.Errorf("error = %q, want substring about invalid chain ID", err.Error())
	}
}

func TestLoad_InvalidTimeout(t *testing.T) {
	clearEnv(t)
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://rpc.example.com")
	t.Setenv("INVENIAM_CHAIN_ID", "1")
	t.Setenv("REQUEST_TIMEOUT", "bad")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid REQUEST_TIMEOUT") {
		t.Errorf("error = %q, want substring about invalid timeout", err.Error())
	}
}

func TestEnvOrDefault(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		envVal   string
		fallback string
		want     string
	}{
		{
			name:     "env set",
			key:      "TEST_ENV_OR_DEFAULT",
			envVal:   "custom",
			fallback: "default",
			want:     "custom",
		},
		{
			name:     "env empty uses fallback",
			key:      "TEST_ENV_OR_DEFAULT",
			envVal:   "",
			fallback: "default",
			want:     "default",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envVal != "" {
				t.Setenv(tc.key, tc.envVal)
			} else {
				os.Unsetenv(tc.key)
			}

			got := envOrDefault(tc.key, tc.fallback)
			if got != tc.want {
				t.Errorf("envOrDefault(%q, %q) = %q, want %q",
					tc.key, tc.fallback, got, tc.want)
			}
		})
	}
}

func TestLoad_TelemetryDefaults(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OTELEndpoint != "" {
		t.Errorf("OTELEndpoint = %q, want empty (disabled by default)", cfg.OTELEndpoint)
	}
	if cfg.OTELServiceName != "inveniam-mcp-server" {
		t.Errorf("OTELServiceName = %q, want %q", cfg.OTELServiceName, "inveniam-mcp-server")
	}
	if !cfg.EnablePrometheus {
		t.Error("EnablePrometheus should default to true")
	}
	if cfg.EnableStdoutTel {
		t.Error("EnableStdoutTel should default to false")
	}
	if cfg.MetricsAddr != ":9090" {
		t.Errorf("MetricsAddr = %q, want %q", cfg.MetricsAddr, ":9090")
	}
}

func TestLoad_TelemetryOverrides(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
	t.Setenv("OTEL_SERVICE_NAME", "custom-service")
	t.Setenv("ENABLE_PROMETHEUS", "false")
	t.Setenv("ENABLE_STDOUT_TELEMETRY", "true")
	t.Setenv("METRICS_ADDR", ":7070")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.OTELEndpoint != "localhost:4317" {
		t.Errorf("OTELEndpoint = %q, want %q", cfg.OTELEndpoint, "localhost:4317")
	}
	if cfg.OTELServiceName != "custom-service" {
		t.Errorf("OTELServiceName = %q, want %q", cfg.OTELServiceName, "custom-service")
	}
	if cfg.EnablePrometheus {
		t.Error("EnablePrometheus should be false when set to 'false'")
	}
	if !cfg.EnableStdoutTel {
		t.Error("EnableStdoutTel should be true when set to 'true'")
	}
	if cfg.MetricsAddr != ":7070" {
		t.Errorf("MetricsAddr = %q, want %q", cfg.MetricsAddr, ":7070")
	}
}

func TestLoad_ResilienceDefaults(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.RPCMaxRetries != 3 {
		t.Errorf("RPCMaxRetries = %d, want 3", cfg.RPCMaxRetries)
	}
	if cfg.RPCInitialBackoff != 500*time.Millisecond {
		t.Errorf("RPCInitialBackoff = %v, want 500ms", cfg.RPCInitialBackoff)
	}
	if cfg.RPCMaxBackoff != 10*time.Second {
		t.Errorf("RPCMaxBackoff = %v, want 10s", cfg.RPCMaxBackoff)
	}
	if cfg.RPCRateLimit != 100 {
		t.Errorf("RPCRateLimit = %f, want 100", cfg.RPCRateLimit)
	}
	if cfg.RPCRateBurst != 20 {
		t.Errorf("RPCRateBurst = %d, want 20", cfg.RPCRateBurst)
	}
	if cfg.CircuitBreakerThreshold != 5 {
		t.Errorf("CircuitBreakerThreshold = %d, want 5", cfg.CircuitBreakerThreshold)
	}
	if cfg.CircuitBreakerTimeout != 30*time.Second {
		t.Errorf("CircuitBreakerTimeout = %v, want 30s", cfg.CircuitBreakerTimeout)
	}
	if cfg.OTELTraceSampleRatio != 1.0 {
		t.Errorf("OTELTraceSampleRatio = %f, want 1.0", cfg.OTELTraceSampleRatio)
	}
}

func TestLoad_ResilienceOverrides(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	t.Setenv("RPC_MAX_RETRIES", "5")
	t.Setenv("RPC_INITIAL_BACKOFF", "1s")
	t.Setenv("RPC_MAX_BACKOFF", "30s")
	t.Setenv("RPC_RATE_LIMIT", "50")
	t.Setenv("RPC_RATE_BURST", "10")
	t.Setenv("CIRCUIT_BREAKER_THRESHOLD", "10")
	t.Setenv("CIRCUIT_BREAKER_TIMEOUT", "1m")
	t.Setenv("OTEL_TRACE_SAMPLE_RATIO", "0.1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.RPCMaxRetries != 5 {
		t.Errorf("RPCMaxRetries = %d, want 5", cfg.RPCMaxRetries)
	}
	if cfg.RPCInitialBackoff != 1*time.Second {
		t.Errorf("RPCInitialBackoff = %v, want 1s", cfg.RPCInitialBackoff)
	}
	if cfg.RPCMaxBackoff != 30*time.Second {
		t.Errorf("RPCMaxBackoff = %v, want 30s", cfg.RPCMaxBackoff)
	}
	if cfg.RPCRateLimit != 50 {
		t.Errorf("RPCRateLimit = %f, want 50", cfg.RPCRateLimit)
	}
	if cfg.RPCRateBurst != 10 {
		t.Errorf("RPCRateBurst = %d, want 10", cfg.RPCRateBurst)
	}
	if cfg.CircuitBreakerThreshold != 10 {
		t.Errorf("CircuitBreakerThreshold = %d, want 10", cfg.CircuitBreakerThreshold)
	}
	if cfg.CircuitBreakerTimeout != 1*time.Minute {
		t.Errorf("CircuitBreakerTimeout = %v, want 1m", cfg.CircuitBreakerTimeout)
	}
	if cfg.OTELTraceSampleRatio != 0.1 {
		t.Errorf("OTELTraceSampleRatio = %f, want 0.1", cfg.OTELTraceSampleRatio)
	}
}

func TestLoad_WriteApprovalDefault(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WriteApprovalDefault != "required" {
		t.Errorf("WriteApprovalDefault = %q, want %q", cfg.WriteApprovalDefault, "required")
	}
}

func TestLoad_WriteApprovalDefault_Auto(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("WRITE_APPROVAL_DEFAULT", "auto")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WriteApprovalDefault != "auto" {
		t.Errorf("WriteApprovalDefault = %q, want %q", cfg.WriteApprovalDefault, "auto")
	}
}

func TestLoad_WriteApprovalDefault_Invalid(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("WRITE_APPROVAL_DEFAULT", "yolo")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidWriteApproval) {
		t.Errorf("error = %q, want ErrInvalidWriteApproval", err.Error())
	}
}

func TestLoad_AuthProviderDefaults(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AuthProvider != "apikey" {
		t.Errorf("AuthProvider = %q, want %q", cfg.AuthProvider, "apikey")
	}
	if cfg.JWTRolesClaim != "roles" {
		t.Errorf("JWTRolesClaim = %q, want %q", cfg.JWTRolesClaim, "roles")
	}
	if cfg.JWTClockSkew != 60*time.Second {
		t.Errorf("JWTClockSkew = %v, want 60s", cfg.JWTClockSkew)
	}
}

func TestLoad_AuthProviderFusionAuth(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("AUTH_PROVIDER", "fusionauth")
	t.Setenv("FUSIONAUTH_URL", "https://auth.example.com")
	t.Setenv("FUSIONAUTH_APPLICATION_ID", "app-uuid-123")
	t.Setenv("FUSIONAUTH_ISSUER", "https://custom-issuer.example.com")
	t.Setenv("FUSIONAUTH_JWKS_URL", "https://auth.example.com/custom-jwks")
	t.Setenv("JWT_CLOCK_SKEW", "30s")
	t.Setenv("JWT_ROLES_CLAIM", "app_roles")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.AuthProvider != "fusionauth" {
		t.Errorf("AuthProvider = %q, want %q", cfg.AuthProvider, "fusionauth")
	}
	if cfg.FusionAuthURL != "https://auth.example.com" {
		t.Errorf("FusionAuthURL = %q", cfg.FusionAuthURL)
	}
	if cfg.FusionAuthAppID != "app-uuid-123" {
		t.Errorf("FusionAuthAppID = %q", cfg.FusionAuthAppID)
	}
	if cfg.FusionAuthIssuer != "https://custom-issuer.example.com" {
		t.Errorf("FusionAuthIssuer = %q", cfg.FusionAuthIssuer)
	}
	if cfg.FusionAuthJWKSURL != "https://auth.example.com/custom-jwks" {
		t.Errorf("FusionAuthJWKSURL = %q", cfg.FusionAuthJWKSURL)
	}
	if cfg.JWTClockSkew != 30*time.Second {
		t.Errorf("JWTClockSkew = %v, want 30s", cfg.JWTClockSkew)
	}
	if cfg.JWTRolesClaim != "app_roles" {
		t.Errorf("JWTRolesClaim = %q, want %q", cfg.JWTRolesClaim, "app_roles")
	}
}

func TestLoad_AuthProviderInvalid(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("AUTH_PROVIDER", "oauth2")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidAuthProvider) {
		t.Errorf("error = %q, want ErrInvalidAuthProvider", err.Error())
	}
}

func TestLoad_FusionAuth_MissingURL(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("AUTH_PROVIDER", "fusionauth")
	t.Setenv("FUSIONAUTH_APPLICATION_ID", "app-uuid-123")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingFusionAuthURL) {
		t.Errorf("error = %q, want ErrMissingFusionAuthURL", err.Error())
	}
}

func TestLoad_FusionAuth_MissingAppID(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("AUTH_PROVIDER", "fusionauth")
	t.Setenv("FUSIONAUTH_URL", "https://auth.example.com")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMissingFusionAuthAppID) {
		t.Errorf("error = %q, want ErrMissingFusionAuthAppID", err.Error())
	}
}

func TestLoad_FusionAuth_InvalidURL(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("AUTH_PROVIDER", "fusionauth")
	t.Setenv("FUSIONAUTH_URL", "ws://auth.example.com")
	t.Setenv("FUSIONAUTH_APPLICATION_ID", "app-uuid-123")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidFusionAuthURL) {
		t.Errorf("error = %q, want ErrInvalidFusionAuthURL", err.Error())
	}
}

func TestGetFusionAuthIssuer(t *testing.T) {
	cfg := &Config{FusionAuthURL: "https://auth.example.com"}

	if got := cfg.GetFusionAuthIssuer(); got != "https://auth.example.com" {
		t.Errorf("GetFusionAuthIssuer() = %q, want base URL fallback", got)
	}

	cfg.FusionAuthIssuer = "https://custom.example.com"
	if got := cfg.GetFusionAuthIssuer(); got != "https://custom.example.com" {
		t.Errorf("GetFusionAuthIssuer() = %q, want custom issuer", got)
	}
}

func TestGetFusionAuthJWKSURL(t *testing.T) {
	cfg := &Config{FusionAuthURL: "https://auth.example.com"}

	if got := cfg.GetFusionAuthJWKSURL(); got != "https://auth.example.com/.well-known/jwks.json" {
		t.Errorf("GetFusionAuthJWKSURL() = %q, want default JWKS path", got)
	}

	cfg.FusionAuthJWKSURL = "https://auth.example.com/custom-jwks"
	if got := cfg.GetFusionAuthJWKSURL(); got != "https://auth.example.com/custom-jwks" {
		t.Errorf("GetFusionAuthJWKSURL() = %q, want custom JWKS URL", got)
	}
}

func TestLoad_ChainEnvironment_DefaultsToTestnetForUnknownChainID(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t) // chain ID 58887 -- the old manveniam-1, not in the recognized list
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainEnvironment != EnvTestnet {
		t.Errorf("ChainEnvironment = %q, want EnvTestnet (default for unrecognized chain ID)", cfg.ChainEnvironment)
	}
}

func TestLoad_ChainEnvironment_InferredFromTestnetChainID(t *testing.T) {
	clearEnv(t)
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://evm.testnet.nvnmchain.io")
	t.Setenv("INVENIAM_CHAIN_ID", "787111")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainEnvironment != EnvTestnet {
		t.Errorf("ChainEnvironment = %q, want EnvTestnet (inferred from 787111)", cfg.ChainEnvironment)
	}
}

func TestLoad_ChainEnvironment_InferredFromMainnetChainID(t *testing.T) {
	clearEnv(t)
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://evm.nvnmchain.io")
	t.Setenv("INVENIAM_CHAIN_ID", "1611")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainEnvironment != EnvMainnet {
		t.Errorf("ChainEnvironment = %q, want EnvMainnet (inferred from 1611)", cfg.ChainEnvironment)
	}
}

func TestLoad_ChainEnvironment_ExplicitOverridesInference(t *testing.T) {
	clearEnv(t)
	t.Setenv("INVENIAM_EVM_RPC_URL", "https://evm.testnet.nvnmchain.io")
	t.Setenv("INVENIAM_CHAIN_ID", "787111") // testnet by inference
	t.Setenv("NVNM_CHAIN_ENVIRONMENT", "mainnet")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ChainEnvironment != EnvMainnet {
		t.Errorf("ChainEnvironment = %q, want EnvMainnet (explicit override of inference)", cfg.ChainEnvironment)
	}
}

func TestLoad_ChainEnvironment_InvalidExplicitValue(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("NVNM_CHAIN_ENVIRONMENT", "prod")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid NVNM_CHAIN_ENVIRONMENT, got nil")
	}
	if !errors.Is(err, ErrInvalidChainEnvironment) {
		t.Errorf("error = %v, want ErrInvalidChainEnvironment", err)
	}
}

func TestLoad_OnboardingURLs_LoadFromEnv(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("NVNM_DOCS_URL", "https://docs.nvnmchain.io")
	t.Setenv("NVNM_EXPLORER_URL", "https://explorer.evm.testnet.nvnmchain.io")
	t.Setenv("NVNM_BRIDGE_URL", "https://bridge.nvnmchain.io")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DocsURL != "https://docs.nvnmchain.io" {
		t.Errorf("DocsURL = %q, want https://docs.nvnmchain.io", cfg.DocsURL)
	}
	if cfg.ExplorerURL != "https://explorer.evm.testnet.nvnmchain.io" {
		t.Errorf("ExplorerURL = %q, want https://explorer.evm.testnet.nvnmchain.io", cfg.ExplorerURL)
	}
	if cfg.BridgeURL != "https://bridge.nvnmchain.io" {
		t.Errorf("BridgeURL = %q, want https://bridge.nvnmchain.io", cfg.BridgeURL)
	}
}

func TestLoad_OnboardingURLs_EmptyByDefault(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DocsURL != "" || cfg.ExplorerURL != "" || cfg.BridgeURL != "" {
		t.Errorf("onboarding URLs should default to empty; got docs=%q explorer=%q bridge=%q",
			cfg.DocsURL, cfg.ExplorerURL, cfg.BridgeURL)
	}
}
