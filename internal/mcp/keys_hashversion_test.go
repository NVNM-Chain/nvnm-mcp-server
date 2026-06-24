// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestNewKeyEntryWithHasher_SetsVersionAndHash(t *testing.T) {
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	e := NewKeyEntryWithHasher("client-1", "raw-secret", []string{"writer"}, h)

	if e.HashVersion != 1 {
		t.Fatalf("HashVersion = %d, want 1 under an active pepper", e.HashVersion)
	}
	wantHash, _ := h.HashForStore("raw-secret")
	if e.KeyHash != wantHash {
		t.Fatalf("KeyHash = %q, want HMAC digest %q", e.KeyHash, wantHash)
	}
}

func TestNewKeyEntry_LegacyIsV0(t *testing.T) {
	e := NewKeyEntry("client-1", "raw-secret", []string{"writer"})
	if e.HashVersion != 0 {
		t.Fatalf("legacy NewKeyEntry HashVersion = %d, want 0", e.HashVersion)
	}
	if e.KeyHash != auth.HashKey("raw-secret") {
		t.Fatalf("legacy NewKeyEntry must use plain sha256")
	}
}

func TestKeyStore_Lookup_MatchesV1Key(t *testing.T) {
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	e := NewKeyEntryWithHasher("client-1", "raw-secret", nil, h)

	ks := NewKeyStoreWithHasher([]KeyEntry{e}, h)
	got := ks.Lookup("raw-secret")
	if got == nil || got.ID != "client-1" {
		t.Fatalf("Lookup of a v1 key failed: got %+v", got)
	}
	if ks.Lookup("wrong-secret") != nil {
		t.Fatal("Lookup of an unknown key must return nil")
	}
}

func TestKeyStore_Lookup_LegacyV0KeyStillResolvesUnderPepper(t *testing.T) {
	// A key stored as v0 (plain sha256) must keep authenticating after a
	// pepper is configured, via the v0 candidate. This is the core
	// "legacy v0 keeps existing keys working" guarantee.
	legacy := NewKeyEntry("legacy-1", "old-secret", nil) // v0
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)      // pepper now on

	ks := NewKeyStoreWithHasher([]KeyEntry{legacy}, h)
	if got := ks.Lookup("old-secret"); got == nil || got.ID != "legacy-1" {
		t.Fatalf("legacy v0 key did not resolve under an active pepper: got %+v", got)
	}
}

func TestKeyStore_Lookup_PreviousPepperWindow(t *testing.T) {
	// A key minted under the (now previous) pepper must still resolve
	// while the rotation window is open.
	old := auth.NewKeyHasher([]byte("pepper-OLD"), nil)
	entry := NewKeyEntryWithHasher("rot-1", "secret", nil, old) // hashed under OLD

	rotating := auth.NewKeyHasher([]byte("pepper-NEW"), []byte("pepper-OLD"))
	ks := NewKeyStoreWithHasher([]KeyEntry{entry}, rotating)
	if got := ks.Lookup("secret"); got == nil || got.ID != "rot-1" {
		t.Fatalf("key under the previous pepper did not resolve mid-rotation: got %+v", got)
	}
}

func TestKeyStore_ZeroHasher_RegressionEqualsLegacy(t *testing.T) {
	// With no hasher, Lookup must behave exactly as the pre-pepper store.
	e := NewKeyEntry("client-1", "raw-secret", nil) // v0
	ks := NewKeyStore([]KeyEntry{e})                // nil hasher
	if got := ks.Lookup("raw-secret"); got == nil || got.ID != "client-1" {
		t.Fatalf("zero-hasher lookup regressed: got %+v", got)
	}
}
