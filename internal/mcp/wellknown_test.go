// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWellKnownGuard_OAuthDiscoveryPaths404 asserts that the two OAuth
// discovery paths return 404 (signaling "no OAuth here, use configured
// credentials") and never reach the wrapped handler, while any other
// path passes through untouched. Returning 401 on these paths is the
// bug that makes Claude-class clients report "Needs authentication"
// instead of using a configured static Bearer token.
func TestWellKnownGuard_OAuthDiscoveryPaths404(t *testing.T) {
	tests := []struct {
		name           string
		method         string
		path           string
		wantStatus     int
		wantNextCalled bool
	}{
		{
			name:           "protected-resource metadata is 404",
			method:         http.MethodGet,
			path:           "/.well-known/oauth-protected-resource",
			wantStatus:     http.StatusNotFound,
			wantNextCalled: false,
		},
		{
			name:           "authorization-server metadata is 404",
			method:         http.MethodGet,
			path:           "/.well-known/oauth-authorization-server",
			wantStatus:     http.StatusNotFound,
			wantNextCalled: false,
		},
		{
			name:           "404 regardless of method",
			method:         http.MethodPost,
			path:           "/.well-known/oauth-protected-resource",
			wantStatus:     http.StatusNotFound,
			wantNextCalled: false,
		},
		{
			name:           "MCP endpoint passes through to next",
			method:         http.MethodPost,
			path:           "/",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
		},
		{
			name:           "unrelated well-known path passes through",
			method:         http.MethodGet,
			path:           "/.well-known/something-else",
			wantStatus:     http.StatusOK,
			wantNextCalled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			handler := wellKnownGuard(next)
			req := httptest.NewRequest(tt.method, tt.path, http.NoBody)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status: got %d, want %d", w.Code, tt.wantStatus)
			}
			if nextCalled != tt.wantNextCalled {
				t.Errorf("next called: got %v, want %v", nextCalled, tt.wantNextCalled)
			}
		})
	}
}
