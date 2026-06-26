// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
)

func TestClassifyEntry(t *testing.T) {
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	tests := []struct {
		name string
		e    *KeyEntry
		want auth.RejectReason
	}{
		{"nil -> not found", nil, auth.RejectNotFound},
		{"enabled no expiry -> ok", &KeyEntry{Enabled: true}, auth.RejectNone},
		{"enabled future expiry -> ok", &KeyEntry{Enabled: true, ExpiresAt: future}, auth.RejectNone},
		{"enabled past expiry -> expired", &KeyEntry{Enabled: true, ExpiresAt: past}, auth.RejectExpired},
		{"enabled expiry == now -> expired", &KeyEntry{Enabled: true, ExpiresAt: now}, auth.RejectExpired},
		{"disabled -> revoked", &KeyEntry{Enabled: false}, auth.RejectRevoked},
		{"disabled AND expired -> revoked (precedence)", &KeyEntry{Enabled: false, ExpiresAt: past}, auth.RejectRevoked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyEntry(tt.e, now); got != tt.want {
				t.Errorf("classifyEntry = %v, want %v", got, tt.want)
			}
		})
	}
}
