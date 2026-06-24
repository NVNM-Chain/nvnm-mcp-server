// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import (
	"errors"
	"testing"
)

// stubLookup is a minimal KeyLookup returning a fixed entry by exact match.
type stubLookup struct {
	raw   string
	entry *KeyResult
}

func (s *stubLookup) Lookup(rawKey string) *KeyResult {
	if rawKey == s.raw {
		return s.entry
	}
	return nil
}
func (s *stubLookup) Empty() bool { return s.entry == nil }

func TestValidate_V1Key_PassesReverify(t *testing.T) {
	h := NewKeyHasher([]byte("pepper-A"), nil)
	v1hash, _ := h.HashForStore("raw-secret")
	lk := &stubLookup{raw: "raw-secret", entry: &KeyResult{ID: "c1", KeyHash: v1hash, Roles: []string{"writer"}}}

	v := NewAPIKeyValidatorWithHasher(lk, h)
	claims, err := v.Validate("raw-secret")
	if err != nil {
		t.Fatalf("Validate of a v1 key failed re-verify: %v", err)
	}
	if claims.ClientID != "c1" {
		t.Fatalf("claims.ClientID = %q, want c1", claims.ClientID)
	}
}

func TestValidate_LegacyValidator_V0StillWorks(t *testing.T) {
	v0hash := HashKey("raw-secret")
	lk := &stubLookup{raw: "raw-secret", entry: &KeyResult{ID: "c1", KeyHash: v0hash}}

	v := NewAPIKeyValidator(lk) // nil hasher => v0-only candidates
	if _, err := v.Validate("raw-secret"); err != nil {
		t.Fatalf("legacy validator regressed on a v0 key: %v", err)
	}
}

func TestValidate_UnknownKey_Rejected(t *testing.T) {
	h := NewKeyHasher([]byte("pepper-A"), nil)
	lk := &stubLookup{raw: "raw-secret", entry: &KeyResult{ID: "c1", KeyHash: "deadbeef"}}
	v := NewAPIKeyValidatorWithHasher(lk, h)
	if _, err := v.Validate("not-the-key"); !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("unknown key error = %v, want ErrInvalidAPIKey", err)
	}
}

// TestValidate_FoundButHashMismatch_Rejected exercises the branch where Lookup
// returns a non-nil entry (key is "known") but its stored KeyHash matches none
// of the hasher's candidates (v0 + v1). This is distinct from
// TestValidate_UnknownKey_Rejected which only exercises the entry==nil miss path.
//
// The stored hash is 64 hex zeros — equal in length to every SHA-256 hex digest
// emitted by the hasher (v0 and v1 both produce 64-char hex). The equal-length
// invariant is guaranteed upstream by the hasher, not enforced in the compare
// loop; using the same length here ensures ConstantTimeCompare cannot shortcut
// on length before the content comparison.
func TestValidate_FoundButHashMismatch_Rejected(t *testing.T) {
	h := NewKeyHasher([]byte("pepper-A"), nil)
	// A 64-char all-zero hex string — valid length, wrong content.
	const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"
	lk := &stubLookup{
		raw:   "raw-secret",
		entry: &KeyResult{ID: "c1", KeyHash: zeroHash},
	}
	v := NewAPIKeyValidatorWithHasher(lk, h)
	_, err := v.Validate("raw-secret")
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Fatalf("found-but-hash-mismatch error = %v, want ErrInvalidAPIKey", err)
	}
}
