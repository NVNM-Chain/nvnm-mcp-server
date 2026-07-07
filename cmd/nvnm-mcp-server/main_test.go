// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestLoadWriteAudit_ProvisioningByMode covers F1: the write-audit store is
// provisioned whenever MCP_KEYLESS_PG_DSN is set -- including authed /
// self-host mode (keyless writes OFF) -- so authed broadcasts are auditable.
// The per-signer quota + blacklist gates stay keyless-only.
func TestLoadWriteAudit_ProvisioningByMode(t *testing.T) {
	dsn := os.Getenv("NVNM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("NVNM_TEST_PG_DSN not set; skipping Postgres-backed test")
	}

	// No DSN: nothing is provisioned, in either mode.
	a, q, b, cleanup, err := loadWriteAudit(&config.Config{KeylessPGDSN: ""}, discardLogger())
	if err != nil {
		t.Fatalf("loadWriteAudit (no dsn): %v", err)
	}
	cleanup()
	if a != nil || q != nil || b != nil {
		t.Error("no DSN must provision nothing")
	}

	// Authed mode (keyless writes off) + DSN: audit store provisioned so
	// authed broadcasts persist (F1); the keyless gates stay nil.
	a, q, b, cleanup, err = loadWriteAudit(
		&config.Config{KeylessWrites: false, KeylessPGDSN: dsn}, discardLogger())
	if err != nil {
		t.Fatalf("loadWriteAudit (authed): %v", err)
	}
	defer cleanup()
	if a == nil {
		t.Error("F1: authed mode with a DSN must provision the write-audit store, got nil")
	}
	if q != nil || b != nil {
		t.Error("authed mode must NOT provision the keyless quota/blacklist gates")
	}

	// Keyless mode + DSN: audit + quota + blacklist all provisioned.
	a2, q2, b2, cleanup2, err := loadWriteAudit(
		&config.Config{KeylessWrites: true, KeylessPGDSN: dsn}, discardLogger())
	if err != nil {
		t.Fatalf("loadWriteAudit (keyless): %v", err)
	}
	defer cleanup2()
	if a2 == nil || q2 == nil || b2 == nil {
		t.Error("keyless mode with a DSN must provision audit + quota + blacklist")
	}
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

func TestLoadAPIKeys_StaticKeyCarriesRoles(t *testing.T) {
	cfg := &config.Config{
		Transport:    "http",
		AuthProvider: "apikey",
		APIKey:       "static-secret",
		APIKeyRoles:  []string{"reader", "writer"},
	}
	logger := slog.New(slog.DiscardHandler)
	_, managed, _, err := loadAPIKeys(cfg, logger)
	if err != nil {
		t.Fatalf("loadAPIKeys: %v", err)
	}
	summaries := managed.List()
	if len(summaries) != 1 {
		t.Fatalf("want 1 key, got %d", len(summaries))
	}
	got := summaries[0].Roles
	if len(got) != 2 || got[0] != "reader" || got[1] != "writer" {
		t.Fatalf("static key roles = %v, want [reader writer]", got)
	}
}
