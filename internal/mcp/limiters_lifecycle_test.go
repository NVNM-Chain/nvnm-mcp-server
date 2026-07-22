// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestKeyedRateLimiter_JanitorSweepsIdleBuckets(t *testing.T) {
	l := newKeyedRateLimiter(100, 1)
	l.idleTTL = 40 * time.Millisecond
	l.allow("stale-key")
	if l.Size() != 1 {
		t.Fatalf("Size = %d, want 1", l.Size())
	}

	l.Start()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && l.Size() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	l.Stop()
	l.Stop() // idempotent

	if got := l.Size(); got != 0 {
		t.Errorf("Size after janitor = %d, want 0", got)
	}
}

func TestKeyedRateLimiter_SweepKeepsFreshBuckets(t *testing.T) {
	l := newKeyedRateLimiter(100, 1)
	l.allow("fresh-key")
	l.sweep(time.Now()) // fresh bucket survives a sweep at "now"
	if got := l.Size(); got != 1 {
		t.Errorf("Size = %d, want 1", got)
	}
	l.sweep(time.Now().Add(l.idleTTL * 2)) // far-future sweep evicts
	if got := l.Size(); got != 0 {
		t.Errorf("Size after future sweep = %d, want 0", got)
	}
}

func TestClientRateLimiter_Lifecycle(t *testing.T) {
	l := NewClientRateLimiter(100, 1)
	l.Start()
	defer l.Stop()
	if got := l.Size(); got != 0 {
		t.Errorf("Size = %d, want 0", got)
	}
}

func TestAnonReadRateLimiter_Lifecycle(t *testing.T) {
	l := NewAnonReadRateLimiter(100, 1, false, 1)
	l.Start()
	defer l.Stop()
	if got := l.Size(); got != 0 {
		t.Errorf("Size = %d, want 0", got)
	}
}

func TestIPFailRateLimiter_Lifecycle(t *testing.T) {
	l := NewIPFailRateLimiter(100, 1, false, 1)
	l.idleTTL = 40 * time.Millisecond
	l.Penalize("9.9.9.9")
	if l.Size() != 1 {
		t.Fatalf("Size = %d, want 1", l.Size())
	}

	l.Start()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && l.Size() != 0 {
		time.Sleep(10 * time.Millisecond)
	}
	l.Stop()
	l.Stop() // idempotent

	if got := l.Size(); got != 0 {
		t.Errorf("Size after janitor = %d, want 0", got)
	}
}

func TestIPFailRateLimiter_WrapBlocksExhaustedIP(t *testing.T) {
	// Tiny refill rate so a consumed burst stays consumed for the test.
	l := NewIPFailRateLimiter(0.001, 1, false, 1)
	l.Penalize("1.2.3.4") // consume the whole burst

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := l.Wrap(next, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing")
	}

	// Same rejection with a broken writer: the write-error branch logs
	// and must not panic.
	h.ServeHTTP(newFailingResponseWriter(), req)

	// A different IP still passes through.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "5.6.7.8:1234"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("status for fresh IP = %d, want 200", rec2.Code)
	}
}

func TestIPFailRateLimiter_EmptyIPEdges(t *testing.T) {
	l := NewIPFailRateLimiter(1, 1, false, 1)
	l.Penalize("") // no-op
	if !l.peek("") {
		t.Error("peek(\"\") should always be true")
	}
	if got := l.Size(); got != 0 {
		t.Errorf("Size = %d, want 0 (empty IP never creates a bucket)", got)
	}
}

// TestAuthMiddleware_PenalizesFailLimiter covers the penalize hook: a
// rejected request must consume the source IP's failure budget.
func TestAuthMiddleware_PenalizesFailLimiter(t *testing.T) {
	entries := []KeyEntry{NewKeyEntry("c1", "valid-key-material", []string{"reader"})}
	mks := NewManagedKeyStoreFromEntries(tempKeysFile(t), entries)
	validator := auth.NewAPIKeyValidator(NewKeyLookupAdapter(mks))
	if validator == nil {
		t.Fatal("validator should be non-nil with keys configured")
	}

	failLimiter := NewIPFailRateLimiter(0.001, 2, false, 1)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := AuthMiddleware(next, validator, failLimiter, false, testLogger(), "")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "4.4.4.4:9999"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req) // no Authorization header -> 401 + penalize
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := failLimiter.Size(); got != 1 {
		t.Errorf("failLimiter buckets = %d, want 1 (penalized)", got)
	}
}
