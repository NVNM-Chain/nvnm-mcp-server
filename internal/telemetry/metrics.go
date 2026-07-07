// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

const meterName = "nvnm-mcp-server"

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
	// HTTPResponses counts MCP HTTP responses, keyed by `class` (see
	// ClassifyStatus) for the Phase 10 error-rate SLI. Exported to
	// Prometheus as mcp_http_responses_total.
	HTTPResponses metric.Int64Counter
	// WriteBroadcasts counts broadcast attempts that passed relay scope,
	// labeled outcome=ok|failed. No signer/address label (bounded cardinality).
	WriteBroadcasts metric.Int64Counter
	// WriteRelayScopeRejected counts pre-broadcast write rejections, labeled
	// cause=decode|anchor_misconfig|relay_scope|signer_blacklist|
	// signer_quota|quota_store_error|blacklist_store_error. No
	// signer/address label.
	WriteRelayScopeRejected metric.Int64Counter
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

	httpResponses, err := meter.Int64Counter(
		"mcp.http.responses",
		metric.WithDescription(
			"Total MCP HTTP responses, labeled by SLI class (server_fault|customer_impact|client_error|success)",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("http responses counter: %w", err)
	}

	writeBroadcasts, err := meter.Int64Counter(
		"mcp.write.broadcasts",
		metric.WithDescription("Broadcast attempts that passed relay scope, by outcome (ok|failed)"),
	)
	if err != nil {
		return nil, fmt.Errorf("write broadcasts counter: %w", err)
	}

	writeRelayRejected, err := meter.Int64Counter(
		"mcp.write.relay_scope_rejected",
		metric.WithDescription(
			"Pre-broadcast write rejections, by cause "+
				"(decode|anchor_misconfig|relay_scope|signer_blacklist|"+
				"signer_quota|quota_store_error|blacklist_store_error)",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("write relay-scope rejected counter: %w", err)
	}

	return &Metrics{
		ToolCallDuration:        toolDur,
		ToolCallCount:           toolCount,
		ToolErrorCount:          toolErrors,
		ActiveRequests:          active,
		RPCDuration:             rpcDur,
		RPCErrorCount:           rpcErrors,
		RPCRetryCount:           rpcRetries,
		CircuitBreakerState:     cbState,
		RPCRateLimited:          rateLimited,
		HTTPResponses:           httpResponses,
		WriteBroadcasts:         writeBroadcasts,
		WriteRelayScopeRejected: writeRelayRejected,
	}, nil
}

// RecordBroadcast increments the broadcast counter for the given outcome
// ("ok" or "failed"). Safe to call on a nil *Metrics (no-op) so stdio and
// test callers need no telemetry wiring.
func (m *Metrics) RecordBroadcast(ctx context.Context, outcome string) {
	if m == nil || m.WriteBroadcasts == nil {
		return
	}
	m.WriteBroadcasts.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// RelayRejectCause is the closed set of pre-broadcast rejection reasons used
// as the "cause" metric label. It is a distinct type (not a string) so that
// caller-derived data -- e.g. a signer address -- CANNOT compile into a
// /metrics label value. This is the C2 leak + cardinality-DoS defense, made
// structural.
type RelayRejectCause string

const (
	CauseDecode            RelayRejectCause = "decode"
	CauseAnchorMisconfig   RelayRejectCause = "anchor_misconfig"
	CauseRelayScope        RelayRejectCause = "relay_scope"
	CauseSignerBlacklist   RelayRejectCause = "signer_blacklist"
	CauseSignerQuota       RelayRejectCause = "signer_quota"
	CauseQuotaStoreErr     RelayRejectCause = "quota_store_error"
	CauseBlacklistStoreErr RelayRejectCause = "blacklist_store_error"
)

// RecordRelayReject increments the pre-broadcast rejection counter for
// cause. Safe to call on a nil *Metrics (no-op).
func (m *Metrics) RecordRelayReject(ctx context.Context, cause RelayRejectCause) {
	if m == nil || m.WriteRelayScopeRejected == nil {
		return
	}
	m.WriteRelayScopeRejected.Add(ctx, 1, metric.WithAttributes(attribute.String("cause", string(cause))))
}
