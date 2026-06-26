// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcpkeys "github.com/NVNM-Chain/nvnm-mcp-server/internal/mcp"
)

// TestParseRoles_ValidRole verifies a valid role parses without error.
func TestParseRoles_ValidRole(t *testing.T) {
	roles, err := parseRoles("reader")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 1 || roles[0] != "reader" {
		t.Fatalf("expected [reader], got %v", roles)
	}
}

// TestParseRoles_MultipleValidRoles verifies comma-separated valid roles parse correctly.
func TestParseRoles_MultipleValidRoles(t *testing.T) {
	roles, err := parseRoles("reader,writer,admin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roles) != 3 {
		t.Fatalf("expected 3 roles, got %v", roles)
	}
}

// TestParseRoles_InvalidRole verifies an unrecognized role returns an error.
func TestParseRoles_InvalidRole(t *testing.T) {
	_, err := parseRoles("superuser")
	if err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
}

// TestParseRoles_Empty verifies an empty string returns nil roles (no error).
// set-roles "" is allowed; runCreate rejects nil roles separately.
func TestParseRoles_Empty(t *testing.T) {
	roles, err := parseRoles("")
	if err != nil {
		t.Fatalf("unexpected error for empty string: %v", err)
	}
	if roles != nil {
		t.Fatalf("expected nil roles for empty string, got %v", roles)
	}
}

// TestRunCreate_NoRoles verifies that creating a key with no roles is rejected.
func TestRunCreate_NoRoles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	// runCreate with no --roles flag → parseRoles("") → nil → should error.
	err := runCreate(path, []string{"test-client"})
	if !errors.Is(err, errNoRoles) {
		t.Fatalf("expected errNoRoles, got: %v", err)
	}

	// Key file must not have been created (or must be empty).
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("keys file should not be created when create is rejected")
	}
}

// TestRunCreate_WithRole verifies that creating a key with a valid role succeeds.
func TestRunCreate_WithRole(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	err := runCreate(path, []string{"test-client", "--roles", "reader"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("keys file should have been created: %v", statErr)
	}
}

// --- resolveCLIExpiry tests ---

// TestResolveCLIExpiry_FlagAbsent verifies that omitting --ttl applies the
// default TTL from the defaultTTL argument.
func TestResolveCLIExpiry_FlagAbsent(t *testing.T) {
	now := time.Now()
	defaultTTL := 8760 * time.Hour

	got, err := resolveCLIExpiry("", defaultTTL, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.Add(defaultTTL)
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Second {
		t.Fatalf("expected ExpiresAt ≈ now+8760h, got %v (diff %v)", got, diff)
	}
}

// TestResolveCLIExpiry_Zero verifies that "0" produces a zero time (no expiry).
func TestResolveCLIExpiry_Zero(t *testing.T) {
	now := time.Now()
	got, err := resolveCLIExpiry("0", 8760*time.Hour, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time for ttl=0, got %v", got)
	}
}

// TestResolveCLIExpiry_None verifies that "none" produces a zero time (no expiry).
func TestResolveCLIExpiry_None(t *testing.T) {
	now := time.Now()
	got, err := resolveCLIExpiry("none", 8760*time.Hour, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time for ttl=none, got %v", got)
	}
}

// TestResolveCLIExpiry_Never verifies that "never" produces a zero time (no expiry).
func TestResolveCLIExpiry_Never(t *testing.T) {
	now := time.Now()
	got, err := resolveCLIExpiry("never", 8760*time.Hour, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time for ttl=never, got %v", got)
	}
}

// TestResolveCLIExpiry_ValidDuration verifies a valid duration resolves to now+dur.
func TestResolveCLIExpiry_ValidDuration(t *testing.T) {
	now := time.Now()
	got, err := resolveCLIExpiry("24h", 8760*time.Hour, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := now.Add(24 * time.Hour)
	diff := got.Sub(want)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Second {
		t.Fatalf("expected ExpiresAt ≈ now+24h, got %v (diff %v)", got, diff)
	}
}

// TestResolveCLIExpiry_InvalidDuration verifies a non-parseable duration returns an error.
func TestResolveCLIExpiry_InvalidDuration(t *testing.T) {
	now := time.Now()
	_, err := resolveCLIExpiry("not-a-duration", 8760*time.Hour, now)
	if err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

// TestResolveCLIExpiry_NegativeDuration verifies that a negative parsed duration
// returns an error rather than silently producing an already-expired key.
func TestResolveCLIExpiry_NegativeDuration(t *testing.T) {
	now := time.Now()
	_, err := resolveCLIExpiry("-1h", 8760*time.Hour, now)
	if err == nil {
		t.Fatal("expected error for negative duration, got nil")
	}
}

// TestResolveCLIExpiry_ZeroDuration verifies that "0s" (parses to 0, bypasses
// the "0" string sentinel) is also rejected with an error.
func TestResolveCLIExpiry_ZeroDuration(t *testing.T) {
	now := time.Now()
	_, err := resolveCLIExpiry("0s", 8760*time.Hour, now)
	if err == nil {
		t.Fatal("expected error for zero-duration TTL, got nil")
	}
}

// TestResolveCLIExpiry_DefaultTTLZero verifies that when defaultTTL is 0 and
// ttlStr is empty, the result is zero (no expiry).
func TestResolveCLIExpiry_DefaultTTLZero(t *testing.T) {
	now := time.Now()
	got, err := resolveCLIExpiry("", 0, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got.IsZero() {
		t.Fatalf("expected zero time when defaultTTL=0 and flag absent, got %v", got)
	}
}

// --- create --ttl integration tests ---

// TestRunCreate_TTL_24h verifies that --ttl 24h sets ExpiresAt ≈ now+24h.
func TestRunCreate_TTL_24h(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	before := time.Now()

	err := runCreate(path, []string{"client-ttl", "--roles", "reader", "--ttl", "24h"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after := time.Now()
	entries, loadErr := mcpkeys.LoadKeysFile(path)
	if loadErr != nil {
		t.Fatalf("failed to load keys file: %v", loadErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ExpiresAt.IsZero() {
		t.Fatal("expected non-zero ExpiresAt for --ttl 24h")
	}
	minExpiry := before.Add(24 * time.Hour)
	maxExpiry := after.Add(24 * time.Hour)
	if e.ExpiresAt.Before(minExpiry) || e.ExpiresAt.After(maxExpiry) {
		t.Fatalf("ExpiresAt %v not in [%v, %v]", e.ExpiresAt, minExpiry, maxExpiry)
	}
}

// TestRunCreate_TTL_Zero verifies that --ttl 0 results in a zero ExpiresAt.
func TestRunCreate_TTL_Zero(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	err := runCreate(path, []string{"client-no-expiry", "--roles", "reader", "--ttl", "0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	entries, loadErr := mcpkeys.LoadKeysFile(path)
	if loadErr != nil {
		t.Fatalf("failed to load keys file: %v", loadErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt for --ttl 0, got %v", entries[0].ExpiresAt)
	}
}

// TestRunCreate_TTL_Default verifies that omitting --ttl uses the KEY_DEFAULT_TTL env
// (default 8760h when env is unset).
func TestRunCreate_TTL_Default(t *testing.T) {
	t.Setenv("KEY_DEFAULT_TTL", "8760h")
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")
	before := time.Now()

	err := runCreate(path, []string{"client-default", "--roles", "reader"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after := time.Now()
	entries, loadErr := mcpkeys.LoadKeysFile(path)
	if loadErr != nil {
		t.Fatalf("failed to load keys file: %v", loadErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ExpiresAt.IsZero() {
		t.Fatal("expected non-zero ExpiresAt for default TTL of 8760h")
	}
	minExpiry := before.Add(8760 * time.Hour)
	maxExpiry := after.Add(8760 * time.Hour)
	if e.ExpiresAt.Before(minExpiry) || e.ExpiresAt.After(maxExpiry) {
		t.Fatalf("ExpiresAt %v not in [%v, %v]", e.ExpiresAt, minExpiry, maxExpiry)
	}
}

// --- renew subcommand tests ---

// TestRunRenew_UpdatesExpiry verifies that renew updates an existing entry's ExpiresAt.
func TestRunRenew_UpdatesExpiry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	// Seed a key with no expiry.
	if err := runCreate(path, []string{"renew-client", "--roles", "reader", "--ttl", "0"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	before := time.Now()
	err := runRenew(path, []string{"renew-client", "--ttl", "48h"})
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}
	after := time.Now()

	entries, loadErr := mcpkeys.LoadKeysFile(path)
	if loadErr != nil {
		t.Fatalf("failed to load keys file: %v", loadErr)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.ExpiresAt.IsZero() {
		t.Fatal("expected non-zero ExpiresAt after renew")
	}
	minExpiry := before.Add(48 * time.Hour)
	maxExpiry := after.Add(48 * time.Hour)
	if e.ExpiresAt.Before(minExpiry) || e.ExpiresAt.After(maxExpiry) {
		t.Fatalf("ExpiresAt %v not in [%v, %v]", e.ExpiresAt, minExpiry, maxExpiry)
	}
}

// TestRunRenew_ClearsExpiry verifies that renew with --ttl 0 clears ExpiresAt.
func TestRunRenew_ClearsExpiry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	// Seed a key with a 24h TTL.
	if err := runCreate(path, []string{"renew-clear", "--roles", "reader", "--ttl", "24h"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err := runRenew(path, []string{"renew-clear", "--ttl", "0"})
	if err != nil {
		t.Fatalf("renew failed: %v", err)
	}

	entries, loadErr := mcpkeys.LoadKeysFile(path)
	if loadErr != nil {
		t.Fatalf("failed to load keys file: %v", loadErr)
	}
	if !entries[0].ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt after renew --ttl 0, got %v", entries[0].ExpiresAt)
	}
}

// TestRunRenew_MissingClient verifies that renewing a non-existent client returns an error.
func TestRunRenew_MissingClient(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	// Create one key so the file exists.
	if err := runCreate(path, []string{"existing", "--roles", "reader"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err := runRenew(path, []string{"nonexistent", "--ttl", "24h"})
	if !errors.Is(err, errClientMissing) {
		t.Fatalf("expected errClientMissing, got: %v", err)
	}
}

// TestRunRenew_MissingTTL verifies that omitting --ttl on renew returns an error
// (renew always requires an explicit TTL).
func TestRunRenew_MissingTTL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.json")

	if err := runCreate(path, []string{"existing", "--roles", "reader"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err := runRenew(path, []string{"existing"})
	if err == nil {
		t.Fatal("expected error when --ttl is missing on renew, got nil")
	}
}
