// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestPostgresKeyStore_CreateLookupRoundTrip(t *testing.T) {
	pool := testPool(t)
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	ks := NewPostgresKeyStore(pool, h)

	res, err := ks.Create(context.Background(), "client-1", []string{"writer"}, time.Time{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, reason := ks.Lookup(context.Background(), res.Key)
	if reason != auth.RejectNone || got == nil || got.ID != "client-1" || got.HashVersion != 1 {
		t.Fatalf("Lookup of created v1 key failed: %+v reason=%v", got, reason)
	}
	if _, r := ks.Lookup(context.Background(), "not-the-key"); r == auth.RejectNone {
		t.Fatal("unknown key must not match")
	}
}

func TestPostgresKeyStore_DisabledKeyNotFound(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	res, err := ks.Create(context.Background(), "c1", []string{"reader"}, time.Time{})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := ks.Update("c1", KeyUpdate{Enabled: ptrBool(false)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, reason2 := ks.Lookup(context.Background(), res.Key); reason2 == auth.RejectNone {
		t.Fatal("disabled key must not authenticate")
	}
}

func TestPostgresKeyStore_ListDeleteCounts(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	_, _ = ks.Create(context.Background(), "c1", []string{"reader"}, time.Time{})
	_, _ = ks.Create(context.Background(), "c2", []string{"writer"}, time.Time{})
	if ks.TotalCount() != 2 || ks.ActiveCount() != 2 || len(ks.List()) != 2 {
		t.Fatalf("counts/list wrong: total=%d active=%d list=%d",
			ks.TotalCount(), ks.ActiveCount(), len(ks.List()))
	}
	if err := ks.Delete("c1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ks.TotalCount() != 1 {
		t.Fatalf("after delete total=%d want 1", ks.TotalCount())
	}
	if err := ks.Delete("nope"); !errors.Is(err, ErrClientMissing) {
		t.Fatalf("delete missing should return ErrClientMissing, got %v", err)
	}
}

func TestPostgresKeyStore_DuplicateCreate(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	if _, err := ks.Create(context.Background(), "dup", []string{"reader"}, time.Time{}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := ks.Create(context.Background(), "dup", []string{"writer"}, time.Time{}); !errors.Is(err, ErrClientExists) {
		t.Fatalf("duplicate Create should return ErrClientExists, got %v", err)
	}
}

func TestPostgresKeyStore_UpdateMissing(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	if _, err := ks.Update("no-such-client", KeyUpdate{Enabled: ptrBool(false)}); !errors.Is(err, ErrClientMissing) {
		t.Fatalf("Update missing should return ErrClientMissing, got %v", err)
	}
}

func TestPostgresKeyStore_Empty(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	if !ks.Empty() {
		t.Fatal("fresh store must be empty")
	}
	if _, err := ks.Create(context.Background(), "e1", []string{"reader"}, time.Time{}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ks.Empty() {
		t.Fatal("store with one enabled key must not be empty")
	}
}

func TestPostgresKeyStore_LazyRehash_V0ToV1(t *testing.T) {
	pool := testPool(t)
	// Insert a legacy v0 row directly: plain sha256 of the raw key.
	const raw = "legacy-raw-secret"
	v0 := auth.HashKey(raw) // plain sha256 hex
	_, err := pool.Exec(context.Background(),
		`INSERT INTO api_keys (id, key_hash, hash_version, key_prefix, roles, enabled, created_at)
		 VALUES ('legacy', $1, 0, 'legacy..', '{reader}', true, now())`,
		digestBytes(v0))
	if err != nil {
		t.Fatalf("seed v0: %v", err)
	}

	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	ks := NewPostgresKeyStore(pool, h)

	// First lookup authenticates via the v0 candidate AND upgrades the row.
	got, r := ks.Lookup(context.Background(), raw)
	if r != auth.RejectNone || got == nil || got.ID != "legacy" {
		t.Fatalf("v0 key did not authenticate under pepper: %+v reason=%v", got, r)
	}
	// The row is now v1 with the HMAC digest.
	var version int
	var hashB []byte
	if err := pool.QueryRow(context.Background(),
		`SELECT hash_version, key_hash FROM api_keys WHERE id='legacy'`).Scan(&version, &hashB); err != nil {
		t.Fatalf("reread: %v", err)
	}
	wantHash, _ := h.HashForStore(raw)
	if version != 1 || hex.EncodeToString(hashB) != wantHash {
		t.Fatalf("row not upgraded: version=%d hash=%s want v1 %s", version, hex.EncodeToString(hashB), wantHash)
	}
	// And it still authenticates after the upgrade (now via the v1 candidate).
	if _, r2 := ks.Lookup(context.Background(), raw); r2 != auth.RejectNone {
		t.Fatal("key failed after lazy rehash")
	}
}

func ptrBool(b bool) *bool { return &b }

// TestPostgresKeyStore_Create_PersistsExpiry verifies that expires_at is stored
// and scanned correctly for both the expiry and NULL (no-expiry) cases.
func TestPostgresKeyStore_Create_PersistsExpiry(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	p := NewPostgresKeyStore(pool, auth.NewKeyHasher(nil, nil))
	p.now = func() time.Time { return now }

	exp := now.Add(48 * time.Hour)

	// --- with-expiry case ---
	res, err := p.Create(context.Background(), "exp-client", []string{"writer"}, exp)
	if err != nil {
		t.Fatalf("Create with expiry: %v", err)
	}
	e, reason := p.Lookup(context.Background(), res.Key)
	if reason != auth.RejectNone {
		t.Fatalf("with-expiry Lookup reason = %v, want RejectNone", reason)
	}
	if !e.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt = %v, want %v", e.ExpiresAt, exp)
	}

	// --- no-expiry case: NULL round-trips to zero time ---
	res2, err := p.Create(context.Background(), "noexp-client", []string{"writer"}, time.Time{})
	if err != nil {
		t.Fatalf("Create without expiry: %v", err)
	}
	e2, reason2 := p.Lookup(context.Background(), res2.Key)
	if reason2 != auth.RejectNone {
		t.Fatalf("no-expiry Lookup reason = %v, want RejectNone", reason2)
	}
	if !e2.ExpiresAt.IsZero() {
		t.Errorf("NULL expiry round-tripped as %v, want zero", e2.ExpiresAt)
	}
}

func TestUpdate_Expiry_Postgres(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	p := NewPostgresKeyStore(pool, auth.NewKeyHasher(nil, nil))
	p.now = func() time.Time { return now }

	// Create a key already expired (expiresAt = now-1h).
	res, err := p.Create(context.Background(), "k", []string{"writer"}, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// Precondition: Lookup must reject as expired.
	if _, r := p.Lookup(context.Background(), res.Key); r != auth.RejectExpired {
		t.Fatalf("precondition: want RejectExpired, got %v", r)
	}

	// Renew: set expiry to now+1h.
	renewed := now.Add(time.Hour)
	if _, err := p.Update("k", KeyUpdate{ExpiresAt: &renewed}); err != nil {
		t.Fatal(err)
	}
	if _, r := p.Lookup(context.Background(), res.Key); r != auth.RejectNone {
		t.Errorf("after renew: want RejectNone, got %v", r)
	}

	// Clear to no-expiry: zero time → SQL NULL.
	var zero time.Time
	if _, err := p.Update("k", KeyUpdate{ExpiresAt: &zero}); err != nil {
		t.Fatal(err)
	}
	if _, r := p.Lookup(context.Background(), res.Key); r != auth.RejectNone {
		t.Errorf("after clear: want RejectNone, got %v", r)
	}
}

// TestPostgresKeyStore_ListAndSummary_ExpiresAt asserts that List() and the
// KeySummary returned by Update() both surface the correct expires_at value.
// A key created with an expiry must report that expiry; a key with no expiry
// must report the zero time.Time.
func TestPostgresKeyStore_ListAndSummary_ExpiresAt(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	p := NewPostgresKeyStore(pool, auth.NewKeyHasher(nil, nil))
	p.now = func() time.Time { return now }

	exp := now.Add(72 * time.Hour)

	// Create one key with expiry and one without.
	if _, err := p.Create(context.Background(), "list-exp", []string{"reader"}, exp); err != nil {
		t.Fatalf("Create with expiry: %v", err)
	}
	if _, err := p.Create(context.Background(), "list-noexp", []string{"reader"}, time.Time{}); err != nil {
		t.Fatalf("Create without expiry: %v", err)
	}

	// List() must include the correct expires_at for each.
	summaries := p.List()
	byID := make(map[string]KeySummary, len(summaries))
	for _, s := range summaries {
		byID[s.ID] = s
	}

	if got := byID["list-exp"].ExpiresAt; !got.Equal(exp) {
		t.Errorf("List: list-exp ExpiresAt = %v, want %v", got, exp)
	}
	if got := byID["list-noexp"].ExpiresAt; !got.IsZero() {
		t.Errorf("List: list-noexp ExpiresAt = %v, want zero", got)
	}

	// Update() returns a KeySummary via summary(); verify expires_at propagates.
	renewed := now.Add(24 * time.Hour)
	summ, err := p.Update("list-noexp", KeyUpdate{ExpiresAt: &renewed})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !summ.ExpiresAt.Equal(renewed) {
		t.Errorf("Update summary ExpiresAt = %v, want %v", summ.ExpiresAt, renewed)
	}

	// Clear expiry: zero ExpiresAt in the summary must be zero.
	var zero time.Time
	summ2, err := p.Update("list-exp", KeyUpdate{ExpiresAt: &zero})
	if err != nil {
		t.Fatalf("Update clear expiry: %v", err)
	}
	if !summ2.ExpiresAt.IsZero() {
		t.Errorf("Update cleared ExpiresAt = %v, want zero", summ2.ExpiresAt)
	}
}

func TestPostgresKeyStore_Lookup_Reasons(t *testing.T) {
	pool := testPool(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	p := NewPostgresKeyStore(pool, auth.NewKeyHasher(nil, nil))
	p.now = func() time.Time { return now }

	good, _ := p.Create(context.Background(), "good", []string{"writer"}, time.Time{})
	expd, _ := p.Create(context.Background(), "expd", []string{"writer"}, now.Add(-time.Hour))
	revd, _ := p.Create(context.Background(), "revd", []string{"writer"}, time.Time{})
	if _, err := p.Update("revd", KeyUpdate{Enabled: ptrBool(false)}); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name string
		raw  string
		want auth.RejectReason
	}{
		{"valid", good.Key, auth.RejectNone},
		{"expired", expd.Key, auth.RejectExpired},
		{"revoked", revd.Key, auth.RejectRevoked},
		{"unknown", "nvnm_bogus", auth.RejectNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, reason := p.Lookup(context.Background(), c.raw)
			if reason != c.want {
				t.Errorf("reason = %v, want %v", reason, c.want)
			}
		})
	}
}
