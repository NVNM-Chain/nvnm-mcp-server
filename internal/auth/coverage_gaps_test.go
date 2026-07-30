// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// rejectingLookup returns a fixed RejectReason for every lookup so the
// validator's expired/revoked branches can be exercised directly.
type rejectingLookup struct {
	reason RejectReason
}

func (r *rejectingLookup) Lookup(_ context.Context, _ string) (*KeyResult, RejectReason) {
	return nil, r.reason
}

func (r *rejectingLookup) Empty() bool { return false }

func TestAPIKeyValidator_ExpiredKey(t *testing.T) {
	v := NewAPIKeyValidator(&rejectingLookup{reason: RejectExpired})
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	_, err := v.Validate(context.Background(), "some-key")
	if !errors.Is(err, ErrKeyExpired) {
		t.Fatalf("error = %v, want ErrKeyExpired", err)
	}
}

func TestAPIKeyValidator_RevokedKey(t *testing.T) {
	v := NewAPIKeyValidator(&rejectingLookup{reason: RejectRevoked})
	if v == nil {
		t.Fatal("expected non-nil validator")
	}
	_, err := v.Validate(context.Background(), "some-key")
	if !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("error = %v, want ErrKeyRevoked", err)
	}
}

func TestNewFusionAuthValidator_RequiresBaseURL(t *testing.T) {
	_, err := NewFusionAuthValidator(&FusionAuthConfig{
		ApplicationID:   "app",
		ClientIDHMACKey: []byte("k"),
	}, authDiscardLogger())
	if !errors.Is(err, ErrMissingBaseURL) {
		t.Fatalf("error = %v, want ErrMissingBaseURL", err)
	}
}

func TestNewFusionAuthValidator_RequiresApplicationID(t *testing.T) {
	_, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ClientIDHMACKey: []byte("k"),
	}, authDiscardLogger())
	if !errors.Is(err, ErrMissingAppID) {
		t.Fatalf("error = %v, want ErrMissingAppID", err)
	}
}

func TestNewFusionAuthValidator_JWKSFetchFailure(t *testing.T) {
	// A JWKS URL that cannot even be parsed must fail construction
	// loudly (keyfunc tolerates transient HTTP failures, so a malformed
	// URL is the deterministic, network-free way to hit this branch).
	_, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "app",
		JWKSURL:         "://not-a-url",
		ClientIDHMACKey: []byte("k"),
	}, authDiscardLogger())
	if err == nil {
		t.Fatal("expected JWKS fetch error, got nil")
	}
}

// TestNewFusionAuthValidator_DerivesJWKSURLFromBaseURL exercises the
// "no explicit JWKSURL" branch: the validator must fetch
// BaseURL + /.well-known/jwks.json. The test JWKS server ignores the
// path, so a successful construction proves the derived URL was hit.
// Issuer is also left empty so Validate falls back to BaseURL as the
// expected issuer.
func TestNewFusionAuthValidator_DerivesJWKSURLFromBaseURL(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         jwksServer.URL + "/", // trailing slash exercises TrimRight
		ApplicationID:   "test-app",
		ClientIDHMACKey: []byte("k"),
	}, authDiscardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": "someone",
		"iss": jwksServer.URL + "/",
		"aud": "test-app",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	claims, err := v.Validate(context.Background(), token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if claims.ClientID == "" {
		t.Error("ClientID should be non-empty for a token with a sub")
	}
}

func TestFusionAuth_MissingAudienceRejected(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("k"),
	}, authDiscardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	token := signTestJWT(t, key, jwt.MapClaims{
		"sub": "someone",
		"iss": "https://auth.example.com",
		// no aud claim at all
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})
	if _, err := v.Validate(context.Background(), token); !errors.Is(err, ErrInvalidAudience) {
		t.Fatalf("error = %v, want ErrInvalidAudience", err)
	}
}

func TestFusionAuth_AudienceList(t *testing.T) {
	key, jwksServer := setupTestJWKS(t)
	defer jwksServer.Close()

	v, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:         "https://auth.example.com",
		ApplicationID:   "test-app",
		Issuer:          "https://auth.example.com",
		JWKSURL:         jwksServer.URL,
		ClientIDHMACKey: []byte("k"),
	}, authDiscardLogger())
	if err != nil {
		t.Fatalf("NewFusionAuthValidator: %v", err)
	}
	defer v.Close()

	cases := []struct {
		name    string
		aud     any
		wantErr bool
	}{
		{"list containing app id", []any{"other-app", "test-app"}, false},
		{"list without app id", []any{"other-app", 42}, true},
		{"non-string non-list aud", 12345, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			token := signTestJWT(t, key, jwt.MapClaims{
				"sub": "someone",
				"iss": "https://auth.example.com",
				"aud": tc.aud,
				"exp": time.Now().Add(time.Hour).Unix(),
				"iat": time.Now().Unix(),
			})
			_, err := v.Validate(context.Background(), token)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidAudience) {
					t.Fatalf("error = %v, want ErrInvalidAudience", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}
