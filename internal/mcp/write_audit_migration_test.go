// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"testing"
)

func TestWriteAuditMigration_TableExists(t *testing.T) {
	pool := testPool(t) // skips unless NVNM_TEST_PG_DSN is set; runs migrations
	var reg *string
	if err := pool.QueryRow(context.Background(),
		"SELECT to_regclass('write_audit')::text").Scan(&reg); err != nil {
		t.Fatalf("query to_regclass: %v", err)
	}
	if reg == nil || *reg != "write_audit" {
		t.Fatalf("write_audit table not created by migrations; got %v", reg)
	}
}
