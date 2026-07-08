// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// fakeAdminAudit is an in-memory AdminAuditStore for unit tests. It captures
// the last recorded entry and can be made to fail via injectErr so callers
// can assert recordAdminAudit's best-effort error handling. calls counts
// every Record invocation so handler tests can assert exactly one audit row
// was written (not zero, not a duplicate).
type fakeAdminAudit struct {
	last      AdminAuditEntry
	recorded  bool
	calls     int
	injectErr error
}

//nolint:gocritic // hugeParam accepted; mirrors PostgresAdminAuditStore.Record
func (f *fakeAdminAudit) Record(_ context.Context, e AdminAuditEntry) error {
	f.last = e
	f.recorded = true
	f.calls++
	if f.injectErr != nil {
		return f.injectErr
	}
	return nil
}

// fakeErrorKeyStore is a minimal KeyStoreBackend whose Create/Update/Delete
// return a configurable non-sentinel error, letting tests drive the admin
// handlers' internal-error (500) branches without needing a real backend
// failure (e.g. a read-only file).
type fakeErrorKeyStore struct {
	createErr error
	updateErr error
	deleteErr error
}

func (f *fakeErrorKeyStore) Lookup(_ context.Context, _ string) (*KeyEntry, auth.RejectReason) {
	return nil, auth.RejectNone
}
func (f *fakeErrorKeyStore) Empty() bool        { return true }
func (f *fakeErrorKeyStore) List() []KeySummary { return nil }
func (f *fakeErrorKeyStore) Create(_ context.Context, _ string, _ []string, _ time.Time) (*KeyCreateResult, error) {
	return nil, f.createErr
}
func (f *fakeErrorKeyStore) Update(_ string, _ KeyUpdate) (*KeySummary, error) {
	return nil, f.updateErr
}
func (f *fakeErrorKeyStore) Delete(_ string) error { return f.deleteErr }
func (f *fakeErrorKeyStore) ActiveCount() int      { return 0 }
func (f *fakeErrorKeyStore) TotalCount() int       { return 0 }

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

// --- Task 6: audit wiring in the 7 admin mutation handlers ---
//
// Each test drives a handler directly (mirroring admin_signer_blacklist_test.go's
// direct-call pattern) with a fakeAdminAudit attached and a
// contextWithAdminActor-wrapped request context, then asserts exactly one
// audit row with the expected Action/Target/Detail/Outcome. The end-to-end
// actor-attribution test at the bottom instead drives the real adminAuth
// middleware chain to prove the actor propagates all the way through, not
// just via a hand-injected context.

func TestAdminAudit_HandleCreate_Success(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), mks, 0, testLogger()).WithAdminAuditStore(fa)

	body := `{"client_id":"audit-client","roles":["reader","writer"]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", strings.NewReader(body))
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleCreate(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionKeyCreate,
		Target:  "audit-client",
		Detail:  "roles=reader,writer",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
	// Guard against the minted key ever leaking into the audit detail.
	var result KeyCreateResult
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Key == "" {
		t.Fatal("expected raw key in create response")
	}
	if strings.Contains(fa.last.Detail, result.Key) {
		t.Fatal("audit detail must not contain the minted API key")
	}
}

// TestAdminAudit_HandleCreate_Error pins the one representative error-path
// audit: a mutation was attempted (a.keys.Create called) and failed for a
// reason other than ErrClientExists (which is a 409, not a 500, and does
// not get audited since Create was rejected before any mutation).
func TestAdminAudit_HandleCreate_Error(t *testing.T) {
	fakeStore := &fakeErrorKeyStore{createErr: errors.New("boom")}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), fakeStore, 0, testLogger()).WithAdminAuditStore(fa)

	body := `{"client_id":"err-client","roles":["reader"]}`
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", strings.NewReader(body))
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleCreate(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionKeyCreate,
		Target:  "err-client",
		Detail:  "",
		Outcome: "error",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

func TestAdminAudit_HandleUpdate_Success(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "upd-client", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), mks, 0, testLogger()).WithAdminAuditStore(fa)

	body := `{"enabled":false,"roles":["writer"]}`
	req := httptest.NewRequest(http.MethodPatch, "/admin/keys/upd-client", strings.NewReader(body))
	req.SetPathValue("id", "upd-client")
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleUpdate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionKeyUpdate,
		Target:  "upd-client",
		Detail:  "fields=enabled,roles",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

func TestAdminAudit_HandleDelete_Success(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "del-client", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), mks, 0, testLogger()).WithAdminAuditStore(fa)

	req := httptest.NewRequest(http.MethodDelete, "/admin/keys/del-client", http.NoBody)
	req.SetPathValue("id", "del-client")
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleDelete(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionKeyDelete,
		Target:  "del-client",
		Detail:  "",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

func TestAdminAudit_HandleAddBlacklist_Success(t *testing.T) {
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{}}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), nil, 0, testLogger()).
		WithSignerBlacklistStore(store).WithAdminAuditStore(fa)

	signer := "0xAbc0000000000000000000000000000000000001"
	body := `{"signer":"` + signer + `","reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/signer-blacklist", strings.NewReader(body))
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleAddBlacklist(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionBlacklistAdd,
		Target:  signer,
		Detail:  "reason=spam",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

func TestAdminAudit_HandleDeleteBlacklist_Success(t *testing.T) {
	signer := "0xabc0000000000000000000000000000000000001"
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{signer: {Signer: signer}}}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), nil, 0, testLogger()).
		WithSignerBlacklistStore(store).WithAdminAuditStore(fa)

	req := httptest.NewRequest(http.MethodDelete, "/admin/signer-blacklist/"+signer, http.NoBody)
	req.SetPathValue("signer", signer)
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleDeleteBlacklist(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionBlacklistRemove,
		Target:  signer,
		Detail:  "",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

func TestAdminAudit_HandleApprovePending_Success(t *testing.T) {
	keysPath := tempKeysFile(t)
	mks, err := NewManagedKeyStore(keysPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	pendingPath := filepath.Join(t.TempDir(), "keys_pending.json")
	ps, err := NewPendingKeyStore(pendingPath)
	if err != nil {
		t.Fatal(err)
	}
	preq, err := ps.Add("a@example.test", "Acme", "use case", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), mks, 0, testLogger()).
		WithPendingKeyStore(ps, nil).WithAdminAuditStore(fa)

	req := httptest.NewRequest(http.MethodPost, "/admin/keys/pending/"+preq.ID+"/approve", http.NoBody)
	req.SetPathValue("id", preq.ID)
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleApprovePending(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	if fa.last.ActorID != "alice" || fa.last.Action != AdminActionPendingApprove ||
		fa.last.Target != preq.ID || fa.last.Outcome != "ok" {
		t.Fatalf("recorded entry = %+v", fa.last)
	}
	if !strings.Contains(fa.last.Detail, "roles=reader") {
		t.Fatalf("detail missing roles context: %q", fa.last.Detail)
	}

	// Guard against the minted key ever leaking into the audit detail.
	var out approveResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.APIKey == "" {
		t.Fatal("expected minted key in approve response")
	}
	if strings.Contains(fa.last.Detail, out.APIKey) {
		t.Fatal("audit detail must not contain the minted API key")
	}
}

func TestAdminAudit_HandleRejectPending_Success(t *testing.T) {
	pendingPath := filepath.Join(t.TempDir(), "keys_pending.json")
	ps, err := NewPendingKeyStore(pendingPath)
	if err != nil {
		t.Fatal(err)
	}
	preq, err := ps.Add("a@example.test", "", "use case", "")
	if err != nil {
		t.Fatal(err)
	}

	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", singleAdminKey("admin-secret"), nil, 0, testLogger()).
		WithPendingKeyStore(ps, nil).WithAdminAuditStore(fa)

	body := `{"reason":"not eligible"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/keys/pending/"+preq.ID+"/reject", strings.NewReader(body))
	req.SetPathValue("id", preq.ID)
	req = req.WithContext(contextWithAdminActor(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	a.handleRejectPending(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	want := AdminAuditEntry{
		ActorID: "alice",
		Action:  AdminActionPendingReject,
		Target:  preq.ID,
		Detail:  "reason=not eligible",
		Outcome: "ok",
	}
	if fa.last != want {
		t.Fatalf("recorded entry = %+v, want %+v", fa.last, want)
	}
}

// TestAdminAudit_EndToEnd_ActorAttribution proves the actor propagates
// end-to-end through the real adminAuth middleware -> handler ->
// recordAdminAudit, not just via a hand-injected context (all the tests
// above bypass adminAuth by calling the handler directly). Two admin
// identities are registered; the request authenticates as "alice", and
// adminActorFromContext's default fallback is "admin" -- so only a
// genuine propagation bug would make this test see "admin" instead of
// "alice".
func TestAdminAudit_EndToEnd_ActorAttribution(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	identity := map[[32]byte]string{
		sha256.Sum256([]byte("kalice")): "alice",
		sha256.Sum256([]byte("kadmin")): "admin",
	}
	fa := &fakeAdminAudit{}
	a := NewAdminServer(":0", identity, mks, 0, testLogger()).WithAdminAuditStore(fa)

	ts := httptest.NewServer(a.srv.Handler)
	defer ts.Close()

	body := `{"client_id":"e2e-client","roles":["reader"]}`
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/keys", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer kalice")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 201; body=%s", resp.StatusCode, b)
	}
	if fa.calls != 1 {
		t.Fatalf("audit calls = %d, want exactly 1", fa.calls)
	}
	if fa.last.ActorID != "alice" {
		t.Fatalf(`ActorID = %q, want "alice" -- proves the actor resolved by adminAuth `+
			`from the "kalice" bearer propagated through the handler to recordAdminAudit, `+
			`not the adminActorFromContext "admin" default`, fa.last.ActorID)
	}
}
