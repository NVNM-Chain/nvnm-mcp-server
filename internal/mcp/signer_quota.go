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

// SignerQuotaStore is the shared-state per-signer write counter. Count reads
// the current window's tally; Increment adds one to it. Fixed-window: the
// caller passes a WindowStart-truncated timestamp.
//
// Soft ceiling (accepted, EA-1): the read (Count) and the write (Increment) are
// deliberately separate calls -- Count is checked before the broadcast and
// Increment runs only after a successful one, so a failed broadcast never
// consumes quota and a success is never double-counted. The gap between them is
// not locked, so concurrent broadcasts by the same signer can each pass the same
// count < RATE check and over-admit past the limit by roughly the concurrency
// width. This is intentional: the quota is a coarse anti-abuse throttle, the
// over-admission is gas-bounded and self-correcting, and an exact cap would need
// a per-signer advisory lock / serialized transaction on every broadcast. See
// docs/DATA_HANDLING.md § 8.2.
type SignerQuotaStore interface {
	Count(ctx context.Context, signer string, windowStart time.Time) (int, error)
	Increment(ctx context.Context, signer string, windowStart time.Time) error
}

// WindowStart truncates now to the fixed-window boundary (UTC). A 24h window
// yields midnight UTC of the current day.
func WindowStart(now time.Time, window time.Duration) time.Time {
	return now.UTC().Truncate(window)
}

// PostgresSignerQuotaStore backs SignerQuotaStore on the keyless-bundle pool.
type PostgresSignerQuotaStore struct {
	pool *pgxpool.Pool
}

// NewPostgresSignerQuotaStore returns a store backed by pool (the keyless
// bundle pool). The caller owns the pool lifecycle.
func NewPostgresSignerQuotaStore(pool *pgxpool.Pool) *PostgresSignerQuotaStore {
	return &PostgresSignerQuotaStore{pool: pool}
}

// Count returns the current-window count for signer (0 if no row).
func (s *PostgresSignerQuotaStore) Count(ctx context.Context, signer string, windowStart time.Time) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT count FROM signer_quota WHERE signer = $1 AND window_start = $2`,
		strings.ToLower(signer), windowStart).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("count signer_quota: %w", err)
	}
	return n, nil
}

// Increment adds one to the current-window count, inserting the row if absent.
func (s *PostgresSignerQuotaStore) Increment(ctx context.Context, signer string, windowStart time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO signer_quota (signer, window_start, count)
		 VALUES ($1, $2, 1)
		 ON CONFLICT (signer, window_start)
		 DO UPDATE SET count = signer_quota.count + 1`,
		strings.ToLower(signer), windowStart)
	if err != nil {
		return fmt.Errorf("increment signer_quota: %w", err)
	}
	return nil
}
