// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"embed"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// migrationsFS holds the embedded goose SQL migrations applied to the
// Postgres key store at boot. Files are named NNNN_description.sql and
// are append-only: never edit a shipped migration.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationsLockID is the application-chosen pg_advisory_lock key that
// serializes concurrent replica boots running migrations. Any stable
// 64-bit constant unique to this app works.
const migrationsLockID int64 = 0x6e766e6d6b6579 // "nvnmkey"

// RunMigrations applies the embedded goose migrations to pool under a
// pg_advisory_lock so concurrent replica boots serialize. It is
// idempotent: a fully-migrated database is a no-op.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	// goose needs a *sql.DB; bridge the pgx pool via stdlib.
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration conn: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationsLockID); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		//nolint:errcheck // advisory unlock is best-effort; conn closes immediately after
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationsLockID)
	}()

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	logger.Info("key-store migrations applied")
	return nil
}
