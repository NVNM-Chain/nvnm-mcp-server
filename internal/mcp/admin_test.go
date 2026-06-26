// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

const testAdminKey = "admin-secret-key-for-testing"

func startAdminTestServer(t *testing.T) (*httptest.Server, *ManagedKeyStore) {
	t.Helper()

	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSrv := NewAdminServer(":0", testAdminKey, mks, 0, logger)

	ts := httptest.NewServer(adminSrv.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, mks
}

func adminRequest(t *testing.T, ts *httptest.Server, method, path, token string, body interface{}) *http.Response {
	t.Helper()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, ts.URL+path, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
}

// --- Auth Tests ---

func TestAdmin_Auth_MissingToken(t *testing.T) {
	ts, _ := startAdminTestServer(t)
	resp := adminRequest(t, ts, "GET", "/admin/keys", "", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", resp.StatusCode)
	}
}

func TestAdmin_Auth_InvalidToken(t *testing.T) {
	ts, _ := startAdminTestServer(t)
	resp := adminRequest(t, ts, "GET", "/admin/keys", "wrong-key", nil)
	defer resp.Body.Close()
	// 401 per RFC 7235: an invalid bearer is an authentication failure,
	// not an authorization failure. The caller must (re)authenticate.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", resp.StatusCode)
	}
}

func TestAdmin_Auth_ValidToken(t *testing.T) {
	ts, _ := startAdminTestServer(t)
	resp := adminRequest(t, ts, "GET", "/admin/keys", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}
}

// --- Create Tests ---

func TestAdmin_Create_Success(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]interface{}{"client_id": "new-client", "roles": []string{"reader"}}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("got status %d, want 201; body: %s", resp.StatusCode, b)
	}

	var result KeyCreateResult
	decodeJSON(t, resp, &result)

	if result.Key == "" {
		t.Fatal("expected raw key in create response")
	}
	if result.ID != "new-client" {
		t.Fatalf("got client_id %q, want new-client", result.ID)
	}
	if !result.Enabled {
		t.Fatal("expected enabled=true for new key")
	}
}

func TestAdmin_Create_Duplicate(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]interface{}{"client_id": "dup-client", "roles": []string{"reader"}}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first create: got status %d, want 201", resp.StatusCode)
	}

	resp2 := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("second create: got status %d, want 409", resp2.StatusCode)
	}
}

func TestAdmin_Create_MissingClientID(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]string{}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", resp.StatusCode)
	}
}

// --- List Tests ---

func TestAdmin_List_Empty(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	resp := adminRequest(t, ts, "GET", "/admin/keys", testAdminKey, nil)
	var summaries []KeySummary
	decodeJSON(t, resp, &summaries)

	if len(summaries) != 0 {
		t.Fatalf("got %d summaries, want 0", len(summaries))
	}
}

func TestAdmin_List_WithKeys(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "alpha", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create(context.Background(), "beta", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	resp := adminRequest(t, ts, "GET", "/admin/keys", testAdminKey, nil)
	var summaries []KeySummary
	decodeJSON(t, resp, &summaries)

	if len(summaries) != 2 {
		t.Fatalf("got %d summaries, want 2", len(summaries))
	}

	for _, s := range summaries {
		if len(s.KeyPrefix) > 11 {
			t.Fatalf("list response appears to leak full key for %q (prefix too long)", s.ID)
		}
	}
}

// --- Update Tests ---

func TestAdmin_Update_DisableAndEnable(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	result, err := mks.Create(context.Background(), "target", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	disabled := false
	body := map[string]interface{}{"enabled": disabled}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/target", testAdminKey, body)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("disable: got status %d, want 200; body: %s", resp.StatusCode, b)
	}
	var summary KeySummary
	decodeJSON(t, resp, &summary)
	if summary.Enabled {
		t.Fatal("expected enabled=false after disable")
	}

	if _, r := mks.Lookup(context.Background(), result.Key); r == auth.RejectNone {
		t.Fatal("expected disabled key to be nil on Lookup")
	}

	body = map[string]interface{}{"enabled": true}
	resp2 := adminRequest(t, ts, "PATCH", "/admin/keys/target", testAdminKey, body)
	if resp2.StatusCode != http.StatusOK {
		defer resp2.Body.Close()
		t.Fatalf("enable: got status %d, want 200", resp2.StatusCode)
	}
	resp2.Body.Close()

	if _, r := mks.Lookup(context.Background(), result.Key); r != auth.RejectNone {
		t.Fatal("expected re-enabled key to be findable")
	}
}

func TestAdmin_Update_NotFound(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]interface{}{"enabled": true}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/nonexistent", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want 404", resp.StatusCode)
	}
}

func TestAdmin_Update_EmptyBody(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "target", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	body := map[string]interface{}{}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/target", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", resp.StatusCode)
	}
}

// --- Delete Tests ---

func TestAdmin_Delete_Success(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "to-delete", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	resp := adminRequest(t, ts, "DELETE", "/admin/keys/to-delete", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("got status %d, want 204", resp.StatusCode)
	}

	if mks.TotalCount() != 0 {
		t.Fatalf("got total count %d, want 0", mks.TotalCount())
	}
}

func TestAdmin_Delete_NotFound(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	resp := adminRequest(t, ts, "DELETE", "/admin/keys/nonexistent", testAdminKey, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want 404", resp.StatusCode)
	}
}

// --- Lifecycle Test ---

func TestAdmin_FullLifecycle(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	createBody := map[string]interface{}{"client_id": "lifecycle-client", "roles": []string{"reader"}}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("create: got status %d, want 201", resp.StatusCode)
	}
	var created KeyCreateResult
	decodeJSON(t, resp, &created)
	rawKey := created.Key

	if _, r := mks.Lookup(context.Background(), rawKey); r != auth.RejectNone {
		t.Fatal("key not findable via Lookup immediately after creation")
	}

	listResp := adminRequest(t, ts, "GET", "/admin/keys", testAdminKey, nil)
	var summaries []KeySummary
	decodeJSON(t, listResp, &summaries)
	if len(summaries) != 1 {
		t.Fatalf("list: got %d, want 1", len(summaries))
	}

	updateBody := map[string]interface{}{"enabled": false}
	patchResp := adminRequest(t, ts, "PATCH", "/admin/keys/lifecycle-client", testAdminKey, updateBody)
	if patchResp.StatusCode != http.StatusOK {
		patchResp.Body.Close()
		t.Fatalf("disable: got status %d, want 200", patchResp.StatusCode)
	}
	patchResp.Body.Close()
	if _, r := mks.Lookup(context.Background(), rawKey); r == auth.RejectNone {
		t.Fatal("disabled key should not be findable via Lookup")
	}

	updateBody2 := map[string]interface{}{"enabled": true}
	patchResp2 := adminRequest(t, ts, "PATCH", "/admin/keys/lifecycle-client", testAdminKey, updateBody2)
	if patchResp2.StatusCode != http.StatusOK {
		patchResp2.Body.Close()
		t.Fatalf("enable: got status %d, want 200", patchResp2.StatusCode)
	}
	patchResp2.Body.Close()
	if _, r := mks.Lookup(context.Background(), rawKey); r != auth.RejectNone {
		t.Fatal("re-enabled key should be findable via Lookup")
	}

	delResp := adminRequest(t, ts, "DELETE", "/admin/keys/lifecycle-client", testAdminKey, nil)
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: got status %d, want 204", delResp.StatusCode)
	}
	if _, r := mks.Lookup(context.Background(), rawKey); r == auth.RejectNone {
		t.Fatal("deleted key should not be findable via Lookup")
	}
	if mks.TotalCount() != 0 {
		t.Fatalf("total count after delete: got %d, want 0", mks.TotalCount())
	}
}

// --- Hot Reload: admin-created key immediately usable by MCP auth ---

func TestAdmin_HotReload_CreatedKeyImmediatelyUsable(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	createBody := map[string]interface{}{"client_id": "hot-client", "roles": []string{"reader"}}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create: got status %d", resp.StatusCode)
	}
	var created KeyCreateResult
	decodeJSON(t, resp, &created)

	entry, lookupR := mks.Lookup(context.Background(), created.Key)
	if lookupR != auth.RejectNone {
		t.Fatal("newly created key not immediately findable via ManagedKeyStore.Lookup")
	}
	if entry.ID != "hot-client" {
		t.Fatalf("got ID %q, want hot-client", entry.ID)
	}
}

func TestAdmin_HotReload_DisabledKeyImmediatelyRejected(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	result, err := mks.Create(context.Background(), "soon-disabled", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	updateBody := map[string]interface{}{"enabled": false}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/soon-disabled", testAdminKey, updateBody)
	resp.Body.Close()

	_, lookupR := mks.Lookup(context.Background(), rawKey)
	if lookupR == auth.RejectNone {
		t.Fatal("disabled key should be immediately rejected by Lookup")
	}
}

func TestAdmin_Create_WithRoles(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	createBody := map[string]interface{}{
		"client_id": "writer-client",
		"roles":     []string{"reader", "writer"},
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create with roles: got status %d", resp.StatusCode)
	}
	var result KeyCreateResult
	decodeJSON(t, resp, &result)

	if len(result.Roles) != 2 {
		t.Errorf("expected 2 roles, got %v", result.Roles)
	}
}

func TestAdmin_Create_InvalidRole(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	createBody := map[string]interface{}{
		"client_id": "bad-client",
		"roles":     []string{"superuser"},
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid role should return 400, got %d", resp.StatusCode)
	}
}

func TestAdmin_Update_SetRoles(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	result, err := mks.Create(context.Background(), "no-role-client", nil, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	updateBody := map[string]interface{}{
		"roles": []string{"admin"},
	}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/no-role-client", testAdminKey, updateBody)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("update roles: got status %d", resp.StatusCode)
	}
	var summary KeySummary
	decodeJSON(t, resp, &summary)

	if len(summary.Roles) != 1 || summary.Roles[0] != "admin" {
		t.Errorf("expected [admin] roles, got %v", summary.Roles)
	}

	// Lookup confirms roles propagated to in-memory store
	entry, lookupR2 := mks.Lookup(context.Background(), result.Key)
	if lookupR2 != auth.RejectNone || entry == nil || len(entry.Roles) != 1 || entry.Roles[0] != "admin" {
		t.Errorf("roles not propagated to store: %v", entry)
	}
}

func TestAdmin_Update_InvalidRole(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "some-client", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	updateBody := map[string]interface{}{
		"roles": []string{"god"},
	}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/some-client", testAdminKey, updateBody)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid role should return 400, got %d", resp.StatusCode)
	}
}

func TestAdmin_Update_NothingProvided(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "some-client", nil, time.Time{}); err != nil {
		t.Fatal(err)
	}

	resp := adminRequest(t, ts, "PATCH", "/admin/keys/some-client", testAdminKey, map[string]interface{}{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty update should return 400, got %d", resp.StatusCode)
	}
}

// --- Role enforcement: issuance must assign >=1 role (Spec §4) ---

func TestAdmin_Create_EmptyRoles_Rejected(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]interface{}{
		"client_id": "roleless-client",
		"roles":     []string{},
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("empty roles should return 400, got %d; body: %s", resp.StatusCode, b)
	}
}

func TestAdmin_Create_NoRolesField_Rejected(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	// Omitting roles entirely (nil slice after decode) must also be rejected.
	body := map[string]interface{}{
		"client_id": "roleless-client2",
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("missing roles field should return 400, got %d; body: %s", resp.StatusCode, b)
	}
}

func TestAdmin_Create_UnknownRole_Rejected(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]interface{}{
		"client_id": "bad-role-client",
		"roles":     []string{"superuser"},
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unknown role should return 400, got %d; body: %s", resp.StatusCode, b)
	}
}

func TestAdmin_Create_ValidRoles_Created(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]interface{}{
		"client_id": "reader-client",
		"roles":     []string{"reader"},
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("valid role should return 201, got %d; body: %s", resp.StatusCode, b)
	}
	var result KeyCreateResult
	decodeJSON(t, resp, &result)
	if len(result.Roles) != 1 || result.Roles[0] != "reader" {
		t.Errorf("expected [reader] roles, got %v", result.Roles)
	}
}

func TestAdmin_Update_ClearRoles_Rejected(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "role-client", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}

	// PATCH with roles:[] must be rejected — use enabled:false to deactivate.
	updateBody := map[string]interface{}{
		"roles": []string{},
	}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/role-client", testAdminKey, updateBody)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("clearing roles should return 400, got %d; body: %s", resp.StatusCode, b)
	}
}

func TestAdmin_Update_ValidRoles_Accepted(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create(context.Background(), "role-client2", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}

	updateBody := map[string]interface{}{
		"roles": []string{"writer", "admin"},
	}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/role-client2", testAdminKey, updateBody)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("valid role update should return 200, got %d; body: %s", resp.StatusCode, b)
	}
	var summary KeySummary
	decodeJSON(t, resp, &summary)
	if len(summary.Roles) != 2 {
		t.Errorf("expected 2 roles after update, got %v", summary.Roles)
	}
}

// --- TTL / expiry tests ---

// startAdminTestServerTTL creates an admin test server with a controlled clock
// and defaultTTL. The same clock is wired into both the AdminServer and the
// ManagedKeyStore so create-time and lookup-time agree.
func startAdminTestServerTTL(t *testing.T, defaultTTL time.Duration, startTime time.Time) (*httptest.Server, *ManagedKeyStore) {
	t.Helper()

	path := tempKeysFile(t)
	mks, err := NewManagedKeyStore(path, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Fixed clock shared by both AdminServer and ManagedKeyStore.
	clock := startTime
	nowFn := func() time.Time { return clock }
	mks.now = nowFn

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	adminSrv := NewAdminServer(":0", testAdminKey, mks, defaultTTL, logger)
	adminSrv.now = nowFn

	// Allow tests to advance the shared clock by mutating mks.now and
	// adminSrv.now directly — but expose a single setter via mks.now so
	// callers can just replace mks.now and adminSrv.now.

	ts := httptest.NewServer(adminSrv.srv.Handler)
	t.Cleanup(ts.Close)
	return ts, mks
}

// TestResolveExpiry covers all branches of the helper.
func TestResolveExpiry(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	defaultTTL := 8760 * time.Hour // 1 year

	// nil ttl → now + defaultTTL
	got, err := resolveExpiry(nil, defaultTTL, now)
	if err != nil {
		t.Fatalf("nil ttl: unexpected error: %v", err)
	}
	want := now.Add(defaultTTL)
	if !got.Equal(want) {
		t.Errorf("nil ttl: got %v, want %v", got, want)
	}

	// "0" → zero time (no expiry)
	for _, sentinel := range []string{"0", "none", "never"} {
		s := sentinel
		got, err = resolveExpiry(&s, defaultTTL, now)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", sentinel, err)
		}
		if !got.IsZero() {
			t.Errorf("%q: got %v, want zero time", sentinel, got)
		}
	}

	// valid duration → now + parsed
	dur := "24h"
	got, err = resolveExpiry(&dur, defaultTTL, now)
	if err != nil {
		t.Fatalf("24h: unexpected error: %v", err)
	}
	if !got.Equal(now.Add(24 * time.Hour)) {
		t.Errorf("24h: got %v, want %v", got, now.Add(24*time.Hour))
	}

	// invalid duration → error
	bad := "banana"
	_, err = resolveExpiry(&bad, defaultTTL, now)
	if err == nil {
		t.Error("invalid ttl: expected error, got nil")
	}

	// negative parsed duration → error (would produce already-expired key)
	neg := "-1h"
	_, err = resolveExpiry(&neg, defaultTTL, now)
	if err == nil {
		t.Error("negative ttl: expected error, got nil")
	}

	// "0s" parses to zero duration, bypasses string sentinel → error
	zeroS := "0s"
	_, err = resolveExpiry(&zeroS, defaultTTL, now)
	if err == nil {
		t.Error("0s ttl: expected error, got nil")
	}
}

// TestAdmin_Create_TTL_Explicit verifies that a key created with ttl:"24h"
// is valid immediately and rejected as expired after 25h.
func TestAdmin_Create_TTL_Explicit(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ts, mks := startAdminTestServerTTL(t, 8760*time.Hour, now)

	body := map[string]interface{}{
		"client_id": "x",
		"roles":     []string{"writer"},
		"ttl":       "24h",
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: got status %d; body: %s", resp.StatusCode, b)
	}
	var result KeyCreateResult
	decodeJSON(t, resp, &result)
	rawKey := result.Key

	// Key should be valid at creation time.
	_, reason := mks.Lookup(context.Background(), rawKey)
	if reason != auth.RejectNone {
		t.Fatalf("key should be valid immediately after creation, got reason %v", reason)
	}

	// Advance clock past 24h → key should be expired.
	mks.now = func() time.Time { return now.Add(25 * time.Hour) }
	_, reason = mks.Lookup(context.Background(), rawKey)
	if reason != auth.RejectExpired {
		t.Errorf("want RejectExpired after 25h, got %v", reason)
	}

	// Response body should include expires_at.
	if result.ExpiresAt.IsZero() {
		t.Error("create response missing expires_at")
	}
}

// TestAdmin_Create_TTL_Default verifies that when ttl is omitted the expiry
// is approximately now + defaultTTL.
func TestAdmin_Create_TTL_Default(t *testing.T) {
	defaultTTL := 8760 * time.Hour
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ts, mks := startAdminTestServerTTL(t, defaultTTL, now)

	body := map[string]interface{}{
		"client_id": "y",
		"roles":     []string{"reader"},
		// ttl omitted → defaultTTL applied
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: got status %d; body: %s", resp.StatusCode, b)
	}
	var result KeyCreateResult
	decodeJSON(t, resp, &result)

	wantExpiry := now.Add(defaultTTL)
	if result.ExpiresAt.IsZero() {
		t.Fatal("create response missing expires_at when defaultTTL is set")
	}
	diff := result.ExpiresAt.Sub(wantExpiry)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Second {
		t.Errorf("expires_at = %v, want ≈ %v (diff %v)", result.ExpiresAt, wantExpiry, diff)
	}

	// Advance clock past defaultTTL → key expired.
	mks.now = func() time.Time { return now.Add(defaultTTL + time.Hour) }
	_, reason := mks.Lookup(context.Background(), result.Key)
	if reason != auth.RejectExpired {
		t.Errorf("want RejectExpired after defaultTTL+1h, got %v", reason)
	}
}

// TestAdmin_Create_TTL_NoExpiry verifies that ttl:"0" creates a key with no expiry.
func TestAdmin_Create_TTL_NoExpiry(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ts, mks := startAdminTestServerTTL(t, 8760*time.Hour, now)

	ttlZero := "0"
	body := map[string]interface{}{
		"client_id": "z",
		"roles":     []string{"reader"},
		"ttl":       ttlZero,
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: got status %d; body: %s", resp.StatusCode, b)
	}
	var result KeyCreateResult
	decodeJSON(t, resp, &result)

	// Key valid far in the future — no expiry.
	mks.now = func() time.Time { return now.Add(100 * 365 * 24 * time.Hour) }
	_, reason := mks.Lookup(context.Background(), result.Key)
	if reason != auth.RejectNone {
		t.Errorf("ttl:0 key should never expire, got reason %v", reason)
	}
}

// TestAdmin_Update_TTL_Renew verifies that PATCH with ttl:"48h" renews an
// expired key back to valid.
func TestAdmin_Update_TTL_Renew(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ts, mks := startAdminTestServerTTL(t, 8760*time.Hour, now)

	// Create a key that expires in 1h.
	body := map[string]interface{}{
		"client_id": "renew-client",
		"roles":     []string{"writer"},
		"ttl":       "1h",
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create: got status %d; body: %s", resp.StatusCode, b)
	}
	var created KeyCreateResult
	decodeJSON(t, resp, &created)
	rawKey := created.Key

	// Advance to 2h → key expired.
	future := now.Add(2 * time.Hour)
	mks.now = func() time.Time { return future }
	_, reason := mks.Lookup(context.Background(), rawKey)
	if reason != auth.RejectExpired {
		t.Fatalf("want RejectExpired after 2h, got %v", reason)
	}

	// PATCH with ttl:"48h" from the future clock → renews.
	updateBody := map[string]interface{}{
		"ttl": "48h",
	}
	patchResp := adminRequest(t, ts, "PATCH", "/admin/keys/renew-client", testAdminKey, updateBody)
	if patchResp.StatusCode != http.StatusOK {
		defer patchResp.Body.Close()
		b, _ := io.ReadAll(patchResp.Body)
		t.Fatalf("patch renew: got status %d; body: %s", patchResp.StatusCode, b)
	}
	patchResp.Body.Close()

	// Key should now be valid again (new expiry = future + 48h).
	_, reason = mks.Lookup(context.Background(), rawKey)
	if reason != auth.RejectNone {
		t.Errorf("want RejectNone after renew, got %v", reason)
	}
}

// TestAdmin_Create_TTL_Invalid verifies that an unparseable ttl returns 400.
func TestAdmin_Create_TTL_Invalid(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ts, _ := startAdminTestServerTTL(t, 8760*time.Hour, now)

	body := map[string]interface{}{
		"client_id": "bad-ttl-client",
		"roles":     []string{"reader"},
		"ttl":       "banana",
	}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("invalid ttl: want 400, got %d; body: %s", resp.StatusCode, b)
	}
}

// TestAdmin_Update_TTL_Invalid verifies that an unparseable ttl in PATCH returns 400.
func TestAdmin_Update_TTL_Invalid(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	ts, mks := startAdminTestServerTTL(t, 8760*time.Hour, now)

	if _, err := mks.Create(context.Background(), "patch-bad-ttl", []string{"reader"}, time.Time{}); err != nil {
		t.Fatal(err)
	}

	body := map[string]interface{}{
		"ttl": "banana",
	}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/patch-bad-ttl", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("invalid ttl patch: want 400, got %d; body: %s", resp.StatusCode, b)
	}
}
