// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"log/slog"
	"net/http"
)

// CORS response-header values. The Streamable HTTP transport needs the
// client to send Authorization (bearer auth on writes), Content-Type
// (JSON-RPC body), and Mcp-Session-Id (the session handle the server
// issues on initialize); the browser must also be allowed to *read*
// Mcp-Session-Id off responses to echo it back on later requests.
const (
	corsAllowMethods  = "GET, POST, OPTIONS"
	corsAllowHeaders  = "Authorization, Content-Type, Mcp-Session-Id"
	corsExposeHeaders = "Mcp-Session-Id"
	// corsMaxAge caches a successful preflight so the browser does not
	// re-OPTIONS every request in a burst. Ten minutes.
	corsMaxAge = "600"
)

// CORSMiddleware grants browser-based MCP clients cross-origin
// permission. It shares the same OriginAllowlist as the Origin guard
// (NVNM_ALLOWED_ORIGINS), but answers a different question: the Origin
// guard is a server-side anti-spoof / DNS-rebinding defense that
// *rejects* disallowed origins, whereas CORS is the browser-facing
// permission grant that tells a compliant browser it may read the
// response. Both run; CORS sits outermost so it can answer OPTIONS
// preflight before the guard or any parser.
//
// Credentials (cookies) are never used, so Access-Control-Allow-
// Credentials is set to "false". A request with no Origin header
// (server-to-server, CLI, curl) passes through untouched.
func CORSMiddleware(next http.Handler, allow *OriginAllowlist, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Preflight: an OPTIONS carrying Access-Control-Request-Method.
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if origin == "" || !allow.Allowed(origin) {
				logger.Warn("rejecting CORS preflight from disallowed origin",
					slog.String("origin", origin),
					slog.String("remote_addr", r.RemoteAddr),
				)
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Allow-Credentials", "false")
			h.Set("Access-Control-Max-Age", corsMaxAge)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Actual request: attach permission headers when the origin is
		// allowed, then forward. Disallowed or absent origins are not
		// decorated; the Origin guard downstream handles rejection.
		if origin != "" && allow.Allowed(origin) {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "false")
			h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		}
		next.ServeHTTP(w, r)
	})
}
