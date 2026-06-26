// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestManagedKeyStore_SatisfiesBackend(_ *testing.T) {
	var _ KeyStoreBackend = (*ManagedKeyStore)(nil) // compile-time assertion
}

func TestKeyLookupAdapter_AcceptsBackend(t *testing.T) {
	// The adapter must accept any KeyStoreBackend, not just the concrete store.
	m := NewManagedKeyStoreFromEntries("", []KeyEntry{NewKeyEntry("c1", "secret", []string{"reader"})})
	var be KeyStoreBackend = m
	a := NewKeyLookupAdapter(be)
	if _, r := a.Lookup(context.Background(), "secret"); r != auth.RejectNone {
		t.Fatal("adapter over KeyStoreBackend failed to look up a known key")
	}
}
