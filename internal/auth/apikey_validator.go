// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import (
	"crypto/subtle"
	"errors"
)

// ErrInvalidAPIKey is returned when the API key is not found or disabled.
var ErrInvalidAPIKey = errors.New("invalid API key")

// KeyResult holds the fields needed from a key entry for authentication.
// Note: the raw key is intentionally not part of this struct. After the
// Phase 8.6 migration the validator never sees a raw key beyond the
// initial token argument; storage indexes by KeyHash.
type KeyResult struct {
	ID      string
	KeyHash string // stored key digest: sha256 hex (v0) or HMAC-SHA256 hex under the active pepper (v1)
	// Roles are the RBAC roles assigned to this key. Empty means no enforcement.
	Roles []string
}

// KeyLookup abstracts read-only key operations needed by the API key validator.
// Both *KeyStore and *ManagedKeyStore in the mcp package satisfy this interface.
//
// Implementations of Lookup must hash the input internally; callers pass
// the raw token, never a pre-hashed value.
type KeyLookup interface {
	Lookup(rawKey string) *KeyResult
	Empty() bool
}

// APIKeyValidator implements TokenValidator by looking up API keys
// in a KeyLookup store and performing constant-time comparison of
// the hash bytes against the hasher's candidate digests.
type APIKeyValidator struct {
	keys   KeyLookup
	hasher *KeyHasher
}

// NewAPIKeyValidator creates a version-0 (no-pepper) validator. Legacy
// entry point; production should use NewAPIKeyValidatorWithHasher.
func NewAPIKeyValidator(keys KeyLookup) *APIKeyValidator {
	return NewAPIKeyValidatorWithHasher(keys, nil)
}

// NewAPIKeyValidatorWithHasher creates a TokenValidator that re-verifies
// matched keys against hasher's candidate digests (nil = version-0 only).
// Returns nil if keys is nil or empty (no authentication enforced).
func NewAPIKeyValidatorWithHasher(keys KeyLookup, hasher *KeyHasher) *APIKeyValidator {
	if keys == nil || keys.Empty() {
		return nil
	}
	return &APIKeyValidator{keys: keys, hasher: hasher}
}

// missPathPlaceholder is the byte string the miss-path burns through
// subtle.ConstantTimeCompare. Its only job is to make the miss path
// spend roughly the same CPU as the hit path so a remote attacker
// cannot distinguish "unknown key" from "known key with wrong digest"
// by measuring response time. The content is irrelevant; the length
// matches a sha256 hex digest (64 chars) so the constant-time compare
// touches the same number of bytes as the hit path.
//
//nolint:gochecknoglobals // immutable; package-level by design
var missPathPlaceholder = []byte("00000000000000000000000000000000" +
	"00000000000000000000000000000000")

// Validate looks up the token in the key store and returns claims on
// success. Miss and hit paths burn the same constant-time compare so
// a network observer cannot use response timing to distinguish
// unknown-key from known-key-with-wrong-digest.
func (v *APIKeyValidator) Validate(token string) (*Claims, error) {
	entry := v.keys.Lookup(token)
	if entry == nil {
		// Flatten the hit/miss timing distinction. The compare is on
		// equal placeholder bytes so it always "succeeds" against
		// itself; the discarded result is correct -- the rejection
		// happens unconditionally below.
		_ = subtle.ConstantTimeCompare(missPathPlaceholder, missPathPlaceholder)
		return nil, ErrInvalidAPIKey
	}

	// Defense-in-depth: Lookup already established exact hash equality,
	// but we re-derive the digest from the token under constant time to
	// guard against any future hashmap side-channel. With versioned
	// hashing the stored digest may be a peppered HMAC, so we compare
	// against every candidate (v0 + active/previous pepper) and
	// OR-accumulate without an early return to keep the path
	// data-independent. All candidates are 64-char SHA-256 hex, matching
	// entry.KeyHash's length, so ConstantTimeCompare's length shortcut
	// cannot leak the digest. The equal-length (64-hex) invariant is
	// guaranteed upstream by the hasher (both v0 and v1 emit 64-char hex);
	// it is not re-enforced here.
	var match int
	for _, c := range v.hasher.Candidates(token) {
		match |= subtle.ConstantTimeCompare([]byte(c.Hash), []byte(entry.KeyHash))
	}
	if match != 1 {
		return nil, ErrInvalidAPIKey
	}

	return &Claims{
		ClientID: entry.ID,
		Roles:    entry.Roles,
	}, nil
}

// Close is a no-op for API key validation.
func (v *APIKeyValidator) Close() error { return nil }
