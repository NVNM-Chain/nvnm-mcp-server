// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BlacklistEntry is one banned signer.
type BlacklistEntry struct {
	Signer    string
	Reason    string
	CreatedAt time.Time
}

// SignerBlacklistStore is the shared-state signer ban list. IsBlacklisted is
// queried per write (no cache) so an admin Add takes effect immediately across
// replicas.
type SignerBlacklistStore interface {
	IsBlacklisted(ctx context.Context, signer string) (bool, error)
	Add(ctx context.Context, signer, reason string) error
	Remove(ctx context.Context, signer string) error
	List(ctx context.Context) ([]BlacklistEntry, error)
}

// PostgresSignerBlacklistStore backs SignerBlacklistStore on the keyless pool.
type PostgresSignerBlacklistStore struct {
	pool *pgxpool.Pool
}

// NewPostgresSignerBlacklistStore returns a store backed by pool.
func NewPostgresSignerBlacklistStore(pool *pgxpool.Pool) *PostgresSignerBlacklistStore {
	return &PostgresSignerBlacklistStore{pool: pool}
}

// IsBlacklisted reports whether signer is banned (case-insensitive).
func (s *PostgresSignerBlacklistStore) IsBlacklisted(ctx context.Context, signer string) (bool, error) {
	var one int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM signer_blacklist WHERE signer = $1`,
		strings.ToLower(signer)).Scan(&one)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query signer_blacklist: %w", err)
	}
	return true, nil
}

// Add upserts a ban for signer with reason.
func (s *PostgresSignerBlacklistStore) Add(ctx context.Context, signer, reason string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO signer_blacklist (signer, reason) VALUES ($1, $2)
		 ON CONFLICT (signer) DO UPDATE SET reason = EXCLUDED.reason`,
		strings.ToLower(signer), reason)
	if err != nil {
		return fmt.Errorf("add signer_blacklist: %w", err)
	}
	return nil
}

// Remove deletes a ban. Removing an absent signer is a no-op (no error).
func (s *PostgresSignerBlacklistStore) Remove(ctx context.Context, signer string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM signer_blacklist WHERE signer = $1`, strings.ToLower(signer))
	if err != nil {
		return fmt.Errorf("remove signer_blacklist: %w", err)
	}
	return nil
}

// List returns all bans, newest first.
func (s *PostgresSignerBlacklistStore) List(ctx context.Context) ([]BlacklistEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT signer, reason, created_at FROM signer_blacklist ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list signer_blacklist: %w", err)
	}
	defer rows.Close()
	var out []BlacklistEntry
	for rows.Next() {
		var e BlacklistEntry
		if err := rows.Scan(&e.Signer, &e.Reason, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan signer_blacklist row: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate signer_blacklist rows: %w", err)
	}
	return out, nil
}
