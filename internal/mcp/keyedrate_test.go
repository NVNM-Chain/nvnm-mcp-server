// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"testing"
	"time"
)

// TestKeyedRateLimiter_LRUEvictionUnderCap verifies the shared keyed
// limiter evicts the least-recently-used bucket when the key cap is hit.
// (Relocated from a misfiled ClientRateLimiter test in failrate_test.go
// during the Phase 9.16 keyedRateLimiter extraction; the eviction logic
// now lives on keyedRateLimiter, so the coverage lives with it.)
func TestKeyedRateLimiter_LRUEvictionUnderCap(t *testing.T) {
	l := newKeyedRateLimiter(1, 1)
	l.maxKeys = 3
	_ = l.limiterFor("key-a")
	_ = l.limiterFor("key-b")
	_ = l.limiterFor("key-c")
	if got := l.Size(); got != 3 {
		t.Fatalf("Size = %d, want 3", got)
	}
	time.Sleep(2 * time.Millisecond)
	_ = l.limiterFor("key-d")
	if got := l.Size(); got != 3 {
		t.Errorf("Size after 4th key with cap=3 = %d, want 3", got)
	}
}
