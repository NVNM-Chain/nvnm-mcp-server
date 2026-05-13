package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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
	adminSrv := NewAdminServer(":0", testAdminKey, mks, logger)

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

	body := map[string]string{"client_id": "new-client", "write_approval": "auto"}
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
	if result.WriteApproval != "auto" {
		t.Fatalf("got write_approval %q, want auto", result.WriteApproval)
	}
	if !result.Enabled {
		t.Fatal("expected enabled=true for new key")
	}
}

func TestAdmin_Create_Duplicate(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]string{"client_id": "dup-client"}
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

	body := map[string]string{"write_approval": "auto"}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", resp.StatusCode)
	}
}

func TestAdmin_Create_InvalidApproval(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	body := map[string]string{"client_id": "test", "write_approval": "maybe"}
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

	if _, err := mks.Create("alpha", "required", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := mks.Create("beta", "auto", nil); err != nil {
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

	result, err := mks.Create("target", "", nil)
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

	if entry := mks.Lookup(result.Key); entry != nil {
		t.Fatal("expected disabled key to be nil on Lookup")
	}

	body = map[string]interface{}{"enabled": true}
	resp2 := adminRequest(t, ts, "PATCH", "/admin/keys/target", testAdminKey, body)
	if resp2.StatusCode != http.StatusOK {
		defer resp2.Body.Close()
		t.Fatalf("enable: got status %d, want 200", resp2.StatusCode)
	}
	resp2.Body.Close()

	if entry := mks.Lookup(result.Key); entry == nil {
		t.Fatal("expected re-enabled key to be findable")
	}
}

func TestAdmin_Update_SetApproval(t *testing.T) {
	ts, _ := startAdminTestServer(t)

	createBody := map[string]string{"client_id": "target", "write_approval": "required"}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	resp.Body.Close()

	updateBody := map[string]interface{}{"write_approval": "auto"}
	resp2 := adminRequest(t, ts, "PATCH", "/admin/keys/target", testAdminKey, updateBody)
	if resp2.StatusCode != http.StatusOK {
		defer resp2.Body.Close()
		t.Fatalf("got status %d, want 200", resp2.StatusCode)
	}
	var summary KeySummary
	decodeJSON(t, resp2, &summary)
	if summary.WriteApproval != "auto" {
		t.Fatalf("got write_approval %q, want auto", summary.WriteApproval)
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

	if _, err := mks.Create("target", "", nil); err != nil {
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

	if _, err := mks.Create("to-delete", "", nil); err != nil {
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

	createBody := map[string]string{"client_id": "lifecycle-client", "write_approval": "required"}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	if resp.StatusCode != http.StatusCreated {
		defer resp.Body.Close()
		t.Fatalf("create: got status %d, want 201", resp.StatusCode)
	}
	var created KeyCreateResult
	decodeJSON(t, resp, &created)
	rawKey := created.Key

	if entry := mks.Lookup(rawKey); entry == nil {
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
	if entry := mks.Lookup(rawKey); entry != nil {
		t.Fatal("disabled key should not be findable via Lookup")
	}

	updateBody2 := map[string]interface{}{"enabled": true}
	patchResp2 := adminRequest(t, ts, "PATCH", "/admin/keys/lifecycle-client", testAdminKey, updateBody2)
	if patchResp2.StatusCode != http.StatusOK {
		patchResp2.Body.Close()
		t.Fatalf("enable: got status %d, want 200", patchResp2.StatusCode)
	}
	patchResp2.Body.Close()
	if entry := mks.Lookup(rawKey); entry == nil {
		t.Fatal("re-enabled key should be findable via Lookup")
	}

	delResp := adminRequest(t, ts, "DELETE", "/admin/keys/lifecycle-client", testAdminKey, nil)
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: got status %d, want 204", delResp.StatusCode)
	}
	if entry := mks.Lookup(rawKey); entry != nil {
		t.Fatal("deleted key should not be findable via Lookup")
	}
	if mks.TotalCount() != 0 {
		t.Fatalf("total count after delete: got %d, want 0", mks.TotalCount())
	}
}

// --- Hot Reload: admin-created key immediately usable by MCP auth ---

func TestAdmin_HotReload_CreatedKeyImmediatelyUsable(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	createBody := map[string]string{"client_id": "hot-client"}
	resp := adminRequest(t, ts, "POST", "/admin/keys", testAdminKey, createBody)
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("create: got status %d", resp.StatusCode)
	}
	var created KeyCreateResult
	decodeJSON(t, resp, &created)

	entry := mks.Lookup(created.Key)
	if entry == nil {
		t.Fatal("newly created key not immediately findable via ManagedKeyStore.Lookup")
	}
	if entry.ID != "hot-client" {
		t.Fatalf("got ID %q, want hot-client", entry.ID)
	}
}

func TestAdmin_HotReload_DisabledKeyImmediatelyRejected(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	result, err := mks.Create("soon-disabled", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	rawKey := result.Key

	updateBody := map[string]interface{}{"enabled": false}
	resp := adminRequest(t, ts, "PATCH", "/admin/keys/soon-disabled", testAdminKey, updateBody)
	resp.Body.Close()

	entry := mks.Lookup(rawKey)
	if entry != nil {
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

	result, err := mks.Create("no-role-client", "", nil)
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
	entry := mks.Lookup(result.Key)
	if entry == nil || len(entry.Roles) != 1 || entry.Roles[0] != "admin" {
		t.Errorf("roles not propagated to store: %v", entry)
	}
}

func TestAdmin_Update_InvalidRole(t *testing.T) {
	ts, mks := startAdminTestServer(t)

	if _, err := mks.Create("some-client", "", nil); err != nil {
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

	if _, err := mks.Create("some-client", "", nil); err != nil {
		t.Fatal(err)
	}

	resp := adminRequest(t, ts, "PATCH", "/admin/keys/some-client", testAdminKey, map[string]interface{}{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty update should return 400, got %d", resp.StatusCode)
	}
}
