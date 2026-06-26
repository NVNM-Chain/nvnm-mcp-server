// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// fakeValidator is a minimal auth.TokenValidator double. With err set,
// Validate always fails; otherwise it returns claims (default non-nil).
type fakeValidator struct {
	claims *auth.Claims
	err    error
}

func (f fakeValidator) Validate(_ context.Context, _ string) (*auth.Claims, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.claims != nil {
		return f.claims, nil
	}
	return &auth.Claims{ClientID: "test-client"}, nil
}

func (f fakeValidator) Close() error { return nil }

func TestAuthMiddleware_KeylessAllowsMissingHeader(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if auth.ClaimsFromContext(r.Context()) != nil {
			t.Error("anonymous request must carry no claims")
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := AuthMiddleware(next, fakeValidator{}, nil, true, discardLogger(), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody) // no Authorization
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK || !called {
		t.Errorf("keyless missing-header: code=%d called=%v, want 200/true", w.Code, called)
	}
}

func TestAuthMiddleware_KeylessStillRejectsBadToken(t *testing.T) {
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not be reached with a bad token")
	})

	handler := AuthMiddleware(next, fakeValidator{err: auth.ErrInvalidAPIKey}, nil, true, discardLogger(), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.Header.Set("Authorization", "Bearer garbage")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("keyless bad-token: got %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_FlagOffRejectsMissingHeader(t *testing.T) {
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not be reached when flag off and no header")
	})

	handler := AuthMiddleware(next, fakeValidator{}, nil, false, discardLogger(), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("flag-off missing-header: got %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_KeylessStillRejectsWrongScheme(t *testing.T) {
	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("handler must not be reached with wrong scheme")
	})
	handler := AuthMiddleware(next, fakeValidator{}, nil, true, discardLogger(), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("keyless wrong-scheme: got %d, want 401", w.Code)
	}
}

func TestAuthMiddleware_KeylessAuthenticatesValidToken(t *testing.T) {
	var gotClientID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClientID = auth.ClientIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	handler := AuthMiddleware(next, fakeValidator{claims: &auth.Claims{ClientID: "alice"}}, nil, true, discardLogger(), "")
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK || gotClientID != "alice" {
		t.Errorf("keyless valid-token: code=%d clientID=%q, want 200/alice", w.Code, gotClientID)
	}
}

// TestAuthMiddleware_Sets_WWWAuthenticate_On401 asserts every 401 carries a
// plain "WWW-Authenticate: Bearer" challenge (RFC 6750 / 7235). A bare 401
// with no challenge is the MCP-authorization-spec gap that leaves Claude-class
// clients unable to determine the scheme. The challenge is plain Bearer with
// NO resource_metadata parameter: this server authenticates opaque API keys /
// FusionAuth JWTs supplied out-of-band, not an OAuth discovery flow, so it must
// not point clients at OAuth metadata they cannot use.
func TestAuthMiddleware_Sets_WWWAuthenticate_On401(t *testing.T) {
	tests := []struct {
		name      string
		authValue string // "" with setHeader=false means do not set the header
		setHeader bool
		validator auth.TokenValidator
	}{
		{
			name:      "missing Authorization header",
			setHeader: false,
			validator: fakeValidator{},
		},
		{
			name:      "non-Bearer scheme",
			authValue: "Basic dXNlcjpwYXNz",
			setHeader: true,
			validator: fakeValidator{},
		},
		{
			name:      "invalid credentials",
			authValue: "Bearer garbage",
			setHeader: true,
			validator: fakeValidator{err: auth.ErrInvalidAPIKey},
		},
	}

	const want = "Bearer"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("handler must not be reached on a 401 path")
			})
			// keylessReads=false so a missing header is a 401, not anonymous.
			handler := AuthMiddleware(next, tt.validator, nil, false, discardLogger(), "")
			req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
			if tt.setHeader {
				req.Header.Set("Authorization", tt.authValue)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status: got %d, want 401", w.Code)
			}
			if got := w.Header().Get("WWW-Authenticate"); got != want {
				t.Errorf("WWW-Authenticate: got %q, want %q", got, want)
			}
		})
	}
}

func TestAuthMiddleware_RejectMessages(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		renewalURL string
		wantMsg    string
	}{
		{"invalid_key", auth.ErrInvalidAPIKey, "", "invalid API key"},
		{"expired_no_url", auth.ErrKeyExpired, "", "key expired"},
		{"expired_with_url", auth.ErrKeyExpired, "https://example.com/renew", "key expired — renew at https://example.com/renew"},
		{"revoked", auth.ErrKeyRevoked, "", "key revoked"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
				t.Error("handler must not be reached")
			})
			handler := AuthMiddleware(next, fakeValidator{err: c.err}, nil, false, discardLogger(), c.renewalURL)
			req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
			req.Header.Set("Authorization", "Bearer token")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if !strings.Contains(w.Body.String(), c.wantMsg) {
				t.Errorf("body %q does not contain %q", w.Body.String(), c.wantMsg)
			}
		})
	}
}
