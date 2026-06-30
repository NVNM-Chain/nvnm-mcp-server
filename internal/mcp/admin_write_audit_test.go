// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminWriteAudit_ReturnsEntries(t *testing.T) {
	fa := &fakeWriteAudit{recorded: []WriteAuditEntry{
		{Signer: "0xaaa", Outcome: "broadcast_ok", TxHash: "0x1"},
	}}
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger()).WithWriteAuditStore(fa)

	req := httptest.NewRequest(http.MethodGet, "/admin/write-audit?signer=0xaaa", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rec := httptest.NewRecorder()
	a.handleWriteAudit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Entries []WriteAuditEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Signer != "0xaaa" {
		t.Fatalf("unexpected entries: %+v", body.Entries)
	}
}

func TestAdminWriteAudit_NilStore404(t *testing.T) {
	a := NewAdminServer(":0", "admin-secret", nil, 0, testLogger())
	req := httptest.NewRequest(http.MethodGet, "/admin/write-audit", http.NoBody)
	rec := httptest.NewRecorder()
	a.handleWriteAudit(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when store unconfigured", rec.Code)
	}
}
