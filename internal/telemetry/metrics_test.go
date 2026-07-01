// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
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
	m.RecordRelayReject(context.Background(), "relay_scope")
}
