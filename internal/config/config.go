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
	ErrMissingRPCURL          = errors.New("INVENIAM_EVM_RPC_URL is required")
	ErrInvalidRPCURL          = errors.New("INVENIAM_EVM_RPC_URL must start with http:// or https://")
	ErrInvalidChainID         = errors.New("INVENIAM_CHAIN_ID must be a positive integer")
	ErrInvalidTimeout         = errors.New("REQUEST_TIMEOUT must be a positive duration")
	ErrInvalidTransport       = errors.New("MCP_TRANSPORT must be \"stdio\" or \"http\"")
	ErrInvalidRetries         = errors.New("RPC_MAX_RETRIES must be non-negative")
	ErrInvalidBackoff         = errors.New("RPC_INITIAL_BACKOFF and RPC_MAX_BACKOFF must be positive durations")
	ErrInvalidRateLimit       = errors.New("RPC_RATE_LIMIT must be positive")
	ErrInvalidRateBurst       = errors.New("RPC_RATE_BURST must be positive")
	ErrInvalidBreakerSettings = errors.New("CIRCUIT_BREAKER_THRESHOLD and CIRCUIT_BREAKER_TIMEOUT must be positive")
	ErrInvalidSampleRatio     = errors.New("OTEL_TRACE_SAMPLE_RATIO must be between 0.0 and 1.0 inclusive")
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
	cfg.APIKey = os.Getenv("MCP_API_KEY")
	cfg.APIKeysFile = os.Getenv("MCP_API_KEYS_FILE")

	cfg.OTELEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	cfg.OTELServiceName = envOrDefault("OTEL_SERVICE_NAME", "inveniam-mcp-server")
	cfg.EnablePrometheus = envOrDefault("ENABLE_PROMETHEUS", "true") == "true"
	cfg.EnableStdoutTel = envOrDefault("ENABLE_STDOUT_TELEMETRY", "false") == "true"
	cfg.OTLPInsecure = envOrDefault("OTLP_INSECURE", "true") == "true"
	cfg.MetricsAddr = envOrDefault("METRICS_ADDR", ":9090")

	retryStr := envOrDefault("RPC_MAX_RETRIES", "3")
	retries, err := strconv.Atoi(retryStr)
	if err != nil {
		return nil, fmt.Errorf("invalid RPC_MAX_RETRIES %q: %w", retryStr, err)
	}
	cfg.RPCMaxRetries = retries

	initialBackoffStr := envOrDefault("RPC_INITIAL_BACKOFF", "500ms")
	initialBackoff, err := time.ParseDuration(initialBackoffStr)
	if err != nil {
		return nil, fmt.Errorf("invalid RPC_INITIAL_BACKOFF %q: %w", initialBackoffStr, err)
	}
	cfg.RPCInitialBackoff = initialBackoff

	maxBackoffStr := envOrDefault("RPC_MAX_BACKOFF", "10s")
	maxBackoff, err := time.ParseDuration(maxBackoffStr)
	if err != nil {
		return nil, fmt.Errorf("invalid RPC_MAX_BACKOFF %q: %w", maxBackoffStr, err)
	}
	cfg.RPCMaxBackoff = maxBackoff

	rateLimitStr := envOrDefault("RPC_RATE_LIMIT", "100")
	rateLimit, err := strconv.ParseFloat(rateLimitStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid RPC_RATE_LIMIT %q: %w", rateLimitStr, err)
	}
	cfg.RPCRateLimit = rateLimit

	rateBurstStr := envOrDefault("RPC_RATE_BURST", "20")
	rateBurst, err := strconv.Atoi(rateBurstStr)
	if err != nil {
		return nil, fmt.Errorf("invalid RPC_RATE_BURST %q: %w", rateBurstStr, err)
	}
	cfg.RPCRateBurst = rateBurst

	breakerThresholdStr := envOrDefault("CIRCUIT_BREAKER_THRESHOLD", "5")
	breakerThreshold, err := strconv.ParseUint(breakerThresholdStr, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid CIRCUIT_BREAKER_THRESHOLD %q: %w", breakerThresholdStr, err)
	}
	cfg.CircuitBreakerThreshold = uint32(breakerThreshold)

	breakerTimeoutStr := envOrDefault("CIRCUIT_BREAKER_TIMEOUT", "30s")
	breakerTimeout, err := time.ParseDuration(breakerTimeoutStr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIRCUIT_BREAKER_TIMEOUT %q: %w", breakerTimeoutStr, err)
	}
	cfg.CircuitBreakerTimeout = breakerTimeout

	sampleRatioStr := envOrDefault("OTEL_TRACE_SAMPLE_RATIO", "1.0")
	sampleRatio, err := strconv.ParseFloat(sampleRatioStr, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid OTEL_TRACE_SAMPLE_RATIO %q: %w", sampleRatioStr, err)
	}
	cfg.OTELTraceSampleRatio = sampleRatio

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
	return c.validateResilience()
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
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
