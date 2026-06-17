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
)

const (
	adminMaxRequestBody = 1 * 1024 * 1024 // 1 MB
)

var validRoleValues = map[string]bool{
	"reader":     true,
	"writer":     true,
	"admin":      true,
	"automation": true,
}

func validateRoles(roles []string) bool {
	for _, r := range roles {
		if !validRoleValues[r] {
			return false
		}
	}
	return true
}

// AdminServer serves the key management REST API on a dedicated port,
// authenticated by a separate admin bearer token.
type AdminServer struct {
	srv          *http.Server
	keys         *ManagedKeyStore
	pendingStore *PendingKeyStore
	email        EmailSender
	logger       *slog.Logger
}

// NewAdminServer creates an admin API server.
// adminKey is the bearer token required for all requests.
func NewAdminServer(addr, adminKey string, keys *ManagedKeyStore, logger *slog.Logger) *AdminServer {
	a := &AdminServer{
		keys:   keys,
		logger: logger,
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

	handler := adminAuth(
		limitAdminRequestBody(mux),
		adminKey,
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

// --- Handlers ---

type createRequest struct {
	ClientID string   `json:"client_id"`
	Roles    []string `json:"roles,omitempty"`
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

	if !validateRoles(req.Roles) {
		a.writeError(w, http.StatusBadRequest,
			"roles must be one or more of: reader, writer, admin, automation")
		return
	}

	result, err := a.keys.Create(req.ClientID, req.Roles)
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

	if req.Enabled == nil && req.Roles == nil {
		a.writeError(w, http.StatusBadRequest,
			"at least one of enabled or roles must be provided")
		return
	}

	if req.Roles != nil && !validateRoles(*req.Roles) {
		a.writeError(w, http.StatusBadRequest,
			"roles must be one or more of: reader, writer, admin, automation")
		return
	}

	summary, err := a.keys.Update(clientID, KeyUpdate(req))
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
// The token comparison hashes both sides with SHA-256 and compares
// fixed-length digests. subtle.ConstantTimeCompare returns 0 fast on
// length mismatch -- comparing raw tokens directly would leak the
// admin-key length to a length-probing attacker. Hashing equalizes
// lengths so the constant-time guarantee is meaningful.
//
// All failures return 401 per RFC 7235: a missing/wrong-scheme/wrong
// bearer is an authentication failure, not an authorization failure.
func adminAuth(next http.Handler, adminKey string, logger *slog.Logger) http.Handler {
	want := sha256.Sum256([]byte(adminKey))

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
		if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
			logger.Warn("admin: rejected request with invalid admin key",
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
			)
			http.Error(w, `{"error":"invalid admin key"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func limitAdminRequestBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, adminMaxRequestBody)
		next.ServeHTTP(w, r)
	})
}
