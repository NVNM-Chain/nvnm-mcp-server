// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunMigrations_CreatesSchema(t *testing.T) {
	pool := testPool(t) // already runs RunMigrations once
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_name='api_keys'`).Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("api_keys table missing after migrate: n=%d err=%v", n, err)
	}
}

func TestRunMigrations_ConcurrentBootIsSerialized(t *testing.T) {
	// Ensure the schema exists before the concurrent run so testPool's
	// TRUNCATE api_keys does not race with migration creation.
	testPool(t)

	dsn := os.Getenv("NVNM_TEST_PG_DSN")
	// testPool already skipped if DSN is unset; this is belt-and-suspenders.
	if dsn == "" {
		t.Skip("NVNM_TEST_PG_DSN not set; skipping Postgres-backed test")
	}

	// Each goroutine gets its OWN pgxpool, modeling independent replica
	// processes. This prevents pool-exhaustion deadlock: with a shared pool
	// of N conns, 8 goroutines each holding one advisory-lock conn and
	// needing a second for goose would stall on any machine with <16 CPUs
	// (default MaxConns = max(4, NumCPU)). Separate pools also model reality
	// correctly — each real replica is a separate OS process with its own pool.
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := pgxpool.New(context.Background(), dsn)
			if err != nil {
				errs[i] = fmt.Errorf("open pool %d: %w", i, err)
				return
			}
			defer p.Close()
			errs[i] = RunMigrations(context.Background(), p, nil)
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent migrate #%d failed: %v", i, e)
		}
	}
}
