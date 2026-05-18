// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func newTestMetrics(t *testing.T) *Metrics {
	t.Helper()
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
	return m
}

func TestNewMCPMiddleware_NotNil(t *testing.T) {
	metrics := newTestMetrics(t)
	mw := NewMCPMiddleware(metrics, testLogger())
	if mw == nil {
		t.Fatal("NewMCPMiddleware returned nil")
	}
}

func TestRequestIDFromContext_Empty(t *testing.T) {
	id := RequestIDFromContext(context.Background())
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestRequestIDFromContext_Set(t *testing.T) {
	ctx := context.WithValue(context.Background(), requestIDKey{}, "test-id-123")
	id := RequestIDFromContext(ctx)
	if id != "test-id-123" {
		t.Errorf("expected %q, got %q", "test-id-123", id)
	}
}

func TestExtractToolName_NonToolCall(t *testing.T) {
	// For non-tools/call methods, the function returns the method itself
	// without touching the request, so nil is safe.
	name := extractToolName("resources/list", nil)
	if name != "resources/list" {
		t.Errorf("expected %q, got %q", "resources/list", name)
	}
}
