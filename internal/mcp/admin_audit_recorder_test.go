// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// fakeAdminAudit is an in-memory AdminAuditStore for unit tests. It captures
// the last recorded entry and can be made to fail via injectErr so callers
// can assert recordAdminAudit's best-effort error handling.
type fakeAdminAudit struct {
	last      AdminAuditEntry
	recorded  bool
	injectErr error
}

//nolint:gocritic // hugeParam accepted; mirrors PostgresAdminAuditStore.Record
func (f *fakeAdminAudit) Record(_ context.Context, e AdminAuditEntry) error {
	f.last = e
	f.recorded = true
	if f.injectErr != nil {
		return f.injectErr
	}
	return nil
}

// TestRecordAdminAudit_StoreAttached pins that recordAdminAudit resolves the
// actor from context (not the "admin" default -- "alice" here proves real
// propagation, since a broken adminActorFromContext read would still pass
// with the default) and forwards it, along with action/target/detail/
// outcome, to the attached AdminAuditStore.
func TestRecordAdminAudit_StoreAttached(t *testing.T) {
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), nil, 0, testLogger()).WithAdminAuditStore(fa)

	actorCtx := contextWithAdminActor(ctx, "alice")
	a.recordAdminAudit(actorCtx, AdminActionKeyCreate, "bob", "roles=reader", "ok")

	if !fa.recorded {
		t.Fatal("expected Record to be called")
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionKeyCreate,
		Target:  "bob",
		Detail:  "roles=reader",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

// TestRecordAdminAudit_StoreError pins that a Record error is logged at WARN
// and never propagated to the caller -- recordAdminAudit is best-effort and
// must not fail the admin mutation it's auditing.
func TestRecordAdminAudit_StoreError(t *testing.T) {
	fa := &fakeAdminAudit{injectErr: errors.New("boom")}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), nil, 0, logger).WithAdminAuditStore(fa)

	actorCtx := contextWithAdminActor(ctx, "alice")
	a.recordAdminAudit(actorCtx, AdminActionKeyDelete, "bob", "", "ok")

	logStr := buf.String()
	if !strings.Contains(logStr, `"level":"WARN"`) || !strings.Contains(logStr, "boom") {
		t.Fatalf("expected WARN log with error, got: %s", logStr)
	}
}

// TestRecordAdminAudit_NilStoreLogsFallback pins the no-DSN fallback: with no
// AdminAuditStore attached, recordAdminAudit must not panic and must emit an
// attributed INFO slog line carrying actor_id/action/target/outcome instead.
func TestRecordAdminAudit_NilStoreLogsFallback(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), nil, 0, logger)

	actorCtx := contextWithAdminActor(ctx, "alice")
	a.recordAdminAudit(actorCtx, AdminActionKeyUpdate, "bob", "roles=writer", "ok")

	logStr := buf.String()
	for _, want := range []string{
		`"level":"INFO"`, `"actor_id":"alice"`, `"action":"key.update"`,
		`"target":"bob"`, `"outcome":"ok"`,
	} {
		if !strings.Contains(logStr, want) {
			t.Errorf("nil-store fallback log missing %q\nlog: %s", want, logStr)
		}
	}
}
