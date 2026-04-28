package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

func TestClientRateLimiter_AllowsUnderLimit(t *testing.T) {
	limiter := NewClientRateLimiter(100, 5)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := limiter.Middleware(next, logger)

	for i := range 5 {
		req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200", i, w.Code)
		}
	}
}

func TestClientRateLimiter_BlocksOverBurst(t *testing.T) {
	limiter := NewClientRateLimiter(0.0001, 1) // very low rps, burst=1
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := limiter.Middleware(next, logger)

	// First request consumes the burst token
	req1 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request should succeed, got %d", w1.Code)
	}

	// Second request should be rate limited
	req2 := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second request should be rate limited (429), got %d", w2.Code)
	}
}

func TestClientRateLimiter_PerClientIsolation(t *testing.T) {
	limiter := NewClientRateLimiter(0.0001, 1) // burst=1 per client
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := limiter.Middleware(next, logger)

	makeReqForClient := func(clientID string) int {
		ctx := auth.ContextWithClaims(t.Context(), &auth.Claims{ClientID: clientID})
		req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody).WithContext(ctx)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// Each client gets their own burst -- first request should succeed
	if code := makeReqForClient("client-a"); code != http.StatusOK {
		t.Errorf("client-a first request got %d, want 200", code)
	}
	if code := makeReqForClient("client-b"); code != http.StatusOK {
		t.Errorf("client-b first request got %d, want 200", code)
	}

	// Second request for each client should be rate limited independently
	if code := makeReqForClient("client-a"); code != http.StatusTooManyRequests {
		t.Errorf("client-a second request got %d, want 429", code)
	}
	if code := makeReqForClient("client-b"); code != http.StatusTooManyRequests {
		t.Errorf("client-b second request got %d, want 429", code)
	}
}
