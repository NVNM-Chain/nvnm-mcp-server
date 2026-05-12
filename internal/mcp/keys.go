package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
)

// KeyEntry represents a single API key with its associated client identity.
type KeyEntry struct {
	ID            string    `json:"id"`
	Key           string    `json:"key"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	WriteApproval string    `json:"write_approval,omitempty"`
	// Roles controls per-tool RBAC. Valid values: reader, writer, admin, automation.
	// Empty (omitted) means no role enforcement for this key.
	Roles []string `json:"roles,omitempty"`
}

// KeyStore holds a set of API keys indexed by the raw key string for O(1) lookup.
type KeyStore struct {
	byKey map[string]*KeyEntry
}

// NewKeyStore builds a KeyStore from a slice of key entries.
// Only enabled keys are indexed.
func NewKeyStore(entries []KeyEntry) *KeyStore {
	ks := &KeyStore{byKey: make(map[string]*KeyEntry, len(entries))}
	for i := range entries {
		if entries[i].Enabled {
			ks.byKey[entries[i].Key] = &entries[i]
		}
	}
	return ks
}

// NewSingleKeyStore creates a KeyStore from a single legacy MCP_API_KEY value.
// The client identity is "static-key".
func NewSingleKeyStore(apiKey string) *KeyStore {
	entry := KeyEntry{
		ID:        "static-key",
		Key:       apiKey,
		Enabled:   true,
		CreatedAt: time.Now(),
	}
	return NewKeyStore([]KeyEntry{entry})
}

// Lookup returns the KeyEntry for the given raw API key, or nil if not found
// (or disabled).
func (ks *KeyStore) Lookup(rawKey string) *KeyEntry {
	return ks.byKey[rawKey]
}

// Empty returns true if the store has no active keys.
func (ks *KeyStore) Empty() bool {
	return len(ks.byKey) == 0
}

// LoadKeysFile reads a JSON array of KeyEntry from the given path.
func LoadKeysFile(path string) ([]KeyEntry, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-controlled config value
	if err != nil {
		return nil, fmt.Errorf("read keys file: %w", err)
	}
	var entries []KeyEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse keys file: %w", err)
	}
	return entries, nil
}

// SaveKeysFile writes a JSON array of KeyEntry to the given path
// atomically. The new contents are written to a sibling temp file,
// fsync'd, and renamed over the target. A mid-write crash leaves either
// the previous file intact or the temp file orphaned -- never a
// truncated target.
func SaveKeysFile(path string, entries []KeyEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal keys: %w", err)
	}

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

// Lookup returns a KeyResult for the given raw API key, or nil if not found.
func (a *KeyLookupAdapter) Lookup(rawKey string) *auth.KeyResult {
	entry := a.store.Lookup(rawKey)
	if entry == nil {
		return nil
	}
	return &auth.KeyResult{
		ID:            entry.ID,
		Key:           entry.Key,
		WriteApproval: entry.WriteApproval,
		Roles:         entry.Roles,
	}
}

// Empty returns true if the underlying store has no enabled keys.
func (a *KeyLookupAdapter) Empty() bool {
	return a.store.Empty()
}
