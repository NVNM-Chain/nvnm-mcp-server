// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

// newMetricsAndReader returns a freshly-registered Metrics bound to a manual
// reader so tests can assert on collected datapoints without standing up a
// Prometheus endpoint.
func newMetricsAndReader(t *testing.T) (*telemetry.Metrics, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})
	m, err := telemetry.NewMetrics(mp)
	if err != nil {
		t.Fatal(err)
	}
	return m, reader
}

// httpResponsesDatapoints walks the collected metrics for the
// mcp.http.responses counter and returns its raw datapoints. Test convenience
// — keeps the assertion sites short.
func httpResponsesDatapoints(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.DataPoint[int64] {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect: %v", err)
	}
	for _, sm := range collected.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "mcp.http.responses" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("mcp.http.responses is %T, want Sum[int64]", m.Data)
			}
			return sum.DataPoints
		}
	}
	return nil
}

// TestStatusRecorder_DefaultsToOK pins the contract that Write() before any
// WriteHeader() records the implicit 200 status. Without this guarantee, a
// handler that streams without an explicit WriteHeader (the SDK's SSE path
// is one) would be classified as server_fault by ClassifyStatus(0).
func TestStatusRecorder_DefaultsToOK(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	if _, err := sr.Write([]byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if sr.status != http.StatusOK {
		t.Errorf("status = %d, want 200", sr.status)
	}
}

// TestStatusRecorder_CapturesExplicitWriteHeader confirms the recorder
// observes the inner handler's status — the load-bearing behavior for
// the SLI classification.
func TestStatusRecorder_CapturesExplicitWriteHeader(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	sr.WriteHeader(http.StatusTooManyRequests)
	if sr.status != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", sr.status)
	}
}

// TestStatusRecorder_ForwardsFlush keeps the SSE channel honest: a wrapper
// that hides Flusher silently buffers streaming responses. We don't assert
// the underlying Flusher fires (httptest.ResponseRecorder does not surface
// it directly); we only assert the call does not panic and the recorder
// continues to capture status correctly afterwards.
func TestStatusRecorder_ForwardsFlush(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	sr.Flush() // No-op on httptest.ResponseRecorder; asserts no panic.
	sr.WriteHeader(http.StatusAccepted)
	if sr.status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", sr.status)
	}
}

// TestResponseMetricsMiddleware_NilMetricsPassthrough pins the contract that
// nil metrics is a graceful no-op. Stdio callers and tests rely on this.
func TestResponseMetricsMiddleware_NilMetricsPassthrough(t *testing.T) {
	t.Parallel()
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := responseMetricsMiddleware(inner, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// TestResponseMetricsMiddleware_EmitsClassLabel walks the four SLI class
// values through the middleware and asserts each emits one datapoint with
// the right class label. This is the contract the alerts in
// deploy/prometheus/alerts.yaml depend on.
func TestResponseMetricsMiddleware_EmitsClassLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		wantClass string
	}{
		{name: "200 success", status: 200, wantClass: telemetry.ResponseClassSuccess},
		{name: "404 client_error", status: 404, wantClass: telemetry.ResponseClassClientError},
		{name: "429 customer_impact", status: 429, wantClass: telemetry.ResponseClassCustomerImpact},
		{name: "503 server_fault", status: 503, wantClass: telemetry.ResponseClassServerFault},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			metrics, reader := newMetricsAndReader(t)
			inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})
			mw := responseMetricsMiddleware(inner, metrics)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
			mw.ServeHTTP(rec, req)

			dps := httpResponsesDatapoints(t, reader)
			if len(dps) != 1 {
				t.Fatalf("got %d datapoints, want 1", len(dps))
			}
			class, ok := dps[0].Attributes.Value("class")
			if !ok {
				t.Fatalf("class attribute missing on datapoint")
			}
			if class.AsString() != tc.wantClass {
				t.Errorf("class = %q, want %q", class.AsString(), tc.wantClass)
			}
			if dps[0].Value != 1 {
				t.Errorf("counter value = %d, want 1", dps[0].Value)
			}
		})
	}
}
