// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// fakeValidator is a minimal auth.TokenValidator double. With err set,
// Validate always fails; otherwise it returns claims (default non-nil).
type fakeValidator struct {
	claims *auth.Claims
	err    error
}

func (f fakeValidator) Validate(_ string) (*auth.Claims, error) {
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

	handler := AuthMiddleware(next, fakeValidator{}, nil, true, discardLogger())
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

	handler := AuthMiddleware(next, fakeValidator{err: auth.ErrInvalidAPIKey}, nil, true, discardLogger())
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

	handler := AuthMiddleware(next, fakeValidator{}, nil, false, discardLogger())
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
	handler := AuthMiddleware(next, fakeValidator{}, nil, true, discardLogger())
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

	handler := AuthMiddleware(next, fakeValidator{claims: &auth.Claims{ClientID: "alice"}}, nil, true, discardLogger())
	req := httptest.NewRequest(http.MethodPost, "/mcp", http.NoBody)
	req.Header.Set("Authorization", "Bearer good")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK || gotClientID != "alice" {
		t.Errorf("keyless valid-token: code=%d clientID=%q, want 200/alice", w.Code, gotClientID)
	}
}
