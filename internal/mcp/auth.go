package mcp

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// AuthMiddleware wraps an http.Handler with Bearer token authentication
// using the provided TokenValidator. When validator is nil, the handler is
// returned unwrapped (no authentication enforced).
// On successful authentication the Claims are stored in the request context
// (retrievable via auth.ClaimsFromContext).
func AuthMiddleware(next http.Handler, validator auth.TokenValidator, logger *slog.Logger) http.Handler {
	if validator == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			logger.Warn("rejected unauthenticated request",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
			)
			http.Error(w, "missing Authorization header", http.StatusUnauthorized)
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			logger.Warn("rejected request with invalid auth scheme",
				slog.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "invalid Authorization scheme; expected Bearer", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(authHeader, prefix)

		claims, err := validator.Validate(token)
		if err != nil {
			status := http.StatusForbidden
			msg := "invalid credentials"
			if errors.Is(err, auth.ErrInvalidAPIKey) {
				msg = "invalid API key"
			}
			logger.Warn("rejected request with invalid credentials",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("error", err.Error()),
			)
			http.Error(w, msg, status)
			return
		}

		ctx := auth.ContextWithClaims(r.Context(), claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
