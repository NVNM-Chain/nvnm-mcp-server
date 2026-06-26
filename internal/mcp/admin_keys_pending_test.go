// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// fakeEmailSender records the to / subject / body of every Send call
// in memory so tests can assert delivery shape without standing up an
// SMTP server. Concurrency-safe; mirrors the EmailSender contract.
type fakeEmailSender struct {
	mu      sync.Mutex
	sent    []sentEmail
	sendErr error
}

type sentEmail struct {
	To      string
	Subject string
	Body    string
}

func (f *fakeEmailSender) Send(_ context.Context, to, subject, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sendErr != nil {
		return f.sendErr
	}
	f.sent = append(f.sent, sentEmail{To: to, Subject: subject, Body: body})
	return nil
}

// last returns the most-recently-sent email, or the zero value if no
// sends have happened. Tests already guard with count() before calling
// last(), so a bool return is dead surface.
func (f *fakeEmailSender) last() sentEmail {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		return sentEmail{}
	}
	return f.sent[len(f.sent)-1]
}

func (f *fakeEmailSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// startPendingAdminTestServer mirrors startAdminTestServer in admin_test.go
// but wires a real PendingKeyStore + fake email sender so the pending
// endpoints exercise the full Decide -> issue-key -> email flow.
func startPendingAdminTestServer(t *testing.T) (*httptest.Server, *ManagedKeyStore, *PendingKeyStore, *fakeEmailSender) {
	t.Helper()

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
	email := &fakeEmailSender{}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSrv := NewAdminServer(":0", testAdminKey, mks, 0, logger).
		WithPendingKeyStore(ps, email)

	ts := httptest.NewServer(adminSrv.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, mks, ps, email
}

// TestAdminPending_ListEmpty pins that the list endpoint returns an
// empty JSON array (not null) when no requests have been submitted --
// the JSON contract matters for client-side .length checks.
func TestAdminPending_ListEmpty(t *testing.T) {
	t.Parallel()
	ts, _, _, _ := startPendingAdminTestServer(t)

	resp := adminRequest(t, ts, http.MethodGet, "/admin/keys/pending", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out []pendingItemSummary
	decodeJSON(t, resp, &out)
	if out == nil || len(out) != 0 {
		t.Errorf("list = %+v, want empty non-nil slice", out)
	}
}

func TestAdminPending_ListReturnsAllStatuses(t *testing.T) {
	t.Parallel()
	ts, _, ps, _ := startPendingAdminTestServer(t)
	_, _ = ps.Add("a@example.test", "", "uc1", "")
	r2, _ := ps.Add("b@example.test", "", "uc2", "")
	r3, _ := ps.Add("c@example.test", "", "uc3", "")
	_, _ = ps.Decide(r2.ID, PendingStatusApproved, "admin-x", "key-1")
	_, _ = ps.Decide(r3.ID, PendingStatusRejected, "admin-x", "")

	resp := adminRequest(t, ts, http.MethodGet, "/admin/keys/pending", testAdminKey, nil)
	defer resp.Body.Close()
	var out []pendingItemSummary
	decodeJSON(t, resp, &out)
	if len(out) != 3 {
		t.Fatalf("len(list) = %d, want 3", len(out))
	}
	// Status counts must include both decided and pending rows --
	// reviewer audit view needs full history, not just current queue.
	counts := map[string]int{}
	for _, item := range out {
		counts[item.Status]++
	}
	for s, want := range map[string]int{
		PendingStatusPending:  1,
		PendingStatusApproved: 1,
		PendingStatusRejected: 1,
	} {
		if counts[s] != want {
			t.Errorf("count[%q] = %d, want %d", s, counts[s], want)
		}
	}
}

func TestAdminPending_Approve_Success(t *testing.T) {
	t.Parallel()
	ts, mks, ps, email := startPendingAdminTestServer(t)
	req, _ := ps.Add("a@example.test", "Acme", "use case", "10.0.0.1")

	resp := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/"+req.ID+"/approve", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out approveResponse
	decodeJSON(t, resp, &out)
	if out.Status != PendingStatusApproved {
		t.Errorf("Status = %q, want %q", out.Status, PendingStatusApproved)
	}
	if out.APIKey == "" {
		t.Error("APIKey empty in response")
	}
	if !out.EmailDelivered {
		t.Error("EmailDelivered = false; want true with fake sender")
	}

	// Store mutation
	stored, ok := ps.Get(req.ID)
	if !ok || stored.Status != PendingStatusApproved {
		t.Errorf("store status after approve = %+v", stored)
	}
	if stored.KeyID == "" {
		t.Error("stored KeyID empty after approve")
	}

	// Key issued in ManagedKeyStore
	if _, r := mks.Lookup(context.Background(), out.APIKey); r != auth.RejectNone {
		t.Error("issued key not found via Lookup")
	}

	// Email assertions
	if email.count() != 1 {
		t.Fatalf("email count = %d, want 1", email.count())
	}
	last := email.last()
	if last.To != "a@example.test" {
		t.Errorf("email.To = %q", last.To)
	}
	if last.Subject == "" || last.Body == "" {
		t.Error("email subject/body empty")
	}
}

func TestAdminPending_Approve_DoubleFails(t *testing.T) {
	t.Parallel()
	ts, _, ps, _ := startPendingAdminTestServer(t)
	req, _ := ps.Add("a@example.test", "", "use case", "")

	first := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/"+req.ID+"/approve", testAdminKey, nil)
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first approve status = %d", first.StatusCode)
	}

	second := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/"+req.ID+"/approve", testAdminKey, nil)
	defer second.Body.Close()
	// Both the AdminServer's Get-before-Decide pre-check (already
	// decided) and the Decide guard surface as 409 to the caller.
	if second.StatusCode != http.StatusConflict {
		t.Errorf("second approve status = %d, want 409", second.StatusCode)
	}
}

func TestAdminPending_Approve_NotFound(t *testing.T) {
	t.Parallel()
	ts, _, _, _ := startPendingAdminTestServer(t)
	resp := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/nonexistent/approve", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestAdminPending_Reject_Success(t *testing.T) {
	t.Parallel()
	ts, _, ps, email := startPendingAdminTestServer(t)
	req, _ := ps.Add("a@example.test", "", "use case", "")

	resp := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/"+req.ID+"/reject", testAdminKey,
		map[string]string{"reason": "applicant not eligible for beta"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out rejectResponse
	decodeJSON(t, resp, &out)
	if out.Status != PendingStatusRejected {
		t.Errorf("Status = %q, want %q", out.Status, PendingStatusRejected)
	}
	if !out.EmailDelivered {
		t.Error("EmailDelivered false; want true")
	}

	if email.count() != 1 {
		t.Errorf("email count = %d, want 1", email.count())
	}
	if last := email.last(); last.To != "a@example.test" {
		t.Errorf("email.To = %q", last.To)
	}
}

func TestAdminPending_Reject_NoBody(t *testing.T) {
	t.Parallel()
	ts, _, ps, _ := startPendingAdminTestServer(t)
	req, _ := ps.Add("a@example.test", "", "use case", "")

	resp := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/"+req.ID+"/reject", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (empty body should be accepted)", resp.StatusCode)
	}
}

// TestAdminPending_NotConfigured503 pins that without WithPendingKeyStore
// the routes still respond, but with 503 -- operators don't see a
// confusing 404. Uses the bare AdminServer constructor without the
// pending wiring.
func TestAdminPending_NotConfigured503(t *testing.T) {
	t.Parallel()
	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSrv := NewAdminServer(":0", testAdminKey, mks, 0, logger) // no WithPendingKeyStore

	ts := httptest.NewServer(adminSrv.srv.Handler)
	defer ts.Close()

	resp := adminRequest(t, ts, http.MethodGet, "/admin/keys/pending", testAdminKey, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("list status = %d, want 503", resp.StatusCode)
	}

	resp2 := adminRequest(t, ts, http.MethodPost,
		"/admin/keys/pending/anything/approve", testAdminKey, nil)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("approve status = %d, want 503", resp2.StatusCode)
	}
}
