// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	mcpkeys "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
)

// setKeysFileEnv points MCP_API_KEYS_FILE at a fresh temp path and
// returns it, so run() operates on an isolated keys file.
func setKeysFileEnv(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys.json")
	t.Setenv("MCP_API_KEYS_FILE", path)
	return path
}

// TestRun_NoArgs verifies that run with no arguments prints usage and
// returns errUsage.
func TestRun_NoArgs(t *testing.T) {
	if err := run(nil); !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got: %v", err)
	}
}

// TestRun_UnknownCommand verifies an unrecognized subcommand returns errUsage.
func TestRun_UnknownCommand(t *testing.T) {
	if err := run([]string{"frobnicate"}); !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got: %v", err)
	}
}

// TestRun_CreateUsage verifies "create" with no client-id returns errUsage.
func TestRun_CreateUsage(t *testing.T) {
	setKeysFileEnv(t)
	if err := run([]string{"create"}); !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got: %v", err)
	}
}

// TestRun_Lifecycle exercises the full create → list → disable → enable →
// set-roles → renew flow through the top-level run dispatcher, asserting
// the on-disk state after each mutation.
func TestRun_Lifecycle(t *testing.T) {
	path := setKeysFileEnv(t)

	if err := run([]string{"create", "client-a", "--roles", "reader"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := run([]string{"list"}); err != nil {
		t.Fatalf("list: %v", err)
	}

	if err := run([]string{"disable", "client-a"}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	entries, err := mcpkeys.LoadKeysFile(path)
	if err != nil {
		t.Fatalf("load after disable: %v", err)
	}
	if len(entries) != 1 || entries[0].Enabled {
		t.Fatalf("expected 1 disabled entry, got %+v", entries)
	}

	if err := run([]string{"enable", "client-a"}); err != nil {
		t.Fatalf("enable: %v", err)
	}
	entries, err = mcpkeys.LoadKeysFile(path)
	if err != nil {
		t.Fatalf("load after enable: %v", err)
	}
	if !entries[0].Enabled {
		t.Fatal("expected entry to be re-enabled")
	}

	if err := run([]string{"set-roles", "client-a", "writer,admin"}); err != nil {
		t.Fatalf("set-roles: %v", err)
	}
	entries, err = mcpkeys.LoadKeysFile(path)
	if err != nil {
		t.Fatalf("load after set-roles: %v", err)
	}
	got := entries[0].Roles
	if len(got) != 2 || got[0] != "writer" || got[1] != "admin" {
		t.Fatalf("roles = %v, want [writer admin]", got)
	}

	// Clearing roles with an explicit empty string is allowed.
	if err := run([]string{"set-roles", "client-a", ""}); err != nil {
		t.Fatalf("set-roles clear: %v", err)
	}
	entries, err = mcpkeys.LoadKeysFile(path)
	if err != nil {
		t.Fatalf("load after clear: %v", err)
	}
	if len(entries[0].Roles) != 0 {
		t.Fatalf("expected roles cleared, got %v", entries[0].Roles)
	}

	if err := run([]string{"renew", "client-a", "--ttl", "24h"}); err != nil {
		t.Fatalf("renew: %v", err)
	}

	// Final list exercises the enabled + no-roles rendering branches too.
	if err := run([]string{"list"}); err != nil {
		t.Fatalf("final list: %v", err)
	}
}

// TestRunSetEnabled_Usage verifies disable/enable without a client-id
// return errUsage (both verbs, to cover the verb-selection branch).
func TestRunSetEnabled_Usage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := runSetEnabled(path, nil, false); !errors.Is(err, errUsage) {
		t.Fatalf("disable: expected errUsage, got: %v", err)
	}
	if err := runSetEnabled(path, nil, true); !errors.Is(err, errUsage) {
		t.Fatalf("enable: expected errUsage, got: %v", err)
	}
}

// TestRunSetRoles_Usage verifies set-roles with fewer than two args
// returns errUsage.
func TestRunSetRoles_Usage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := runSetRoles(path, []string{"client-only"}); !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got: %v", err)
	}
}

// TestRunSetRoles_InvalidRole verifies an invalid role name is rejected
// before any file mutation.
func TestRunSetRoles_InvalidRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	err := runSetRoles(path, []string{"client-a", "superuser"})
	if !errors.Is(err, errInvalidRole) {
		t.Fatalf("expected errInvalidRole, got: %v", err)
	}
}

// TestSetRoles_MissingClient verifies set-roles on an unknown client
// returns errClientMissing.
func TestSetRoles_MissingClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := runCreate(path, []string{"existing", "--roles", "reader"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := setRoles(path, "ghost", []string{"reader"})
	if !errors.Is(err, errClientMissing) {
		t.Fatalf("expected errClientMissing, got: %v", err)
	}
}

// TestSetEnabled_MissingClient verifies disable on an unknown client
// returns errClientMissing.
func TestSetEnabled_MissingClient(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := runCreate(path, []string{"existing", "--roles", "reader"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := setEnabled(path, "ghost", false)
	if !errors.Is(err, errClientMissing) {
		t.Fatalf("expected errClientMissing, got: %v", err)
	}
}

// TestListKeys_EmptyStore verifies listing a nonexistent keys file
// reports "no keys" without error.
func TestListKeys_EmptyStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := listKeys(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestListKeys_LoadError verifies a keys path that cannot be read (a
// directory) propagates the load error.
func TestListKeys_LoadError(t *testing.T) {
	if err := listKeys(t.TempDir()); err == nil {
		t.Fatal("expected error listing a directory path, got nil")
	}
}

// TestListKeys_LegacyRawKeyPrefix covers the pre-8.6 fallback: an entry
// with a raw Key but no KeyPrefix renders a truncated best-effort prefix.
// Also covers the disabled-status and "(none)" roles rendering branches.
func TestListKeys_LegacyRawKeyPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	entries := []mcpkeys.KeyEntry{
		{
			ID:        "legacy-long",
			Key:       "0123456789abcdef-raw-key",
			Enabled:   false,
			CreatedAt: time.Now(),
		},
		{
			ID:        "legacy-short",
			Key:       "short",
			Enabled:   true,
			CreatedAt: time.Now(),
		},
	}
	if err := mcpkeys.SaveKeysFile(path, entries); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := listKeys(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestCreateKey_DuplicateClient verifies creating a key for an existing
// client id is rejected with errClientExists.
func TestCreateKey_DuplicateClient(t *testing.T) {
	path := setKeysFileEnv(t)
	if err := run([]string{"create", "dup", "--roles", "reader"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := run([]string{"create", "dup", "--roles", "reader"})
	if !errors.Is(err, errClientExists) {
		t.Fatalf("expected errClientExists, got: %v", err)
	}
	entries, loadErr := mcpkeys.LoadKeysFile(path)
	if loadErr != nil {
		t.Fatalf("load: %v", loadErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after rejected duplicate, got %d", len(entries))
	}
}

// TestCreateKey_PepperPreviousWithoutActive verifies that setting only
// KEY_HMAC_PEPPER_PREVIOUS (no active pepper) fails loud.
func TestCreateKey_PepperPreviousWithoutActive(t *testing.T) {
	setKeysFileEnv(t)
	t.Setenv("KEY_HMAC_PEPPER", "")
	t.Setenv("KEY_HMAC_PEPPER_PREVIOUS", "old-pepper")
	err := run([]string{"create", "peppered", "--roles", "reader"})
	if !errors.Is(err, config.ErrPepperPreviousWithoutActive) {
		t.Fatalf("expected ErrPepperPreviousWithoutActive, got: %v", err)
	}
}

// TestRunCreate_InvalidDefaultTTL verifies an unparseable KEY_DEFAULT_TTL
// fails key creation.
func TestRunCreate_InvalidDefaultTTL(t *testing.T) {
	path := setKeysFileEnv(t)
	t.Setenv("KEY_DEFAULT_TTL", "not-a-duration")
	if err := runCreate(path, []string{"client-x", "--roles", "reader"}); err == nil {
		t.Fatal("expected error for invalid KEY_DEFAULT_TTL, got nil")
	}
}

// TestRunRenew_Usage verifies renew with no arguments returns errUsage.
func TestRunRenew_Usage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if err := runRenew(path, nil); !errors.Is(err, errUsage) {
		t.Fatalf("expected errUsage, got: %v", err)
	}
}
