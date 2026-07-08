// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "testing"

func TestPostgresAdminAuditStore_RecordRoundTrip(t *testing.T) {
	pool := testPool(t) // skips unless NVNM_TEST_PG_DSN set; runs migrations
	store := NewPostgresAdminAuditStore(pool)
	if err := store.Record(ctx, AdminAuditEntry{
		ActorID: "alice", Action: AdminActionBlacklistAdd,
		Target: "0xabc", Detail: "reason=spam", Outcome: "ok",
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	var actor, action, target, outcome string
	err := pool.QueryRow(ctx,
		`SELECT actor_id, action, target, outcome FROM admin_audit ORDER BY id DESC LIMIT 1`).
		Scan(&actor, &action, &target, &outcome)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if actor != "alice" || action != "blacklist.add" || target != "0xabc" || outcome != "ok" {
		t.Errorf("got %q/%q/%q/%q", actor, action, target, outcome)
	}
}
