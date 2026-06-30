// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool returns a pgx pool against NVNM_TEST_PG_DSN, skipping the test
// when it is unset so `go test ./...` passes on a machine with no Postgres.
// It truncates the key-store tables so each test starts clean.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("NVNM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("NVNM_TEST_PG_DSN not set; skipping Postgres-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test pg: %v", err)
	}
	t.Cleanup(pool.Close)
	// First pass: ensure tables exist before truncating.
	if err := RunMigrations(context.Background(), pool, nil); err != nil {
		t.Fatalf("migrate test pg: %v", err)
	}
	// Truncate both tables so migration-state tests start completely clean.
	// Drop goose_db_version so goose recreates it with the required seed row
	// on the next RunMigrations call; a plain TRUNCATE leaves the table empty
	// which causes EnsureDBVersionContext to return ErrNoNextVersion.
	if _, err := pool.Exec(context.Background(),
		"TRUNCATE api_keys, write_audit; DROP TABLE goose_db_version"); err != nil {
		t.Fatalf("truncate api_keys/write_audit, drop goose_db_version: %v", err)
	}
	// Second pass: goose recreates goose_db_version with seed row + applies
	// migrations, leaving the pool in a fully-migrated, data-clean state.
	if err := RunMigrations(context.Background(), pool, nil); err != nil {
		t.Fatalf("migrate test pg (post-reset): %v", err)
	}
	return pool
}
