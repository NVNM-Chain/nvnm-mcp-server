// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import "testing"

func TestKeyHasher_NoPepper_IsV0(t *testing.T) {
	h := NewKeyHasher(nil, nil)

	hash, version := h.HashForStore("raw-key-abc")
	if version != 0 {
		t.Fatalf("version = %d, want 0 with no pepper", version)
	}
	if hash != HashKey("raw-key-abc") {
		t.Fatalf("HashForStore hash = %q, want plain sha256 %q", hash, HashKey("raw-key-abc"))
	}

	cands := h.Candidates("raw-key-abc")
	if len(cands) != 1 || cands[0].Version != 0 || cands[0].Hash != HashKey("raw-key-abc") {
		t.Fatalf("Candidates = %+v, want exactly the v0 digest", cands)
	}
}

func TestKeyHasher_NilReceiver_IsV0(t *testing.T) {
	var h *KeyHasher // nil
	hash, version := h.HashForStore("k")
	if version != 0 || hash != HashKey("k") {
		t.Fatalf("nil hasher must be v0: got version=%d hash=%q", version, hash)
	}
	if cands := h.Candidates("k"); len(cands) != 1 || cands[0].Version != 0 {
		t.Fatalf("nil hasher Candidates = %+v, want single v0", cands)
	}
}

func TestKeyHasher_EmptyPepperTreatedAsUnset(t *testing.T) {
	h := NewKeyHasher([]byte(""), []byte(""))
	if _, version := h.HashForStore("k"); version != 0 {
		t.Fatalf("empty-string pepper must be treated as unset (v0), got version=%d", version)
	}
}

func TestKeyHasher_ActivePepper_IsV1AndDeterministic(t *testing.T) {
	h := NewKeyHasher([]byte("pepper-A"), nil)

	hash, version := h.HashForStore("raw-key-abc")
	if version != 1 {
		t.Fatalf("version = %d, want 1 with active pepper", version)
	}
	if hash == HashKey("raw-key-abc") {
		t.Fatal("v1 hash must differ from the v0 plain sha256")
	}
	// Deterministic: same input + same pepper => same digest (required for O(1) lookup).
	if again, _ := h.HashForStore("raw-key-abc"); again != hash {
		t.Fatalf("HMAC not deterministic: %q != %q", again, hash)
	}
	if len(hash) != 64 {
		t.Fatalf("v1 digest length = %d, want 64 hex chars", len(hash))
	}
}

func TestKeyHasher_DifferentPeppers_DifferentDigests(t *testing.T) {
	a, _ := NewKeyHasher([]byte("pepper-A"), nil).HashForStore("k")
	b, _ := NewKeyHasher([]byte("pepper-B"), nil).HashForStore("k")
	if a == b {
		t.Fatal("different peppers must yield different digests for the same key")
	}
}

func TestKeyHasher_Candidates_ActivePlusPreviousPlusV0(t *testing.T) {
	h := NewKeyHasher([]byte("pepper-A"), []byte("pepper-prev"))
	cands := h.Candidates("k")
	if len(cands) != 3 {
		t.Fatalf("len(Candidates) = %d, want 3 (v0 + active + previous)", len(cands))
	}
	// v0 first, then active, then previous; both peppered are version 1.
	if cands[0].Version != 0 || cands[0].Hash != HashKey("k") {
		t.Fatalf("candidate[0] = %+v, want v0 plain sha256", cands[0])
	}
	wantActive, _ := NewKeyHasher([]byte("pepper-A"), nil).HashForStore("k")
	if cands[1].Version != 1 || cands[1].Hash != wantActive {
		t.Fatalf("candidate[1] = %+v, want active-pepper HMAC %q", cands[1], wantActive)
	}
	wantPrev, _ := NewKeyHasher([]byte("pepper-prev"), nil).HashForStore("k")
	if cands[2].Version != 1 || cands[2].Hash != wantPrev {
		t.Fatalf("candidate[2] = %+v, want previous-pepper HMAC %q", cands[2], wantPrev)
	}
}
