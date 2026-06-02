// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

// WithPendingKeyStore attaches the Phase 11 L3 pending-request review
// store and the email sender used to deliver approval / rejection
// notices. Returns the receiver so callers can chain. nil arguments
// disable the corresponding features cleanly — passing a non-nil
// store with a nil sender means approvals still issue the key and
// mutate the store, but the customer notification is skipped (the
// operator is the one who delivers it out-of-band).
func (a *AdminServer) WithPendingKeyStore(store *PendingKeyStore, email EmailSender) *AdminServer {
	a.pendingStore = store
	a.email = email
	return a
}

// pendingItemSummary is the on-the-wire shape returned by
// GET /admin/keys/pending. Mirrors PendingKeyRequest but with the
// fields a reviewer needs ordered first; intended_use is the load-
// bearing free-text the reviewer judges on.
type pendingItemSummary struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Company     string `json:"company,omitempty"`
	IntendedUse string `json:"intended_use"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	DecidedAt   string `json:"decided_at,omitempty"`
	DeciderID   string `json:"decider_id,omitempty"`
	KeyID       string `json:"key_id,omitempty"`
	RemoteAddr  string `json:"remote_addr,omitempty"`
}

func toSummary(r *PendingKeyRequest) pendingItemSummary {
	s := pendingItemSummary{
		ID:          r.ID,
		Email:       r.Email,
		Company:     r.Company,
		IntendedUse: r.IntendedUse,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		DeciderID:   r.DeciderID,
		KeyID:       r.KeyID,
		RemoteAddr:  r.RemoteAddr,
	}
	if r.DecidedAt != nil {
		s.DecidedAt = r.DecidedAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return s
}

// handleListPending returns every PendingKeyRequest the store knows
// about, regardless of status. A reviewer wants to see decided rows for
// audit context (who approved, who rejected, when), not just the
// currently-pending ones; status filtering can happen client-side or
// via a query param in a future revision.
func (a *AdminServer) handleListPending(w http.ResponseWriter, _ *http.Request) {
	if a.pendingStore == nil {
		a.writeError(w, http.StatusServiceUnavailable,
			"pending key store is not configured (set NVNM_KEY_REQUEST_ENABLED + NVNM_KEY_PENDING_FILE)")
		return
	}
	items := a.pendingStore.List()
	out := make([]pendingItemSummary, len(items))
	for i := range items {
		out[i] = toSummary(&items[i])
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		a.logger.Error("admin: encode pending list", slog.String("error", err.Error()))
	}
}

// approveResponse is the body returned by POST .../approve. The issued
// key material is included so a reviewer using the API directly (no
// SMTP) can copy it to the customer manually; if SMTP delivery succeeds
// the customer also receives it via email.
type approveResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	KeyID     string `json:"key_id"`
	// APIKey is intentionally returned in the approve response so a
	// reviewer using the API directly (no SMTP) can copy it to the
	// customer manually. Admin-auth gated.
	APIKey         string `json:"api_key"`
	EmailDelivered bool   `json:"email_delivered"`
}

func (a *AdminServer) handleApprovePending(w http.ResponseWriter, r *http.Request) {
	if a.pendingStore == nil {
		a.writeError(w, http.StatusServiceUnavailable,
			"pending key store is not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "missing request id")
		return
	}

	req, ok := a.pendingStore.Get(id)
	if !ok {
		a.writeError(w, http.StatusNotFound, "pending request not found")
		return
	}
	if req.Status != PendingStatusPending {
		a.writeError(w, http.StatusConflict,
			fmt.Sprintf("request already decided: status=%q", req.Status))
		return
	}

	// Issue a new credential. The client_id is derived from the
	// email so log lines and audit records can correlate without
	// the reviewer making up another identifier.
	clientID := "pending:" + req.ID
	created, err := a.keys.Create(clientID, "required", []string{"reader"})
	if err != nil {
		// ErrClientExists is unreachable today (the ID embeds the
		// request UUID) but treat it as a 500 if it ever surfaces so
		// the failure mode is visible.
		a.logger.Error("admin: approve mint key",
			slog.String("request_id", req.ID),
			slog.String("error", err.Error()),
		)
		a.writeError(w, http.StatusInternalServerError, "key mint failed")
		return
	}

	deciderID := auth.ClientIDFromContext(r.Context())
	decided, err := a.pendingStore.Decide(req.ID, PendingStatusApproved, deciderID, created.ID)
	if err != nil {
		// The Decide rollback discipline guarantees the store is
		// consistent; we just need to make sure we don't leave an
		// orphaned key entry. Best-effort delete; log on failure but
		// proceed to surface the original Decide error.
		if delErr := a.keys.Delete(created.ID); delErr != nil {
			a.logger.Warn("admin: approve rollback delete failed",
				slog.String("key_id", created.ID),
				slog.String("error", delErr.Error()),
			)
		}
		a.logger.Error("admin: approve persist decision",
			slog.String("request_id", req.ID),
			slog.String("error", err.Error()),
		)
		status := http.StatusInternalServerError
		if errors.Is(err, ErrPendingNotPending) || errors.Is(err, ErrPendingNotFound) {
			status = http.StatusConflict
		}
		a.writeError(w, status, err.Error())
		return
	}

	emailDelivered := false
	if a.email != nil {
		subject := "Your NVNM Chain MCP Server API key request was approved"
		body := approvalEmailBody(req.Email, created.Key)
		if sendErr := a.email.Send(r.Context(), req.Email, subject, body); sendErr != nil {
			// Logged inside Send; we report partial success so the
			// reviewer knows to deliver out-of-band.
			a.logger.Warn("admin: approval email delivery failed",
				slog.String("request_id", req.ID),
				slog.String("error", sendErr.Error()),
			)
		} else {
			emailDelivered = true
		}
	}

	a.logger.Info("admin: pending request approved",
		slog.String("request_id", req.ID),
		slog.String("decider_id", deciderID),
		slog.String("key_id", created.ID),
		slog.Bool("email_delivered", emailDelivered),
		slog.String("remote_addr", r.RemoteAddr),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// gosec G117: APIKey is intentionally serialized so reviewers
	// using the API directly (no SMTP) can copy the key to the
	// customer. The endpoint is admin-auth gated.
	if err := json.NewEncoder(w).Encode(approveResponse{ //nolint:gosec // G117: admin-only key-delivery contract
		RequestID:      decided.ID,
		Status:         decided.Status,
		KeyID:          created.ID,
		APIKey:         created.Key,
		EmailDelivered: emailDelivered,
	}); err != nil {
		a.logger.Error("admin: encode approve response", slog.String("error", err.Error()))
	}
}

// rejectRequest carries an optional reason the reviewer can supply.
// Logged for audit; included in the rejection email if SMTP is wired.
type rejectRequest struct {
	Reason string `json:"reason,omitempty"`
}

type rejectResponse struct {
	RequestID      string `json:"request_id"`
	Status         string `json:"status"`
	EmailDelivered bool   `json:"email_delivered"`
}

func (a *AdminServer) handleRejectPending(w http.ResponseWriter, r *http.Request) {
	if a.pendingStore == nil {
		a.writeError(w, http.StatusServiceUnavailable,
			"pending key store is not configured")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		a.writeError(w, http.StatusBadRequest, "missing request id")
		return
	}

	var body rejectRequest
	// The reject body is optional; empty body is fine.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			a.writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
	}

	deciderID := auth.ClientIDFromContext(r.Context())
	decided, err := a.pendingStore.Decide(id, PendingStatusRejected, deciderID, "")
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, ErrPendingNotFound):
			status = http.StatusNotFound
		case errors.Is(err, ErrPendingNotPending):
			status = http.StatusConflict
		}
		a.writeError(w, status, err.Error())
		return
	}

	emailDelivered := false
	if a.email != nil {
		subject := "Update on your NVNM Chain MCP Server API key request"
		emailBody := rejectionEmailBody(decided.Email, body.Reason)
		if sendErr := a.email.Send(r.Context(), decided.Email, subject, emailBody); sendErr != nil {
			a.logger.Warn("admin: rejection email delivery failed",
				slog.String("request_id", decided.ID),
				slog.String("error", sendErr.Error()),
			)
		} else {
			emailDelivered = true
		}
	}

	a.logger.Info("admin: pending request rejected",
		slog.String("request_id", decided.ID),
		slog.String("decider_id", deciderID),
		slog.String("reason", body.Reason),
		slog.Bool("email_delivered", emailDelivered),
		slog.String("remote_addr", r.RemoteAddr),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(rejectResponse{
		RequestID:      decided.ID,
		Status:         decided.Status,
		EmailDelivered: emailDelivered,
	}); err != nil {
		a.logger.Error("admin: encode reject response", slog.String("error", err.Error()))
	}
}

// approvalEmailBody is the plain-text body sent on approval. Kept
// deliberately minimal: subject + greeting + key + one-line "next
// steps" pointer. Operators iterating on the wording can replace this
// later; the customer-visible default needs to be safe by construction
// (no HTML, no tracking links, no images).
func approvalEmailBody(toEmail, rawKey string) string {
	const tmpl = `Hello,

Your request for an NVNM Chain MCP Server API key has been approved.

Your API key:

    %s

Use it as a Bearer token on requests to the MCP server:

    Authorization: Bearer <your-key>

If you did not request this key, please contact security@nvnmchain.io.

Welcome to the closed beta.
`
	_ = toEmail // currently unused; reserved for future personalization
	return fmt.Sprintf(tmpl, rawKey)
}

// rejectionEmailBody is the plain-text body sent on rejection.
func rejectionEmailBody(toEmail, reason string) string {
	_ = toEmail
	if reason == "" {
		return "Hello,\n\nYour recent NVNM Chain MCP Server API key request was not approved at this time. " +
			"You can re-apply after addressing any concerns; reply to this email if you have questions.\n"
	}
	return fmt.Sprintf("Hello,\n\nYour recent NVNM Chain MCP Server API key request was not approved at this time.\n\n"+
		"Reason: %s\n\nYou can re-apply after addressing the concern; reply to this email if you have questions.\n", reason)
}
