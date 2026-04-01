package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const meterName = "inveniam-mcp-server"

// Metrics holds all application-level metric instruments.
type Metrics struct {
	// ToolCallDuration records the latency of each MCP tool call in seconds.
	ToolCallDuration metric.Float64Histogram
	// ToolCallCount counts total MCP tool calls, keyed by name and status.
	ToolCallCount metric.Int64Counter
	// ToolErrorCount counts MCP tool calls that returned an error.
	ToolErrorCount metric.Int64Counter
	// ActiveRequests tracks the number of in-flight MCP requests.
	ActiveRequests metric.Int64UpDownCounter
	// RPCDuration records the latency of upstream EVM RPC calls in seconds.
	RPCDuration metric.Float64Histogram
	// RPCErrorCount counts upstream EVM RPC calls that returned an error.
	RPCErrorCount metric.Int64Counter
	// RPCRetryCount counts retry attempts for upstream EVM RPC calls.
	RPCRetryCount metric.Int64Counter
	// CircuitBreakerState reports the current circuit breaker state (0=closed, 1=half-open, 2=open).
	CircuitBreakerState metric.Int64Gauge
	// RPCRateLimited counts rate-limit rejections for upstream RPC calls.
	RPCRateLimited metric.Int64Counter
}

// NewMetrics creates and registers all metric instruments with the provider.
func NewMetrics(provider *sdkmetric.MeterProvider) (*Metrics, error) {
	meter := provider.Meter(meterName)

	toolDur, err := meter.Float64Histogram(
		"mcp.server.tool.duration",
		metric.WithDescription("Duration of MCP tool calls in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("tool duration histogram: %w", err)
	}

	toolCount, err := meter.Int64Counter(
		"mcp.server.tool.calls",
		metric.WithDescription("Total MCP tool calls by name and status"),
	)
	if err != nil {
		return nil, fmt.Errorf("tool call counter: %w", err)
	}

	toolErrors, err := meter.Int64Counter(
		"mcp.server.tool.errors",
		metric.WithDescription("Total MCP tool errors by name and error type"),
	)
	if err != nil {
		return nil, fmt.Errorf("tool error counter: %w", err)
	}

	active, err := meter.Int64UpDownCounter(
		"mcp.server.active_requests",
		metric.WithDescription("Number of in-flight MCP requests"),
	)
	if err != nil {
		return nil, fmt.Errorf("active requests gauge: %w", err)
	}

	rpcDur, err := meter.Float64Histogram(
		"evm.rpc.duration",
		metric.WithDescription("Duration of upstream EVM RPC calls in seconds"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("rpc duration histogram: %w", err)
	}

	rpcErrors, err := meter.Int64Counter(
		"evm.rpc.errors",
		metric.WithDescription("Total upstream EVM RPC errors by method"),
	)
	if err != nil {
		return nil, fmt.Errorf("rpc error counter: %w", err)
	}

	rpcRetries, err := meter.Int64Counter(
		"evm.rpc.retries",
		metric.WithDescription("Total retry attempts for upstream EVM RPC calls"),
	)
	if err != nil {
		return nil, fmt.Errorf("rpc retry counter: %w", err)
	}

	cbState, err := meter.Int64Gauge(
		"evm.rpc.circuit_breaker.state",
		metric.WithDescription("Circuit breaker state: 0=closed, 1=half-open, 2=open"),
	)
	if err != nil {
		return nil, fmt.Errorf("circuit breaker state gauge: %w", err)
	}

	rateLimited, err := meter.Int64Counter(
		"evm.rpc.rate_limited",
		metric.WithDescription("Total rate-limit rejections for upstream RPC calls"),
	)
	if err != nil {
		return nil, fmt.Errorf("rate limited counter: %w", err)
	}

	return &Metrics{
		ToolCallDuration:    toolDur,
		ToolCallCount:       toolCount,
		ToolErrorCount:      toolErrors,
		ActiveRequests:      active,
		RPCDuration:         rpcDur,
		RPCErrorCount:       rpcErrors,
		RPCRetryCount:       rpcRetries,
		CircuitBreakerState: cbState,
		RPCRateLimited:      rateLimited,
	}, nil
}
