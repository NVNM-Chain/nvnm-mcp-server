// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"os"
	"testing"
)

// TestEnvBool verifies the boolean env parser accepts every strconv.ParseBool
// spelling, trims whitespace, returns the fallback only when unset/empty, and
// fails loud (rather than silently coercing) on an unrecognized value. The
// last property is the regression guard for the ENABLE_WRITE_TOOLS=1 / =True
// silent read-only trap.
func TestEnvBool(t *testing.T) {
	const key = "NVNM_TEST_ENVBOOL"

	tests := []struct {
		name     string
		set      bool
		value    string
		fallback bool
		want     bool
		wantErr  bool
	}{
		{name: "unset returns fallback true", set: false, fallback: true, want: true},
		{name: "unset returns fallback false", set: false, fallback: false, want: false},
		{name: "empty returns fallback", set: true, value: "", fallback: true, want: true},
		{name: "whitespace-only returns fallback", set: true, value: "   ", fallback: true, want: true},
		{name: "lowercase true", set: true, value: "true", fallback: false, want: true},
		{name: "TitleCase True", set: true, value: "True", fallback: false, want: true},
		{name: "UPPER TRUE", set: true, value: "TRUE", fallback: false, want: true},
		{name: "numeric 1", set: true, value: "1", fallback: false, want: true},
		{name: "short t", set: true, value: "t", fallback: false, want: true},
		{name: "leading/trailing space trimmed", set: true, value: "  true  ", fallback: false, want: true},
		{name: "lowercase false", set: true, value: "false", fallback: true, want: false},
		{name: "UPPER FALSE", set: true, value: "FALSE", fallback: true, want: false},
		{name: "numeric 0", set: true, value: "0", fallback: true, want: false},
		{name: "yes is rejected", set: true, value: "yes", fallback: false, wantErr: true},
		{name: "on is rejected", set: true, value: "on", fallback: false, wantErr: true},
		{name: "numeric 2 is rejected", set: true, value: "2", fallback: false, wantErr: true},
		{name: "garbage is rejected", set: true, value: "maybe", fallback: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				t.Setenv(key, tt.value)
			} else {
				// Guard against the key leaking in from the ambient env.
				if err := os.Unsetenv(key); err != nil {
					t.Fatalf("unsetenv: %v", err)
				}
			}

			got, err := envBool(key, tt.fallback)
			if (err != nil) != tt.wantErr {
				t.Fatalf("envBool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got != tt.want {
				t.Errorf("envBool(%q, %v) = %v, want %v", tt.value, tt.fallback, got, tt.want)
			}
		})
	}
}
