// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminAction identifies the kind of admin mutation being audited.
type AdminAction string

// Admin actions recorded in the admin_audit table.
const (
	AdminActionKeyCreate       AdminAction = "key.create"
	AdminActionKeyUpdate       AdminAction = "key.update"
	AdminActionKeyDelete       AdminAction = "key.delete"
	AdminActionBlacklistAdd    AdminAction = "blacklist.add"
	AdminActionBlacklistRemove AdminAction = "blacklist.remove"
	AdminActionPendingApprove  AdminAction = "pending.approve"
	AdminActionPendingReject   AdminAction = "pending.reject"
)

// AdminAuditEntry is one recorded admin mutation.
type AdminAuditEntry struct {
	ActorID string
	Action  AdminAction
	Target  string
	Detail  string
	Outcome string
}

// AdminAuditStore persists admin mutations. Append-only: no update/delete.
type AdminAuditStore interface {
	Record(ctx context.Context, e AdminAuditEntry) error
}

// PostgresAdminAuditStore is the shared-state backend for admin audit entries.
type PostgresAdminAuditStore struct {
	pool *pgxpool.Pool
}

// NewPostgresAdminAuditStore returns a store backed by pool. The caller owns
// the pool lifecycle (and runs RunMigrations on it).
func NewPostgresAdminAuditStore(pool *pgxpool.Pool) *PostgresAdminAuditStore {
	return &PostgresAdminAuditStore{pool: pool}
}

// Record appends one admin mutation. created_at defaults to now() in SQL.
//
//nolint:gocritic // hugeParam accepted
func (s *PostgresAdminAuditStore) Record(ctx context.Context, e AdminAuditEntry) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO admin_audit (actor_id, action, target, detail, outcome)
		 VALUES ($1, $2, $3, $4, $5)`,
		e.ActorID, e.Action, e.Target, e.Detail, e.Outcome)
	if err != nil {
		return fmt.Errorf("record admin_audit: %w", err)
	}
	return nil
}
