// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func tempKeysFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "keys.json")
}

func TestManagedKeyStore_CreateAndLookup(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create(context.Background(), "client-a", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Key == "" {
		t.Fatal("expected raw key in create result")
	}
	if result.ID != "client-a" {
		t.Fatalf("got client_id %q, want client-a", result.ID)
	}

	entry, reason := mks.Lookup(context.Background(), result.Key)
	if reason != auth.RejectNone {
		t.Fatal("expected Lookup to find newly created key")
	}
	if entry.ID != "client-a" {
		t.Fatalf("got ID %q, want client-a", entry.ID)
	}
}

func TestManagedKeyStore_CreateDuplicate(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mks.Create(context.Background(), "client-a", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "client-a", nil, time.Time{}); err == nil {
		t.Fatal("expected error for duplicate client ID")
	}
}

func TestManagedKeyStore_List(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mks.Create(context.Background(), "alpha", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "beta", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	summaries := mks.List()
	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}
	for _, s := range summaries {
		if len(s.KeyPrefix) > 11 {
			t.Fatalf("List appears to have full key for %q (prefix too long: %d chars)", s.ID, len(s.KeyPrefix))
		}
	}
}

func TestManagedKeyStore_UpdateEnabled(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create(context.Background(), "client-a", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	if _, r := mks.Lookup(context.Background(), rawKey); r != auth.RejectNone {
		t.Fatal("expected key to be findable before disable")
	}

	disabled := false
	if _, err := mks.Update("client-a", KeyUpdate{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}

	if _, r := mks.Lookup(context.Background(), rawKey); r == auth.RejectNone {
		t.Fatal("expected disabled key to be nil on Lookup")
	}

	enabled := true
	if _, err := mks.Update("client-a", KeyUpdate{Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}

	if _, r := mks.Lookup(context.Background(), rawKey); r != auth.RejectNone {
		t.Fatal("expected re-enabled key to be findable")
	}
}

func TestManagedKeyStore_UpdateMissing(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	enabled := true
	_, err = mks.Update("nonexistent", KeyUpdate{Enabled: &enabled})
	if err == nil {
		t.Fatal("expected error for missing client")
	}
}

func TestManagedKeyStore_Delete(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks.Create(context.Background(), "client-a", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	if err := mks.Delete("client-a"); err != nil {
		t.Fatal(err)
	}

	if _, r := mks.Lookup(context.Background(), rawKey); r == auth.RejectNone {
		t.Fatal("expected deleted key to be nil on Lookup")
	}

	if mks.TotalCount() != 0 {
		t.Fatalf("got total count %d, want 0", mks.TotalCount())
	}
}

func TestManagedKeyStore_DeleteMissing(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := mks.Delete("nonexistent"); err == nil {
		t.Fatal("expected error for missing client")
	}
}

func TestManagedKeyStore_PersistenceAcrossReloads(t *testing.T) {
	path := tempKeysFile(t)

	mks1, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mks1.Create(context.Background(), "persistent-client", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	mks2, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	entry, reason2 := mks2.Lookup(context.Background(), rawKey)
	if reason2 != auth.RejectNone {
		t.Fatal("expected key to survive reload from disk")
	}
	if entry.ID != "persistent-client" {
		t.Fatalf("got ID %q, want persistent-client", entry.ID)
	}
}

func TestManagedKeyStore_Counters(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mks.Create(context.Background(), "a", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "b", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	if mks.TotalCount() != 2 {
		t.Fatalf("got total %d, want 2", mks.TotalCount())
	}
	if mks.ActiveCount() != 2 {
		t.Fatalf("got active %d, want 2", mks.ActiveCount())
	}

	disabled := false
	if _, err := mks.Update("a", KeyUpdate{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if mks.ActiveCount() != 1 {
		t.Fatalf("got active %d after disable, want 1", mks.ActiveCount())
	}
	if mks.TotalCount() != 2 {
		t.Fatalf("got total %d after disable, want 2", mks.TotalCount())
	}
}

func TestManagedKeyStore_EmptyOnNewFile(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if !mks.Empty() {
		t.Fatal("expected new store to be empty")
	}
	if mks.TotalCount() != 0 {
		t.Fatalf("got total %d, want 0", mks.TotalCount())
	}
}

func TestManagedKeyStore_FilePermissions(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, createErr := mks.Create(context.Background(), "test", nil, time.Time{}); createErr != nil {
		t.Fatal(createErr)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Fatalf("got file perm %o, want 0600", perm)
	}
}

func TestManagedKeyStore_Lookup_Reasons(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	mks.now = func() time.Time { return now }

	good, _ := mks.Create(context.Background(), "good", []string{"writer"}, time.Time{})
	expK, _ := mks.Create(context.Background(), "expd", []string{"writer"}, now.Add(-time.Hour))
	revK, _ := mks.Create(context.Background(), "revd", []string{"writer"}, time.Time{})
	if _, err := mks.Update("revd", KeyUpdate{Enabled: boolPtr(false)}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		raw  string
		want auth.RejectReason
	}{
		{"valid", good.Key, auth.RejectNone},
		{"expired", expK.Key, auth.RejectExpired},
		{"revoked", revK.Key, auth.RejectRevoked},
		{"unknown", "nvnm_bogus", auth.RejectNotFound},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, reason := mks.Lookup(context.Background(), c.raw)
			if reason != c.want {
				t.Errorf("reason = %v, want %v", reason, c.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

// TestManagedKeyStore_Create_PersistsExpiry verifies that ExpiresAt survives a
// store reload from disk for both the expiry and no-expiry cases.
func TestManagedKeyStore_Create_PersistsExpiry(t *testing.T) {
	exp := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	// --- with-expiry case ---
	mks, err := NewManagedKeyStore(tempKeysFile(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := mks.Create(context.Background(), "expiring", []string{"writer"}, exp)
	if err != nil {
		t.Fatal(err)
	}

	// Reload from the same path to prove the value was written to disk.
	mks2, err := NewManagedKeyStore(mks.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	e, reason := mks2.Lookup(context.Background(), res.Key)
	if reason != auth.RejectNone {
		t.Fatalf("expiring key: Lookup reason = %v, want RejectNone", reason)
	}
	if !e.ExpiresAt.Equal(exp) {
		t.Errorf("expiring key: ExpiresAt = %v, want %v", e.ExpiresAt, exp)
	}

	// --- no-expiry case ---
	mksZ, err := NewManagedKeyStore(tempKeysFile(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	resZ, err := mksZ.Create(context.Background(), "no-expiry", []string{"writer"}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	mksZ2, err := NewManagedKeyStore(mksZ.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	eZ, reasonZ := mksZ2.Lookup(context.Background(), resZ.Key)
	if reasonZ != auth.RejectNone {
		t.Fatalf("no-expiry key: Lookup reason = %v, want RejectNone", reasonZ)
	}
	if !eZ.ExpiresAt.IsZero() {
		t.Errorf("no-expiry key: ExpiresAt = %v, want zero", eZ.ExpiresAt)
	}
}

// TestManagedKeyStore_Empty_EnabledOnly asserts the file-store Empty() contract:
// a store with only disabled keys reports true (no enabled keys), and a store
// with at least one enabled key reports false. This pins the fix that restored
// enabled-only semantics to KeyStore.Empty() after Task 3 started indexing
// disabled entries too.
func TestUpdate_Expiry_FileStore(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	mks, err := NewManagedKeyStore(tempKeysFile(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	mks.now = func() time.Time { return now }

	// Create a key already expired (expiresAt = now-1h).
	res, err := mks.Create(context.Background(), "k", []string{"writer"}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: Lookup must reject as expired.
	if _, r := mks.Lookup(context.Background(), res.Key); r != auth.RejectExpired {
		t.Fatalf("precondition: want RejectExpired, got %v", r)
	}

	// Renew: set expiry to now+1h.
	renewed := now.Add(time.Hour)
	if _, err := mks.Update("k", KeyUpdate{ExpiresAt: &renewed}); err != nil {
		t.Fatal(err)
	}
	if _, r := mks.Lookup(context.Background(), res.Key); r != auth.RejectNone {
		t.Errorf("after renew: want RejectNone, got %v", r)
	}

	// Clear to no-expiry: zero time.
	var zero time.Time
	if _, err := mks.Update("k", KeyUpdate{ExpiresAt: &zero}); err != nil {
		t.Fatal(err)
	}
	if _, r := mks.Lookup(context.Background(), res.Key); r != auth.RejectNone {
		t.Errorf("after clear: want RejectNone, got %v", r)
	}
}

func TestManagedKeyStore_Empty_EnabledOnly(t *testing.T) {
	// Case 1: one disabled key → Empty() must be true.
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "disabled-only", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := mks.Update("disabled-only", KeyUpdate{Enabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	if !mks.Empty() {
		t.Fatal("Empty() = false, want true: store with only a disabled key should be empty")
	}

	// Case 2: add one enabled key → Empty() must be false.
	if _, err := mks.Create(context.Background(), "enabled-one", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if mks.Empty() {
		t.Fatal("Empty() = true, want false: store with one enabled key should not be empty")
	}
}
