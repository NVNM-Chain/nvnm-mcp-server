// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "net/http"

// OAuth discovery well-known paths (RFC 8414 authorization-server metadata
// and RFC 9728 protected-resource metadata). Claude-class MCP clients probe
// these during the connection handshake to decide whether to run an OAuth
// flow. This server has no OAuth authorization server — it authenticates
// opaque API keys / FusionAuth JWTs supplied out-of-band — so it must answer
// 404 here, meaning "no OAuth discovery; use your configured credentials."
const (
	wellKnownProtectedResource = "/.well-known/oauth-protected-resource"
	wellKnownAuthServer        = "/.well-known/oauth-authorization-server"
)

// wellKnownGuard intercepts the OAuth discovery well-known paths and answers
// 404 before the request reaches AuthMiddleware. Without it those paths fall
// through to the authenticated MCP handler and return 401, which a Claude-class
// client reads as "this is an OAuth-protected server I can't authenticate to"
// — surfacing as "Needs authentication" even when a valid static Bearer token
// is configured. A 404 tells the client there is no OAuth discovery here, so it
// proceeds with the configured credentials. All other paths pass through
// unchanged. Placed OUTSIDE AuthMiddleware so the 404 is not itself gated.
func wellKnownGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case wellKnownProtectedResource, wellKnownAuthServer:
			http.NotFound(w, r)
			return
		default:
			next.ServeHTTP(w, r)
		}
	})
}
