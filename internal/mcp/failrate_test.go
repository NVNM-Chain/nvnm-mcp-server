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
	l := NewIPFailRateLimiter(rps, burst, trustProxy)
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
	l := newTestFailLimiter(t, 1, 1, true)
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.RemoteAddr = "10.0.0.9:8080"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	if got := l.IPFromRequest(req); got != "1.2.3.4" {
		t.Errorf("trust-proxy=true: IPFromRequest = %q, want 1.2.3.4 (leftmost XFF)", got)
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

func TestClientRateLimiter_LRUEvictionUnderCap(t *testing.T) {
	l := NewClientRateLimiter(1, 1)
	l.maxClients = 3
	_ = l.getLimiter("client-a")
	_ = l.getLimiter("client-b")
	_ = l.getLimiter("client-c")
	if got := l.Size(); got != 3 {
		t.Fatalf("Size = %d, want 3", got)
	}
	time.Sleep(2 * time.Millisecond)
	_ = l.getLimiter("client-d")
	if got := l.Size(); got != 3 {
		t.Errorf("Size after 4th client with cap=3 = %d, want 3", got)
	}
}
