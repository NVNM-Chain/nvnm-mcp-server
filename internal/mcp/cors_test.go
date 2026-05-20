// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func corsTestAllowlist() *OriginAllowlist {
	return NewOriginAllowlist([]string{
		"https://claude.ai",
		"https://mcp.nvnmchain.io",
	})
}

func corsDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// A preflight request from an allowed origin must be answered by the
// CORS middleware itself (204, with the permission headers) and must NOT
// be forwarded to the wrapped handler.
func TestCORSMiddleware_PreflightAllowedOrigin(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })
	h := CORSMiddleware(next, corsTestAllowlist(), corsDiscardLogger())

	req := httptest.NewRequest(http.MethodOptions, "/", http.NoBody)
	req.Header.Set("Origin", "https://claude.ai")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("preflight must be answered by CORS, not forwarded to next handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://claude.ai" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}
	allowHeaders := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	for _, want := range []string{"mcp-session-id", "authorization", "content-type"} {
		if !strings.Contains(allowHeaders, want) {
			t.Fatalf("Access-Control-Allow-Headers = %q, want it to include %q", allowHeaders, want)
		}
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "false" {
		t.Fatalf("Access-Control-Allow-Credentials = %q, want \"false\" (no cookies)", got)
	}
	// A preflight the browser will accept must advertise the methods.
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("Access-Control-Allow-Methods = %q, want it to include POST", got)
	}
	// Echoing a per-request Origin without Vary: Origin lets a shared
	// cache serve one origin's permission headers to another.
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Fatalf("Vary = %q, want it to include Origin", got)
	}
}

// A preflight request from a disallowed origin must be rejected (403)
// without emitting an Access-Control-Allow-Origin header, and must not
// reach the wrapped handler.
func TestCORSMiddleware_PreflightDisallowedOrigin(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })
	h := CORSMiddleware(next, corsTestAllowlist(), corsDiscardLogger())

	req := httptest.NewRequest(http.MethodOptions, "/", http.NoBody)
	req.Header.Set("Origin", "https://attacker.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("disallowed-origin preflight must not reach next handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disallowed preflight status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

// A non-preflight (simple/actual) request must pass through to the
// wrapped handler. When the origin is allowed it gains the
// Access-Control-Allow-Origin header and exposes Mcp-Session-Id so the
// browser client can read the session id the server sets on init.
func TestCORSMiddleware_SimpleRequestPassthrough(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	h := CORSMiddleware(next, corsTestAllowlist(), corsDiscardLogger())

	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	req.Header.Set("Origin", "https://claude.ai")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("simple request must pass through to next handler")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("simple request status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://claude.ai" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want the echoed origin", got)
	}
	if got := strings.ToLower(rec.Header().Get("Access-Control-Expose-Headers")); !strings.Contains(got, "mcp-session-id") {
		t.Fatalf("Access-Control-Expose-Headers = %q, want it to include Mcp-Session-Id", got)
	}
}

// A request with no Origin header (server-to-server, CLI, curl) passes
// through untouched and gains no CORS headers.
func TestCORSMiddleware_NoOriginPassthrough(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })
	h := CORSMiddleware(next, corsTestAllowlist(), corsDiscardLogger())

	req := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("no-Origin request must pass through to next handler")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want empty when no Origin header is present", got)
	}
}
