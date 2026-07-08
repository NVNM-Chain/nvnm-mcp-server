// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
)

func TestLoadAdminKeys(t *testing.T) {
	// single key only -> one "admin" entry
	m, err := loadAdminKeys(&config.Config{AdminAPIKey: "single"})
	if err != nil {
		t.Fatalf("single: %v", err)
	}
	if got := m[sha256.Sum256([]byte("single"))]; got != "admin" {
		t.Errorf("single key id = %q, want admin", got)
	}
	// file with two named admins, plus the single key
	dir := t.TempDir()
	fp := dir + "/admins.json"
	os.WriteFile(fp, []byte(`[{"id":"alice","key":"ka"},{"id":"bob","key":"kb"}]`), 0o600)
	m, err = loadAdminKeys(&config.Config{AdminAPIKey: "single", AdminAPIKeysFile: fp})
	if err != nil {
		t.Fatalf("file: %v", err)
	}
	if m[sha256.Sum256([]byte("ka"))] != "alice" || m[sha256.Sum256([]byte("kb"))] != "bob" || m[sha256.Sum256([]byte("single"))] != "admin" {
		t.Errorf("map = %v", m)
	}
	// duplicate id -> error
	os.WriteFile(fp, []byte(`[{"id":"x","key":"k1"},{"id":"x","key":"k2"}]`), 0o600)
	if _, err := loadAdminKeys(&config.Config{AdminAPIKeysFile: fp}); err == nil {
		t.Error("duplicate id must error")
	}
}

// TestLoadAdminKeys_DuplicateHash pins that two distinct admin ids sharing
// the same key hash is rejected -- a silent admin-identity collision would
// let one admin's bearer token authenticate as a different admin's id.
func TestLoadAdminKeys_DuplicateHash(t *testing.T) {
	dir := t.TempDir()
	fp := dir + "/admins.json"

	if err := os.WriteFile(fp, []byte(`[{"id":"a","key":"k"},{"id":"b","key":"k"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := loadAdminKeys(&config.Config{AdminAPIKeysFile: fp})
	if !errors.Is(err, ErrAdminKeyDuplicateHash) {
		t.Fatalf("file-file collision: err = %v, want ErrAdminKeyDuplicateHash", err)
	}

	// Same collision, but between the ADMIN_API_KEY seed ("admin") and a
	// file row with a different id -- must still be caught.
	if werr := os.WriteFile(fp, []byte(`[{"id":"b","key":"k"}]`), 0o600); werr != nil {
		t.Fatal(werr)
	}
	_, err = loadAdminKeys(&config.Config{AdminAPIKey: "k", AdminAPIKeysFile: fp})
	if !errors.Is(err, ErrAdminKeyDuplicateHash) {
		t.Fatalf("seed-file collision: err = %v, want ErrAdminKeyDuplicateHash", err)
	}
}

// TestLoadAdminKeys_EmptyFileNoSeed pins that a file-only configuration
// (ADMIN_API_KEYS_FILE set, no ADMIN_API_KEY) whose file has zero entries
// fails loudly instead of yielding a zero-entry key map that would start
// an admin server where every request 401s.
func TestLoadAdminKeys_EmptyFileNoSeed(t *testing.T) {
	dir := t.TempDir()
	fp := dir + "/admins.json"
	if err := os.WriteFile(fp, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadAdminKeys(&config.Config{AdminAPIKeysFile: fp})
	if !errors.Is(err, ErrAdminKeyNoneConfigured) {
		t.Fatalf("err = %v, want ErrAdminKeyNoneConfigured", err)
	}
}
