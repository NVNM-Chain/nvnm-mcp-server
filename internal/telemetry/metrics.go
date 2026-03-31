package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const meterName = "inveniam-mcp-server"

// Metrics holds all application-level metric instruments.
type Metrics struct {
	ToolCallDuration metric.Float64Histogram
	ToolCallCount    metric.Int64Counter
	ToolErrorCount   metric.Int64Counter
	ActiveRequests   metric.Int64UpDownCounter
	RPCDuration      metric.Float64Histogram
	RPCErrorCount    metric.Int64Counter
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

	return &Metrics{
		ToolCallDuration: toolDur,
		ToolCallCount:    toolCount,
		ToolErrorCount:   toolErrors,
		ActiveRequests:   active,
		RPCDuration:      rpcDur,
		RPCErrorCount:    rpcErrors,
	}, nil
}
