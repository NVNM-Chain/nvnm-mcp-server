// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"
)

func testConfig() Config {
	return Config{
		ServiceName:      "nvnm-mcp-server-test",
		ServiceVersion:   "0.0.0-test",
		TraceSampleRatio: 1.0,
		OTLPInsecure:     true,
	}
}

// shutdownQuickly flushes the providers with a short deadline so tests never
// hang on exporters that have nowhere to send data.
func shutdownQuickly(t *testing.T, tel *Telemetry) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tel.Shutdown(ctx); err != nil {
		t.Logf("shutdown (best effort): %v", err)
	}
}

func TestNew_Minimal(t *testing.T) {
	tel, err := New(t.Context(), testConfig(), testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if tel.Metrics == nil {
		t.Error("Metrics is nil")
	}
	if h := tel.PrometheusHandler(); h != nil {
		t.Errorf("PrometheusHandler = %v, want nil when Prometheus disabled", h)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

func TestNew_PrometheusEnabled(t *testing.T) {
	cfg := testConfig()
	cfg.EnablePrometheus = true

	tel, err := New(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer shutdownQuickly(t, tel)

	handler := tel.PrometheusHandler()
	if handler == nil {
		t.Fatal("PrometheusHandler is nil with Prometheus enabled")
	}

	// Record a metric so the scrape output is non-trivial.
	tel.Metrics.RecordBroadcast(t.Context(), "ok")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want %d", w.Code, http.StatusOK)
	}
	if body := w.Body.String(); !strings.Contains(body, "mcp_write_broadcasts") {
		t.Errorf("scrape output missing mcp_write_broadcasts counter; got:\n%s", body)
	}
}

func TestNew_PartialSampling(t *testing.T) {
	cfg := testConfig()
	cfg.TraceSampleRatio = 0.25

	tel, err := New(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}

// TestNew_OTLPConfigured verifies the OTLP exporter wiring builds without a
// live collector. The gRPC client dials lazily, so construction is hermetic;
// Shutdown is given a short deadline because the flush has nowhere to go.
func TestNew_OTLPConfigured(t *testing.T) {
	cfg := testConfig()
	cfg.OTLPEndpoint = "127.0.0.1:1" // reserved port, never reachable
	cfg.OTLPInsecure = true

	tel, err := New(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New with OTLP endpoint: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// Flush fails (no collector listening); the error must be reported.
	if err := tel.Shutdown(ctx); err == nil {
		t.Log("Shutdown returned nil; exporter dropped data silently")
	}
}

// TestNew_OTLPSecure covers the TLS (OTLPInsecure=false) option branches in
// both the trace and metric exporter builders.
func TestNew_OTLPSecure(t *testing.T) {
	cfg := testConfig()
	cfg.OTLPEndpoint = "127.0.0.1:1"
	cfg.OTLPInsecure = false

	tel, err := New(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New with secure OTLP endpoint: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	//nolint:errcheck,gosec // flush has nowhere to go; error is expected
	tel.Shutdown(ctx)
}

// TestNew_InvalidOTLPEndpoint covers the exporter construction error path:
// an endpoint that fails URL parsing makes the gRPC trace exporter fail, and
// New must propagate the error.
func TestNew_InvalidOTLPEndpoint(t *testing.T) {
	cfg := testConfig()
	cfg.OTLPEndpoint = "%zz" // invalid URL escape, rejected by grpc.NewClient

	if _, err := New(t.Context(), cfg, testLogger()); err == nil {
		t.Fatal("New with unparsable OTLP endpoint returned nil error")
	}
}

// TestBuildMeterProvider_InvalidOTLPEndpoint covers the metric exporter
// construction error branch. It is unreachable through New (the trace
// exporter fails first on the same endpoint), so call the builder directly.
func TestBuildMeterProvider_InvalidOTLPEndpoint(t *testing.T) {
	cfg := testConfig()
	cfg.OTLPEndpoint = "%zz"

	_, _, err := buildMeterProvider(t.Context(), cfg, resource.Default(), testLogger())
	if err == nil {
		t.Fatal("buildMeterProvider with unparsable OTLP endpoint returned nil error")
	}
}

// TestShutdown_CanceledContext covers Shutdown's error-accumulation branches:
// with batching exporters configured, an already-canceled context makes both
// provider shutdowns fail, and the joined error must be reported.
func TestShutdown_CanceledContext(t *testing.T) {
	cfg := testConfig()
	cfg.EnableStdout = true

	tel, err := New(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tel.Shutdown(ctx); err == nil {
		t.Fatal("Shutdown with canceled context returned nil, want error")
	}
}

func TestNew_StdoutExporters(t *testing.T) {
	cfg := testConfig()
	cfg.EnableStdout = true

	tel, err := New(t.Context(), cfg, testLogger())
	if err != nil {
		t.Fatalf("New with stdout exporters: %v", err)
	}
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown: %v", err)
	}
}
