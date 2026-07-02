// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"testing"
)

func TestSignerBlacklist_AddIsRemove(t *testing.T) {
	pool := testPool(t)
	store := NewPostgresSignerBlacklistStore(pool)
	ctx := context.Background()
	signer := "0xDEAD000000000000000000000000000000000001" // mixed case on purpose

	if bl, _ := store.IsBlacklisted(ctx, signer); bl {
		t.Fatal("fresh signer should not be blacklisted")
	}
	if err := store.Add(ctx, signer, "spam"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Case-insensitive: query with lowercase must still match.
	if bl, _ := store.IsBlacklisted(ctx, "0xdead000000000000000000000000000000000001"); !bl {
		t.Fatal("added signer should be blacklisted (case-insensitive)")
	}
	if err := store.Remove(ctx, signer); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if bl, _ := store.IsBlacklisted(ctx, signer); bl {
		t.Fatal("removed signer should not be blacklisted")
	}
}
