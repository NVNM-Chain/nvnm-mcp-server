// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ---------------------------------------------------------------------------
// Claims tests
// ---------------------------------------------------------------------------

func TestClaims_HasRole(t *testing.T) {
	c := &Claims{Roles: []string{"reader", "writer"}}
	if !c.HasRole("reader") {
		t.Error("expected HasRole(reader) = true")
	}
	if c.HasRole("admin") {
		t.Error("expected HasRole(admin) = false")
	}
}

func TestClaims_HasAnyRole(t *testing.T) {
	c := &Claims{Roles: []string{"reader"}}
	if !c.HasAnyRole("admin", "reader") {
		t.Error("expected HasAnyRole(admin, reader) = true")
	}
	if c.HasAnyRole("admin", "writer") {
		t.Error("expected HasAnyRole(admin, writer) = false")
	}
}

func TestClaims_HasRole_Empty(t *testing.T) {
	c := &Claims{}
	if c.HasRole("reader") {
		t.Error("expected HasRole on empty roles = false")
	}
	if c.HasAnyRole("reader") {
		t.Error("expected HasAnyRole on empty roles = false")
	}
}

// ---------------------------------------------------------------------------
// Context tests
// ---------------------------------------------------------------------------

func TestContextWithClaims_RoundTrip(t *testing.T) {
	c := &Claims{ClientID: "agent-1", Roles: []string{"writer"}}
	ctx := ContextWithClaims(context.Background(), c)

	got := ClaimsFromContext(ctx)
	if got == nil {
		t.Fatal("expected claims, got nil")
	}
	if got.ClientID != "agent-1" {
		t.Errorf("ClientID = %q, want %q", got.ClientID, "agent-1")
	}
}

func TestClaimsFromContext_EmptyContext(t *testing.T) {
	if got := ClaimsFromContext(context.Background()); got != nil {
		t.Errorf("expected nil claims, got %+v", got)
	}
}

func TestClientIDFromContext_BackwardCompat(t *testing.T) {
	ctx := ContextWithClaims(context.Background(), &Claims{ClientID: "my-client"})
	if got := ClientIDFromContext(ctx); got != "my-client" {
		t.Errorf("ClientIDFromContext = %q, want %q", got, "my-client")
	}

	if got := ClientIDFromContext(context.Background()); got != "" {
		t.Errorf("ClientIDFromContext on empty ctx = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// APIKeyValidator tests
// ---------------------------------------------------------------------------

// fakeKeyLookup indexes entries by raw key for test ergonomics. The
// validator expects KeyResult.KeyHash to be populated and to match
// HashKey(rawKey); fakeKeyLookup synthesizes it from the map key on
// each call so test setups don't have to compute hashes manually.
type fakeKeyLookup struct {
	entries map[string]*KeyResult
}

func (f *fakeKeyLookup) Lookup(_ context.Context, rawKey string) (*KeyResult, RejectReason) {
	e, ok := f.entries[rawKey]
	if !ok {
		return nil, RejectNotFound
	}
	// Defensive copy with KeyHash synthesized from the raw key.
	return &KeyResult{
		ID:      e.ID,
		KeyHash: HashKey(rawKey),
		Roles:   e.Roles,
	}, RejectNone
}

func (f *fakeKeyLookup) Empty() bool {
	return len(f.entries) == 0
}

func TestAPIKeyValidator_ValidKey(t *testing.T) {
	lookup := &fakeKeyLookup{entries: map[string]*KeyResult{
		"test-key-123": {ID: "client-a"},
	}}
	v := NewAPIKeyValidator(lookup)
	if v == nil {
		t.Fatal("expected non-nil validator")
	}

	claims, err := v.Validate(context.Background(), "test-key-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if claims.ClientID != "client-a" {
		t.Errorf("ClientID = %q, want %q", claims.ClientID, "client-a")
	}
}

func TestAPIKeyValidator_InvalidKey(t *testing.T) {
	lookup := &fakeKeyLookup{entries: map[string]*KeyResult{
		"real-key": {ID: "client-a"},
	}}
	v := NewAPIKeyValidator(lookup)

	_, err := v.Validate(context.Background(), "wrong-key")
	if err == nil {
		t.Fatal("expected error for invalid key")
	}
	if !errors.Is(err, ErrInvalidAPIKey) {
		t.Errorf("error = %v, want ErrInvalidAPIKey", err)
	}
}

func TestAPIKeyValidator_NilLookup(t *testing.T) {
	v := NewAPIKeyValidator(nil)
	if v != nil {
		t.Error("expected nil validator for nil lookup")
	}
}

func TestAPIKeyValidator_EmptyLookup(t *testing.T) {
	lookup := &fakeKeyLookup{entries: map[string]*KeyResult{}}
	v := NewAPIKeyValidator(lookup)
	if v != nil {
		t.Error("expected nil validator for empty lookup")
	}
}

func TestAPIKeyValidator_RolesPropagated(t *testing.T) {
	lookup := &fakeKeyLookup{entries: map[string]*KeyResult{
		"key": {ID: "agent", Roles: []string{"writer", "automation"}},
	}}
	v := NewAPIKeyValidator(lookup)

	claims, err := v.Validate(context.Background(), "key")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !claims.HasRole("writer") || !claims.HasRole("automation") {
		t.Errorf("roles = %v, want writer and automation", claims.Roles)
	}
}

func TestAPIKeyValidator_EmptyRolesNoEnforcement(t *testing.T) {
	lookup := &fakeKeyLookup{entries: map[string]*KeyResult{
		"key": {ID: "agent", Roles: nil},
	}}
	v := NewAPIKeyValidator(lookup)

	claims, err := v.Validate(context.Background(), "key")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(claims.Roles) != 0 {
		t.Errorf("expected empty roles, got %v", claims.Roles)
	}
}

func TestAPIKeyValidator_Close(t *testing.T) {
	lookup := &fakeKeyLookup{entries: map[string]*KeyResult{
		"k": {ID: "c"},
	}}
	v := NewAPIKeyValidator(lookup)
	if err := v.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// FusionAuth helper tests (matchIssuer, extractRolesFromValue)
// ---------------------------------------------------------------------------

func TestMatchIssuer(t *testing.T) {
	tests := []struct {
		actual, expected string
		want             bool
	}{
		{"https://auth.example.com", "https://auth.example.com", true},
		{"auth.example.com", "https://auth.example.com", true},
		{"auth.example.com", "http://auth.example.com", true},
		{"other.example.com", "https://auth.example.com", false},
		{"", "https://auth.example.com", false},
	}
	for _, tc := range tests {
		if got := matchIssuer(tc.actual, tc.expected); got != tc.want {
			t.Errorf("matchIssuer(%q, %q) = %v, want %v", tc.actual, tc.expected, got, tc.want)
		}
	}
}

func TestExtractRolesFromValue(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want int
	}{
		{"nil", nil, 0},
		{"string slice interface", []interface{}{"reader", "writer"}, 2},
		{"mixed types", []interface{}{"reader", 42, true}, 1},
		{"string slice", []string{"admin"}, 1},
		{"int", 42, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractRolesFromValue(tc.val)
			if len(got) != tc.want {
				t.Errorf("extractRolesFromValue(%v) returned %d roles, want %d", tc.val, len(got), tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FusionAuth JWT validation tests
// ---------------------------------------------------------------------------

// The raw JWT sub is email-reversible personal data and must never reach
// a log sink — not even at DEBUG. This runs Validate with a DEBUG-level
// logger (so any sub log line would fire) and asserts the sub is absent.
func TestFusionAuth_DoesNotLogRawSub(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	var buf bytes.Buffer
	debugLogger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
	}, debugLogger)
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	const rawSub = "super-secret-subject-uuid-9f8a2b40"
	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": rawSub,
		"iss": "https://auth.example.com",
		"aud": "test-app",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})

	if _, err := v.Validate(context.Background(), token); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	if strings.Contains(buf.String(), rawSub) {
		t.Errorf("validator logged the raw sub %q; it must never reach a log sink, even at DEBUG", rawSub)
	}
}

// TestFusionAuth_AutomationRoleInClaims verifies that the automation role
// is correctly extracted and present in Claims.Roles. The write-approval
// derivation (automation→auto) was removed in Option 0; roles are now
// used directly by RBAC, not by a separate approval gate.
func TestFusionAuth_AutomationRoleInClaims(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
		RolesClaim:      "roles",
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub":   "pipeline-agent",
		"iss":   "https://auth.example.com",
		"aud":   "test-app",
		"roles": []string{"writer", "automation"},
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	})

	claims, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	// ClientID is the keyed HMAC of the sub, never the raw sub — that is
	// the privacy guarantee that keeps the email-reversible sub out of logs.
	if claims.ClientID == "pipeline-agent" {
		t.Error("ClientID must not be the raw sub")
	}
	if want := hmacClientID("pipeline-agent", []byte("test-client-id-hmac-key")); claims.ClientID != want {
		t.Errorf("ClientID = %q, want HMAC(sub) = %q", claims.ClientID, want)
	}
	if !claims.HasRole("writer") || !claims.HasRole("automation") {
		t.Errorf("roles = %v, want writer and automation", claims.Roles)
	}
}

func TestFusionAuth_InvalidIssuer(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": "user",
		"iss": "https://evil.example.com",
		"aud": "test-app",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestFusionAuth_InvalidAudience(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": "user",
		"iss": "https://auth.example.com",
		"aud": "wrong-app",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestFusionAuth_ExpiredToken(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
		ClockSkew:       1 * time.Second,
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": "user",
		"iss": "https://auth.example.com",
		"aud": "test-app",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	_, err = v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestFusionAuth_AppScopedRoles(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "app-uuid-123",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": "user",
		"iss": "https://auth.example.com",
		"aud": "app-uuid-123",
		"exp": time.Now().Add(time.Hour).Unix(),
		"app-uuid-123": map[string]interface{}{
			"roles": []interface{}{"admin", "automation"},
		},
	})

	claims, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !claims.HasRole("admin") {
		t.Error("expected admin role from app-scoped claims")
	}
	if !claims.HasRole("automation") {
		t.Error("expected automation role from app-scoped claims")
	}
}

func TestFusionAuth_BadSignature(t *testing.T) {
	_, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	differentKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token := signTestJWT(t, differentKey, jwt.MapClaims{
		"sub": "user",
		"iss": "https://auth.example.com",
		"aud": "test-app",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	_, err = v.Validate(context.Background(), token)
	if err == nil {
		t.Fatal("expected error for bad signature")
	}
}

func TestFusionAuth_GarbageToken(t *testing.T) {
	_, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("test-client-id-hmac-key"),
	}, discardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	_, err = v.Validate(context.Background(), "not.a.jwt")
	if err == nil {
		t.Fatal("expected error for garbage token")
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func setupTestJWKS(t *testing.T) (*rsa.PrivateKey, *httptest.Server) {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	jwksJSON := buildJWKS(t, &key.PublicKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	}))

	return key, srv
}

func buildJWKS(t *testing.T, pub *rsa.PublicKey) []byte {
	t.Helper()

	eInt := big.NewInt(int64(pub.E))

	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": "test-key-id",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(eInt.Bytes()),
			},
		},
	}

	data, err := json.Marshal(jwks)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	return data
}

func signTestJWT(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = "test-key-id"
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign JWT: %v", err)
	}
	return signed
}
