// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
)

// purgeBatchSize caps the rows removed per DELETE statement. A retention purge
// on a table that has accumulated for months can span millions of rows, and a
// single unbounded DELETE would hold row locks and bloat one transaction for
// the whole sweep. Batching by ctid keeps each statement short and lets the
// purge yield between batches; the loop simply runs until a batch comes back
// short.
const purgeBatchSize = 5000

// purgeMaxBatchesPerTable bounds one table's work in a single tick so a table
// with an enormous backlog cannot monopolize the purge goroutine and starve
// the other three. Whatever is left is picked up on the next tick -- purging
// is eventually-consistent by nature, and a retention window is a ceiling on
// age, not a promise of instantaneous deletion.
const purgeMaxBatchesPerTable = 200

// ErrPurgeNoPool is returned when a Purger is constructed without a pool.
var ErrPurgeNoPool = errors.New("retention purge requires a Postgres pool")

// Purger enforces the configured retention windows by deleting rows older than
// their window from the four keyless-bundle tables.
//
// It exists because a retention period stated in a privacy policy but enforced
// by nothing is not a retention period. Every window is operator-configured and
// defaults to zero (retain indefinitely), so a self-hosted deployment keeps its
// historical behavior unless its operator opts in.
type Purger struct {
	pool   *pgxpool.Pool
	cfg    config.RetentionConfig
	logger *slog.Logger

	// grantRoleSelector is the 0x-prefixed ABI selector for grantRole,
	// derived from the loaded ABI at boot (never hardcoded -- this
	// precompile's grantRole takes four args, so it is NOT the well-known
	// OpenZeppelin selector). Empty means the ABI was unavailable: the
	// carve-out is then disabled and every write_audit row falls under the
	// ordinary window. That is the safe direction to fail -- it cannot
	// over-retain admin records, and purgeWriteAudit refuses to run at all
	// if a grantRole window was configured but the selector is unknown.
	grantRoleSelector string
}

// PurgeResult reports how many rows each table gave up in one sweep.
type PurgeResult struct {
	WriteAudit          int64
	WriteAuditGrantRole int64
	SignerQuota         int64
	SignerBlacklist     int64
	AdminAudit          int64
}

// Total returns the row count across all tables.
func (r PurgeResult) Total() int64 {
	return r.WriteAudit + r.WriteAuditGrantRole + r.SignerQuota +
		r.SignerBlacklist + r.AdminAudit
}

// NewPurger returns a Purger over pool. grantRoleSelector should come from
// anchor.Client.MethodSelector("grantRole"); pass "" when no ABI is loaded.
func NewPurger(
	pool *pgxpool.Pool,
	cfg config.RetentionConfig,
	grantRoleSelector string,
	logger *slog.Logger,
) (*Purger, error) {
	if pool == nil {
		return nil, ErrPurgeNoPool
	}
	if logger == nil {
		logger = slog.Default()
	}
	// Fail loudly rather than silently mis-classify. If the operator asked for
	// a distinct grantRole window but we cannot tell which rows are grantRole,
	// every one of them would be purged on the SHORTER ordinary window -- we
	// would destroy the administrative audit trail the longer window exists to
	// protect, and report success while doing it.
	if cfg.WriteAuditGrantRole > 0 && grantRoleSelector == "" {
		return nil, fmt.Errorf(
			"MCP_WRITE_AUDIT_GRANT_ROLE_RETENTION is set but the grantRole ABI "+
				"selector is unknown (no ABI loaded), so grantRole rows cannot be "+
				"distinguished from ordinary writes: %w", ErrPurgeNoPool)
	}
	return &Purger{
		pool:              pool,
		cfg:               cfg,
		logger:            logger,
		grantRoleSelector: grantRoleSelector,
	}, nil
}

// Run purges on cfg.PurgeInterval until ctx is canceled. It performs one
// sweep immediately so a freshly-started server with a backlog does not wait a
// full interval before honoring its retention policy.
//
// A failing sweep is logged and retried on the next tick: a transient database
// error must not kill the goroutine and leave retention silently unenforced
// for the lifetime of the process.
func (p *Purger) Run(ctx context.Context) {
	p.logger.LogAttrs(ctx, slog.LevelInfo, "retention purge started",
		slog.Duration("interval", p.cfg.PurgeInterval),
		slog.Duration("write_audit", p.cfg.WriteAudit),
		slog.Duration("write_audit_grant_role", p.cfg.WriteAuditGrantRole),
		slog.Duration("signer_quota", p.cfg.SignerQuota),
		slog.Duration("signer_blacklist", p.cfg.SignerBlacklist),
		slog.Duration("admin_audit", p.cfg.AdminAudit),
	)

	ticker := time.NewTicker(p.cfg.PurgeInterval)
	defer ticker.Stop()

	for {
		p.sweepAndLog(ctx)
		select {
		case <-ctx.Done():
			p.logger.LogAttrs(ctx, slog.LevelInfo, "retention purge stopped")
			return
		case <-ticker.C:
		}
	}
}

func (p *Purger) sweepAndLog(ctx context.Context) {
	start := time.Now()
	res, err := p.PurgeOnce(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return // shutting down; not a failure worth logging as one
		}
		p.logger.LogAttrs(ctx, slog.LevelError, "retention purge failed",
			slog.String("error", err.Error()),
		)
		return
	}
	if res.Total() == 0 {
		return // nothing aged out; stay quiet
	}
	p.logger.LogAttrs(ctx, slog.LevelInfo, "retention purge completed",
		slog.Int64("write_audit", res.WriteAudit),
		slog.Int64("write_audit_grant_role", res.WriteAuditGrantRole),
		slog.Int64("signer_quota", res.SignerQuota),
		slog.Int64("signer_blacklist", res.SignerBlacklist),
		slog.Int64("admin_audit", res.AdminAudit),
		slog.Duration("took", time.Since(start)),
	)
}

// PurgeOnce runs a single sweep across every table with a configured window.
// Tables whose window is zero are skipped entirely -- they retain indefinitely.
func (p *Purger) PurgeOnce(ctx context.Context) (PurgeResult, error) {
	var res PurgeResult
	now := time.Now().UTC()

	wa, waGrant, err := p.purgeWriteAudit(ctx, now)
	if err != nil {
		return res, err
	}
	res.WriteAudit, res.WriteAuditGrantRole = wa, waGrant

	// signer_quota ages on window_start, not created_at: the table has no
	// created_at column, and window_start is precisely the "this counter no
	// longer applies to anyone" timestamp.
	if p.cfg.SignerQuota > 0 {
		n, qErr := p.deleteBatched(ctx,
			`DELETE FROM signer_quota WHERE ctid IN (
			   SELECT ctid FROM signer_quota WHERE window_start < $1 LIMIT $2)`,
			now.Add(-p.cfg.SignerQuota))
		if qErr != nil {
			return res, fmt.Errorf("purge signer_quota: %w", qErr)
		}
		res.SignerQuota = n
	}

	// Deleting a blacklist row UN-BANS that signer. This is opt-in for exactly
	// that reason and is left unset by default.
	if p.cfg.SignerBlacklist > 0 {
		n, bErr := p.deleteBatched(ctx,
			`DELETE FROM signer_blacklist WHERE ctid IN (
			   SELECT ctid FROM signer_blacklist WHERE created_at < $1 LIMIT $2)`,
			now.Add(-p.cfg.SignerBlacklist))
		if bErr != nil {
			return res, fmt.Errorf("purge signer_blacklist: %w", bErr)
		}
		res.SignerBlacklist = n
	}

	if p.cfg.AdminAudit > 0 {
		n, aErr := p.deleteBatched(ctx,
			`DELETE FROM admin_audit WHERE ctid IN (
			   SELECT ctid FROM admin_audit WHERE created_at < $1 LIMIT $2)`,
			now.Add(-p.cfg.AdminAudit))
		if aErr != nil {
			return res, fmt.Errorf("purge admin_audit: %w", aErr)
		}
		res.AdminAudit = n
	}

	return res, nil
}

// purgeWriteAudit applies the two windows Privacy Policy § 8 promises: an
// ordinary window for routine anchor broadcasts, and a longer administrative
// window for grantRole. Rows are told apart by the ABI method selector stored
// in method_selector (migration 0005).
//
// Rows written before that migration carry method_selector = ” and are treated
// as ordinary writes. That is the deliberate choice: the alternative -- treating
// unknown-selector rows as possible grantRole calls and holding them for the
// longer window -- would over-retain the entire pre-migration backlog, which
// cuts against data minimization for what is overwhelmingly routine traffic.
func (p *Purger) purgeWriteAudit(
	ctx context.Context, now time.Time,
) (ordinary, grantRole int64, err error) {
	carveOut := p.cfg.WriteAuditGrantRole > 0 && p.grantRoleSelector != ""

	if carveOut {
		grantRole, err = p.deleteBatched(ctx,
			`DELETE FROM write_audit WHERE ctid IN (
			   SELECT ctid FROM write_audit
			    WHERE created_at < $1 AND method_selector = $3 LIMIT $2)`,
			now.Add(-p.cfg.WriteAuditGrantRole), p.grantRoleSelector)
		if err != nil {
			return 0, 0, fmt.Errorf("purge write_audit (grantRole): %w", err)
		}
	}

	if p.cfg.WriteAudit == 0 {
		return 0, grantRole, nil // ordinary rows retained indefinitely
	}

	cutoff := now.Add(-p.cfg.WriteAudit)
	if carveOut {
		// Exclude grantRole rows: they are governed by the longer window above.
		ordinary, err = p.deleteBatched(ctx,
			`DELETE FROM write_audit WHERE ctid IN (
			   SELECT ctid FROM write_audit
			    WHERE created_at < $1 AND method_selector <> $3 LIMIT $2)`,
			cutoff, p.grantRoleSelector)
	} else {
		ordinary, err = p.deleteBatched(ctx,
			`DELETE FROM write_audit WHERE ctid IN (
			   SELECT ctid FROM write_audit WHERE created_at < $1 LIMIT $2)`,
			cutoff)
	}
	if err != nil {
		return 0, grantRole, fmt.Errorf("purge write_audit: %w", err)
	}
	return ordinary, grantRole, nil
}

// deleteBatched runs sql repeatedly until a batch deletes fewer than
// purgeBatchSize rows (i.e. the table is drained) or the per-tick batch ceiling
// is hit. sql must take the cutoff as $1 and the batch size as $2; any extra
// args follow as $3...
func (p *Purger) deleteBatched(
	ctx context.Context, sql string, cutoff time.Time, extra ...any,
) (int64, error) {
	args := append([]any{cutoff, purgeBatchSize}, extra...)

	var total int64
	for range purgeMaxBatchesPerTable {
		tag, err := p.pool.Exec(ctx, sql, args...)
		if err != nil {
			return total, err
		}
		n := tag.RowsAffected()
		total += n
		if n < purgeBatchSize {
			return total, nil
		}
		// Yield between batches so a large backlog cannot starve live traffic
		// of a pool connection.
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
	}
	p.logger.LogAttrs(ctx, slog.LevelWarn,
		"retention purge hit the per-tick batch ceiling; remainder deferred to next tick",
		slog.Int("batches", purgeMaxBatchesPerTable),
		slog.Int64("rows_deleted", total),
	)
	return total, nil
}
