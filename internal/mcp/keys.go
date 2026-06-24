// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// ErrLegacyKeyWriteApproval is returned when the key store contains entries
// with the retired write_approval field. Remove the field from every entry
// per docs/RUNBOOK.md#write-approval-removal.
var ErrLegacyKeyWriteApproval = errors.New(
	"key store contains the retired write_approval field; remove it from " +
		"every entry (server-side write approval was removed in Option 0). " +
		"See docs/RUNBOOK.md#write-approval-removal")

// keyPrefixLen is the number of raw-key characters captured as
// KeyPrefix at creation time, for operator-visible listings. 8 chars
// of a 43-char base64url-encoded 32-byte key keeps enough information
// for visual recognition without leaking the digest's preimage to a
// reasonable degree (43 chars * 6 bits = 258 bits of entropy; the
// prefix discloses 8*6 = 48 bits, leaving ~210 unknown).
const keyPrefixLen = 8

// KeyEntry represents a single API key with its associated client identity.
//
// Phase 8.6 changed the on-disk and in-memory representation:
//   - KeyHash (sha256 hex) is the primary identifier and the index key.
//   - KeyPrefix (first 8 chars of the raw key at creation) supports
//     operator-visible listings.
//   - Key is retained ONLY as a load-only legacy field for one-shot
//     migration of pre-8.6 key stores. After migration runs it is
//     cleared and never written back. New code (in production, not
//     tests-of-migration) must not set this field; use NewKeyEntry.
type KeyEntry struct {
	ID        string    `json:"id"`
	Key       string    `json:"key,omitempty"`
	KeyHash   string    `json:"key_hash,omitempty"`
	KeyPrefix string    `json:"key_prefix,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	// HashVersion identifies the digest scheme in KeyHash: 0 = legacy
	// plain SHA-256; 1 = HMAC-SHA-256 under a configured pepper. Written
	// at creation; omitempty keeps v0 entries byte-identical on disk.
	HashVersion   int    `json:"hash_version,omitempty"`
	WriteApproval string `json:"write_approval,omitempty"`
	// Roles controls per-tool RBAC. Valid values: reader, writer, admin, automation.
	// Empty (omitted) means no role enforcement for this key.
	Roles []string `json:"roles,omitempty"`
}

// NewKeyEntry is the legacy constructor: it produces a version-0
// (plain SHA-256) entry, preserving pre-pepper behavior. Production
// callers that have a hasher should use NewKeyEntryWithHasher.
// Direct KeyEntry literals with Key: set are reserved for the migration
// helper in this file and the migration regression tests;
// TestNoRawKeyLiteralsOutsideMigrationTests grep-enforces that constraint.
func NewKeyEntry(id, rawKey string, roles []string) KeyEntry {
	return NewKeyEntryWithHasher(id, rawKey, roles, nil)
}

// NewKeyEntryWithHasher computes the key's digest and version via hasher
// (a nil hasher means version-0 plain SHA-256). The hash and prefix are
// computed once and the raw key is never retained.
func NewKeyEntryWithHasher(id, rawKey string, roles []string, hasher *auth.KeyHasher) KeyEntry {
	hash, version := hasher.HashForStore(rawKey)
	return KeyEntry{
		ID:          id,
		KeyHash:     hash,
		HashVersion: version,
		KeyPrefix:   keyPrefixOf(rawKey),
		Enabled:     true,
		CreatedAt:   time.Now().UTC(),
		Roles:       roles,
	}
}

func keyPrefixOf(rawKey string) string {
	if len(rawKey) <= keyPrefixLen {
		return rawKey
	}
	return rawKey[:keyPrefixLen] + "..."
}

// KeyStore indexes enabled keys by their stored digest for O(1) lookup.
// Lookup callers pass the raw token; the store derives the candidate
// digests via its KeyHasher and probes the index with each. Raw key
// bytes are not retained.
type KeyStore struct {
	byHash map[string]*KeyEntry
	hasher *auth.KeyHasher
}

// NewKeyStore builds a version-0 (no-pepper) KeyStore. Legacy entry
// point; production callers with a hasher should use NewKeyStoreWithHasher.
func NewKeyStore(entries []KeyEntry) *KeyStore {
	return NewKeyStoreWithHasher(entries, nil)
}

// NewKeyStoreWithHasher builds a KeyStore that derives lookup candidates
// via hasher (nil = version-0 only). Pre-8.6 entries are normalized in
// place via migrateLegacyEntries before indexing; only enabled entries
// with a non-empty KeyHash are indexed.
func NewKeyStoreWithHasher(entries []KeyEntry, hasher *auth.KeyHasher) *KeyStore {
	_, _ = migrateLegacyEntries(entries) // normalizes in place
	ks := &KeyStore{
		byHash: make(map[string]*KeyEntry, len(entries)),
		hasher: hasher,
	}
	for i := range entries {
		if entries[i].Enabled && entries[i].KeyHash != "" {
			ks.byHash[entries[i].KeyHash] = &entries[i]
		}
	}
	return ks
}

// migrateLegacyEntries normalizes any pre-8.6 entries in place: it
// computes KeyHash and KeyPrefix from the raw Key, then clears Key.
// Returns (true, count) when at least one entry was rewritten so the
// caller can decide whether to opportunistically re-save the file.
//
// Entries that already have KeyHash set are left untouched (including
// their Key field -- a no-op on the re-load path keeps idempotence
// even if a partially-migrated file makes it to disk).
func migrateLegacyEntries(entries []KeyEntry) (changed bool, count int) {
	for i := range entries {
		if entries[i].KeyHash != "" {
			continue
		}
		if entries[i].Key == "" {
			// Entry has neither hash nor raw key. It cannot
			// authenticate anything; leave it for the operator to
			// notice and clean up rather than silently dropping it.
			continue
		}
		entries[i].KeyHash = auth.HashKey(entries[i].Key)
		if entries[i].KeyPrefix == "" {
			entries[i].KeyPrefix = keyPrefixOf(entries[i].Key)
		}
		entries[i].Key = ""
		changed = true
		count++
	}
	return changed, count
}

// Lookup returns the enabled KeyEntry whose stored KeyHash matches any
// candidate digest for rawKey (version-0 plain SHA-256, plus the active
// and previous pepper HMAC digests when configured), or nil if none
// match. The first hit wins. The raw key is hashed per candidate; the
// input string is otherwise not retained.
func (ks *KeyStore) Lookup(rawKey string) *KeyEntry {
	for _, c := range ks.hasher.Candidates(rawKey) {
		if e := ks.byHash[c.Hash]; e != nil {
			return e
		}
	}
	return nil
}

// Empty returns true if the store has no active keys.
func (ks *KeyStore) Empty() bool {
	return len(ks.byHash) == 0
}

// LoadKeysFile reads a JSON array of KeyEntry from the given path. If
// the primary file is unparseable, it falls back to reading
// <path>.tmp -- a remnant of an interrupted SaveKeysFile rename --
// before returning the original parse error. This is best-effort
// recovery; nothing is mutated.
func LoadKeysFile(path string) ([]KeyEntry, error) {
	entries, primaryErr := loadKeysFromPath(path)
	if primaryErr == nil {
		return entries, nil
	}

	// The primary file may be a malformed leftover of an interrupted
	// SaveKeysFile (write to .tmp, fsync, rename). The .tmp side
	// would have the *new* contents in that scenario.
	tmpPath := path + ".tmp"
	if _, statErr := os.Stat(tmpPath); statErr == nil {
		if tmpEntries, tmpErr := loadKeysFromPath(tmpPath); tmpErr == nil {
			return tmpEntries, nil
		}
	}
	return nil, primaryErr
}

func loadKeysFromPath(path string) ([]KeyEntry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-controlled config value
	if err != nil {
		return nil, fmt.Errorf("read keys file: %w", err)
	}
	var entries []KeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse keys file: %w", err)
	}
	// Fail loud on any entry carrying the retired write_approval field.
	// Operators must remove it before the server will start; silent
	// acceptance would mask stale config the same way dual-populated
	// INVENIAM_*/NVNM_* would.
	var legacyIDs []string
	for i := range entries {
		if entries[i].WriteApproval != "" {
			legacyIDs = append(legacyIDs, entries[i].ID)
		}
	}
	if len(legacyIDs) > 0 {
		return nil, fmt.Errorf(
			"key store %q: entries with retired write_approval field: %v: %w",
			path, legacyIDs, ErrLegacyKeyWriteApproval,
		)
	}
	return entries, nil
}

// SaveKeysFile writes a JSON array of KeyEntry to the given path
// atomically and with an advisory exclusive lock held during the
// write. The atomic write path is tmp file -> fsync -> rename; a
// mid-write crash leaves either the previous file intact or the temp
// file orphaned. The flock guards against two cooperating processes
// (admin CLI + running server) overwriting each other when both
// honor the lock; a non-cooperating process can still race.
//
// The advisory lock is taken on the target file when it exists
// (LOCK_EX | LOCK_NB so a stuck holder fails fast rather than
// indefinitely blocking startup) and released when the function
// returns. If the target file does not exist (first write), the
// caller's filesystem isolation is the only serialization.
func SaveKeysFile(path string, entries []KeyEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal keys: %w", err)
	}

	if lockErr := withExclusiveLock(path, func() error {
		return writeKeysAtomic(path, data)
	}); lockErr != nil {
		return lockErr
	}
	return nil
}

// withExclusiveLock acquires LOCK_EX | LOCK_NB on path (if it exists)
// for the duration of fn. If path does not exist, fn runs unguarded.
func withExclusiveLock(path string, fn func() error) error {
	// path is the operator-supplied MCP_API_KEYS_FILE; not user input.
	f, err := os.OpenFile(path, os.O_RDWR, 0o600) //nolint:gosec // operator-controlled config path
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// First write; no existing file to lock against.
			return fn()
		}
		return fmt.Errorf("open keys file for lock: %w", err)
	}
	defer f.Close()

	// int(f.Fd()) is the standard Go pattern for passing a file
	// descriptor to syscall wrappers; the uintptr->int conversion
	// cannot meaningfully overflow on any supported platform since
	// file descriptors are small non-negative integers from the
	// kernel.
	//
	// Local golangci-lint (v2.12) does not fire gosec G115 here,
	// but CI runs v2.11 which does. The "nolintlint" suppression
	// hides the "unused directive" complaint from the local
	// version so both can be satisfied with the same source.
	fd := int(f.Fd()) //nolint:gosec,nolintlint // fd pattern; CI/local lint skew
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("acquire exclusive lock on keys file (another process holds it?): %w", err)
	}
	// Unlock failure on shutdown is not actionable; the OS releases
	// the lock when the fd closes via the deferred Close above.
	defer func() { _ = unix.Flock(fd, unix.LOCK_UN) }() //nolint:errcheck // see comment

	return fn()
}

func writeKeysAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp keys file: %w", err)
	}
	tmpPath := tmp.Name()

	// Best-effort cleanup if anything below fails before the rename.
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp keys file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp keys file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp keys file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp keys file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp keys file: %w", err)
	}
	tmpPath = "" // committed; suppress the deferred cleanup
	return nil
}

// preMigrationBackupSuffix is the suffix appended to the keys-file
// path when NewManagedKeyStore detects a pre-8.6 file and is about to
// rewrite it in hashed form. The backup is written ONCE: if a file
// with this suffix already exists from a prior migration, it is left
// alone so the truest "what did we have before hashing ever ran"
// record is preserved across multiple restart cycles.
const preMigrationBackupSuffix = ".pre-migration"

// GenerateKey produces a cryptographically random 32-byte base64url key.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// KeyLookupAdapter wraps a ManagedKeyStore to satisfy auth.KeyLookup.
type KeyLookupAdapter struct {
	store *ManagedKeyStore
}

// NewKeyLookupAdapter creates an adapter that bridges ManagedKeyStore to auth.KeyLookup.
func NewKeyLookupAdapter(store *ManagedKeyStore) *KeyLookupAdapter {
	return &KeyLookupAdapter{store: store}
}

// Lookup returns a KeyResult for the given raw API key, or nil if not
// found. The raw key is hashed by the underlying KeyStore; this
// adapter does not retain the input.
func (a *KeyLookupAdapter) Lookup(rawKey string) *auth.KeyResult {
	entry := a.store.Lookup(rawKey)
	if entry == nil {
		return nil
	}
	return &auth.KeyResult{
		ID:      entry.ID,
		KeyHash: entry.KeyHash,
		Roles:   entry.Roles,
	}
}

// Empty returns true if the underlying store has no enabled keys.
func (a *KeyLookupAdapter) Empty() bool {
	return a.store.Empty()
}
