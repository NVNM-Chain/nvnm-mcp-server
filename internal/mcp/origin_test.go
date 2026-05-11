package mcp

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOriginAllowlist_Allowed(t *testing.T) {
	a := NewOriginAllowlist([]string{
		"https://claude.ai",
		"https://mcp.nvnmchain.io",
		"http://localhost",
		"http://127.0.0.1",
	})

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		// Empty Origin: no header on the request -> always allowed
		// (server-to-server, CLI, curl).
		{"empty header passes", "", true},

		// Exact matches.
		{"exact claude.ai", "https://claude.ai", true},
		{"exact mcp.nvnmchain.io", "https://mcp.nvnmchain.io", true},
		{"exact localhost http", "http://localhost", true},

		// Case-insensitive: clients are supposed to lowercase Origin
		// per RFC 6454, but defensive normalization costs nothing.
		{"upper-case claude.ai", "HTTPS://CLAUDE.AI", true},
		{"mixed-case localhost", "HTTP://LocalHost", true},

		// Localhost variants accept any port when the bare host is
		// allowed.
		{"localhost any port", "http://localhost:8080", true},
		{"127.0.0.1 any port", "http://127.0.0.1:31337", true},

		// HTTPS localhost is in the default allowlist (covered in a
		// separate test); this allowlist only had http://localhost,
		// so HTTPS variants should NOT match.
		{"https localhost not in this list", "https://localhost", false},

		// Rejection cases.
		{"unrelated origin", "https://attacker.example.com", false},
		{"subdomain attack on claude", "https://attacker.claude.ai", false},
		{"prefix attack on localhost", "http://localhost.attacker.tld", false},
		{"trailing slash mismatch", "https://claude.ai/", false},

		// Whitespace handling.
		{"whitespace around origin", "  https://claude.ai  ", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.Allowed(tc.origin); got != tc.want {
				t.Errorf("Allowed(%q) = %v, want %v", tc.origin, got, tc.want)
			}
		})
	}
}

func TestDefaultOriginAllowlist_LocalhostVariants(t *testing.T) {
	a := DefaultOriginAllowlist()

	// Default must include BOTH http:// and https:// for all three
	// loopback hosts. Q1 fix from the PR #20 review surfaced that
	// some defaults only included http:// and locked out self-hosted
	// TLS clients.
	mustAllow := []string{
		"http://localhost",
		"https://localhost",
		"http://localhost:3000",
		"https://localhost:3000",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"http://127.0.0.1:9000",
		"https://127.0.0.1:9000",
		"http://[::1]",
		"https://[::1]",
		"http://[::1]:8080",
		"https://[::1]:8080",
	}
	for _, o := range mustAllow {
		if !a.Allowed(o) {
			t.Errorf("default allowlist rejected %q -- expected to pass", o)
		}
	}

	mustReject := []string{
		"https://claude.ai",
		"https://mcp.nvnmchain.io",
		"http://localhost.attacker.tld",
	}
	for _, o := range mustReject {
		if a.Allowed(o) {
			t.Errorf("default allowlist accepted %q -- expected to reject", o)
		}
	}
}

func TestOriginAllowlist_IgnoresEmptyAndWhitespaceEntries(t *testing.T) {
	a := NewOriginAllowlist([]string{
		"",
		"   ",
		"\t",
		"https://claude.ai",
	})
	if !a.Allowed("https://claude.ai") {
		t.Error("real entry should still be in the allowlist")
	}
	// Make sure the empty-string Origin still passes via the
	// no-header path, not via a stale empty-string entry.
	if !a.Allowed("") {
		t.Error("empty Origin (no header) must always pass")
	}
}

func TestOriginAllowlist_Resolved_Sorted(t *testing.T) {
	a := NewOriginAllowlist([]string{
		"https://mcp.nvnmchain.io",
		"http://localhost",
		"https://claude.ai",
	})
	resolved := a.Resolved()
	want := []string{"http://localhost", "https://claude.ai", "https://mcp.nvnmchain.io"}
	if len(resolved) != len(want) {
		t.Fatalf("resolved len = %d, want %d (%v)", len(resolved), len(want), resolved)
	}
	for i, v := range want {
		if resolved[i] != v {
			t.Errorf("resolved[%d] = %q, want %q (full: %v)", i, resolved[i], v, resolved)
		}
	}
}

func TestOriginGuard_AllowedOriginPassesThrough(t *testing.T) {
	allow := NewOriginAllowlist([]string{"https://claude.ai"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	guard := originGuard(next, allow, logger)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
	req.Header.Set("Origin", "https://claude.ai")
	rec := httptest.NewRecorder()

	guard.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler was not invoked despite allowed Origin")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestOriginGuard_NoOriginHeaderPassesThrough(t *testing.T) {
	allow := NewOriginAllowlist([]string{"https://claude.ai"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	guard := originGuard(next, allow, logger)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
	// Deliberately no Origin header.
	rec := httptest.NewRecorder()

	guard.ServeHTTP(rec, req)

	if !called {
		t.Error("inner handler must run when Origin header is absent")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestOriginGuard_DisallowedOriginRejected(t *testing.T) {
	allow := NewOriginAllowlist([]string{"https://claude.ai"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	guard := originGuard(next, allow, logger)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(""))
	req.Header.Set("Origin", "https://attacker.example.com")
	rec := httptest.NewRecorder()

	guard.ServeHTTP(rec, req)

	if called {
		t.Error("inner handler ran despite disallowed Origin")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestOriginGuard_OPTIONSUniformlyGuarded(t *testing.T) {
	// CORS preflight handling is Phase 9. Until then, OPTIONS is
	// guarded identically to other methods -- an OPTIONS request
	// with a disallowed Origin gets 403, not a CORS-style 204.
	allow := NewOriginAllowlist([]string{"https://claude.ai"})
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	guard := originGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), allow, logger)

	req := httptest.NewRequest(http.MethodOptions, "/mcp", strings.NewReader(""))
	req.Header.Set("Origin", "https://attacker.example.com")
	rec := httptest.NewRecorder()

	guard.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("OPTIONS status = %d, want 403", rec.Code)
	}
}
