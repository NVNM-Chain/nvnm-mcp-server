// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultWriteAuditQueryLimit caps an unbounded admin query so a missing
// limit cannot scan the whole append-only table.
const defaultWriteAuditQueryLimit = 100

// maxWriteAuditQueryLimit is the hard ceiling on a single query so a caller
// cannot force a full scan of the append-only table with a huge Limit.
const maxWriteAuditQueryLimit = 1000

// WriteAuditEntry is one recorded broadcast attempt. Addresses and tx hashes
// only -- there is no key material on the authless path.
type WriteAuditEntry struct {
	Signer      string
	To          string
	ValueWei    string
	CalldataLen int
	TxHash      string
	Outcome     string // "broadcast_ok" | "broadcast_failed"
	Error       string
	CreatedAt   time.Time
}

// WriteAuditFilter narrows an admin query. Zero Signer means any signer; nil
// From/To means unbounded on that side; Limit <= 0 means
// defaultWriteAuditQueryLimit and any Limit above maxWriteAuditQueryLimit is
// clamped to that ceiling.
type WriteAuditFilter struct {
	Signer string
	From   *time.Time
	To     *time.Time
	Limit  int
}

// WriteAuditStore persists and queries broadcast attempts. Append-only: no
// update/delete. A nil store means logs-only (self-host / no MCP_KEYLESS_PG_DSN).
type WriteAuditStore interface {
	Record(ctx context.Context, e WriteAuditEntry) error
	Query(ctx context.Context, f WriteAuditFilter) ([]WriteAuditEntry, error)
}

// PostgresWriteAuditStore is the shared-state backend for the authless bundle.
type PostgresWriteAuditStore struct {
	pool *pgxpool.Pool
}

// NewPostgresWriteAuditStore returns a store backed by pool. The caller owns
// the pool lifecycle (and runs RunMigrations on it).
func NewPostgresWriteAuditStore(pool *pgxpool.Pool) *PostgresWriteAuditStore {
	return &PostgresWriteAuditStore{pool: pool}
}

// Record appends one broadcast attempt. created_at defaults to now() in SQL.
//
//nolint:gocritic // hugeParam accepted
func (s *PostgresWriteAuditStore) Record(ctx context.Context, e WriteAuditEntry) error {

	_, err := s.pool.Exec(ctx,
		`INSERT INTO write_audit
		   (signer, to_addr, value_wei, calldata_len, tx_hash, outcome, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.Signer, e.To, e.ValueWei, e.CalldataLen, e.TxHash, e.Outcome, e.Error)
	if err != nil {
		return fmt.Errorf("record write_audit: %w", err)
	}
	return nil
}

// Query returns matching rows, newest first, capped by Limit.
func (s *PostgresWriteAuditStore) Query(
	ctx context.Context, f WriteAuditFilter,
) ([]WriteAuditEntry, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultWriteAuditQueryLimit
	}
	if limit > maxWriteAuditQueryLimit {
		limit = maxWriteAuditQueryLimit
	}
	rows, err := s.pool.Query(ctx,
		`SELECT signer, to_addr, value_wei, calldata_len, tx_hash, outcome, error, created_at
		   FROM write_audit
		  WHERE ($1 = '' OR signer = $1)
		    AND ($2::timestamptz IS NULL OR created_at >= $2)
		    AND ($3::timestamptz IS NULL OR created_at <= $3)
		  ORDER BY created_at DESC
		  LIMIT $4`,
		f.Signer, f.From, f.To, limit)
	if err != nil {
		return nil, fmt.Errorf("query write_audit: %w", err)
	}
	defer rows.Close()

	var out []WriteAuditEntry
	for rows.Next() {
		var e WriteAuditEntry
		if scanErr := rows.Scan(
			&e.Signer, &e.To, &e.ValueWei, &e.CalldataLen,
			&e.TxHash, &e.Outcome, &e.Error, &e.CreatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("scan write_audit row: %w", scanErr)
		}
		out = append(out, e)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate write_audit rows: %w", rows.Err())
	}
	return out, nil
}
