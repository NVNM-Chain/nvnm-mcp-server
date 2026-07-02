// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestFailLimiter(t *testing.T, rps float64, burst int, trustProxy bool) *IPFailRateLimiter {
	t.Helper()
	l := NewIPFailRateLimiter(rps, burst, trustProxy, 1)
	// Tests do not need the janitor; LRU + bounded-cap still bound the map.
	return l
}

func TestIPFailRateLimiter_AllowsBeforePenalize(t *testing.T) {
	l := newTestFailLimiter(t, 1, 2, false)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := l.Wrap(next, logger)

	// No prior failures -> request passes through.
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.RemoteAddr = "10.0.0.5:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected pass-through with empty budget, got %d", w.Code)
	}
}

func TestIPFailRateLimiter_BlocksAfterBurstExceeded(t *testing.T) {
	// burst=2 with very low rps so refill is effectively zero across the test.
	l := newTestFailLimiter(t, 0.0001, 2, false)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := l.Wrap(next, logger)

	const ip = "10.0.0.6"
	// Burn the burst directly via Penalize -- this is what AuthMiddleware
	// does on every failed bearer.
	l.Penalize(ip)
	l.Penalize(ip)

	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.RemoteAddr = ip + ":4242"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("after %d Penalize calls (burst=2), expected 429, got %d", 2, w.Code)
	}
	if got := w.Header().Get("Retry-After"); got == "" {
		t.Error("429 response should include Retry-After header")
	}
}

func TestIPFailRateLimiter_PerIPIsolation(t *testing.T) {
	l := newTestFailLimiter(t, 0.0001, 1, false)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := l.Wrap(next, logger)

	l.Penalize("10.0.0.7") // exhaust IP A
	// IP B should still pass.
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.RemoteAddr = "10.0.0.8:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("unrelated IP should pass, got %d", w.Code)
	}
	// IP A should be blocked.
	req2 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req2.RemoteAddr = "10.0.0.7:9999"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("penalized IP should be blocked, got %d", w2.Code)
	}
}

func TestIPFailRateLimiter_IPFromRequest_TrustProxyOff(t *testing.T) {
	l := newTestFailLimiter(t, 1, 1, false)
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.RemoteAddr = "10.0.0.9:8080"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := l.IPFromRequest(req); got != "10.0.0.9" {
		t.Errorf("trust-proxy=false: IPFromRequest = %q, want 10.0.0.9 (RemoteAddr host)", got)
	}
}

func TestIPFailRateLimiter_IPFromRequest_TrustProxyOn(t *testing.T) {
	// newTestFailLimiter builds with hops=1: a single trusted proxy, so
	// the derived IP is the XFF entry immediately left of RemoteAddr
	// (the one the trusted proxy itself appended), not the forgeable
	// leftmost entry.
	l := newTestFailLimiter(t, 1, 1, true)
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.RemoteAddr = "10.0.0.9:8080"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := l.IPFromRequest(req); got != "5.6.7.8" {
		t.Errorf("trust-proxy=true, hops=1: IPFromRequest = %q, want 5.6.7.8 (hop-derived, not leftmost)", got)
	}
}

func TestClientIP_Helper(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string // empty => header not set
		trustProxy bool
		hops       int
		want       string
	}{
		{
			name:       "trust proxy off ignores XFF, uses RemoteAddr host",
			remoteAddr: "10.0.0.9:8080",
			xff:        "1.2.3.4, 5.6.7.8",
			trustProxy: false,
			hops:       1,
			want:       "10.0.0.9",
		},
		{
			name:       "trust proxy on, hops=1 uses hop-derived entry, not leftmost",
			remoteAddr: "10.0.0.9:8080",
			xff:        "1.2.3.4, 5.6.7.8",
			trustProxy: true,
			hops:       1,
			want:       "5.6.7.8",
		},
		{
			name:       "trust proxy on but XFF absent falls back to RemoteAddr host",
			remoteAddr: "10.0.0.9:8080",
			xff:        "",
			trustProxy: true,
			hops:       1,
			want:       "10.0.0.9",
		},
		{
			name:       "RemoteAddr without port returns raw value",
			remoteAddr: "10.0.0.9", // no port => SplitHostPort errors
			xff:        "",
			trustProxy: false,
			hops:       1,
			want:       "10.0.0.9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if got := clientIP(req, tt.trustProxy, tt.hops); got != tt.want {
				t.Errorf("clientIP(trustProxy=%v, hops=%d) = %q, want %q", tt.trustProxy, tt.hops, got, tt.want)
			}
		})
	}
}

func TestClientIPHopCount(t *testing.T) {
	mk := func(remote, xff string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		return r
	}
	tests := []struct {
		name       string
		remote     string
		xff        string
		trustProxy bool
		hops       int
		want       string
	}{
		{"trust off ignores xff", "10.0.0.9:5555", "1.2.3.4", false, 1, "10.0.0.9"},
		{"single ingress", "10.0.0.1:5555", "203.0.113.7", true, 1, "203.0.113.7"},
		{"single ingress prepend attack", "10.0.0.1:5555", "6.6.6.6, 203.0.113.7", true, 1, "203.0.113.7"},
		{"cdn plus ingress", "10.0.0.2:5555", "203.0.113.7, 70.0.0.1", true, 2, "203.0.113.7"},
		{"cdn plus ingress prepend", "10.0.0.2:5555", "6.6.6.6, 203.0.113.7, 70.0.0.1", true, 2, "203.0.113.7"},
		{"missing xff falls back to remote", "10.0.0.2:5555", "", true, 2, "10.0.0.2"},
		{"hops exceeds chain falls back to remote", "10.0.0.2:5555", "203.0.113.7", true, 5, "10.0.0.2"},
		{"whitespace trimmed", "10.0.0.1:5555", " 203.0.113.7 , 70.0.0.1 ", true, 2, "203.0.113.7"},
		{"ipv6 remote no port-split crash", "[2001:db8::1]:5555", "", true, 1, "2001:db8::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clientIP(mk(tt.remote, tt.xff), tt.trustProxy, tt.hops)
			if got != tt.want {
				t.Fatalf("clientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestForgedLeftmostXFFDoesNotMintOwnBucket(t *testing.T) {
	l := NewIPFailRateLimiter(1.0, 5, true, 1) // trust proxy, single hop
	mk := func(xff string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
		r.RemoteAddr = "10.0.0.1:5555" // the proxy
		r.Header.Set("X-Forwarded-For", xff)
		return r
	}
	// Same real client (203.0.113.7 appended by the proxy), attacker varies
	// the forged left prefix. All must resolve to the same derived IP.
	a := l.IPFromRequest(mk("1.1.1.1, 203.0.113.7"))
	b := l.IPFromRequest(mk("2.2.2.2, 203.0.113.7"))
	if a != "203.0.113.7" || b != "203.0.113.7" {
		t.Fatalf("forged prefixes leaked into IP derivation: a=%q b=%q", a, b)
	}
}

func TestIPFailRateLimiter_LRUEvictionUnderCap(t *testing.T) {
	l := newTestFailLimiter(t, 1, 1, false)
	l.maxIPs = 3 // shrink the cap for the test
	l.Penalize("a")
	l.Penalize("b")
	l.Penalize("c")
	if l.Size() != 3 {
		t.Fatalf("Size after 3 distinct IPs = %d, want 3", l.Size())
	}
	// Sleep one tick so "d" is strictly newer than a/b/c -- otherwise
	// time.Now() could collide and the LRU pick is non-deterministic.
	time.Sleep(2 * time.Millisecond)
	l.Penalize("d")
	if l.Size() != 3 {
		t.Errorf("Size after 4th distinct IP with cap=3 = %d, want 3 (one evicted)", l.Size())
	}
}
