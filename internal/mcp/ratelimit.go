package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"golang.org/x/time/rate"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// ClientRateLimiter enforces per-client token-bucket rate limits on HTTP
// requests. Each authenticated client ID gets its own limiter; unauthenticated
// requests share a single "anonymous" bucket.
//
// The middleware must be placed inside AuthMiddleware so that the client ID is
// already present on the request context.
type ClientRateLimiter struct {
	mu      sync.Mutex
	clients map[string]*rate.Limiter
	rps     float64
	burst   int
}

// NewClientRateLimiter creates a rate limiter with the given requests-per-second
// and burst capacity per client.
func NewClientRateLimiter(rps float64, burst int) *ClientRateLimiter {
	return &ClientRateLimiter{
		clients: make(map[string]*rate.Limiter),
		rps:     rps,
		burst:   burst,
	}
}

// Middleware returns an http.Handler that enforces the per-client rate limit.
// Requests that exceed the limit receive HTTP 429 with a JSON error body.
func (l *ClientRateLimiter) Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := auth.ClientIDFromContext(r.Context())
		if clientID == "" {
			clientID = "__anonymous__"
		}

		limiter := l.getLimiter(clientID)
		if !limiter.Allow() {
			logger.Warn("MCP rate limit exceeded",
				slog.String("client_id", clientID),
				slog.String("remote_addr", r.RemoteAddr),
			)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			if encErr := json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded",
			}); encErr != nil {
				logger.Warn("rate limit: encode error response", slog.String("error", encErr.Error()))
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getLimiter returns the rate limiter for the given client ID, creating one
// lazily if it does not yet exist.
func (l *ClientRateLimiter) getLimiter(clientID string) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	if lim, ok := l.clients[clientID]; ok {
		return lim
	}

	lim := rate.NewLimiter(rate.Limit(l.rps), l.burst)
	l.clients[clientID] = lim
	return lim
}
