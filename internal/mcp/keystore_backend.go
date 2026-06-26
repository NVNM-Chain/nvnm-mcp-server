// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// KeyStoreBackend is the api-key store surface used by the auth path and
// the admin REST API. It is satisfied by the file-backed ManagedKeyStore
// and the Postgres-backed PostgresKeyStore, selected by KEY_STORE_BACKEND.
//
// Lookup hashes the raw token internally (versioned candidate digests)
// and returns the matching entry (enabled or not) plus a RejectReason.
// RejectNone means the key is valid and usable. Mutations persist
// immediately to the backing store.
type KeyStoreBackend interface {
	Lookup(ctx context.Context, rawKey string) (*KeyEntry, auth.RejectReason)
	Empty() bool
	List() []KeySummary
	Create(ctx context.Context, clientID string, roles []string, expiresAt time.Time) (*KeyCreateResult, error)
	Update(clientID string, upd KeyUpdate) (*KeySummary, error)
	Delete(clientID string) error
	ActiveCount() int
	TotalCount() int
}
