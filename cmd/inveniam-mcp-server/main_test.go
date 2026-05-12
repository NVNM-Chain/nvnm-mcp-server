package main

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestLoadAPIKeys_HTTPFailsClosedWithoutAuth verifies that the HTTP
// transport refuses to start when neither MCP_API_KEYS_FILE nor
// MCP_API_KEY is set. The pre-fix behavior was a WARN log and a
// silently-unauthenticated listener; the new behavior is a hard error.
func TestLoadAPIKeys_HTTPFailsClosedWithoutAuth(t *testing.T) {
	cfg := &config.Config{
		Transport:    "http",
		AuthProvider: "apikey",
		// neither APIKey nor APIKeysFile set
	}
	_, _, _, err := loadAPIKeys(cfg, discardLogger())
	if err == nil {
		t.Fatal("loadAPIKeys returned nil error; expected ErrHTTPAuthRequired")
	}
	if !errors.Is(err, config.ErrHTTPAuthRequired) {
		t.Errorf("error = %v, want ErrHTTPAuthRequired", err)
	}
}

// TestLoadAPIKeys_StdioAllowsNoAuth verifies the stdio transport
// continues to allow unauthenticated operation -- stdio is a local
// pipe, the transport itself is the trust boundary.
func TestLoadAPIKeys_StdioAllowsNoAuth(t *testing.T) {
	cfg := &config.Config{
		Transport:    "stdio",
		AuthProvider: "apikey",
	}
	v, mks, cleanup, err := loadAPIKeys(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != nil {
		t.Errorf("validator should be nil for stdio without keys, got %T", v)
	}
	if mks != nil {
		t.Error("managed key store should be nil when no keys configured")
	}
	if cleanup != nil {
		t.Error("cleanup should be nil when no validator was created")
	}
}

// TestLoadAPIKeys_SingleKeyHappyPath confirms the legacy single-key
// path still works after the fail-closed change.
func TestLoadAPIKeys_SingleKeyHappyPath(t *testing.T) {
	cfg := &config.Config{
		Transport:    "http",
		AuthProvider: "apikey",
		APIKey:       "test-key-32bytes-or-whatever-base64url",
	}
	v, mks, _, err := loadAPIKeys(cfg, discardLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v == nil {
		t.Error("validator should be set when MCP_API_KEY is configured")
	}
	if mks == nil {
		t.Error("managed key store should be set even for single-key mode")
	}
}
