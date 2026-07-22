// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"unicode"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// KeyRequestPath is the URL path the public self-serve endpoint
// answers at. Exported so server.go's path mux and tests can pin to
// the same string. Phase 11 L3 / RD3.
const KeyRequestPath = "/api/v1/keys/request"

// KeyRequestRateLimiter enforces a per-source-IP token-bucket on
// POST /api/v1/keys/request. Wraps the package-internal
// keyedRateLimiter the same way AnonReadRateLimiter does, so the
// rate-limit lifecycle (Start / Stop / Size) is symmetric across the
// two anonymous-traffic surfaces.
type KeyRequestRateLimiter struct {
	inner      *keyedRateLimiter
	trustProxy bool
}

// NewKeyRequestRateLimiter builds the per-IP limiter. trustProxy
// mirrors NVNM_TRUST_PROXY_HEADERS — same gating as the other public-
// traffic limiters so an operator's proxy posture stays uniform.
func NewKeyRequestRateLimiter(rps float64, burst int, trustProxy bool) *KeyRequestRateLimiter {
	return &KeyRequestRateLimiter{
		inner:      newKeyedRateLimiter(rps, burst),
		trustProxy: trustProxy,
	}
}

// Start launches the background TTL janitor.
func (l *KeyRequestRateLimiter) Start() { l.inner.Start() }

// Stop signals the janitor to exit and blocks until it has.
func (l *KeyRequestRateLimiter) Stop() { l.inner.Stop() }

// Size reports the current number of per-IP buckets retained.
func (l *KeyRequestRateLimiter) Size() int { return l.inner.Size() }

// allowIP wraps the inner allow() so the handler doesn't dereference
// the package-private primitive directly.
func (l *KeyRequestRateLimiter) allowIP(ip string) bool { return l.inner.allow(ip) }

// keyRequestMaxEmailLen, keyRequestMaxIntendedUseLen, keyRequestMaxCompanyLen
// cap the customer-supplied string fields. The body-size cap on the
// JSON envelope is the outer ceiling; these are inner caps so the
// reviewer queue does not display unreadable walls of text.
const (
	keyRequestMaxEmailLen       = 320  // RFC 5321 SMTP path maximum
	keyRequestMaxIntendedUseLen = 2000 // operator-friendly prose budget
	keyRequestMaxCompanyLen     = 200
)

// KeyRequestInput is the JSON body shape for POST /api/v1/keys/request.
// Matches the Phase 11 RD1 PII schema: required email and intended_use
// free-text, optional company.
type KeyRequestInput struct {
	Email       string `json:"email"`
	Company     string `json:"company,omitempty"`
	IntendedUse string `json:"intended_use"`
}

// KeyRequestResponse is the 202 body returned by the public endpoint.
// Phase 11 RD3 fixed this shape so the on-the-wire contract can absorb
// a later transition from manual approval to auto-issuance without
// breaking callers.
type KeyRequestResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
}

// KeyRequestHandlerConfig wires the handler's dependencies. Store is
// required; RateLimiter is optional (nil = no per-IP throttle, useful
// in tests). MaxBodyBytes defaults to 16 KiB if zero. TrustProxy mirrors
// the project-wide NVNM_TRUST_PROXY_HEADERS gating for the IP the
// handler derives via clientIP: that single derived IP gates BOTH
// cfg.RateLimiter.allowIP (the per-IP throttle) and the audit-trail IP
// captured on the PendingKeyRequest.
type KeyRequestHandlerConfig struct {
	Store        *PendingKeyStore
	RateLimiter  *KeyRequestRateLimiter
	MaxBodyBytes int64
	TrustProxy   bool

	// TrustedProxyHops mirrors Config.TrustedProxyHops; number of trusted
	// proxy hops for clientIP derivation. Only used when TrustProxy is true.
	TrustedProxyHops int
	Logger           *slog.Logger
}

// NewKeyRequestHandler returns an http.Handler that accepts
// POST /api/v1/keys/request and enqueues a PendingKeyRequest for human
// review. Returns 202 with { request_id, status } on success.
//
// Trust boundary: the handler runs OUTSIDE AuthMiddleware (anyone can
// hit it) and OUTSIDE ClientRateLimiter (which keys on auth claims).
// The endpoint-specific keyedRateLimiter handles the spam-flood
// posture; the outer originGuard + body-size + IPFailRateLimiter
// layers still apply.
func NewKeyRequestHandler(cfg KeyRequestHandlerConfig) http.Handler {
	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = 16 * 1024
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeKeyRequestError(w, cfg.Logger, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, "application/json") {
			writeKeyRequestError(w, cfg.Logger, http.StatusUnsupportedMediaType,
				"Content-Type must be application/json")
			return
		}

		ip := clientIP(r, cfg.TrustProxy, cfg.TrustedProxyHops)

		if cfg.RateLimiter != nil && !cfg.RateLimiter.allowIP(ip) {
			cfg.Logger.Warn("key request rate limited",
				slog.String("ip", ip),
				slog.String("remote_addr", r.RemoteAddr),
			)
			writeRateLimited(w, cfg.Logger)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
		var in KeyRequestInput
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			writeKeyRequestError(w, cfg.Logger, http.StatusBadRequest,
				"invalid JSON body")
			return
		}
		if msg := validateKeyRequest(in); msg != "" {
			writeKeyRequestError(w, cfg.Logger, http.StatusBadRequest, msg)
			return
		}

		req, err := cfg.Store.Add(in.Email, in.Company, in.IntendedUse, ip)
		if err != nil {
			// Persistence failure is a server-side bug or a disk
			// problem; surface 500 and log details internally. Do NOT
			// echo err.Error() to the caller; leaks store path.
			cfg.Logger.Error("key request: store add failed",
				slog.String("error", err.Error()),
				slog.String("ip", ip),
			)
			writeKeyRequestError(w, cfg.Logger, http.StatusInternalServerError,
				"internal error")
			return
		}

		// Redact the caller email: this is a public unauthenticated path, and
		// request_id already links to the full stored record for follow-up. Only
		// a masked form (first char + domain) reaches the log sink (LG-3).
		cfg.Logger.Info("key request: accepted",
			slog.String("request_id", req.ID),
			logging.SafeEmail("email", req.Email),
			slog.String("ip", ip),
		)
		writeKeyRequestJSON(w, cfg.Logger, http.StatusAccepted, KeyRequestResponse{
			RequestID: req.ID,
			Status:    req.Status,
		})
	})
}

// validateKeyRequest enforces the Phase 11 RD1 schema. Returns the
// caller-visible error message; empty string means valid. The strings
// are returned (not logged) so callers can fix their request without
// us paying the structured-log cost on every failure.
func validateKeyRequest(in KeyRequestInput) string {
	switch {
	case strings.TrimSpace(in.Email) == "":
		return "email is required"
	case len(in.Email) > keyRequestMaxEmailLen:
		return "email too long"
	}
	if _, err := mail.ParseAddress(in.Email); err != nil {
		return "email is not a valid address"
	}
	if len(in.Company) > keyRequestMaxCompanyLen {
		return "company too long"
	}
	if hasDisallowedControlChars(in.Company) {
		return "company contains disallowed control characters"
	}
	if strings.TrimSpace(in.IntendedUse) == "" {
		return "intended_use is required"
	}
	if len(in.IntendedUse) > keyRequestMaxIntendedUseLen {
		return "intended_use too long"
	}
	if hasDisallowedControlChars(in.IntendedUse) {
		return "intended_use contains disallowed control characters"
	}
	return ""
}

// hasDisallowedControlChars reports whether s contains a disallowed
// character. F3: the key-request free-text fields are an unauthenticated
// write, later stored and rendered to the reviewing operator, so we reject
// the spoofing/injection primitives while still allowing ordinary
// multi-line prose (tab and newline pass):
//   - Unicode Cc control chars: NUL, carriage-return (CRLF injection),
//     terminal escape sequences.
//   - Unicode Cf format/bidi controls: U+202E RLO and the Trojan-Source
//     bidi-override class, zero-width spaces, BOM -- invisible glyphs that
//     let untrusted text misrepresent itself to a human reviewer.
//
// Email is separately gated by mail.ParseAddress.
func hasDisallowedControlChars(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' {
			continue
		}
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return true
		}
	}
	return false
}

func writeKeyRequestJSON(w http.ResponseWriter, logger *slog.Logger, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		logger.Warn("key request: encode response", slog.String("error", err.Error()))
	}
}

func writeKeyRequestError(w http.ResponseWriter, logger *slog.Logger, status int, msg string) {
	writeKeyRequestJSON(w, logger, status, map[string]string{"error": msg})
}
