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
