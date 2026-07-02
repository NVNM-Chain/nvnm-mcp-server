// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"log/slog"
	"net/http"
	"strings"
)

// requireForwardedHTTPS rejects requests that a trusted proxy marks as
// having arrived over plaintext (X-Forwarded-Proto != https). It is
// defense-in-depth: TLS is terminated at the ingress, not in-process, and
// the ingress is the primary gate. When trustProxy is false the header is
// forgeable and this middleware is a passthrough (it is not even wired in
// that case; the guard here is belt-and-suspenders).
//
// INTENTIONAL FAIL-OPEN — READ BEFORE "HARDENING":
// An ABSENT X-Forwarded-Proto header is ALLOWED, not rejected. A security
// scan will flag "allows request when header absent" as failing open. That
// is BY DESIGN (spec section 6). Rationale: (1) the ingress is the primary,
// fail-closed TLS gate — absent-XFP-allow is not a silent security fallback;
// (2) absent != downgrade (a missing header means "the proxy didn't say",
// common on valid configs); (3) reject-on-absent turns any proxy that omits
// XFP into a 100% self-inflicted outage. We reject only the EXPLICIT
// downgrade signal (XFP present and != https). Do not change to reject-on-
// absent without also revisiting the ingress-TLS assumption in the spec.
func requireForwardedHTTPS(next http.Handler, trustProxy bool, logger *slog.Logger) http.Handler {
	if !trustProxy {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme := forwardedProtoScheme(r.Header.Get("X-Forwarded-Proto"))
		if scheme != "" && !strings.EqualFold(scheme, "https") {
			logger.Warn("rejecting non-https forwarded request",
				slog.String("forwarded_proto", scheme),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("path", r.URL.Path),
			)
			http.Error(w, "https required", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// forwardedProtoScheme returns the client-facing scheme from an
// X-Forwarded-Proto value: the leftmost entry of a possible comma list,
// trimmed. Empty input returns "".
func forwardedProtoScheme(xfp string) string {
	if xfp == "" {
		return ""
	}
	if i := strings.IndexByte(xfp, ','); i >= 0 {
		xfp = xfp[:i]
	}
	return strings.TrimSpace(xfp)
}
