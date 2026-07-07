// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
)

// TestValidateKeyRequestEmail covers the F4 fail-closed guard: when the
// self-serve key-request flow is enabled without SMTP, the approval path
// would fall back to logging freshly-minted API keys at WARN. That leaky
// path must be an explicit operator opt-in (NVNM_ALLOW_KEY_IN_LOGS), not a
// silent default.
func TestValidateKeyRequestEmail(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		smtpHost  string
		allowLogs bool
		wantErr   error
	}{
		{
			name:    "enabled, no SMTP, no opt-in -> fail closed",
			enabled: true, smtpHost: "", allowLogs: false,
			wantErr: ErrKeyInLogsNotAllowed,
		},
		{
			name:    "enabled, no SMTP, explicit opt-in -> ok",
			enabled: true, smtpHost: "", allowLogs: true,
			wantErr: nil,
		},
		{
			name:    "enabled, SMTP configured -> ok (keys delivered by email, never logged)",
			enabled: true, smtpHost: "smtp.example.com", allowLogs: false,
			wantErr: nil,
		},
		{
			name:    "key-request disabled -> guard silent (log-only sender never mints/logs a key)",
			enabled: false, smtpHost: "", allowLogs: false,
			wantErr: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{
				KeyRequestEnabled: tc.enabled,
				SMTPHost:          tc.smtpHost,
				AllowKeyInLogs:    tc.allowLogs,
			}
			err := c.validateKeyRequestEmail()
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("validateKeyRequestEmail() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("validateKeyRequestEmail() = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
