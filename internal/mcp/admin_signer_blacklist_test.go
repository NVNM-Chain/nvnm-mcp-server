// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAdminBlacklist is a minimal in-memory SignerBlacklistStore for handler
// tests. It lowercases signer keys on every path, mirroring
// PostgresSignerBlacklistStore's normalization, since the handlers rely on
// the store (not themselves) to normalize case. When err is non-nil, every
// method returns it instead of touching banned, so tests can exercise the
// handlers' store-error 500 paths.
type fakeAdminBlacklist struct {
	banned map[string]BlacklistEntry
	err    error
}

func (f *fakeAdminBlacklist) IsBlacklisted(_ context.Context, signer string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	_, ok := f.banned[strings.ToLower(signer)]
	return ok, nil
}

func (f *fakeAdminBlacklist) Add(_ context.Context, signer, reason string) error {
	if f.err != nil {
		return f.err
	}
	lower := strings.ToLower(signer)
	f.banned[lower] = BlacklistEntry{Signer: lower, Reason: reason}
	return nil
}

func (f *fakeAdminBlacklist) Remove(_ context.Context, signer string) error {
	if f.err != nil {
		return f.err
	}
	delete(f.banned, strings.ToLower(signer))
	return nil
}

func (f *fakeAdminBlacklist) List(_ context.Context) ([]BlacklistEntry, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]BlacklistEntry, 0, len(f.banned))
	for _, e := range f.banned {
		out = append(out, e)
	}
	return out, nil
}

func TestAdmin_SignerBlacklist_RoundTrip(t *testing.T) {
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{}}
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger()).WithSignerBlacklistStore(store)

	// POST add.
	body := `{"signer":"0xAbc0000000000000000000000000000000000001","reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/signer-blacklist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	a.handleAddBlacklist(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("POST = %d, body %s", rec.Code, rec.Body.String())
	}

	// GET list contains it (store normalizes to lowercase).
	req = httptest.NewRequest(http.MethodGet, "/admin/signer-blacklist", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec = httptest.NewRecorder()
	a.handleListBlacklist(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d, body %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "0xabc0000000000000000000000000000000000001") {
		t.Fatalf("list missing signer: %s", rec.Body.String())
	}

	// DELETE removes it.
	req = httptest.NewRequest(http.MethodDelete, "/admin/signer-blacklist/0xabc0000000000000000000000000000000000001", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-secret")
	req.SetPathValue("signer", "0xabc0000000000000000000000000000000000001")
	rec = httptest.NewRecorder()
	a.handleDeleteBlacklist(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestAdmin_SignerBlacklist_InvalidSigner400(t *testing.T) {
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{}}
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger()).WithSignerBlacklistStore(store)

	body := `{"signer":"not-an-address","reason":"spam"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/signer-blacklist", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	a.handleAddBlacklist(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid signer", rec.Code)
	}
}

func TestAdmin_SignerBlacklist_InvalidJSON400(t *testing.T) {
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{}}
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger()).WithSignerBlacklistStore(store)

	req := httptest.NewRequest(http.MethodPost, "/admin/signer-blacklist", strings.NewReader("not-json"))
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	a.handleAddBlacklist(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for malformed JSON body", rec.Code)
	}
}

func TestAdmin_SignerBlacklist_DeleteMissingSigner400(t *testing.T) {
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{}}
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger()).WithSignerBlacklistStore(store)

	req := httptest.NewRequest(http.MethodDelete, "/admin/signer-blacklist/", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	a.handleDeleteBlacklist(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing signer path value", rec.Code)
	}
}

func TestAdmin_SignerBlacklist_StoreError500(t *testing.T) {
	store := &fakeAdminBlacklist{banned: map[string]BlacklistEntry{}, err: errors.New("db unavailable")}
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger()).WithSignerBlacklistStore(store)

	req := httptest.NewRequest(http.MethodGet, "/admin/signer-blacklist", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	a.handleListBlacklist(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when store returns an error", rec.Code)
	}
}

func TestAdmin_SignerBlacklist_NilStore404(t *testing.T) {
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger())

	tests := []struct {
		name    string
		method  string
		path    string
		handler http.HandlerFunc
	}{
		{"list", http.MethodGet, "/admin/signer-blacklist", a.handleListBlacklist},
		{"add", http.MethodPost, "/admin/signer-blacklist", a.handleAddBlacklist},
		{"delete", http.MethodDelete, "/admin/signer-blacklist/0xabc0000000000000000000000000000000000001", a.handleDeleteBlacklist},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			req.Header.Set("Authorization", "Bearer admin-secret")
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 when store unconfigured", rec.Code)
			}
		})
	}
}
