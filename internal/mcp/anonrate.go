// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"log/slog"
	"net/http"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// AnonReadRateLimiter enforces a per-source-IP token-bucket limit on
// anonymous requests (no claims on context). Authenticated requests pass
// through untouched -- the per-client ClientRateLimiter owns them. The
// limits are expected to be tighter than the per-client limits
// (documented invariant). IP derivation honors the same trust-proxy
// setting as the fail-rate limiter (NVNM_TRUST_PROXY_HEADERS).
type AnonReadRateLimiter struct {
	inner      *keyedRateLimiter
	trustProxy bool
}

// NewAnonReadRateLimiter creates a per-source-IP anon limiter with the
// given requests-per-second and burst. trustProxy controls IP derivation:
// when true, the source IP is taken from the leftmost X-Forwarded-For
// entry (set only behind a reverse proxy that strips client-supplied
// values); when false, the socket RemoteAddr host is used. Fed by
// NVNM_TRUST_PROXY_HEADERS, matching the fail-rate limiter.
func NewAnonReadRateLimiter(rps float64, burst int, trustProxy bool) *AnonReadRateLimiter {
	return &AnonReadRateLimiter{
		inner:      newKeyedRateLimiter(rps, burst),
		trustProxy: trustProxy,
	}
}

// Start launches the background TTL janitor. Stop with Stop.
func (l *AnonReadRateLimiter) Start() { l.inner.Start() }

// Stop signals the janitor to exit and blocks until it has.
func (l *AnonReadRateLimiter) Stop() { l.inner.Stop() }

// Size reports the current number of per-IP buckets retained.
func (l *AnonReadRateLimiter) Size() int { return l.inner.Size() }

// Middleware enforces the per-IP limit on anonymous requests only.
func (l *AnonReadRateLimiter) Middleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.ClaimsFromContext(r.Context()) != nil {
			next.ServeHTTP(w, r) // authenticated: not this limiter's concern
			return
		}
		ip := clientIP(r, l.trustProxy)
		if !l.inner.allow(ip) {
			logger.Warn("anonymous read rate limit exceeded",
				slog.String("ip", ip),
				slog.String("remote_addr", r.RemoteAddr),
			)
			writeRateLimited(w, logger)
			return
		}
		next.ServeHTTP(w, r)
	})
}
