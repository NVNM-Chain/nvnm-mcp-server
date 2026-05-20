// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import (
	"errors"
	"io"
	"log/slog"
	"testing"
)

func authDiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// The FusionAuth sub must be turned into a client_id via a keyed,
// one-way transform: stable for audit correlation, never equal to the
// raw sub, key-dependent so logs cannot be reversed to identity without
// the server-held key, and empty for a token that carries no sub.
func TestHMACClientID(t *testing.T) {
	key := []byte("server-held-hmac-key-for-tests")
	sub := "9f8a2b40-4f1d-4d28-8e3f-71f0c2a9e3c1"

	got := hmacClientID(sub, key)

	if got == "" {
		t.Fatal("hmacClientID returned empty for a non-empty sub")
	}
	if got == sub {
		t.Fatal("hmacClientID must not return the raw sub")
	}
	if again := hmacClientID(sub, key); again != got {
		t.Fatalf("hmacClientID must be stable: %q != %q", again, got)
	}
	if other := hmacClientID("a-different-sub", key); other == got {
		t.Fatal("distinct subs must produce distinct client ids")
	}
	if diffKey := hmacClientID(sub, []byte("a-different-key")); diffKey == got {
		t.Fatal("a different HMAC key must produce a different client id")
	}
	if empty := hmacClientID("", key); empty != "" {
		t.Fatalf("empty sub must map to empty client id, got %q", empty)
	}
}

// FusionAuth mode must fail loud at startup if the client-id HMAC key is
// not configured, rather than silently logging raw subs. The check must
// fire before the JWKS network fetch so a misconfiguration is caught
// without a live FusionAuth instance.
func TestNewFusionAuthValidator_RequiresClientIDHMACKey(t *testing.T) {
	_, err := NewFusionAuthValidator(&FusionAuthConfig{
		BaseURL:       "https://fusionauth.example.com",
		ApplicationID: "app-uuid",
		// ClientIDHMACKey deliberately unset.
	}, authDiscardLogger())

	if !errors.Is(err, ErrMissingClientIDHMACKey) {
		t.Fatalf("want ErrMissingClientIDHMACKey when the HMAC key is unset, got %v", err)
	}
}
