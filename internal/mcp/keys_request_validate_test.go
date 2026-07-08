// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "testing"

// TestValidateKeyRequest_ControlChars covers F3: the untrusted free-text
// fields (company, intended_use) must reject disallowed control characters
// (NUL, carriage return, terminal escapes) while still accepting ordinary
// prose including newlines and tabs. Email is separately guarded by
// mail.ParseAddress, which already rejects CR/LF.
func TestValidateKeyRequest_ControlChars(t *testing.T) {
	base := KeyRequestInput{
		Email:       "user@example.com",
		Company:     "Acme Corp",
		IntendedUse: "Anchoring document hashes for our audit workflow.",
	}
	// Sanity: the baseline input is valid.
	if msg := validateKeyRequest(base); msg != "" {
		t.Fatalf("baseline input rejected: %q", msg)
	}

	tests := []struct {
		name   string
		mutate func(in *KeyRequestInput)
		wantOK bool
	}{
		{"company with NUL", func(in *KeyRequestInput) { in.Company = "Acme\x00Corp" }, false},
		{"company with CR (CRLF injection)", func(in *KeyRequestInput) { in.Company = "Acme\rEvil" }, false},
		{"intended_use with escape seq", func(in *KeyRequestInput) { in.IntendedUse = "prose\x1b[31mred" }, false},
		{"company with RLO bidi override (Trojan Source)", func(in *KeyRequestInput) { in.Company = "Acme\u202eEvil" }, false},
		{"intended_use with zero-width space", func(in *KeyRequestInput) { in.IntendedUse = "ze\u200bro" }, false},
		{"intended_use with newline is fine (prose)", func(in *KeyRequestInput) { in.IntendedUse = "line one\nline two" }, true},
		{"intended_use with tab is fine", func(in *KeyRequestInput) { in.IntendedUse = "col1\tcol2" }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mutate(&in)
			msg := validateKeyRequest(in)
			if tc.wantOK && msg != "" {
				t.Errorf("validateKeyRequest = %q, want valid", msg)
			}
			if !tc.wantOK && msg == "" {
				t.Errorf("validateKeyRequest accepted input with a disallowed control char")
			}
		})
	}
}
