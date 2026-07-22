// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// newCollectingMetrics returns Metrics wired to a ManualReader so tests can
// assert what was actually recorded.
func newCollectingMetrics(t *testing.T) (*Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})
	m, err := NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	return m, reader
}

// sumDataPoints returns the recorded int64 sum data points for the named
// instrument, or nil if the instrument recorded nothing.
func sumDataPoints(t *testing.T, reader *sdkmetric.ManualReader, name string) []metricdata.DataPoint[int64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("instrument %s data type = %T, want Sum[int64]", name, m.Data)
			}
			return sum.DataPoints
		}
	}
	return nil
}

func attrValue(dp metricdata.DataPoint[int64], key string) string {
	v, ok := dp.Attributes.Value(attribute.Key(key))
	if !ok {
		return ""
	}
	return v.AsString()
}

func TestRecordBroadcast_RecordsOutcome(t *testing.T) {
	m, reader := newCollectingMetrics(t)

	m.RecordBroadcast(t.Context(), "ok")
	m.RecordBroadcast(t.Context(), "failed")
	m.RecordBroadcast(t.Context(), "ok")

	points := sumDataPoints(t, reader, "mcp.write.broadcasts")
	if len(points) != 2 {
		t.Fatalf("data points = %d, want 2 (ok, failed)", len(points))
	}
	got := map[string]int64{}
	for _, dp := range points {
		got[attrValue(dp, "outcome")] = dp.Value
	}
	if got["ok"] != 2 {
		t.Errorf("outcome=ok count = %d, want 2", got["ok"])
	}
	if got["failed"] != 1 {
		t.Errorf("outcome=failed count = %d, want 1", got["failed"])
	}
}

func TestRecordRelayReject_RecordsCause(t *testing.T) {
	m, reader := newCollectingMetrics(t)

	m.RecordRelayReject(t.Context(), CauseDecode)
	m.RecordRelayReject(t.Context(), CauseSignerQuota)

	points := sumDataPoints(t, reader, "mcp.write.relay_scope_rejected")
	if len(points) != 2 {
		t.Fatalf("data points = %d, want 2", len(points))
	}
	got := map[string]int64{}
	for _, dp := range points {
		got[attrValue(dp, "cause")] = dp.Value
	}
	if got["decode"] != 1 {
		t.Errorf("cause=decode count = %d, want 1", got["decode"])
	}
	if got["signer_quota"] != 1 {
		t.Errorf("cause=signer_quota count = %d, want 1", got["signer_quota"])
	}
}

// TestNewMetrics_InstrumentErrors exercises every instrument-creation error
// branch in NewMetrics. The OTel SDK rejects instrument creation when a View
// assigns an incompatible aggregation (LastValue for counters/histograms, Sum
// for gauges), so each case breaks exactly one named instrument and asserts
// NewMetrics surfaces the wrapped error.
func TestNewMetrics_InstrumentErrors(t *testing.T) {
	lastValue := sdkmetric.AggregationLastValue{}
	sum := sdkmetric.AggregationSum{}

	tests := []struct {
		instrument string
		agg        sdkmetric.Aggregation
		wantMsg    string
	}{
		{"mcp.server.tool.duration", lastValue, "tool duration histogram"},
		{"mcp.server.tool.calls", lastValue, "tool call counter"},
		{"mcp.server.tool.errors", lastValue, "tool error counter"},
		{"mcp.server.active_requests", lastValue, "active requests gauge"},
		{"evm.rpc.duration", lastValue, "rpc duration histogram"},
		{"evm.rpc.errors", lastValue, "rpc error counter"},
		{"evm.rpc.retries", lastValue, "rpc retry counter"},
		{"evm.rpc.circuit_breaker.state", sum, "circuit breaker state gauge"},
		{"evm.rpc.rate_limited", lastValue, "rate limited counter"},
		{"mcp.http.responses", lastValue, "http responses counter"},
		{"mcp.write.broadcasts", lastValue, "write broadcasts counter"},
		{"mcp.write.relay_scope_rejected", lastValue, "write relay-scope rejected counter"},
	}

	for _, tc := range tests {
		t.Run(tc.instrument, func(t *testing.T) {
			view := sdkmetric.NewView(
				sdkmetric.Instrument{Name: tc.instrument},
				sdkmetric.Stream{Aggregation: tc.agg},
			)
			mp := sdkmetric.NewMeterProvider(
				sdkmetric.WithReader(sdkmetric.NewManualReader()),
				sdkmetric.WithView(view),
			)
			t.Cleanup(func() {
				if err := mp.Shutdown(context.Background()); err != nil {
					t.Logf("meter shutdown: %v", err)
				}
			})

			m, err := NewMetrics(mp)
			if err == nil {
				t.Fatalf("NewMetrics with broken %s returned nil error (metrics=%v)", tc.instrument, m)
			}
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestMetrics_RecordersNilInstrumentSafe covers the nil-instrument guard on a
// non-nil *Metrics (stdio/test callers constructing Metrics{} by hand).
func TestMetrics_RecordersNilInstrumentSafe(_ *testing.T) {
	m := &Metrics{}
	m.RecordBroadcast(context.Background(), "ok")
	m.RecordRelayReject(context.Background(), CauseRelayScope)
}
