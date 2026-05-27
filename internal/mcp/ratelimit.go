// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// ClientRateLimiter enforces per-authenticated-client token-bucket rate
// limits. Anonymous requests (no claims on the context) pass through
// untouched -- the AnonReadRateLimiter owns anonymous traffic when
// keyless reads are enabled. Place this middleware inside AuthMiddleware
// so the client ID is already on the request context.
type ClientRateLimiter struct {
	inner *keyedRateLimiter
}

// NewClientRateLimiter creates a per-client limiter with the given
// requests-per-second and burst capacity.
func NewClientRateLimiter(rps float64, burst int) *ClientRateLimiter {
	return &ClientRateLimiter{inner: newKeyedRateLimiter(rps, burst)}
}

// Start launches the background TTL janitor. Stop with Stop.
func (l *ClientRateLimiter) Start() { l.inner.Start() }

// Stop signals the janitor to exit and blocks until it has.
func (l *ClientRateLimiter) Stop() { l.inner.Stop() }

// Size reports the current number of per-client buckets retained.
func (l *ClientRateLimiter) Size() int { return l.inner.Size() }

// Middleware enforces the per-client limit. Authenticated requests are
// bucketed by client ID; anonymous requests pass through (handled by the
// anon limiter). Over-limit requests get HTTP 429 with a JSON body.
func (l *ClientRateLimiter) Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := auth.ClientIDFromContext(r.Context())
		if clientID == "" {
			// Anonymous request. This is only reachable under keyless reads
			// (otherwise AuthMiddleware rejects no-token requests upstream).
			// Anonymous traffic is throttled per-IP by AnonReadRateLimiter,
			// not here, so pass through untouched.
			next.ServeHTTP(w, r)
			return
		}
		if !l.inner.allow(clientID) {
			logger.Warn("MCP rate limit exceeded",
				slog.String("client_id", clientID),
				slog.String("remote_addr", r.RemoteAddr),
			)
			writeRateLimited(w, logger)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeRateLimited writes the shared 429 JSON response.
func writeRateLimited(w http.ResponseWriter, logger *slog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	if encErr := json.NewEncoder(w).Encode(map[string]string{
		"error": "rate limit exceeded",
	}); encErr != nil {
		logger.Warn("rate limit: encode error response", slog.String("error", encErr.Error()))
	}
}
