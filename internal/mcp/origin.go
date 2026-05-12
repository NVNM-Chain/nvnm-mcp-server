package mcp

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"
)

// OriginAllowlist holds the set of Origin header values the server
// accepts on Streamable HTTP requests. Requests with no Origin header
// (typical for server-to-server, CLI, and curl callers) are always
// allowed; requests carrying an Origin header must match an entry.
//
// The guard defends against DNS rebinding attacks per the MCP
// specification:
//
//	https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
//
// It is not an authentication signal -- a browser sets Origin from the
// document context; non-browser callers can set anything (or nothing).
// Combine with AuthMiddleware for actual identity assertion.
type OriginAllowlist struct {
	// allowed stores lowercased, whitespace-trimmed origin values.
	// Lookups normalize the incoming header the same way.
	allowed map[string]struct{}
}

// NewOriginAllowlist builds an OriginAllowlist from a slice of
// allowed origins. Empty / whitespace entries are ignored. Values are
// stored lowercased for case-insensitive matching.
func NewOriginAllowlist(origins []string) *OriginAllowlist {
	a := &OriginAllowlist{allowed: make(map[string]struct{}, len(origins))}
	for _, o := range origins {
		o = strings.ToLower(strings.TrimSpace(o))
		if o == "" {
			continue
		}
		a.allowed[o] = struct{}{}
	}
	return a
}

// DefaultOriginAllowlist returns the allowlist used for local
// development: HTTP and HTTPS variants of localhost, 127.0.0.1, and
// [::1]. Production deployments must override via NVNM_ALLOWED_ORIGINS.
func DefaultOriginAllowlist() *OriginAllowlist {
	return NewOriginAllowlist([]string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"http://[::1]",
		"https://[::1]",
	})
}

// localhostPrefixes is the set of bare-host prefixes for which any port
// suffix is acceptable when the bare host is on the allowlist. Limited
// to loopback addresses so we never accept "https://localhost.attacker.tld".
//
//nolint:gochecknoglobals // immutable lookup table; package-level by design
var localhostPrefixes = []string{
	"http://localhost:",
	"https://localhost:",
	"http://127.0.0.1:",
	"https://127.0.0.1:",
	"http://[::1]:",
	"https://[::1]:",
}

// Allowed reports whether origin is permitted. The empty string (no
// Origin header) always passes. Non-empty values are matched
// case-insensitively against the allowlist; additionally, any port on
// a loopback host is accepted when the bare host is allowed.
func (a *OriginAllowlist) Allowed(origin string) bool {
	if origin == "" {
		return true
	}
	o := strings.ToLower(strings.TrimSpace(origin))
	if _, ok := a.allowed[o]; ok {
		return true
	}
	for _, prefix := range localhostPrefixes {
		if strings.HasPrefix(o, prefix) {
			bare := strings.TrimSuffix(prefix, ":")
			if _, ok := a.allowed[bare]; ok {
				return true
			}
		}
	}
	return false
}

// Resolved returns the sorted list of allowed origins for startup
// logging. Useful so operators can see at a glance which origins the
// server will accept on this run.
func (a *OriginAllowlist) Resolved() []string {
	out := make([]string, 0, len(a.allowed))
	for o := range a.allowed {
		out = append(out, o)
	}
	sort.Strings(out)
	return out
}

// originGuard wraps next with an Origin-header check. Requests whose
// Origin is not in allow get 403 with a structured log entry; requests
// with no Origin header pass through unchanged. Placed at the
// outermost position in the middleware chain so rejections short-
// circuit cheaper than auth or rate-limiting.
func originGuard(next http.Handler, allow *OriginAllowlist, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if !allow.Allowed(origin) {
			logger.Warn("rejecting request with disallowed Origin",
				slog.String("origin", origin),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
