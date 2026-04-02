package mcp

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// KeyLookup abstracts read-only key operations needed by the auth middleware.
// Both *KeyStore and *ManagedKeyStore implement this interface.
type KeyLookup interface {
	Lookup(rawKey string) *KeyEntry
	Empty() bool
}

// APIKeyAuth wraps an http.Handler with Bearer token authentication backed by
// a KeyLookup. When keys is nil or empty, the handler is returned unwrapped.
// On successful authentication the client identity from the matched KeyEntry
// is stored in the request context (retrievable via ClientIDFromContext).
func APIKeyAuth(next http.Handler, keys KeyLookup, logger *slog.Logger) http.Handler {
	if keys == nil || keys.Empty() {
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

		entry := keys.Lookup(token)
		if entry == nil {
			logger.Warn("rejected request with unknown API key",
				slog.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, "invalid API key", http.StatusForbidden)
			return
		}

		// Constant-time verify even though Lookup already matched, to avoid
		// timing side channels from the map lookup.
		if subtle.ConstantTimeCompare([]byte(token), []byte(entry.Key)) != 1 {
			http.Error(w, "invalid API key", http.StatusForbidden)
			return
		}

		ctx := auth.ContextWithClientID(r.Context(), entry.ID)
		ctx = auth.ContextWithWriteApproval(ctx, entry.WriteApproval)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
