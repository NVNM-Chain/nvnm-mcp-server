package mcp

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// KeyEntry represents a single API key with its associated client identity.
type KeyEntry struct {
	ID            string    `json:"id"`
	Key           string    `json:"key"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
	WriteApproval string    `json:"write_approval,omitempty"`
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

// SaveKeysFile writes a JSON array of KeyEntry to the given path.
func SaveKeysFile(path string, entries []KeyEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal keys: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// GenerateKey produces a cryptographically random 32-byte base64url key.
func GenerateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
