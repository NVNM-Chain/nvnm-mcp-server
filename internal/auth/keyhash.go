package auth

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashKey returns the lowercased SHA-256 hex digest of the raw bearer
// token. This is the on-disk and in-memory representation of an API
// key after the Phase 8.6 migration; raw key bytes are never persisted
// or kept in memory after the initial constructor.
//
// SHA-256 is the chosen one-way primitive because it is deterministic
// (the same raw key always hashes to the same digest -- required for
// O(1) lookup) and fast (no per-request key derivation cost). It does
// not protect against an offline brute-force of a leaked digest the
// way a password-hashing KDF would, but bearer tokens here are
// 32 bytes of crypto/rand entropy (256 bits), which is well above the
// brute-force horizon for SHA-256.
func HashKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}
