// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"testing"
	"time"
)

func TestSignerQuota_CountIncrement(t *testing.T) {
	pool := testPool(t)
	store := NewPostgresSignerQuotaStore(pool)
	signer := "0xabc0000000000000000000000000000000000001"
	ws := WindowStart(time.Date(2026, 7, 2, 13, 5, 0, 0, time.UTC), 24*time.Hour)

	got, err := store.Count(ctx, signer, ws)
	if err != nil || got != 0 {
		t.Fatalf("initial Count = %d, %v; want 0, nil", got, err)
	}
	for i := 0; i < 3; i++ {
		if err = store.Increment(ctx, signer, ws); err != nil {
			t.Fatalf("Increment: %v", err)
		}
	}
	got, err = store.Count(ctx, signer, ws)
	if err != nil || got != 3 {
		t.Fatalf("Count after 3 = %d, %v; want 3, nil", got, err)
	}
	// Different window is independent.
	ws2 := WindowStart(time.Date(2026, 7, 3, 13, 5, 0, 0, time.UTC), 24*time.Hour)
	if n, _ := store.Count(ctx, signer, ws2); n != 0 {
		t.Fatalf("next-window Count = %d; want 0", n)
	}
}

func TestWindowStart_TruncatesToBoundary(t *testing.T) {
	now := time.Date(2026, 7, 2, 13, 5, 0, 0, time.UTC)
	got := WindowStart(now, 24*time.Hour)
	want := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("WindowStart = %v; want %v", got, want)
	}
}
