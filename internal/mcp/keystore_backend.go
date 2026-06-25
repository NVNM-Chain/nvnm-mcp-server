// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

// KeyStoreBackend is the api-key store surface used by the auth path and
// the admin REST API. It is satisfied by the file-backed ManagedKeyStore
// and the Postgres-backed PostgresKeyStore, selected by KEY_STORE_BACKEND.
//
// Lookup hashes the raw token internally (versioned candidate digests)
// and returns the matching enabled entry, or nil. Mutations persist
// immediately to the backing store.
type KeyStoreBackend interface {
	Lookup(rawKey string) *KeyEntry
	Empty() bool
	List() []KeySummary
	Create(clientID string, roles []string) (*KeyCreateResult, error)
	Update(clientID string, upd KeyUpdate) (*KeySummary, error)
	Delete(clientID string) error
	ActiveCount() int
	TotalCount() int
}
