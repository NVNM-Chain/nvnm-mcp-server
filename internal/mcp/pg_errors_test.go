// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
)

// retentionCfg keeps the purge test tables terse.
type retentionCfg = config.RetentionConfig

// closedTestPool returns a pool against NVNM_TEST_PG_DSN that has
// already been closed, so every operation on it fails deterministically
// without touching the database. Used to drive the store error branches.
func closedTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("NVNM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("NVNM_TEST_PG_DSN not set; skipping Postgres-backed test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect test pg: %v", err)
	}
	pool.Close()
	return pool
}

func TestDigestBytes_PanicsOnInvalidHex(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("digestBytes should panic on non-hex input")
		}
	}()
	digestBytes("zz-not-hex")
}

func TestPostgresKeyStore_ClosedPoolErrors(t *testing.T) {
	p := NewPostgresKeyStore(closedTestPool(t), auth.NewKeyHasher(nil, nil))
	ctx := context.Background()

	if e, reason := p.Lookup(ctx, "any-key"); e != nil || reason != auth.RejectNotFound {
		t.Errorf("Lookup on closed pool = (%v, %v), want (nil, RejectNotFound)", e, reason)
	}
	if _, err := p.Create(ctx, "client-x", []string{"reader"}, time.Time{}); err == nil {
		t.Error("Create on closed pool should fail")
	}
	enabled := false
	if _, err := p.Update("client-x", KeyUpdate{Enabled: &enabled}); err == nil {
		t.Error("Update on closed pool should fail")
	}
	// No-field update takes the summary-only path; the summary query fails.
	if _, err := p.Update("client-x", KeyUpdate{}); err == nil {
		t.Error("no-field Update on closed pool should fail via summary")
	}
	if err := p.Delete("client-x"); err == nil {
		t.Error("Delete on closed pool should fail")
	}
	if got := p.List(); got != nil {
		t.Errorf("List on closed pool = %v, want nil", got)
	}
	if got := p.TotalCount(); got != 0 {
		t.Errorf("TotalCount on closed pool = %d, want 0", got)
	}
	if got := p.ActiveCount(); got != 0 {
		t.Errorf("ActiveCount on closed pool = %d, want 0", got)
	}
	if !p.Empty() {
		t.Error("Empty on closed pool should report true (count 0)")
	}
}

func TestPostgresKeyStore_UpdateExpiryAndNoFields(t *testing.T) {
	pool := testPool(t)
	p := NewPostgresKeyStore(pool, auth.NewKeyHasher([]byte("test-pepper"), nil))
	ctx := context.Background()

	created, err := p.Create(ctx, "expiry-client", []string{"reader"}, time.Now().Add(time.Hour).UTC())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ExpiresAt.IsZero() {
		t.Error("Create with expiry should surface a non-zero ExpiresAt")
	}

	// Clear the expiry with a non-nil zero time (SQL NULL path).
	zero := time.Time{}
	s, err := p.Update("expiry-client", KeyUpdate{ExpiresAt: &zero})
	if err != nil {
		t.Fatalf("Update clear expiry: %v", err)
	}
	if !s.ExpiresAt.IsZero() {
		t.Errorf("expiry after clear = %v, want zero", s.ExpiresAt)
	}

	// No-field update returns the current summary unchanged.
	s2, err := p.Update("expiry-client", KeyUpdate{})
	if err != nil {
		t.Fatalf("no-field Update: %v", err)
	}
	if s2.ID != "expiry-client" {
		t.Errorf("summary ID = %q, want expiry-client", s2.ID)
	}
}

func TestPostgresSignerBlacklist_ClosedPoolErrors(t *testing.T) {
	s := NewPostgresSignerBlacklistStore(closedTestPool(t))
	ctx := context.Background()

	if _, err := s.IsBlacklisted(ctx, "0xabc"); err == nil {
		t.Error("IsBlacklisted on closed pool should fail")
	}
	if err := s.Add(ctx, "0xabc", "spam"); err == nil {
		t.Error("Add on closed pool should fail")
	}
	if err := s.Remove(ctx, "0xabc"); err == nil {
		t.Error("Remove on closed pool should fail")
	}
	if _, err := s.List(ctx); err == nil {
		t.Error("List on closed pool should fail")
	}
}

func TestPostgresSignerBlacklist_ListReturnsEntries(t *testing.T) {
	pool := testPool(t)
	s := NewPostgresSignerBlacklistStore(pool)
	ctx := context.Background()

	if err := s.Add(ctx, "0xAAAA000000000000000000000000000000000001", "first"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(ctx, "0xBBBB000000000000000000000000000000000002", "second"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	entries, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Signer != strings.ToLower(e.Signer) {
			t.Errorf("signer %q not normalized to lowercase", e.Signer)
		}
		if e.CreatedAt.IsZero() {
			t.Error("CreatedAt should be populated")
		}
	}
}

func TestPostgresSignerQuota_ClosedPoolErrors(t *testing.T) {
	s := NewPostgresSignerQuotaStore(closedTestPool(t))
	ctx := context.Background()
	ws := WindowStart(time.Now(), 24*time.Hour)

	if _, err := s.Count(ctx, "0xabc", ws); err == nil {
		t.Error("Count on closed pool should fail")
	}
	if err := s.Increment(ctx, "0xabc", ws); err == nil {
		t.Error("Increment on closed pool should fail")
	}
}

func TestPostgresWriteAudit_ClosedPoolErrors(t *testing.T) {
	s := NewPostgresWriteAuditStore(closedTestPool(t))
	ctx := context.Background()

	if err := s.Record(ctx, WriteAuditEntry{Signer: "0xabc", Outcome: "broadcast_ok"}); err == nil {
		t.Error("Record on closed pool should fail")
	}
	if _, err := s.Query(ctx, WriteAuditFilter{}); err == nil {
		t.Error("Query on closed pool should fail")
	}
}

func TestPostgresAdminAudit_ClosedPoolError(t *testing.T) {
	s := NewPostgresAdminAuditStore(closedTestPool(t))
	if err := s.Record(context.Background(), AdminAuditEntry{
		ActorID: "admin", Action: AdminActionKeyCreate, Target: "x", Outcome: "ok",
	}); err == nil {
		t.Error("Record on closed pool should fail")
	}
}

func TestRunMigrations_ClosedPoolError(t *testing.T) {
	err := RunMigrations(context.Background(), closedTestPool(t), testLogger())
	if err == nil {
		t.Fatal("RunMigrations on closed pool should fail")
	}
	if !strings.Contains(err.Error(), "acquire migration conn") {
		t.Errorf("error = %v, want acquire-migration-conn wrap", err)
	}
}

// --- retention purge error / lifecycle paths ---

func TestPurger_SweepAndLog_ErrorAndCancel(t *testing.T) {
	pool := closedTestPool(t)
	cfg := retentionCfg{WriteAudit: time.Hour, PurgeInterval: 10 * time.Millisecond}
	p, err := NewPurger(pool, cfg, "", testLogger())
	if err != nil {
		t.Fatalf("NewPurger: %v", err)
	}

	// Live context: PurgeOnce fails on the closed pool; error is logged.
	p.sweepAndLog(context.Background())

	// Canceled context: failure is attributed to shutdown and stays quiet.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	p.sweepAndLog(canceled)
}

func TestPurger_PerTableErrorBranches(t *testing.T) {
	pool := closedTestPool(t)
	ctx := context.Background()

	cases := []struct {
		name string
		cfg  retentionCfg
		sel  string
	}{
		{"write audit", retentionCfg{WriteAudit: time.Hour, PurgeInterval: time.Hour}, ""},
		{"grant role carve-out", retentionCfg{
			WriteAudit: time.Hour, WriteAuditGrantRole: 2 * time.Hour, PurgeInterval: time.Hour,
		}, grantRoleSel},
		{"signer quota", retentionCfg{SignerQuota: time.Hour, PurgeInterval: time.Hour}, ""},
		{"signer blacklist", retentionCfg{SignerBlacklist: time.Hour, PurgeInterval: time.Hour}, ""},
		{"admin audit", retentionCfg{AdminAudit: time.Hour, PurgeInterval: time.Hour}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPurger(pool, tc.cfg, tc.sel, testLogger())
			if err != nil {
				t.Fatalf("NewPurger: %v", err)
			}
			if _, err := p.PurgeOnce(ctx); err == nil {
				t.Error("PurgeOnce on closed pool should fail")
			}
		})
	}
}

func TestPurger_RunSweepsAndStopsOnCancel(t *testing.T) {
	pool := testPool(t)
	insertWriteAudit(t, pool, "0x11223344", 48*time.Hour)

	cfg := retentionCfg{WriteAudit: 24 * time.Hour, PurgeInterval: 10 * time.Millisecond}
	p, err := NewPurger(pool, cfg, "", testLogger())
	if err != nil {
		t.Fatalf("NewPurger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	// Wait for the immediate first sweep to remove the aged row, then a
	// few more quiet ticks, then stop.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if countRows(t, pool, "SELECT count(*) FROM write_audit") == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond) // let at least one quiet tick pass
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Purger.Run did not stop on context cancellation")
	}
	if n := countRows(t, pool, "SELECT count(*) FROM write_audit"); n != 0 {
		t.Errorf("aged write_audit rows remaining = %d, want 0", n)
	}
}
