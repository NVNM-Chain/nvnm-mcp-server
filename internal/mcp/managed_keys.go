package mcp

import (
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"
)

var (
	ErrClientExists  = errors.New("client already exists")
	ErrClientMissing = errors.New("client not found")
)

// KeySummary is a redacted view of a KeyEntry suitable for API responses.
// The raw key is never included.
type KeySummary struct {
	ID            string    `json:"client_id"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	WriteApproval string    `json:"write_approval,omitempty"`
	Roles         []string  `json:"roles,omitempty"`
	KeyPrefix     string    `json:"key_prefix"`
}

// KeyCreateResult is returned from Create. It includes the raw key exactly once.
type KeyCreateResult struct {
	KeySummary
	Key string `json:"key"`
}

// KeyUpdate holds optional fields for a PATCH operation.
// nil pointers mean "do not change".
type KeyUpdate struct {
	Enabled       *bool
	WriteApproval *string
	Roles         *[]string
}

// ManagedKeyStore provides thread-safe CRUD over a KeyStore backed by a JSON file.
// Reads (Lookup, Empty) acquire a read lock; mutations acquire a write lock,
// persist to disk, and rebuild the in-memory index atomically.
type ManagedKeyStore struct {
	mu      sync.RWMutex
	store   *KeyStore
	entries []KeyEntry
	path    string
}

// NewManagedKeyStore loads keys from path (creating an empty store if the file
// does not exist) and returns a thread-safe managed store.
func NewManagedKeyStore(path string) (*ManagedKeyStore, error) {
	entries, err := LoadKeysFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			entries = nil
		} else {
			return nil, err
		}
	}

	return &ManagedKeyStore{
		store:   NewKeyStore(entries),
		entries: entries,
		path:    path,
	}, nil
}

// NewManagedKeyStoreFromEntries builds a ManagedKeyStore from pre-loaded entries.
// Useful when entries were loaded externally (e.g. by main.go) or for testing.
func NewManagedKeyStoreFromEntries(path string, entries []KeyEntry) *ManagedKeyStore {
	return &ManagedKeyStore{
		store:   NewKeyStore(entries),
		entries: copyEntries(entries),
		path:    path,
	}
}

// Lookup delegates to the underlying KeyStore under a read lock.
func (m *ManagedKeyStore) Lookup(rawKey string) *KeyEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store.Lookup(rawKey)
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

// Create generates a new API key for clientID, persists it, and returns the
// result including the raw key. Returns ErrClientExists if clientID is taken.
func (m *ManagedKeyStore) Create(clientID, writeApproval string, roles []string) (*KeyCreateResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, e := range m.entries {
		if e.ID == clientID {
			return nil, fmt.Errorf("client %q: %w", clientID, ErrClientExists)
		}
	}

	rawKey, err := GenerateKey()
	if err != nil {
		return nil, err
	}

	entry := KeyEntry{
		ID:            clientID,
		Key:           rawKey,
		Enabled:       true,
		CreatedAt:     time.Now().UTC(),
		WriteApproval: writeApproval,
		Roles:         roles,
	}

	updated := append(copyEntries(m.entries), entry)
	if err := SaveKeysFile(m.path, updated); err != nil {
		return nil, fmt.Errorf("persist keys: %w", err)
	}

	m.entries = updated
	m.store = NewKeyStore(updated)

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
	if upd.WriteApproval != nil {
		updated[idx].WriteApproval = *upd.WriteApproval
	}
	if upd.Roles != nil {
		updated[idx].Roles = *upd.Roles
	}

	if err := SaveKeysFile(m.path, updated); err != nil {
		return nil, fmt.Errorf("persist keys: %w", err)
	}

	m.entries = updated
	m.store = NewKeyStore(updated)

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
	m.store = NewKeyStore(updated)
	return nil
}

// ActiveCount returns the number of enabled keys.
func (m *ManagedKeyStore) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, e := range m.entries {
		if e.Enabled {
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
	prefix := e.Key
	if len(prefix) > 8 {
		prefix = prefix[:8] + "..."
	}
	return KeySummary{
		ID:            e.ID,
		Enabled:       e.Enabled,
		CreatedAt:     e.CreatedAt,
		WriteApproval: e.WriteApproval,
		Roles:         e.Roles,
		KeyPrefix:     prefix,
	}
}

func copyEntries(src []KeyEntry) []KeyEntry {
	dst := make([]KeyEntry, len(src))
	copy(dst, src)
	return dst
}
