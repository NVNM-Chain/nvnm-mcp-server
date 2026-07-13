// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
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
	Signer      string // recovered signer address; normalized to lowercase on store
	ToAddr      string // destination address (SQL column to_addr)
	ValueWei    string
	CalldataLen int
	// MethodSelector is the 0x-prefixed 4-byte ABI selector taken from the
	// head of the calldata, or "" when the calldata is shorter than 4 bytes
	// (a bare value transfer). It exists so the retention purge can honor
	// the two windows Privacy Policy § 8 promises -- grantRole broadcasts
	// keep a longer window than ordinary anchor writes, and under keyless
	// writes every tx shares one destination, so nothing else distinguishes
	// them. A selector is a public function identifier, not caller data.
	MethodSelector string
	TxHash         string
	Outcome        string // "broadcast_ok" | "broadcast_failed"
	Error          string
	CreatedAt      time.Time
}

// MethodSelectorOf returns the 0x-prefixed 4-byte ABI selector at the head of
// calldata, or "" when there are fewer than 4 bytes to read.
func MethodSelectorOf(calldata []byte) string {
	if len(calldata) < 4 {
		return ""
	}
	return "0x" + hex.EncodeToString(calldata[:4])
}

// WriteAuditFilter narrows an admin query. Zero Signer means any signer; a
// Signer match is case-insensitive (addresses are normalized to lowercase, so
// a checksummed query matches a lowercase-stored row). Nil From/To means
// unbounded on that side; Limit <= 0 means defaultWriteAuditQueryLimit and any
// Limit above maxWriteAuditQueryLimit is clamped to that ceiling.
type WriteAuditFilter struct {
	Signer string
	From   *time.Time
	To     *time.Time
	Limit  int
}

// WriteAuditStore persists and queries broadcast attempts. No update path: a
// row is never edited once written. Rows are deleted only by the retention
// purge (see purge.go), which is off unless the operator configures a window
// -- so the default posture is still append-only. A nil store means logs-only
// (self-host / no MCP_KEYLESS_PG_DSN).
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
		   (signer, to_addr, value_wei, calldata_len, method_selector,
		    tx_hash, outcome, error)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		strings.ToLower(e.Signer), e.ToAddr, e.ValueWei, e.CalldataLen,
		strings.ToLower(e.MethodSelector), e.TxHash, e.Outcome, e.Error)
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
		`SELECT signer, to_addr, value_wei, calldata_len, method_selector,
		        tx_hash, outcome, error, created_at
		   FROM write_audit
		  WHERE ($1 = '' OR signer = $1)
		    AND ($2::timestamptz IS NULL OR created_at >= $2)
		    AND ($3::timestamptz IS NULL OR created_at <= $3)
		  ORDER BY created_at DESC
		  LIMIT $4`,
		strings.ToLower(f.Signer), f.From, f.To, limit)
	if err != nil {
		return nil, fmt.Errorf("query write_audit: %w", err)
	}
	defer rows.Close()

	var out []WriteAuditEntry
	for rows.Next() {
		var e WriteAuditEntry
		if scanErr := rows.Scan(
			&e.Signer, &e.ToAddr, &e.ValueWei, &e.CalldataLen, &e.MethodSelector,
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
