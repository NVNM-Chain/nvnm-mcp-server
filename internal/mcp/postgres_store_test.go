// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestPostgresKeyStore_CreateLookupRoundTrip(t *testing.T) {
	pool := testPool(t)
	h := auth.NewKeyHasher([]byte("pepper-A"), nil)
	ks := NewPostgresKeyStore(pool, h)

	res, err := ks.Create("client-1", []string{"writer"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := ks.Lookup(res.Key)
	if got == nil || got.ID != "client-1" || got.HashVersion != 1 {
		t.Fatalf("Lookup of created v1 key failed: %+v", got)
	}
	if ks.Lookup("not-the-key") != nil {
		t.Fatal("unknown key must not match")
	}
}

func TestPostgresKeyStore_DisabledKeyNotFound(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	res, err := ks.Create("c1", []string{"reader"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := ks.Update("c1", KeyUpdate{Enabled: ptrBool(false)}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if ks.Lookup(res.Key) != nil {
		t.Fatal("disabled key must not authenticate")
	}
}

func TestPostgresKeyStore_ListDeleteCounts(t *testing.T) {
	pool := testPool(t)
	ks := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("p"), nil))
	_, _ = ks.Create("c1", []string{"reader"})
	_, _ = ks.Create("c2", []string{"writer"})
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
	if _, err := ks.Create("dup", []string{"reader"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := ks.Create("dup", []string{"writer"}); !errors.Is(err, ErrClientExists) {
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
	if _, err := ks.Create("e1", []string{"reader"}); err != nil {
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
	if got := ks.Lookup(raw); got == nil || got.ID != "legacy" {
		t.Fatalf("v0 key did not authenticate under pepper: %+v", got)
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
	if ks.Lookup(raw) == nil {
		t.Fatal("key failed after lazy rehash")
	}
}

func ptrBool(b bool) *bool { return &b }
