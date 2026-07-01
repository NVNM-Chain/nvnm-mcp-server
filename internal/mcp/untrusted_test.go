// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapUntrusted(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		max     int
		wantCut bool // whether a truncation marker should appear
	}{
		{"under cap", "hello", 256, false},
		{"at cap", strings.Repeat("a", 10), 10, false},
		{"over cap", strings.Repeat("a", 20), 10, true},
		{"empty", "", 10, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := capUntrusted(tc.in, tc.max)
			if tc.wantCut {
				if !strings.Contains(got, "[truncated,") {
					t.Fatalf("want truncation marker, got %q", got)
				}
				if tc.in == strings.Repeat("a", 20) && !strings.Contains(got, "20 bytes") {
					t.Fatalf("marker should carry original byte length, got %q", got)
				}
			} else if got != tc.in {
				t.Fatalf("under/at cap should be unchanged: got %q want %q", got, tc.in)
			}
		})
	}
}

// A multibyte string truncated near the cap must remain valid UTF-8 (never
// split a rune) so the emitted JSON is well-formed.
func TestCapUntrusted_RuneBoundary(t *testing.T) {
	// "€" is 3 bytes (0xE2 0x82 0xAC). 4 of them = 12 bytes; cap at 10 must
	// back off to a rune boundary (9 bytes = 3 runes), not split the 4th.
	in := strings.Repeat("€", 4)
	got := capUntrusted(in, 10)
	idx := strings.Index(got, "…")
	if idx < 0 {
		t.Fatalf("expected truncation marker in %q", got)
	}
	prefix := got[:idx] // content before the marker
	if !utf8.ValidString(prefix) {
		t.Fatalf("truncated content is not valid UTF-8: %q", prefix)
	}
	if prefix != strings.Repeat("€", 3) {
		t.Fatalf("want 3 runes retained (9 bytes), got %q", prefix)
	}
}
