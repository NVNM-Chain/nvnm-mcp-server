// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package auth

import "testing"

func TestIsValidRole(t *testing.T) {
	for _, r := range []string{"reader", "writer", "admin", "automation"} {
		if !IsValidRole(r) {
			t.Errorf("expected %q to be valid", r)
		}
	}
	for _, r := range []string{"", "Reader", "owner", "writers"} {
		if IsValidRole(r) {
			t.Errorf("expected %q to be invalid", r)
		}
	}
}
