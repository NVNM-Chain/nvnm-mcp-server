// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package main

import (
	"crypto/sha256"
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
