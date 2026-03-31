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
	ErrMissingRPCURL    = errors.New("INVENIAM_EVM_RPC_URL is required")
	ErrInvalidRPCURL    = errors.New("INVENIAM_EVM_RPC_URL must start with http:// or https://")
	ErrInvalidChainID   = errors.New("INVENIAM_CHAIN_ID must be a positive integer")
	ErrInvalidTimeout   = errors.New("REQUEST_TIMEOUT must be a positive duration")
	ErrInvalidTransport = errors.New("MCP_TRANSPORT must be \"stdio\" or \"http\"")
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

	// Telemetry
	OTELEndpoint     string
	OTELServiceName  string
	EnablePrometheus bool
	EnableStdoutTel  bool
	MetricsAddr      string
}

// Load reads configuration from environment variables and returns a validated Config.
func Load() (*Config, error) {
	cfg := &Config{
		EVMRPCURL:        os.Getenv("INVENIAM_EVM_RPC_URL"),
		EVMArchiveRPCURL: os.Getenv("INVENIAM_EVM_ARCHIVE_RPC_URL"),
		AnchorAddress:    envOrDefault("ANCHOR_ADDRESS", "0x0000000000000000000000000000000000000A00"),
		AnchorABIPath:    os.Getenv("ANCHOR_ABI_PATH"),
		LogLevel:         envOrDefault("LOG_LEVEL", "info"),
		Transport:        envOrDefault("MCP_TRANSPORT", "stdio"),
		HTTPAddr:         envOrDefault("MCP_HTTP_ADDR", ":8080"),
	}

	chainIDStr := os.Getenv("INVENIAM_CHAIN_ID")
	if chainIDStr != "" {
		id, err := strconv.ParseInt(chainIDStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid INVENIAM_CHAIN_ID %q: %w", chainIDStr, err)
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

	cfg.OTELEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	cfg.OTELServiceName = envOrDefault("OTEL_SERVICE_NAME", "inveniam-mcp-server")
	cfg.EnablePrometheus = envOrDefault("ENABLE_PROMETHEUS", "true") == "true"
	cfg.EnableStdoutTel = envOrDefault("ENABLE_STDOUT_TELEMETRY", "false") == "true"
	cfg.MetricsAddr = envOrDefault("METRICS_ADDR", ":9090")

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
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
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
