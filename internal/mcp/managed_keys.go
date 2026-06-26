// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

var (
	ErrClientExists  = errors.New("client already exists")
	ErrClientMissing = errors.New("client not found")
)

// KeySummary is a redacted view of a KeyEntry suitable for API responses.
// The raw key is never included. ExpiresAt uses the zero value (not omitempty)
// so callers can distinguish "no expiry set" from a missing field.
type KeySummary struct {
	ID        string    `json:"client_id"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	Roles     []string  `json:"roles,omitempty"`
	KeyPrefix string    `json:"key_prefix"`
	ExpiresAt time.Time `json:"expires_at"`
}

// KeyCreateResult is returned from Create. It includes the raw key exactly once.
type KeyCreateResult struct {
	KeySummary
	Key string `json:"key"`
}

// KeyUpdate holds optional fields for a PATCH operation.
// nil pointers mean "do not change".
type KeyUpdate struct {
	Enabled *bool
	Roles   *[]string
	// ExpiresAt controls expiry: nil leaves it unchanged; a non-nil zero
	// time.Time clears the expiry (no expiry, SQL NULL); a non-nil non-zero
	// time.Time renews the key to that absolute UTC deadline.
	ExpiresAt *time.Time
}

// Compile-time assertion that the file backend satisfies KeyStoreBackend.
var _ KeyStoreBackend = (*ManagedKeyStore)(nil)

// ManagedKeyStore provides thread-safe CRUD over a KeyStore backed by a JSON file.
// Reads (Lookup, Empty) acquire a read lock; mutations acquire a write lock,
// persist to disk, and rebuild the in-memory index atomically.
type ManagedKeyStore struct {
	mu      sync.RWMutex
	store   *KeyStore
	entries []KeyEntry
	path    string
	hasher  *auth.KeyHasher
	now     func() time.Time
}

// NewManagedKeyStore loads keys with no pepper (version-0). Legacy entry
// point; production should use NewManagedKeyStoreWithHasher.
func NewManagedKeyStore(path string, logger *slog.Logger) (*ManagedKeyStore, error) {
	return NewManagedKeyStoreWithHasher(path, nil, logger)
}

// NewManagedKeyStoreWithHasher loads keys from path (creating an empty
// store if the file does not exist) and returns a thread-safe managed
// store whose lookups and new-key digests use hasher (nil = version-0).
// The one-shot pre-8.6 migration is unchanged: see the package docs.
//
// If the on-disk file contains pre-8.6 entries (raw Key set,
// KeyHash empty), the store performs a one-shot migration:
//
//  1. Write <path>.pre-migration as a verbatim copy of the original
//     file IF that backup does not already exist. The backup is the
//     truest "what did we have before any hashing ran" record and is
//     never overwritten by subsequent migrations.
//  2. Normalize the in-memory entries (compute KeyHash + KeyPrefix
//     from raw Key, clear raw Key) via migrateLegacyEntries.
//  3. Opportunistically re-save the file in the new hashed form.
//
// The opportunistic re-save is best-effort: on save failure we log
// at WARN level and continue startup. The next admin CRUD will
// re-persist; in the meantime the in-memory state is correct.
//
// The logger argument may be nil; nil-safe via slog's discard
// handler. Production callers should pass a real logger so the
// migration INFO/WARN lines reach the operator.
func NewManagedKeyStoreWithHasher(path string, hasher *auth.KeyHasher, logger *slog.Logger) (*ManagedKeyStore, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	entries, err := LoadKeysFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			entries = nil
		} else {
			return nil, err
		}
	}

	migrated, count := migrateLegacyEntries(entries)
	if migrated {
		if backupErr := writePreMigrationBackup(path); backupErr != nil {
			// Backup is part of the migration's safety contract;
			// if we cannot write it, refuse to mutate the live
			// file. The in-memory state is normalized so auth
			// keeps working, but disk stays in the pre-migration
			// shape until the operator clears the issue.
			logger.Warn("legacy keys file detected but pre-migration backup write failed; "+
				"skipping disk re-save to preserve the original file",
				slog.String("path", path),
				slog.String("backup", path+preMigrationBackupSuffix),
				slog.String("error", backupErr.Error()),
				slog.Int("entries", count),
			)
		} else if saveErr := SaveKeysFile(path, entries); saveErr != nil {
			// Backup exists; the live file is still in legacy
			// shape. In-memory state is normalized so the server
			// keeps working; next admin CRUD will re-persist.
			logger.Warn("legacy keys migrated in memory but failed to persist; "+
				"next admin CRUD will re-save",
				slog.String("path", path),
				slog.String("error", saveErr.Error()),
				slog.Int("entries", count),
			)
		} else {
			logger.Info("legacy keys file migrated to hashed format",
				slog.String("path", path),
				slog.String("backup", path+preMigrationBackupSuffix),
				slog.Int("entries", count),
			)
		}
	}

	mks := &ManagedKeyStore{
		store:   NewKeyStoreWithHasher(entries, hasher),
		entries: entries,
		path:    path,
		hasher:  hasher,
	}
	mks.now = time.Now
	return mks, nil
}

// writePreMigrationBackup copies path verbatim to
// path+preMigrationBackupSuffix IF the backup does not already exist.
// One-shot: a backup from a prior migration is preserved as the
// truest pre-hashing record.
func writePreMigrationBackup(path string) error {
	backupPath := path + preMigrationBackupSuffix
	if _, err := os.Stat(backupPath); err == nil {
		// Backup already exists from a previous migration; leave it alone.
		return nil
	}
	original, err := os.ReadFile(path) //nolint:gosec // operator-controlled path
	if err != nil {
		return fmt.Errorf("read original for backup: %w", err)
	}
	// backupPath = path + ".pre-migration"; not a separately-tainted input.
	//nolint:gosec // backupPath is derived from operator-controlled path
	if err := os.WriteFile(backupPath, original, 0o600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return nil
}

// NewManagedKeyStoreFromEntries builds a version-0 store from pre-loaded
// entries. Legacy/testing entry point.
func NewManagedKeyStoreFromEntries(path string, entries []KeyEntry) *ManagedKeyStore {
	return NewManagedKeyStoreFromEntriesWithHasher(path, entries, nil)
}

// NewManagedKeyStoreFromEntriesWithHasher builds a ManagedKeyStore from
// pre-loaded entries whose lookups and new-key digests use hasher.
func NewManagedKeyStoreFromEntriesWithHasher(path string, entries []KeyEntry, hasher *auth.KeyHasher) *ManagedKeyStore {
	mks := &ManagedKeyStore{
		store:   NewKeyStoreWithHasher(entries, hasher),
		entries: copyEntries(entries),
		path:    path,
		hasher:  hasher,
	}
	mks.now = time.Now
	return mks
}

// Lookup resolves rawKey to its stored entry (enabled or not) and classifies
// it as of the store clock. ctx is accepted for interface symmetry; the
// file backend does no I/O on the hot path.
func (m *ManagedKeyStore) Lookup(_ context.Context, rawKey string) (*KeyEntry, auth.RejectReason) {
	m.mu.RLock()
	e := m.store.Lookup(rawKey)
	m.mu.RUnlock()
	return e, classifyEntry(e, m.now())
}

// Empty returns true if the store has no enabled keys.
func (m *ManagedKeyStore) Empty() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.Empty()
}

// List returns redacted summaries of all keys.
func (m *ManagedKeyStore) List() []KeySummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	summaries := make([]KeySummary, len(m.entries))
	for i := range m.entries {
		summaries[i] = summarize(&m.entries[i])
	}
	return summaries
}

// Create generates a new API key for clientID with optional expiry, persists it,
// and returns the result including the raw key. Returns ErrClientExists if clientID is taken.
func (m *ManagedKeyStore) Create(
	_ context.Context, clientID string, roles []string, expiresAt time.Time,
) (*KeyCreateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.entries {
		if m.entries[i].ID == clientID {
			return nil, fmt.Errorf("client %q: %w", clientID, ErrClientExists)
		}
	}

	rawKey, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	entry := NewKeyEntryWithHasher(clientID, rawKey, roles, m.hasher)
	entry.ExpiresAt = expiresAt

	updated := append(copyEntries(m.entries), entry)
	if err := SaveKeysFile(m.path, updated); err != nil {
		return nil, fmt.Errorf("persist keys: %w", err)
	}

	m.entries = updated
	m.store = NewKeyStoreWithHasher(updated, m.hasher)

	return &KeyCreateResult{
		KeySummary: summarize(&entry),
		Key:        rawKey,
	}, nil
}

// Update modifies an existing key's enabled status and/or write approval policy.
// Returns ErrClientMissing if clientID is not found.
func (m *ManagedKeyStore) Update(clientID string, upd KeyUpdate) (*KeySummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := -1
	for i := range m.entries {
		if m.entries[i].ID == clientID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, fmt.Errorf("client %q: %w", clientID, ErrClientMissing)
	}

	updated := copyEntries(m.entries)
	if upd.Enabled != nil {
		updated[idx].Enabled = *upd.Enabled
	}
	if upd.Roles != nil {
		updated[idx].Roles = *upd.Roles
	}
	if upd.ExpiresAt != nil {
		updated[idx].ExpiresAt = *upd.ExpiresAt
	}

	if err := SaveKeysFile(m.path, updated); err != nil {
		return nil, fmt.Errorf("persist keys: %w", err)
	}

	m.entries = updated
	m.store = NewKeyStoreWithHasher(updated, m.hasher)

	s := summarize(&updated[idx])
	return &s, nil
}

// Delete permanently removes a key. Returns ErrClientMissing if not found.
func (m *ManagedKeyStore) Delete(clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := -1
	for i := range m.entries {
		if m.entries[i].ID == clientID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("client %q: %w", clientID, ErrClientMissing)
	}

	updated := make([]KeyEntry, 0, len(m.entries)-1)
	updated = append(updated, m.entries[:idx]...)
	updated = append(updated, m.entries[idx+1:]...)

	if err := SaveKeysFile(m.path, updated); err != nil {
		return fmt.Errorf("persist keys: %w", err)
	}

	m.entries = updated
	m.store = NewKeyStoreWithHasher(updated, m.hasher)
	return nil
}

// ActiveCount returns the number of enabled keys.
func (m *ManagedKeyStore) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for i := range m.entries {
		if m.entries[i].Enabled {
			count++
		}
	}
	return count
}

// TotalCount returns the total number of keys (enabled + disabled).
func (m *ManagedKeyStore) TotalCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

func summarize(e *KeyEntry) KeySummary {
	return KeySummary{
		ID:        e.ID,
		Enabled:   e.Enabled,
		CreatedAt: e.CreatedAt,
		Roles:     e.Roles,
		KeyPrefix: e.KeyPrefix,
		ExpiresAt: e.ExpiresAt,
	}
}

func copyEntries(src []KeyEntry) []KeyEntry {
	dst := make([]KeyEntry, len(src))
	copy(dst, src)
	return dst
}
