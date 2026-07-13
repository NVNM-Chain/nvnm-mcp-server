// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
)

// grantRoleSel is a stand-in for the ABI-derived grantRole selector. The real
// one is computed from the loaded ABI at boot (anchor.Client.MethodSelector);
// its literal value is irrelevant to the purge logic, only that it is the
// discriminator.
const grantRoleSel = "0xdeadbeef"

// insertWriteAudit writes one row with an explicit created_at so a test can
// age rows without waiting. Record() cannot do this: created_at defaults to
// now() in SQL, which is correct for production and useless for a retention
// test.
func insertWriteAudit(
	t *testing.T, pool *pgxpool.Pool, selector string, age time.Duration,
) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO write_audit
		   (signer, to_addr, value_wei, calldata_len, method_selector,
		    tx_hash, outcome, error, created_at)
		 VALUES ('0xabc', '0xdef', '0', 4, $1, '0xhash', 'broadcast_ok', '', $2)`,
		selector, time.Now().UTC().Add(-age))
	if err != nil {
		t.Fatalf("insert write_audit: %v", err)
	}
}

func countRows(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func newTestPurger(
	t *testing.T, pool *pgxpool.Pool, cfg config.RetentionConfig, selector string,
) *Purger {
	t.Helper()
	p, err := NewPurger(pool, cfg, selector, nil)
	if err != nil {
		t.Fatalf("NewPurger: %v", err)
	}
	return p
}

// TestPurge_ZeroWindowRetainsIndefinitely is the most important test here: the
// default configuration must not delete anything. A retention purge that
// silently activates on an operator who never asked for it would destroy audit
// data they are obliged to keep.
func TestPurge_ZeroWindowRetainsIndefinitely(t *testing.T) {
	pool := testPool(t)
	insertWriteAudit(t, pool, "0x1111", 10*365*24*time.Hour) // ten years old

	p := newTestPurger(t, pool, config.RetentionConfig{}, "")
	res, err := p.PurgeOnce(context.Background())
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.Total() != 0 {
		t.Errorf("zero-window config deleted %d rows; must delete none", res.Total())
	}
	if n := countRows(t, pool, `SELECT count(*) FROM write_audit`); n != 1 {
		t.Errorf("write_audit has %d rows, want 1 (nothing purged)", n)
	}
}

func TestPurge_WriteAuditOrdinaryWindow(t *testing.T) {
	pool := testPool(t)
	insertWriteAudit(t, pool, "0x1111", 100*24*time.Hour) // older than 90d
	insertWriteAudit(t, pool, "0x1111", 10*24*time.Hour)  // inside 90d

	p := newTestPurger(t, pool, config.RetentionConfig{
		WriteAudit:    90 * 24 * time.Hour,
		PurgeInterval: time.Hour,
	}, "")

	res, err := p.PurgeOnce(context.Background())
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.WriteAudit != 1 {
		t.Errorf("deleted %d write_audit rows, want 1", res.WriteAudit)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM write_audit`); n != 1 {
		t.Errorf("write_audit has %d rows, want 1 (the in-window row survives)", n)
	}
}

// TestPurge_GrantRoleCarveOut is the whole reason migration 0005 exists:
// Privacy Policy § 8 keeps grantRole broadcasts for a longer administrative
// window than routine anchor writes, and under keyless writes every tx shares
// one destination, so the ABI selector is the only thing that tells them apart.
func TestPurge_GrantRoleCarveOut(t *testing.T) {
	pool := testPool(t)
	// Both older than the 90d ordinary window, both inside the 12mo grantRole
	// window. Only the ordinary row may be purged.
	insertWriteAudit(t, pool, "0x1111", 100*24*time.Hour)     // ordinary, aged out
	insertWriteAudit(t, pool, grantRoleSel, 100*24*time.Hour) // grantRole, still in window
	// A grantRole row past even the long window: this one must go.
	insertWriteAudit(t, pool, grantRoleSel, 400*24*time.Hour)

	p := newTestPurger(t, pool, config.RetentionConfig{
		WriteAudit:          90 * 24 * time.Hour,
		WriteAuditGrantRole: 365 * 24 * time.Hour,
		PurgeInterval:       time.Hour,
	}, grantRoleSel)

	res, err := p.PurgeOnce(context.Background())
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.WriteAudit != 1 {
		t.Errorf("deleted %d ordinary rows, want 1", res.WriteAudit)
	}
	if res.WriteAuditGrantRole != 1 {
		t.Errorf("deleted %d grantRole rows, want 1 (only the >12mo one)", res.WriteAuditGrantRole)
	}
	// The 100-day-old grantRole row must survive: it is past the ordinary
	// window but inside the administrative one. If this fails, the carve-out
	// is broken and we are destroying the admin audit trail early.
	surviving := countRows(t, pool,
		`SELECT count(*) FROM write_audit WHERE method_selector = $1`, grantRoleSel)
	if surviving != 1 {
		t.Errorf("grantRole rows surviving = %d, want 1 (the 100-day-old one)", surviving)
	}
}

// TestPurge_UnknownSelectorTreatedAsOrdinary pins the migration-0005 backfill
// decision: rows written before the selector column existed carry an empty
// selector and fall under the ordinary window. The alternative (treating them
// as possible grantRole calls) would over-retain the whole pre-migration
// backlog.
func TestPurge_UnknownSelectorTreatedAsOrdinary(t *testing.T) {
	pool := testPool(t)
	insertWriteAudit(t, pool, "", 100*24*time.Hour) // pre-migration row, aged out

	p := newTestPurger(t, pool, config.RetentionConfig{
		WriteAudit:          90 * 24 * time.Hour,
		WriteAuditGrantRole: 365 * 24 * time.Hour,
		PurgeInterval:       time.Hour,
	}, grantRoleSel)

	res, err := p.PurgeOnce(context.Background())
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.WriteAudit != 1 {
		t.Errorf("deleted %d ordinary rows, want 1 (unknown selector = ordinary)", res.WriteAudit)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM write_audit`); n != 0 {
		t.Errorf("write_audit has %d rows, want 0", n)
	}
}

// TestPurge_SignerQuotaAgesOnWindowStart covers the finding that drove this
// build: the 500/24h quota is enforced by a read-time timestamp predicate, so
// expired counter rows are never deleted by the quota logic and accumulate one
// row per signer per window forever. Only this purge removes them.
func TestPurge_SignerQuotaAgesOnWindowStart(t *testing.T) {
	pool := testPool(t)
	bg := context.Background()
	_, err := pool.Exec(bg,
		`INSERT INTO signer_quota (signer, window_start, count) VALUES
		   ('0xold', $1, 3),
		   ('0xnew', $2, 1)`,
		time.Now().UTC().Add(-100*24*time.Hour),
		time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("insert signer_quota: %v", err)
	}

	p := newTestPurger(t, pool, config.RetentionConfig{
		SignerQuota:   90 * 24 * time.Hour,
		PurgeInterval: time.Hour,
	}, "")

	res, err := p.PurgeOnce(bg)
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.SignerQuota != 1 {
		t.Errorf("deleted %d signer_quota rows, want 1", res.SignerQuota)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM signer_quota`); n != 1 {
		t.Errorf("signer_quota has %d rows, want 1", n)
	}
}

func TestPurge_AdminAuditAndBlacklist(t *testing.T) {
	pool := testPool(t)
	bg := context.Background()

	if _, err := pool.Exec(bg,
		`INSERT INTO admin_audit (actor_id, action, target, detail, outcome, created_at)
		 VALUES ('alice', 'key.create', 'c1', '', 'ok', $1)`,
		time.Now().UTC().Add(-100*24*time.Hour)); err != nil {
		t.Fatalf("insert admin_audit: %v", err)
	}
	if _, err := pool.Exec(bg,
		`INSERT INTO signer_blacklist (signer, reason, created_at)
		 VALUES ('0xbad', 'spam', $1)`,
		time.Now().UTC().Add(-100*24*time.Hour)); err != nil {
		t.Fatalf("insert signer_blacklist: %v", err)
	}

	p := newTestPurger(t, pool, config.RetentionConfig{
		AdminAudit:      90 * 24 * time.Hour,
		SignerBlacklist: 90 * 24 * time.Hour,
		PurgeInterval:   time.Hour,
	}, "")

	res, err := p.PurgeOnce(bg)
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.AdminAudit != 1 {
		t.Errorf("deleted %d admin_audit rows, want 1", res.AdminAudit)
	}
	if res.SignerBlacklist != 1 {
		t.Errorf("deleted %d signer_blacklist rows, want 1", res.SignerBlacklist)
	}
}

// TestPurge_BlacklistUnsetIsPermanent guards the un-ban footgun: with no
// blacklist window configured, a ban must persist forever. An auto-expiring
// blacklist silently restores an abuser's access.
func TestPurge_BlacklistUnsetIsPermanent(t *testing.T) {
	pool := testPool(t)
	bg := context.Background()
	if _, err := pool.Exec(bg,
		`INSERT INTO signer_blacklist (signer, reason, created_at)
		 VALUES ('0xbad', 'spam', $1)`,
		time.Now().UTC().Add(-10*365*24*time.Hour)); err != nil {
		t.Fatalf("insert signer_blacklist: %v", err)
	}

	// Every other window set; blacklist deliberately zero.
	p := newTestPurger(t, pool, config.RetentionConfig{
		WriteAudit:    1 * time.Hour,
		SignerQuota:   1 * time.Hour,
		AdminAudit:    1 * time.Hour,
		PurgeInterval: time.Hour,
	}, "")

	if _, err := p.PurgeOnce(bg); err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM signer_blacklist`); n != 1 {
		t.Errorf("blacklist row count = %d, want 1: an unset window must never un-ban", n)
	}
}

// TestPurge_BatchingDrainsBeyondOneBatch exercises the deleteBatched loop:
// more rows than purgeBatchSize must all be removed in a single sweep.
func TestPurge_BatchingDrainsBeyondOneBatch(t *testing.T) {
	pool := testPool(t)
	bg := context.Background()
	const rows = purgeBatchSize + 250

	if _, err := pool.Exec(bg,
		`INSERT INTO write_audit
		   (signer, to_addr, value_wei, calldata_len, method_selector,
		    tx_hash, outcome, error, created_at)
		 SELECT '0xabc', '0xdef', '0', 4, '0x1111', '0xhash', 'broadcast_ok', '', $1
		   FROM generate_series(1, $2)`,
		time.Now().UTC().Add(-100*24*time.Hour), rows); err != nil {
		t.Fatalf("bulk insert write_audit: %v", err)
	}

	p := newTestPurger(t, pool, config.RetentionConfig{
		WriteAudit:    90 * 24 * time.Hour,
		PurgeInterval: time.Hour,
	}, "")

	res, err := p.PurgeOnce(bg)
	if err != nil {
		t.Fatalf("PurgeOnce: %v", err)
	}
	if res.WriteAudit != int64(rows) {
		t.Errorf("deleted %d rows, want %d (batching must drain past one batch)",
			res.WriteAudit, rows)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM write_audit`); n != 0 {
		t.Errorf("write_audit has %d rows left, want 0", n)
	}
}

// TestNewPurger_RejectsGrantRoleWindowWithoutSelector: if we cannot tell
// grantRole rows apart but the operator configured a distinct window for them,
// running anyway would purge the entire admin trail on the SHORTER ordinary
// window and report success. Refuse to start instead.
func TestNewPurger_RejectsGrantRoleWindowWithoutSelector(t *testing.T) {
	pool := testPool(t)
	_, err := NewPurger(pool, config.RetentionConfig{
		WriteAudit:          90 * 24 * time.Hour,
		WriteAuditGrantRole: 365 * 24 * time.Hour,
		PurgeInterval:       time.Hour,
	}, "", nil)
	if err == nil {
		t.Fatal("NewPurger accepted a grantRole window with no selector; must refuse")
	}
}

func TestNewPurger_RequiresPool(t *testing.T) {
	if _, err := NewPurger(nil, config.RetentionConfig{}, "", nil); err == nil {
		t.Fatal("NewPurger accepted a nil pool; must refuse")
	}
}

func TestMethodSelectorOf(t *testing.T) {
	tests := []struct {
		name     string
		calldata []byte
		want     string
	}{
		{"empty calldata (bare value transfer)", nil, ""},
		{"short calldata", []byte{0x01, 0x02, 0x03}, ""},
		{"exactly four bytes", []byte{0xde, 0xad, 0xbe, 0xef}, "0xdeadbeef"},
		{"selector plus args", []byte{0x2f, 0x2f, 0xf1, 0x5d, 0x00, 0x11}, "0x2f2ff15d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MethodSelectorOf(tt.calldata); got != tt.want {
				t.Errorf("MethodSelectorOf() = %q, want %q", got, tt.want)
			}
		})
	}
}
