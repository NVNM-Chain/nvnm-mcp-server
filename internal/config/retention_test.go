// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
	"time"
)

func TestRetentionConfig_Enabled(t *testing.T) {
	tests := []struct {
		name string
		cfg  RetentionConfig
		want bool
	}{
		{"all zero is the default and disables the purge", RetentionConfig{}, false},
		{"a purge interval alone does not enable anything",
			RetentionConfig{PurgeInterval: time.Hour}, false},
		{"one window enables it", RetentionConfig{WriteAudit: time.Hour}, true},
		{"blacklist-only enables it", RetentionConfig{SignerBlacklist: time.Hour}, true},
		{"admin-audit-only enables it", RetentionConfig{AdminAudit: time.Hour}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRetentionConfig_Validate(t *testing.T) {
	const (
		day90 = 90 * 24 * time.Hour
		yr1   = 365 * 24 * time.Hour
	)
	tests := []struct {
		name    string
		cfg     RetentionConfig
		wantErr error
	}{
		{
			name: "disabled config needs no interval",
			cfg:  RetentionConfig{},
		},
		{
			name: "the hosted posture: 90d ordinary, 12mo grantRole",
			cfg: RetentionConfig{
				WriteAudit:          day90,
				WriteAuditGrantRole: yr1,
				PurgeInterval:       time.Hour,
			},
		},
		{
			name: "a window with no interval would never fire",
			cfg: RetentionConfig{
				WriteAudit:    day90,
				PurgeInterval: 0,
			},
			wantErr: ErrRetentionIntervalInvalid,
		},
		{
			// The inversion that would purge the administrative audit trail
			// before the routine traffic it is meant to outlive.
			name: "grantRole window shorter than the ordinary window",
			cfg: RetentionConfig{
				WriteAudit:          yr1,
				WriteAuditGrantRole: day90,
				PurgeInterval:       time.Hour,
			},
			wantErr: ErrRetentionGrantRoleShorter,
		},
		{
			// Zero means INFINITE, so an unset ordinary window is longer than
			// any finite grantRole window: same inversion, different route.
			name: "grantRole window set while ordinary is unset (= infinite)",
			cfg: RetentionConfig{
				WriteAudit:          0,
				WriteAuditGrantRole: yr1,
				PurgeInterval:       time.Hour,
			},
			wantErr: ErrRetentionGrantRoleShorter,
		},
		{
			name: "equal windows are fine",
			cfg: RetentionConfig{
				WriteAudit:          yr1,
				WriteAuditGrantRole: yr1,
				PurgeInterval:       time.Hour,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadRetentionConfig(t *testing.T) {
	t.Run("unset means retain indefinitely", func(t *testing.T) {
		c := &Config{}
		if err := c.loadRetentionConfig(); err != nil {
			t.Fatalf("loadRetentionConfig: %v", err)
		}
		if c.Retention.Enabled() {
			t.Error("no env vars set must leave retention disabled")
		}
	})

	t.Run("the hosted posture parses", func(t *testing.T) {
		t.Setenv("MCP_WRITE_AUDIT_RETENTION", "2160h")            // 90d
		t.Setenv("MCP_WRITE_AUDIT_GRANT_ROLE_RETENTION", "8760h") // 365d
		c := &Config{}
		if err := c.loadRetentionConfig(); err != nil {
			t.Fatalf("loadRetentionConfig: %v", err)
		}
		if c.Retention.WriteAudit != 2160*time.Hour {
			t.Errorf("WriteAudit = %v, want 2160h", c.Retention.WriteAudit)
		}
		if c.Retention.WriteAuditGrantRole != 8760*time.Hour {
			t.Errorf("WriteAuditGrantRole = %v, want 8760h", c.Retention.WriteAuditGrantRole)
		}
		if c.Retention.PurgeInterval != time.Hour {
			t.Errorf("PurgeInterval = %v, want the 1h default", c.Retention.PurgeInterval)
		}
	})

	t.Run("a negative window is a typo, not an instruction to delete everything", func(t *testing.T) {
		t.Setenv("MCP_WRITE_AUDIT_RETENTION", "-90h")
		c := &Config{}
		if err := c.loadRetentionConfig(); !errors.Is(err, ErrRetentionNegative) {
			t.Fatalf("loadRetentionConfig = %v, want ErrRetentionNegative", err)
		}
	})

	t.Run("an unparseable duration fails boot", func(t *testing.T) {
		t.Setenv("MCP_ADMIN_AUDIT_RETENTION", "90 days")
		c := &Config{}
		if err := c.loadRetentionConfig(); err == nil {
			t.Fatal("loadRetentionConfig accepted an unparseable duration")
		}
	})
}
