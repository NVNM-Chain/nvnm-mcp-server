// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// HashCandidate is a (digest, version) pair to probe during a key
// lookup. Version 0 is the legacy plain-SHA-256 digest; version 1 is an
// HMAC-SHA-256 digest under a configured pepper.
type HashCandidate struct {
	Hash    string
	Version int
}

// KeyHasher computes versioned API-key digests. The active pepper, when
// present, makes HashForStore emit version-1 HMAC digests; the previous
// pepper widens Candidates to cover one in-flight pepper rotation. A nil
// *KeyHasher and a zero-pepper KeyHasher both behave as version-0 only
// (plain SHA-256), preserving the pre-pepper behavior with no config.
type KeyHasher struct {
	active   []byte
	previous []byte
}

// NewKeyHasher builds a KeyHasher from the active and previous pepper
// bytes. Empty (zero-length) peppers are treated as unset, so an empty
// KEY_HMAC_PEPPER env value yields version-0 behavior rather than an
// HMAC keyed on the empty string.
func NewKeyHasher(active, previous []byte) *KeyHasher {
	h := &KeyHasher{}
	if len(active) > 0 {
		h.active = active
	}
	if len(previous) > 0 {
		h.previous = previous
	}
	return h
}

// hashKeyHMAC returns the lowercased hex HMAC-SHA-256 of rawKey under
// pepper. The pepper is the HMAC key (not concatenated), which is the
// analyzed keyed-hash construction.
func hashKeyHMAC(pepper []byte, rawKey string) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write([]byte(rawKey))
	return hex.EncodeToString(mac.Sum(nil))
}

// HashForStore returns the digest and version to persist for a NEW key.
// With an active pepper it is the version-1 HMAC digest; otherwise the
// version-0 plain SHA-256 digest.
func (h *KeyHasher) HashForStore(rawKey string) (hash string, version int) {
	if h == nil || h.active == nil {
		return HashKey(rawKey), 0
	}
	return hashKeyHMAC(h.active, rawKey), 1
}

// Candidates returns every digest that could match a stored key for
// rawKey: always the version-0 plain SHA-256 digest, plus the active and
// previous pepper HMAC digests when those peppers are configured. The
// caller probes the lookup index with each, in order, and takes the
// first hit.
func (h *KeyHasher) Candidates(rawKey string) []HashCandidate {
	cands := []HashCandidate{{Hash: HashKey(rawKey), Version: 0}}
	if h == nil {
		return cands
	}
	if h.active != nil {
		cands = append(cands, HashCandidate{Hash: hashKeyHMAC(h.active, rawKey), Version: 1})
	}
	if h.previous != nil {
		cands = append(cands, HashCandidate{Hash: hashKeyHMAC(h.previous, rawKey), Version: 1})
	}
	return cands
}
