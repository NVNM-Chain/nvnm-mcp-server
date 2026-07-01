// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// WithWriteAuditStore attaches the write-audit query backend. nil leaves the
// endpoint returning 404 (self-host / no MCP_KEYLESS_PG_DSN).
func (a *AdminServer) WithWriteAuditStore(s WriteAuditStore) *AdminServer {
	a.writeAudit = s
	return a
}

// handleWriteAudit serves GET /admin/write-audit?signer=&from=&to=&limit=.
// from/to are RFC3339 timestamps. Append-only: read-only endpoint.
func (a *AdminServer) handleWriteAudit(w http.ResponseWriter, r *http.Request) {
	if a.writeAudit == nil {
		http.Error(w, "write audit not configured", http.StatusNotFound)
		return
	}
	q := r.URL.Query()
	f := WriteAuditFilter{Signer: q.Get("signer")}
	if !parseTimeParam(w, "from", q.Get("from"), &f.From) {
		return
	}
	if !parseTimeParam(w, "to", q.Get("to"), &f.To) {
		return
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "invalid 'limit'", http.StatusBadRequest)
			return
		}
		f.Limit = n
	}
	entries, err := a.writeAudit.Query(r.Context(), f)
	if err != nil {
		a.logger.Error("write-audit query failed", slog.String("error", err.Error()))
		http.Error(w, "query failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		Entries []WriteAuditEntry `json:"entries"`
	}{Entries: entries}); err != nil {
		a.logger.Error("admin: encode write-audit response", slog.String("error", err.Error()))
	}
}

// parseTimeParam parses an optional RFC3339 query param into dst. An empty
// value is a no-op (dst left nil). A present-but-malformed value writes a 400
// and returns false so the caller can stop; a valid value sets *dst.
func parseTimeParam(w http.ResponseWriter, name, v string, dst **time.Time) bool {
	if v == "" {
		return true
	}
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		http.Error(w, "invalid '"+name+"' (want RFC3339)", http.StatusBadRequest)
		return false
	}
	*dst = &ts
	return true
}
