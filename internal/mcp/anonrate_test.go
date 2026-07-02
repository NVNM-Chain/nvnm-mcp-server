// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func newAnonTestHandler(l *AnonReadRateLimiter) http.Handler {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return l.Middleware(next, logger)
}

func TestAnonReadRateLimiter_BlocksOverBurstPerIP(t *testing.T) {
	limiter := NewAnonReadRateLimiter(0.0001, 1, false, 1) // burst=1
	handler := newAnonTestHandler(limiter)

	req1 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req1.RemoteAddr = "203.0.113.5:1111"
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first anon request got %d, want 200", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req2.RemoteAddr = "203.0.113.5:2222" // same IP, different port
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second anon request from same IP got %d, want 429", w2.Code)
	}
}

func TestAnonReadRateLimiter_PerIPIsolation(t *testing.T) {
	limiter := NewAnonReadRateLimiter(0.0001, 1, false, 1)
	handler := newAnonTestHandler(limiter)

	makeReq := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
		req.RemoteAddr = ip + ":9999"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}
	if code := makeReq("198.51.100.1"); code != http.StatusOK {
		t.Errorf("IP-1 first got %d, want 200", code)
	}
	if code := makeReq("198.51.100.2"); code != http.StatusOK {
		t.Errorf("IP-2 first got %d, want 200", code)
	}
}

func TestAnonReadRateLimiter_TrustProxyKeysOnXFF(t *testing.T) {
	limiter := NewAnonReadRateLimiter(0.0001, 1, true, 1) // trustProxy on, single hop, burst=1
	handler := newAnonTestHandler(limiter)

	// Same trusted proxy (RemoteAddr host) forwards for the same real
	// client (the XFF entry immediately to the proxy's left); an
	// attacker-controlled forged prefix must not mint a separate bucket
	// -- both requests key on the same hop-derived IP, not the forgeable
	// leftmost entry.
	req1 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req1.RemoteAddr = "10.0.0.1:1111"
	req1.Header.Set("X-Forwarded-For", "1.1.1.1, 203.0.113.9")
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request got %d, want 200", w1.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req2.RemoteAddr = "10.0.0.1:2222"                          // same proxy, different port
	req2.Header.Set("X-Forwarded-For", "2.2.2.2, 203.0.113.9") // forged prefix differs, real client same
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request (same real client, forged prefix differs) got %d, want 429", w2.Code)
	}
}

func TestAnonReadRateLimiter_AuthenticatedPassesThrough(t *testing.T) {
	limiter := NewAnonReadRateLimiter(0.0001, 1, false, 1) // burst=1
	handler := newAnonTestHandler(limiter)
	authCtx := auth.ContextWithClaims(t.Context(), &auth.Claims{ClientID: "client-x"})

	// Authenticated requests must bypass the anon limiter entirely.
	for i := range 3 {
		req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody).WithContext(authCtx)
		req.RemoteAddr = "203.0.113.5:1111"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("authenticated request %d got %d, want 200 (bypass)", i, w.Code)
		}
	}
}
