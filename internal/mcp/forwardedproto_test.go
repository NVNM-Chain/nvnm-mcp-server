// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireForwardedHTTPS(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	tests := []struct {
		name       string
		trustProxy bool
		setHeader  bool
		xfp        string
		wantStatus int
	}{
		{"trust off passes through even with http", false, true, "http", http.StatusOK},
		{"https allowed", true, true, "https", http.StatusOK},
		{"mixed case https allowed", true, true, "HTTPS", http.StatusOK},
		{"http rejected", true, true, "http", http.StatusForbidden},
		{"list leftmost http rejected", true, true, "http, https", http.StatusForbidden},
		{"list leftmost https allowed", true, true, "https, http", http.StatusOK},
		{"absent header allowed (lenient, see spec 6)", true, false, "", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := requireForwardedHTTPS(next, tt.trustProxy, logger)
			r := httptest.NewRequest(http.MethodPost, "/", http.NoBody)
			if tt.setHeader {
				r.Header.Set("X-Forwarded-Proto", tt.xfp)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}
