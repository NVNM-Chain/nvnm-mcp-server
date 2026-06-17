// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import "testing"

// stubKeyLookup is a tiny stand-in for KeyLookup that does not depend on
// the mcp package, so the auth package's benchmarks stay isolated. It
// exposes a single hot key and rejects everything else.
type stubKeyLookup struct {
	key string
}

func (s *stubKeyLookup) Lookup(rawKey string) *KeyResult {
	if rawKey != s.key {
		return nil
	}
	return &KeyResult{ID: "bench-client", KeyHash: HashKey(s.key)}
}

func (s *stubKeyLookup) Empty() bool { return s.key == "" }

// BenchmarkAPIKeyValidator_HotHit measures the cost of a successful
// Validate call. Auth runs on every HTTP request; this baseline lets
// us set rate-limit budgets and spot regressions in the comparison
// path (the constant-time compare).
func BenchmarkAPIKeyValidator_HotHit(b *testing.B) {
	v := NewAPIKeyValidator(&stubKeyLookup{key: "abcdefghijklmnopqrstuvwxyz0123456789"})
	for i := 0; i < b.N; i++ {
		if _, err := v.Validate("abcdefghijklmnopqrstuvwxyz0123456789"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAPIKeyValidator_Miss measures the cost of a failed lookup.
// The miss path is what credential-stuffing attackers exercise; the
// pre-auth IP failure-rate limiter assumes this is cheap.
func BenchmarkAPIKeyValidator_Miss(b *testing.B) {
	v := NewAPIKeyValidator(&stubKeyLookup{key: "abcdefghijklmnopqrstuvwxyz0123456789"})
	for i := 0; i < b.N; i++ {
		if _, err := v.Validate("not-the-key"); err == nil {
			b.Fatal("expected miss")
		}
	}
}
