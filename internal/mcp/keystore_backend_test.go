// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "testing"

func TestManagedKeyStore_SatisfiesBackend(_ *testing.T) {
	var _ KeyStoreBackend = (*ManagedKeyStore)(nil) // compile-time assertion
}

func TestKeyLookupAdapter_AcceptsBackend(t *testing.T) {
	// The adapter must accept any KeyStoreBackend, not just the concrete store.
	m := NewManagedKeyStoreFromEntries("", []KeyEntry{NewKeyEntry("c1", "secret", []string{"reader"})})
	var be KeyStoreBackend = m
	a := NewKeyLookupAdapter(be)
	if a.Lookup("secret") == nil {
		t.Fatal("adapter over KeyStoreBackend failed to look up a known key")
	}
}
