// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"testing"
	"time"
)

func TestPostgresWriteAuditStore_RecordQueryRoundTrip(t *testing.T) {
	pool := testPool(t)
	s := NewPostgresWriteAuditStore(pool)
	ctx := context.Background()

	in := WriteAuditEntry{
		Signer:   "0x1111111111111111111111111111111111111111",
		ToAddr:   "0x0000000000000000000000000000000000000A00",
		ValueWei: "0", CalldataLen: 36, TxHash: "0xabc", Outcome: "broadcast_ok",
	}
	if err := s.Record(ctx, in); err != nil {
		t.Fatalf("Record: %v", err)
	}
	got, err := s.Query(ctx, WriteAuditFilter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 row, got %d", len(got))
	}
	if got[0].Signer != in.Signer || got[0].TxHash != in.TxHash ||
		got[0].Outcome != in.Outcome || got[0].CalldataLen != in.CalldataLen {
		t.Fatalf("round-trip mismatch: %+v", got[0])
	}
	if got[0].CreatedAt.IsZero() {
		t.Fatalf("created_at not populated")
	}
}

func TestPostgresWriteAuditStore_FilterBySigner(t *testing.T) {
	pool := testPool(t)
	s := NewPostgresWriteAuditStore(pool)
	ctx := context.Background()

	a := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, sgn := range []string{a, a, b} {
		if err := s.Record(ctx, WriteAuditEntry{Signer: sgn, Outcome: "broadcast_ok"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	got, err := s.Query(ctx, WriteAuditFilter{Signer: a})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 rows for signer a, got %d", len(got))
	}
	for _, e := range got {
		if e.Signer != a {
			t.Fatalf("filter leaked signer %s", e.Signer)
		}
	}
}

func TestPostgresWriteAuditStore_FilterByWindow(t *testing.T) {
	pool := testPool(t)
	s := NewPostgresWriteAuditStore(pool)
	ctx := context.Background()

	if err := s.Record(ctx, WriteAuditEntry{Signer: "0xc", Outcome: "broadcast_ok"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	future := time.Now().Add(time.Hour)
	got, err := s.Query(ctx, WriteAuditFilter{From: &future})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 rows from a future window, got %d", len(got))
	}
}

// A Limit above the hard ceiling must be clamped so a caller cannot force a
// full scan of the append-only table.
func TestPostgresWriteAuditStore_QueryClampsToMax(t *testing.T) {
	pool := testPool(t)
	s := NewPostgresWriteAuditStore(pool)
	ctx := context.Background()

	for range maxWriteAuditQueryLimit + 10 {
		if err := s.Record(ctx, WriteAuditEntry{Signer: "0xcap", Outcome: "broadcast_ok"}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
	got, err := s.Query(ctx, WriteAuditFilter{Limit: maxWriteAuditQueryLimit + 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != maxWriteAuditQueryLimit {
		t.Fatalf("want %d rows (clamped to ceiling), got %d", maxWriteAuditQueryLimit, len(got))
	}
}

// Signers are stored lowercase and matched case-insensitively so an operator
// querying with a checksummed address finds the lowercase-stored row (F-7,
// caught by the live testnet write-audit E2E).
func TestPostgresWriteAuditStore_SignerCaseInsensitive(t *testing.T) {
	pool := testPool(t)
	s := NewPostgresWriteAuditStore(pool)
	ctx := context.Background()

	const checksummed = "0x9f8a6425F7AD925701fE1CdF85fd883340b2A9CD"
	const lower = "0x9f8a6425f7ad925701fe1cdf85fd883340b2a9cd"
	if err := s.Record(ctx, WriteAuditEntry{Signer: checksummed, Outcome: "broadcast_ok"}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// Query with the checksummed form must find the row...
	got, err := s.Query(ctx, WriteAuditFilter{Signer: checksummed})
	if err != nil {
		t.Fatalf("Query (checksummed): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("checksummed query: want 1 row, got %d", len(got))
	}
	// ...and the stored value is normalized to lowercase.
	if got[0].Signer != lower {
		t.Fatalf("stored signer = %q, want lowercase %q", got[0].Signer, lower)
	}
	// The lowercase form finds it too.
	got2, err := s.Query(ctx, WriteAuditFilter{Signer: lower})
	if err != nil {
		t.Fatalf("Query (lower): %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("lowercase query: want 1 row, got %d", len(got2))
	}
}
