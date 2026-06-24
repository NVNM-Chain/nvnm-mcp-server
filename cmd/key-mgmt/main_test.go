// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
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
