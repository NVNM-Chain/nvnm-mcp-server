// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func TestNewMetrics(t *testing.T) {
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})

	m, err := NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	if m.ToolCallDuration == nil {
		t.Error("ToolCallDuration is nil")
	}
	if m.ToolCallCount == nil {
		t.Error("ToolCallCount is nil")
	}
	if m.ToolErrorCount == nil {
		t.Error("ToolErrorCount is nil")
	}
	if m.ActiveRequests == nil {
		t.Error("ActiveRequests is nil")
	}
	if m.RPCDuration == nil {
		t.Error("RPCDuration is nil")
	}
	if m.RPCErrorCount == nil {
		t.Error("RPCErrorCount is nil")
	}
}

func TestNewMetrics_WriteDetectionInstruments(t *testing.T) {
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})

	m, err := NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	if m.WriteBroadcasts == nil {
		t.Error("WriteBroadcasts is nil")
	}
	if m.WriteRelayScopeRejected == nil {
		t.Error("WriteRelayScopeRejected is nil")
	}
}

func TestMetrics_RecordersNilSafe(_ *testing.T) {
	var m *Metrics // nil receiver must not panic
	m.RecordBroadcast(context.Background(), "ok")
	m.RecordRelayReject(context.Background(), CauseRelayScope)
}

// TestRelayRejectCause_ClosedSet documents the closed set of relay-reject
// causes as a fixed, lowercase token vocabulary. RelayRejectCause is a
// distinct type (not string) precisely so that caller-derived data -- e.g. a
// signer address -- cannot compile into a /metrics label value; this test
// exists to keep that closed set enumerated and reviewed on change.
func TestRelayRejectCause_ClosedSet(t *testing.T) {
	want := map[RelayRejectCause]bool{
		CauseDecode: true, CauseAnchorMisconfig: true, CauseRelayScope: true,
		CauseSignerBlacklist: true, CauseSignerQuota: true,
		CauseQuotaStoreErr: true, CauseBlacklistStoreErr: true,
	}
	// Every cause value is a fixed, non-empty, lowercase token.
	for c := range want {
		if string(c) == "" || strings.ToLower(string(c)) != string(c) {
			t.Errorf("cause %q must be a fixed lowercase token", c)
		}
	}
	// Compile-time guarantee check: RecordRelayReject must not accept a string.
	// (This is asserted by the signature; the test documents intent.)
}
