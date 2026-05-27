// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// AuthMiddleware wraps an http.Handler with Bearer token authentication
// using the provided TokenValidator. When validator is nil, the handler
// is returned unwrapped (no authentication enforced). The HTTP
// transport entrypoint refuses to start when validator is nil; this nil
// path is for stdio and tests only.
//
// On successful authentication the Claims are stored in the request
// context (retrievable via auth.ClaimsFromContext).
//
// When failLimiter is non-nil, every authentication failure (missing
// header, wrong scheme, invalid credentials) deducts a token from the
// caller's per-IP failure budget so the outer Wrap can reject future
// probes from the same source.
//
// Status codes follow RFC 7235: missing or unparseable credentials
// return 401 (the caller must (re)authenticate); credentials that
// authenticate but fail policy return 403. Here every failure is a
// validation failure -> 401.
//
// When keylessReads is true, a request with NO Authorization header
// passes through anonymously (no claims in context) so downstream
// per-tool enforcement can admit read tools and reject auth-required
// tools. A present-but-invalid token is STILL rejected 401 regardless
// of keylessReads: anonymity requires sending no credential at all.
func AuthMiddleware(
	next http.Handler,
	validator auth.TokenValidator,
	failLimiter *IPFailRateLimiter,
	keylessReads bool,
	logger *slog.Logger,
) http.Handler {
	if validator == nil {
		return next
	}

	penalize := func(r *http.Request) {
		if failLimiter == nil {
			return
		}
		failLimiter.Penalize(failLimiter.IPFromRequest(r))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			if keylessReads {
				// Anonymous: no Authorization header at all. Per-tool
				// enforcement downstream rejects auth-required tools; read
				// tools proceed. Anonymity requires sending no credential.
				next.ServeHTTP(w, r)
				return
			}
			penalize(r)
			logger.Warn("rejected unauthenticated request",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
			)
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			penalize(r)
			logger.Warn("rejected request with invalid auth scheme",
				slog.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "invalid Authorization scheme; expected Bearer", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, prefix)

		claims, err := validator.Validate(token)
		if err != nil {
			penalize(r)
			msg := "invalid credentials"
			if errors.Is(err, auth.ErrInvalidAPIKey) {
				msg = "invalid API key"
			}
			logger.Warn("rejected request with invalid credentials",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("error", err.Error()),
			)
			http.Error(w, msg, http.StatusUnauthorized)
			return
		}

		ctx := auth.ContextWithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
