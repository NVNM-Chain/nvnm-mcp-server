// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// keyReqTestSetup wires a real PendingKeyStore on a tempfile + a
// silenced logger + a handler with no rate limiter (so individual
// scenarios can swap in a rate limiter when they exercise that
// pathway).
func keyReqTestSetup(t *testing.T) (*PendingKeyStore, http.Handler) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "keys_pending.json")
	store, err := NewPendingKeyStore(path)
	if err != nil {
		t.Fatalf("NewPendingKeyStore: %v", err)
	}
	h := NewKeyRequestHandler(KeyRequestHandlerConfig{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return store, h
}

func postJSON(handler http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, KeyRequestPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestKeyRequest_HappyPath202 pins the contract: a valid request returns
// 202 with { request_id, status: "pending" } and writes the entry to the
// store. RD3's response shape is load-bearing for the eventual transition
// to auto-issuance so it's worth a positive assertion here.
func TestKeyRequest_HappyPath202(t *testing.T) {
	t.Parallel()
	store, h := keyReqTestSetup(t)

	rec := postJSON(h, `{"email":"a@example.test","company":"Acme","intended_use":"building an agent"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}

	var got KeyRequestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if got.RequestID == "" {
		t.Error("RequestID empty")
	}
	if got.Status != PendingStatusPending {
		t.Errorf("Status = %q, want %q", got.Status, PendingStatusPending)
	}

	if persisted, ok := store.Get(got.RequestID); !ok {
		t.Errorf("Get(%q) on store returned !ok after Accept", got.RequestID)
	} else if persisted.Email != "a@example.test" || persisted.Company != "Acme" {
		t.Errorf("persisted = %+v", persisted)
	}
}

func TestKeyRequest_GetReturns405(t *testing.T) {
	t.Parallel()
	_, h := keyReqTestSetup(t)
	req := httptest.NewRequest(http.MethodGet, KeyRequestPath, http.NoBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow header = %q, want %q", got, http.MethodPost)
	}
}

// TestKeyRequest_NonJSONContentTypeRejected pins the Content-Type guard.
// A request with text/plain that happens to look like JSON should be
// rejected at the boundary, not silently parsed.
func TestKeyRequest_NonJSONContentTypeRejected(t *testing.T) {
	t.Parallel()
	_, h := keyReqTestSetup(t)
	req := httptest.NewRequest(http.MethodPost, KeyRequestPath, strings.NewReader(`{"email":"a@example.test","intended_use":"x"}`))
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415", rec.Code)
	}
}

func TestKeyRequest_InvalidJSON400(t *testing.T) {
	t.Parallel()
	_, h := keyReqTestSetup(t)
	rec := postJSON(h, `not-a-json-body`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestKeyRequest_UnknownFieldsRejected(t *testing.T) {
	t.Parallel()
	_, h := keyReqTestSetup(t)
	// extra_field is not declared on KeyRequestInput; DisallowUnknownFields
	// is the boundary defense against form-creep ("we added a field client-
	// side, did the server pick it up?").
	rec := postJSON(h, `{"email":"a@example.test","intended_use":"x","extra_field":"surprise"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (DisallowUnknownFields)", rec.Code)
	}
}

func TestKeyRequest_ValidationRejections(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "missing email", body: `{"intended_use":"x"}`},
		{name: "blank email", body: `{"email":"   ","intended_use":"x"}`},
		{name: "invalid email", body: `{"email":"not-an-email","intended_use":"x"}`},
		{name: "email too long", body: `{"email":"` + strings.Repeat("a", 320) + `@example.test","intended_use":"x"}`},
		{name: "missing intended_use", body: `{"email":"a@example.test"}`},
		{name: "blank intended_use", body: `{"email":"a@example.test","intended_use":"  "}`},
		{name: "intended_use too long", body: `{"email":"a@example.test","intended_use":"` + strings.Repeat("x", 3000) + `"}`},
		{name: "company too long", body: `{"email":"a@example.test","intended_use":"x","company":"` + strings.Repeat("c", 250) + `"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, h := keyReqTestSetup(t)
			rec := postJSON(h, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// TestKeyRequest_RateLimited429 exercises the per-IP rate limiter path.
// With burst=1 and rate=0 (no token refill within the test timeline)
// the second request from the same IP must 429.
func TestKeyRequest_RateLimited429(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "keys_pending.json")
	store, err := NewPendingKeyStore(path)
	if err != nil {
		t.Fatalf("NewPendingKeyStore: %v", err)
	}
	limiter := NewKeyRequestRateLimiter(0.0001, 1, false)
	t.Cleanup(limiter.Stop)
	limiter.Start()

	h := NewKeyRequestHandler(KeyRequestHandlerConfig{
		Store:       store,
		RateLimiter: limiter,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, KeyRequestPath,
			bytes.NewReader([]byte(`{"email":"a@example.test","intended_use":"x"}`)))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:5555"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := send(); rec.Code != http.StatusAccepted {
		t.Fatalf("first request status = %d, want 202; body=%s", rec.Code, rec.Body.String())
	}
	rec := send()
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429", rec.Code)
	}
}
