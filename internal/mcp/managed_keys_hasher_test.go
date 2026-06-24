// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"path/filepath"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestManagedKeyStore_Create_UsesHasher_AndLooksUp(t *testing.T) {
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	path := filepath.Join(t.TempDir(), "keys.json")

	m := NewManagedKeyStoreFromEntriesWithHasher(path, nil, h)
	res, err := m.Create("client-1", []string{"writer"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The freshly minted raw key must authenticate against the same store.
	got := m.Lookup(res.Key)
	if got == nil || got.ID != "client-1" {
		t.Fatalf("Lookup of a freshly created v1 key failed: got %+v", got)
	}
	if got.HashVersion != 1 {
		t.Fatalf("created key HashVersion = %d, want 1 under an active pepper", got.HashVersion)
	}
}

func TestNewManagedKeyStoreFromEntries_LegacyDelegatesToV0(t *testing.T) {
	// The legacy constructor must keep behaving as a no-pepper store:
	// a v0 entry resolves, exactly as before this phase.
	e := NewKeyEntry("client-1", "raw-secret", nil) // v0
	m := NewManagedKeyStoreFromEntries("", []KeyEntry{e})
	if got := m.Lookup("raw-secret"); got == nil || got.ID != "client-1" {
		t.Fatalf("legacy ManagedKeyStore regressed: got %+v", got)
	}
}
