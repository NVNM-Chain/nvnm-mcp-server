// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"log/slog"
	"net/http"

	defitypes "github.com/defiweb/go-eth/types"
)

// WithSignerBlacklistStore attaches the per-signer ban-list backend. nil
// leaves the endpoints returning 404 (self-host / no MCP_KEYLESS_PG_DSN).
func (a *AdminServer) WithSignerBlacklistStore(s SignerBlacklistStore) *AdminServer {
	a.blacklist = s
	return a
}

// handleListBlacklist serves GET /admin/signer-blacklist.
func (a *AdminServer) handleListBlacklist(w http.ResponseWriter, r *http.Request) {
	if a.blacklist == nil {
		http.Error(w, "signer blacklist not configured", http.StatusNotFound)
		return
	}
	entries, err := a.blacklist.List(r.Context())
	if err != nil {
		a.logger.Error("signer-blacklist list failed", slog.String("error", err.Error()))
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(struct {
		Entries []BlacklistEntry `json:"entries"`
	}{Entries: entries}); err != nil {
		a.logger.Error("admin: encode signer-blacklist response", slog.String("error", err.Error()))
	}
}

// addBlacklistRequest is the POST /admin/signer-blacklist body.
type addBlacklistRequest struct {
	Signer string `json:"signer"`
	Reason string `json:"reason"`
}

// handleAddBlacklist serves POST /admin/signer-blacklist. signer must parse
// as a hex address; the store itself normalizes case.
func (a *AdminServer) handleAddBlacklist(w http.ResponseWriter, r *http.Request) {
	if a.blacklist == nil {
		http.Error(w, "signer blacklist not configured", http.StatusNotFound)
		return
	}
	var req addBlacklistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if _, err := defitypes.AddressFromHex(req.Signer); err != nil {
		http.Error(w, "signer is not a valid address", http.StatusBadRequest)
		return
	}
	if err := a.blacklist.Add(r.Context(), req.Signer, req.Reason); err != nil {
		a.logger.Error("signer-blacklist add failed", slog.String("error", err.Error()))
		http.Error(w, "add failed", http.StatusInternalServerError)
		return
	}
	a.logger.Info("admin: signer blacklisted",
		slog.String("signer", req.Signer),
		slog.String("remote_addr", r.RemoteAddr),
	)
	w.WriteHeader(http.StatusOK)
}

// handleDeleteBlacklist serves DELETE /admin/signer-blacklist/{signer}.
func (a *AdminServer) handleDeleteBlacklist(w http.ResponseWriter, r *http.Request) {
	if a.blacklist == nil {
		http.Error(w, "signer blacklist not configured", http.StatusNotFound)
		return
	}
	signer := r.PathValue("signer")
	if signer == "" {
		http.Error(w, "signer is required in path", http.StatusBadRequest)
		return
	}
	if err := a.blacklist.Remove(r.Context(), signer); err != nil {
		a.logger.Error("signer-blacklist remove failed", slog.String("error", err.Error()))
		http.Error(w, "remove failed", http.StatusInternalServerError)
		return
	}
	a.logger.Info("admin: signer unblacklisted",
		slog.String("signer", signer),
		slog.String("remote_addr", r.RemoteAddr),
	)
	w.WriteHeader(http.StatusOK)
}
