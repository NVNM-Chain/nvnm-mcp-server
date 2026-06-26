// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestManagedKeyStore_Create_UsesHasher_AndLooksUp(t *testing.T) {
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	path := filepath.Join(t.TempDir(), "keys.json")

	m := NewManagedKeyStoreFromEntriesWithHasher(path, nil, h)
	res, err := m.Create(context.Background(), "client-1", []string{"writer"}, time.Time{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// The freshly minted raw key must authenticate against the same store.
	got, reason := m.Lookup(context.Background(), res.Key)
	if reason != auth.RejectNone || got == nil || got.ID != "client-1" {
		t.Fatalf("Lookup of a freshly created v1 key failed: reason=%v got=%+v", reason, got)
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
	got, reason := m.Lookup(context.Background(), "raw-secret")
	if reason != auth.RejectNone || got == nil || got.ID != "client-1" {
		t.Fatalf("legacy ManagedKeyStore regressed: reason=%v got=%+v", reason, got)
	}
}
