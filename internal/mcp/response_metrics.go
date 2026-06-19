// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

// statusRecorder wraps an http.ResponseWriter to capture the status code the
// inner handler writes. If WriteHeader is never called, Write implicitly
// writes 200 — we record that explicitly so the SLI counter sees the right
// class without depending on the inner handler's discipline.
//
// http.Flusher is forwarded explicitly because the MCP streamable-HTTP handler
// uses Server-Sent Events (SSE). Wrapping a Flusher without re-exporting it
// breaks streaming silently — the response would buffer and the SSE consumer
// would see no chunks until the connection closed.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	if !sr.wroteHeader {
		sr.status = code
		sr.wroteHeader = true
	}
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wroteHeader {
		sr.status = http.StatusOK
		sr.wroteHeader = true
	}
	return sr.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer's Flush when it implements
// http.Flusher. Required for the MCP streamable-HTTP transport's SSE channel
// — without this, intermediate writes buffer until the response ends.
func (sr *statusRecorder) Flush() {
	if f, ok := sr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// responseMetricsMiddleware records a single increment on
// mcp_http_responses_total per HTTP request that reaches it, labeled with
// the SLI class computed from the response status (see
// telemetry.ClassifyStatus). The counter is the numerator-and-denominator
// source for the Phase 10 error-rate SLI (RD3).
//
// Placement: outermost real-request layer, inside CORS so OPTIONS preflight
// responses do not pollute the SLI denominator. CORS rejections do not reach
// this middleware; Origin-guard rejections do (and are intentionally counted
// — a sudden uptick in 403s tells operators something is misconfigured
// upstream).
//
// metrics may be nil for tests or for transports that opt out of telemetry;
// the middleware then becomes a passthrough with the status-recording wrap
// still active (cheap, and keeps the type stable).
func responseMetricsMiddleware(next http.Handler, metrics *telemetry.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sr, r)
		if metrics == nil || metrics.HTTPResponses == nil {
			return
		}
		metrics.HTTPResponses.Add(r.Context(), 1, metric.WithAttributes(
			attribute.String("class", telemetry.ClassifyStatus(sr.status)),
			attribute.String("method", r.Method),
		))
	})
}
