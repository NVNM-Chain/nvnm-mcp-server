// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

const (
	adminMaxRequestBody = 1 * 1024 * 1024 // 1 MB
)

// errTTLNotPositive is the sentinel for the "ttl must be positive" condition
// in resolveExpiry. Wrapping the sentinel with the actual value preserves
// errors.Is unwrapping for callers while satisfying err113.
var errTTLNotPositive = errors.New("ttl must be positive")

// validateRoles reports whether every role in the slice is a recognized RBAC
// role. Vacuously true for an empty slice; emptiness is enforced separately at
// the handler layer. Delegates to auth.IsValidRole so the canonical role set
// lives in exactly one place.
func validateRoles(roles []string) bool {
	for _, r := range roles {
		if !auth.IsValidRole(r) {
			return false
		}
	}
	return true
}

// AdminServer serves the key management REST API on a dedicated port,
// authenticated by a separate admin bearer token.
type AdminServer struct {
	srv          *http.Server
	keys         KeyStoreBackend
	pendingStore *PendingKeyStore
	email        EmailSender
	logger       *slog.Logger
	defaultTTL   time.Duration
	now          func() time.Time
	writeAudit   WriteAuditStore
	blacklist    SignerBlacklistStore
	adminAudit   AdminAuditStore
}

// NewAdminServer creates an admin API server.
// adminKeys maps sha256(admin-key) -> admin id; a bearer must hash to one
// of these entries to authenticate, and the matched id is attributed to
// the request as its admin actor (see adminActorFromContext).
// defaultTTL is applied to newly created keys when no per-key ttl is specified
// in the request; pass 0 for no default expiry.
func NewAdminServer(
	addr string,
	adminKeys map[[32]byte]string,
	keys KeyStoreBackend,
	defaultTTL time.Duration,
	logger *slog.Logger,
) *AdminServer {
	a := &AdminServer{
		keys:       keys,
		logger:     logger,
		defaultTTL: defaultTTL,
		now:        time.Now,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/keys", a.handleList)
	mux.HandleFunc("POST /admin/keys", a.handleCreate)
	mux.HandleFunc("PATCH /admin/keys/{id}", a.handleUpdate)
	mux.HandleFunc("DELETE /admin/keys/{id}", a.handleDelete)
	// Phase 11 L3 PR 3: pending key-request review endpoints. Wired
	// regardless of whether the pending store is attached; the handlers
	// fail fast with 503 when pendingStore is nil so the routing layer
	// stays static and operators see a clear "not configured" error
	// rather than a confusing 404.
	mux.HandleFunc("GET /admin/keys/pending", a.handleListPending)
	mux.HandleFunc("POST /admin/keys/pending/{id}/approve", a.handleApprovePending)
	mux.HandleFunc("POST /admin/keys/pending/{id}/reject", a.handleRejectPending)
	mux.HandleFunc("GET /admin/write-audit", a.handleWriteAudit)
	mux.HandleFunc("GET /admin/signer-blacklist", a.handleListBlacklist)
	mux.HandleFunc("POST /admin/signer-blacklist", a.handleAddBlacklist)
	mux.HandleFunc("DELETE /admin/signer-blacklist/{signer}", a.handleDeleteBlacklist)

	handler := adminAuth(
		limitAdminRequestBody(mux),
		adminKeys,
		logger,
	)

	a.srv = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 17, // 128 KB
	}

	return a
}

// Start begins listening. Blocks until the server stops.
func (a *AdminServer) Start() error {
	a.logger.Info("admin API server started",
		slog.String("addr", a.srv.Addr),
	)
	if err := a.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("admin server: %w", err)
	}
	return nil
}

// Close gracefully shuts down the admin server.
func (a *AdminServer) Close(ctx context.Context) error {
	return a.srv.Shutdown(ctx)
}

// WithAdminAuditStore attaches the per-admin mutation audit backend. nil
// leaves recordAdminAudit falling back to attributed INFO log lines (self-host
// / no MCP_KEYLESS_PG_DSN).
func (a *AdminServer) WithAdminAuditStore(s AdminAuditStore) *AdminServer {
	a.adminAudit = s
	return a
}

// recordAdminAudit attributes an admin mutation to the actor resolved from
// ctx (see adminActorFromContext) and persists it via the attached
// AdminAuditStore. Best-effort: a store failure is logged at WARN and never
// propagated, so an audit-write failure never fails the admin mutation it
// describes. With no store attached, the mutation is instead emitted as an
// attributed INFO log line so single-process / no-DSN deployments retain an
// audit trail in their logs.
func (a *AdminServer) recordAdminAudit(ctx context.Context, action AdminAction, target, detail, outcome string) {
	actor := adminActorFromContext(ctx)

	if a.adminAudit == nil {
		a.logger.Info("admin: mutation",
			slog.String("actor_id", actor),
			slog.String("action", string(action)),
			slog.String("target", target),
			slog.String("outcome", outcome),
		)
		return
	}

	entry := AdminAuditEntry{
		ActorID: actor,
		Action:  action,
		Target:  target,
		Detail:  detail,
		Outcome: outcome,
	}
	if err := a.adminAudit.Record(ctx, entry); err != nil {
		a.logger.Warn("admin: audit record failed",
			slog.String("actor_id", actor),
			slog.String("action", string(action)),
			slog.String("error", err.Error()),
		)
	}
}

// --- Handlers ---

// resolveExpiry maps a request TTL string to an absolute expiry time.
//
//   - ttl == nil         → now + defaultTTL (operator default; zero defaultTTL means no expiry)
//   - *ttl in "0","none","never" → zero time.Time (no expiry)
//   - else               → now + parsed duration; invalid duration returns an error
func resolveExpiry(ttl *string, defaultTTL time.Duration, now time.Time) (time.Time, error) {
	if ttl == nil {
		if defaultTTL == 0 {
			return time.Time{}, nil
		}
		return now.Add(defaultTTL), nil
	}
	switch *ttl {
	case "0", "none", "never":
		return time.Time{}, nil
	}
	d, err := time.ParseDuration(*ttl)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid ttl %q: %w", *ttl, err)
	}
	if d <= 0 {
		return time.Time{}, fmt.Errorf("%w: got %q", errTTLNotPositive, *ttl)
	}
	return now.Add(d), nil
}

type createRequest struct {
	ClientID string   `json:"client_id"`
	Roles    []string `json:"roles,omitempty"`
	TTL      *string  `json:"ttl,omitempty"`
}

func (a *AdminServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.ClientID == "" {
		a.writeError(w, http.StatusBadRequest, "client_id is required")
		return
	}

	if len(req.Roles) == 0 {
		a.writeError(w, http.StatusBadRequest,
			"at least one role is required; a key with no roles authorizes nothing")
		return
	}

	if !validateRoles(req.Roles) {
		a.writeError(w, http.StatusBadRequest,
			"roles must be one or more of: reader, writer, admin, automation")
		return
	}

	expiresAt, err := resolveExpiry(req.TTL, a.defaultTTL, a.now())
	if err != nil {
		a.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	result, err := a.keys.Create(r.Context(), req.ClientID, req.Roles, expiresAt)
	if err != nil {
		if errors.Is(err, ErrClientExists) {
			a.writeError(w, http.StatusConflict, err.Error())
			return
		}
		a.logger.Error("admin: create key failed", slog.String("error", err.Error()))
		a.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	a.logger.Info("admin: key created",
		slog.String("client_id", req.ClientID),
		slog.String("remote_addr", r.RemoteAddr),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(result); err != nil {
		a.logger.Error("admin: encode create response", slog.String("error", err.Error()))
	}
}

func (a *AdminServer) handleList(w http.ResponseWriter, _ *http.Request) {
	summaries := a.keys.List()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summaries); err != nil {
		a.logger.Error("admin: encode list response", slog.String("error", err.Error()))
	}
}

type updateRequest struct {
	Enabled *bool     `json:"enabled,omitempty"`
	Roles   *[]string `json:"roles,omitempty"`
	TTL     *string   `json:"ttl,omitempty"`
}

func (a *AdminServer) handleUpdate(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	if clientID == "" {
		a.writeError(w, http.StatusBadRequest, "client id is required in path")
		return
	}

	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if req.Enabled == nil && req.Roles == nil && req.TTL == nil {
		a.writeError(w, http.StatusBadRequest,
			"at least one of enabled, roles, or ttl must be provided")
		return
	}

	if req.Roles != nil {
		if len(*req.Roles) == 0 {
			a.writeError(w, http.StatusBadRequest,
				"roles cannot be cleared; use enabled:false to deactivate a key")
			return
		}
		if !validateRoles(*req.Roles) {
			a.writeError(w, http.StatusBadRequest,
				"roles must be one or more of: reader, writer, admin, automation")
			return
		}
	}

	upd := KeyUpdate{Enabled: req.Enabled, Roles: req.Roles}
	if req.TTL != nil {
		exp, expErr := resolveExpiry(req.TTL, a.defaultTTL, a.now())
		if expErr != nil {
			a.writeError(w, http.StatusBadRequest, expErr.Error())
			return
		}
		upd.ExpiresAt = &exp
	}

	summary, err := a.keys.Update(clientID, upd)
	if err != nil {
		if errors.Is(err, ErrClientMissing) {
			a.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		a.logger.Error("admin: update key failed",
			slog.String("client_id", clientID),
			slog.String("error", err.Error()),
		)
		a.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	a.logger.Info("admin: key updated",
		slog.String("client_id", clientID),
		slog.String("remote_addr", r.RemoteAddr),
	)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		a.logger.Error("admin: encode update response", slog.String("error", err.Error()))
	}
}

func (a *AdminServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	if clientID == "" {
		a.writeError(w, http.StatusBadRequest, "client id is required in path")
		return
	}

	if err := a.keys.Delete(clientID); err != nil {
		if errors.Is(err, ErrClientMissing) {
			a.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		a.logger.Error("admin: delete key failed",
			slog.String("client_id", clientID),
			slog.String("error", err.Error()),
		)
		a.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	a.logger.Info("admin: key deleted",
		slog.String("client_id", clientID),
		slog.String("remote_addr", r.RemoteAddr),
	)

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminServer) writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		a.logger.Error("admin: encode error response", slog.String("error", err.Error()))
	}
}

// --- Middleware ---

// adminAuth gates the admin REST API behind a Bearer-token check.
//
// keys maps sha256(admin-key) -> admin id, as produced by loadAdminKeys.
// The presented bearer is hashed and compared against every entry with
// subtle.ConstantTimeCompare so that the request's latency does not
// depend on which entry (if any) matches -- comparing raw tokens
// directly would additionally leak token length to a length-probing
// attacker, so both sides are hashed first to equalize lengths.
//
// All failures return 401 per RFC 7235: a missing/wrong-scheme/wrong
// bearer is an authentication failure, not an authorization failure. On
// a match, the resolved admin id is injected into the request context
// via contextWithAdminActor for downstream handlers (e.g. audit writes)
// to read with adminActorFromContext.
func adminAuth(next http.Handler, keys map[[32]byte]string, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			logger.Warn("admin: rejected unauthenticated request",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			http.Error(w, `{"error":"missing Authorization header"}`, http.StatusUnauthorized)
			return
		}

		const prefix = "Bearer "
		if !strings.HasPrefix(authHeader, prefix) {
			logger.Warn("admin: rejected request with invalid auth scheme",
				slog.String("remote_addr", r.RemoteAddr),
			)
			http.Error(w, `{"error":"invalid Authorization scheme; expected Bearer"}`, http.StatusUnauthorized)
			return
		}

		got := sha256.Sum256([]byte(strings.TrimPrefix(authHeader, prefix)))

		// Iterate every entry rather than doing a direct map lookup
		// (`keys[got]`) so the amount of work done is independent of
		// which entry, if any, matches -- a map lookup short-circuits
		// on the first bucket collision and can leak timing signal
		// about the key material. matched/id are only assigned to on
		// a hit; the loop always walks the full map regardless.
		var matched bool
		var id string
		for h, adminID := range keys {
			if subtle.ConstantTimeCompare(got[:], h[:]) == 1 {
				matched = true
				id = adminID
			}
		}

		if !matched {
			logger.Warn("admin: rejected request with invalid admin key",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			http.Error(w, `{"error":"invalid admin key"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(contextWithAdminActor(r.Context(), id)))
	})
}

func limitAdminRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, adminMaxRequestBody)
		next.ServeHTTP(w, r)
	})
}
