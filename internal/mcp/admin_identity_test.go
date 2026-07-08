// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAdminAuth_ResolvesActor pins that adminAuth resolves the presented
// bearer against the identity map and injects the matched admin id into
// the request context, and that unknown bearers still 401 exactly as
// before the identity-map refactor.
func TestAdminAuth_ResolvesActor(t *testing.T) {
	keys := map[[32]byte]string{
		sha256.Sum256([]byte("ka")):     "alice",
		sha256.Sum256([]byte("single")): "admin",
	}
	var gotActor string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActor = adminActorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := adminAuth(next, keys, discardLogger())
	// alice's key
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", http.NoBody)
	req.Header.Set("Authorization", "Bearer ka")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotActor != "alice" {
		t.Fatalf("code=%d actor=%q", rec.Code, gotActor)
	}
	// unknown key -> 401
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/keys", http.NoBody)
	req.Header.Set("Authorization", "Bearer nope")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key code=%d, want 401", rec.Code)
	}
}

// TestAdminAuth_ResolvesActor_SingleKeyAndMissingHeader covers the
// remaining table cases from the brief not exercised above: the
// single-key map resolving to the "admin" actor, and the missing-header
// 401 path staying unchanged.
func TestAdminAuth_ResolvesActor_SingleKeyAndMissingHeader(t *testing.T) {
	keys := map[[32]byte]string{
		sha256.Sum256([]byte("single")): "admin",
	}
	var gotActor string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotActor = adminActorFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	h := adminAuth(next, keys, discardLogger())

	// single-key map -> actor "admin"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/keys", http.NoBody)
	req.Header.Set("Authorization", "Bearer single")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || gotActor != "admin" {
		t.Fatalf("code=%d actor=%q, want 200/admin", rec.Code, gotActor)
	}

	// missing header -> 401, unchanged
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/keys", http.NoBody)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing header code=%d, want 401", rec.Code)
	}
}
