// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// --- stubs ---

// errFailingBackend is the generic failure every failing stub returns.
var errFailingBackend = errors.New("backend exploded")

// failingKeyBackend fails every mutation with a non-sentinel error so
// the admin handlers' 500 branches fire.
type failingKeyBackend struct{}

func (f *failingKeyBackend) Lookup(_ context.Context, _ string) (*KeyEntry, auth.RejectReason) {
	return nil, auth.RejectNotFound
}
func (f *failingKeyBackend) Empty() bool         { return true }
func (f *failingKeyBackend) List() []KeySummary  { return nil }
func (f *failingKeyBackend) ActiveCount() int    { return 0 }
func (f *failingKeyBackend) TotalCount() int     { return 0 }
func (f *failingKeyBackend) Delete(string) error { return errFailingBackend }
func (f *failingKeyBackend) Create(_ context.Context, _ string, _ []string, _ time.Time) (*KeyCreateResult, error) {
	return nil, errFailingBackend
}
func (f *failingKeyBackend) Update(string, KeyUpdate) (*KeySummary, error) {
	return nil, errFailingBackend
}

// mintOKDeleteFailBackend mints keys successfully but fails Delete, to
// drive the approve-rollback-delete-failed branch.
type mintOKDeleteFailBackend struct {
	failingKeyBackend
}

func (m *mintOKDeleteFailBackend) Create(
	_ context.Context, clientID string, roles []string, _ time.Time,
) (*KeyCreateResult, error) {
	res := &KeyCreateResult{KeySummary: KeySummary{ID: clientID, Enabled: true, Roles: roles}}
	res.Key = "minted-raw-key"
	return res, nil
}

// failingEmailSender always fails, driving the email_delivered=false
// warn branches on approve/reject.
type failingEmailSender struct{}

func (f *failingEmailSender) Send(_ context.Context, _, _, _ string) error {
	return errFailingBackend
}

// failingWriteAudit / okWriteAudit drive the /admin/write-audit branches.
type failingWriteAudit struct{}

func (f *failingWriteAudit) Record(_ context.Context, _ WriteAuditEntry) error {
	return errFailingBackend
}
func (f *failingWriteAudit) Query(_ context.Context, _ WriteAuditFilter) ([]WriteAuditEntry, error) {
	return nil, errFailingBackend
}

type okWriteAudit struct {
	gotFilter WriteAuditFilter
}

func (o *okWriteAudit) Record(_ context.Context, _ WriteAuditEntry) error { return nil }
func (o *okWriteAudit) Query(_ context.Context, f WriteAuditFilter) ([]WriteAuditEntry, error) {
	o.gotFilter = f
	return []WriteAuditEntry{{Signer: "0xabc", Outcome: "broadcast_ok"}}, nil
}

// failingBlacklist drives the signer-blacklist 500 branches.
type failingBlacklist struct{}

func (f *failingBlacklist) IsBlacklisted(_ context.Context, _ string) (bool, error) {
	return false, errFailingBackend
}
func (f *failingBlacklist) Add(_ context.Context, _, _ string) error { return errFailingBackend }
func (f *failingBlacklist) Remove(_ context.Context, _ string) error { return errFailingBackend }
func (f *failingBlacklist) List(_ context.Context) ([]BlacklistEntry, error) {
	return nil, errFailingBackend
}

// failingResponseWriter fails every Write so the encode-error logging
// branches fire.
type failingResponseWriter struct {
	header http.Header
	status int
}

func newFailingResponseWriter() *failingResponseWriter {
	return &failingResponseWriter{header: make(http.Header)}
}

func (f *failingResponseWriter) Header() http.Header  { return f.header }
func (f *failingResponseWriter) WriteHeader(code int) { f.status = code }
func (f *failingResponseWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("writer broken")
}

// badSavePendingStore builds a PendingKeyStore whose in-memory items are
// valid but whose save path points into a nonexistent directory, so any
// mutation (Add / Decide) fails at persist time.
func badSavePendingStore(t *testing.T, items ...PendingKeyRequest) *PendingKeyStore {
	t.Helper()
	return &PendingKeyStore{
		path:  filepath.Join(t.TempDir(), "no-such-dir", "pending.json"),
		items: items,
	}
}

func pendingReq(id, status string) PendingKeyRequest {
	return PendingKeyRequest{
		ID:          id,
		Email:       "customer@example.com",
		IntendedUse: "testing",
		Status:      status,
		CreatedAt:   time.Now().UTC(),
	}
}

// startAdminServerWith starts an httptest server around an AdminServer
// built on the given backend, returning both.
func startAdminServerWith(t *testing.T, backend KeyStoreBackend) (*httptest.Server, *AdminServer) {
	t.Helper()
	adminSrv := NewAdminServer(":0", singleAdminKey(testAdminKey), backend, 0, testLogger())
	ts := httptest.NewServer(adminSrv.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, adminSrv
}

// --- lifecycle ---

func TestAdminServer_StartAndClose(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	adminSrv := NewAdminServer("127.0.0.1:0", singleAdminKey(testAdminKey), mks, 0, testLogger())

	done := make(chan error, 1)
	go func() { done <- adminSrv.Start() }()

	// Close after a short delay; Start must return nil on graceful close.
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := adminSrv.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case startErr := <-done:
		if startErr != nil {
			t.Fatalf("Start returned error after graceful close: %v", startErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Close")
	}
}

func TestAdminServer_StartBindError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	adminSrv := NewAdminServer(ln.Addr().String(), singleAdminKey(testAdminKey), mks, 0, testLogger())
	if err := adminSrv.Start(); err == nil {
		t.Fatal("Start on an occupied port should fail")
	}
}

// --- auth / body / handler edge branches ---

func TestAdmin_Auth_WrongScheme(t *testing.T) {
	ts, _ := startAdminTestServer(t)
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/admin/keys", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestAdmin_Create_InvalidJSON(t *testing.T) {
	ts, _ := startAdminTestServer(t)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/admin/keys", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testAdminKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdmin_Update_InvalidJSON(t *testing.T) {
	ts, mks := startAdminTestServer(t)
	if _, err := mks.Create(context.Background(), "c1", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/admin/keys/c1", strings.NewReader("{oops"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testAdminKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestAdmin_CreateUpdateDelete_BackendFailure(t *testing.T) {
	ts, _ := startAdminServerWith(t, &failingKeyBackend{})

	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey,
		map[string]any{"client_id": "c1", "roles": []string{"reader"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("create status = %d, want 500", resp.StatusCode)
	}

	enabled := false
	resp2 := adminRequest(t, ts, "PATCH", "/admin/keys/c1", testAdminKey,
		map[string]any{"enabled": &enabled})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Errorf("update status = %d, want 500", resp2.StatusCode)
	}

	resp3 := adminRequest(t, ts, "DELETE", "/admin/keys/c1", testAdminKey, nil)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusInternalServerError {
		t.Errorf("delete status = %d, want 500", resp3.StatusCode)
	}
}

// TestAdmin_MissingPathValues drives the "" PathValue guards directly:
// the mux never routes an empty {id}, so the guard is only reachable by
// calling the handler without a path value.
func TestAdmin_MissingPathValues(t *testing.T) {
	_, adminSrv := startAdminServerWith(t, &failingKeyBackend{})
	adminSrv.pendingStore = badSavePendingStore(t)

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"update", adminSrv.handleUpdate},
		{"delete", adminSrv.handleDelete},
		{"approve pending", adminSrv.handleApprovePending},
		{"reject pending", adminSrv.handleRejectPending},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
		})
	}
}

// TestAdmin_EncodeFailuresLogged drives every encode-error branch with a
// ResponseWriter whose Write always fails. No assertion beyond "does not
// panic": the branch's contract is logging.
func TestAdmin_EncodeFailuresLogged(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, adminSrv := startAdminServerWith(t, mks)
	adminSrv.writeAudit = &okWriteAudit{}
	adminSrv.pendingStore = &PendingKeyStore{
		path:  filepath.Join(t.TempDir(), "pending.json"),
		items: []PendingKeyRequest{pendingReq("req-1", PendingStatusPending), pendingReq("req-2", PendingStatusPending)},
	}

	if _, err := mks.Create(context.Background(), "c1", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// handleCreate success + encode failure.
	req := httptest.NewRequest(http.MethodPost, "/admin/keys",
		strings.NewReader(`{"client_id":"c2","roles":["reader"]}`))
	adminSrv.handleCreate(newFailingResponseWriter(), req)

	// handleList encode failure.
	adminSrv.handleList(newFailingResponseWriter(), httptest.NewRequest(http.MethodGet, "/admin/keys", nil))

	// handleUpdate success + encode failure.
	upd := httptest.NewRequest(http.MethodPatch, "/admin/keys/c1", strings.NewReader(`{"enabled":true}`))
	upd.SetPathValue("id", "c1")
	adminSrv.handleUpdate(newFailingResponseWriter(), upd)

	// writeError encode failure.
	adminSrv.writeError(newFailingResponseWriter(), http.StatusBadRequest, "boom")

	// pending list encode failure.
	adminSrv.handleListPending(newFailingResponseWriter(),
		httptest.NewRequest(http.MethodGet, "/admin/keys/pending", nil))

	// approve success + encode failure.
	appr := httptest.NewRequest(http.MethodPost, "/admin/keys/pending/req-1/approve", nil)
	appr.SetPathValue("id", "req-1")
	adminSrv.handleApprovePending(newFailingResponseWriter(), appr)

	// reject success + encode failure.
	rej := httptest.NewRequest(http.MethodPost, "/admin/keys/pending/req-2/reject", nil)
	rej.SetPathValue("id", "req-2")
	adminSrv.handleRejectPending(newFailingResponseWriter(), rej)

	// write-audit success + encode failure.
	adminSrv.handleWriteAudit(newFailingResponseWriter(),
		httptest.NewRequest(http.MethodGet, "/admin/write-audit", nil))

	// signer-blacklist list encode failure (empty store list succeeds).
	adminSrv.blacklist = &emptyBlacklist{}
	adminSrv.handleListBlacklist(newFailingResponseWriter(),
		httptest.NewRequest(http.MethodGet, "/admin/signer-blacklist", nil))
}

// emptyBlacklist succeeds with no entries; used for the encode-failure path.
type emptyBlacklist struct{}

func (e *emptyBlacklist) IsBlacklisted(_ context.Context, _ string) (bool, error) { return false, nil }
func (e *emptyBlacklist) Add(_ context.Context, _, _ string) error                { return nil }
func (e *emptyBlacklist) Remove(_ context.Context, _ string) error                { return nil }
func (e *emptyBlacklist) List(_ context.Context) ([]BlacklistEntry, error)        { return nil, nil }

// --- pending approve / reject error branches ---

func TestAdmin_ApprovePending_MintFailure(t *testing.T) {
	ts, adminSrv := startAdminServerWith(t, &failingKeyBackend{})
	adminSrv.pendingStore = &PendingKeyStore{
		path:  filepath.Join(t.TempDir(), "pending.json"),
		items: []PendingKeyRequest{pendingReq("req-1", PendingStatusPending)},
	}

	resp := adminRequest(t, ts, "POST", "/admin/keys/pending/req-1/approve", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestAdmin_ApprovePending_DecidePersistFailureRollsBack(t *testing.T) {
	// Key mint succeeds, Decide fails at persist time, rollback Delete
	// also fails -- covering the deepest warn branch.
	ts, adminSrv := startAdminServerWith(t, &mintOKDeleteFailBackend{})
	adminSrv.pendingStore = badSavePendingStore(t, pendingReq("req-1", PendingStatusPending))

	resp := adminRequest(t, ts, "POST", "/admin/keys/pending/req-1/approve", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	// The in-memory store must have rolled the request back to pending.
	got, ok := adminSrv.pendingStore.Get("req-1")
	if !ok || got.Status != PendingStatusPending {
		t.Errorf("request after failed decide = (%+v, %v), want pending", got, ok)
	}
}

func TestAdmin_ApprovePending_EmailFailureIsPartialSuccess(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts, adminSrv := startAdminServerWith(t, mks)
	adminSrv.pendingStore = &PendingKeyStore{
		path:  filepath.Join(t.TempDir(), "pending.json"),
		items: []PendingKeyRequest{pendingReq("req-1", PendingStatusPending)},
	}
	adminSrv.email = &failingEmailSender{}

	resp := adminRequest(t, ts, "POST", "/admin/keys/pending/req-1/approve", testAdminKey, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body approveResponse
	decodeJSON(t, resp, &body)
	if body.EmailDelivered {
		t.Error("email_delivered should be false when the sender fails")
	}
	if body.APIKey == "" {
		t.Error("api_key must still be returned for manual delivery")
	}
}

func TestAdmin_RejectPending_Branches(t *testing.T) {
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts, adminSrv := startAdminServerWith(t, mks)
	adminSrv.pendingStore = &PendingKeyStore{
		path: filepath.Join(t.TempDir(), "pending.json"),
		items: []PendingKeyRequest{
			pendingReq("req-open", PendingStatusPending),
			pendingReq("req-done", PendingStatusApproved),
		},
	}
	adminSrv.email = &failingEmailSender{}

	// Unknown id -> 404.
	resp := adminRequest(t, ts, "POST", "/admin/keys/pending/nope/reject", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown id status = %d, want 404", resp.StatusCode)
	}

	// Already decided -> 409.
	resp = adminRequest(t, ts, "POST", "/admin/keys/pending/req-done/reject", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("already-decided status = %d, want 409", resp.StatusCode)
	}

	// Malformed body -> 400.
	req, reqErr := http.NewRequest(http.MethodPost, ts.URL+"/admin/keys/pending/req-open/reject",
		strings.NewReader("{broken"))
	if reqErr != nil {
		t.Fatal(reqErr)
	}
	req.Header.Set("Authorization", "Bearer "+testAdminKey)
	badResp, doErr := http.DefaultClient.Do(req)
	if doErr != nil {
		t.Fatal(doErr)
	}
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", badResp.StatusCode)
	}

	// Valid reject with reason; failing email sender -> 200, delivered=false.
	resp = adminRequest(t, ts, "POST", "/admin/keys/pending/req-open/reject", testAdminKey,
		map[string]string{"reason": "not a fit"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reject status = %d, want 200", resp.StatusCode)
	}
	var body rejectResponse
	decodeJSON(t, resp, &body)
	if body.EmailDelivered {
		t.Error("email_delivered should be false when the sender fails")
	}
	if body.Status != PendingStatusRejected {
		t.Errorf("status = %q, want rejected", body.Status)
	}
}

func TestAdmin_RejectPending_PersistFailure(t *testing.T) {
	ts, adminSrv := startAdminServerWith(t, &failingKeyBackend{})
	adminSrv.pendingStore = badSavePendingStore(t, pendingReq("req-1", PendingStatusPending))

	resp := adminRequest(t, ts, "POST", "/admin/keys/pending/req-1/reject", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestAdmin_RejectPending_StoreNotConfigured(t *testing.T) {
	ts, _ := startAdminTestServer(t)
	resp := adminRequest(t, ts, "POST", "/admin/keys/pending/x/reject", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// --- write-audit endpoint branches ---

func TestAdmin_WriteAudit_QueryParamsAndFailure(t *testing.T) {
	ok := &okWriteAudit{}
	ts, adminSrv := startAdminServerWith(t, &failingKeyBackend{})
	adminSrv.writeAudit = ok

	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	resp := adminRequest(t, ts, "GET",
		"/admin/write-audit?signer=0xAbC&from="+from+"&limit=7", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ok.gotFilter.Limit != 7 || ok.gotFilter.From == nil || ok.gotFilter.Signer != "0xAbC" {
		t.Errorf("filter = %+v, want limit=7 from set signer=0xAbC", ok.gotFilter)
	}

	// Malformed from -> 400.
	resp = adminRequest(t, ts, "GET", "/admin/write-audit?from=yesterday", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad from status = %d, want 400", resp.StatusCode)
	}

	// Malformed limit -> 400.
	resp = adminRequest(t, ts, "GET", "/admin/write-audit?limit=-3", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad limit status = %d, want 400", resp.StatusCode)
	}

	// Store failure -> 500.
	adminSrv.writeAudit = &failingWriteAudit{}
	resp = adminRequest(t, ts, "GET", "/admin/write-audit", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("failing store status = %d, want 500", resp.StatusCode)
	}
}

// --- signer-blacklist endpoint failure branches ---

func TestAdmin_SignerBlacklist_StoreFailures(t *testing.T) {
	ts, adminSrv := startAdminServerWith(t, &failingKeyBackend{})
	adminSrv.blacklist = &failingBlacklist{}

	resp := adminRequest(t, ts, "GET", "/admin/signer-blacklist", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("list status = %d, want 500", resp.StatusCode)
	}

	resp = adminRequest(t, ts, "POST", "/admin/signer-blacklist", testAdminKey,
		map[string]string{"signer": testAddr, "reason": "spam"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("add status = %d, want 500", resp.StatusCode)
	}

	resp = adminRequest(t, ts, "DELETE", "/admin/signer-blacklist/"+testAddr, testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("remove status = %d, want 500", resp.StatusCode)
	}
}
