// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
)

// TestLoad_ParseErrors drives every fail-loud env-var parse branch in
// Load's helper loaders: each case sets one malformed variable on top
// of the minimal valid environment and expects Load to fail.
func TestLoad_ParseErrors(t *testing.T) {
	cases := []struct {
		key   string
		value string
	}{
		{"JWT_CLOCK_SKEW", "not-a-duration"},
		{"OTEL_TRACE_SAMPLE_RATIO", "not-a-float"},
		{"RPC_MAX_RETRIES", "not-an-int"},
		{"RPC_INITIAL_BACKOFF", "not-a-duration"},
		{"RPC_MAX_BACKOFF", "not-a-duration"},
		{"RPC_RATE_LIMIT", "not-a-float"},
		{"RPC_RATE_BURST", "not-an-int"},
		{"CIRCUIT_BREAKER_THRESHOLD", "not-a-uint"},
		{"CIRCUIT_BREAKER_TIMEOUT", "not-a-duration"},
		{"MCP_RATE_LIMIT", "not-a-float"},
		{"MCP_RATE_BURST", "not-an-int"},
		{"MCP_KEYLESS_READS", "not-a-bool"},
		{"MCP_KEYLESS_WRITES", "not-a-bool"},
		{"MCP_RELAY_ALLOW_ANY", "not-a-bool"},
		{"MCP_ANON_RATE_LIMIT", "not-a-float"},
		{"MCP_ANON_RATE_BURST", "not-an-int"},
		{"MCP_SIGNER_WRITE_RATE", "not-an-int"},
		{"MCP_SIGNER_WRITE_WINDOW", "not-a-duration"},
		{"MCP_SIGNER_QUOTA_FAIL_OPEN", "not-a-bool"},
		{"MCP_SIGNER_BLACKLIST_FAIL_OPEN", "not-a-bool"},
		{"MCP_RETENTION_PURGE_INTERVAL", "not-a-duration"},
		{"NVNM_KEY_REQUEST_ENABLED", "not-a-bool"},
		{"NVNM_ALLOW_KEY_IN_LOGS", "not-a-bool"},
		{"NVNM_KEY_REQUEST_RATE_LIMIT", "not-a-float"},
		{"NVNM_KEY_REQUEST_RATE_BURST", "not-an-int"},
		{"NVNM_KEY_REQUEST_MAX_BODY_BYTES", "not-an-int"},
		{"NVNM_SMTP_PORT", "not-an-int"},
		{"NVNM_TRUSTED_PROXY_HOPS", "not-an-int"},
		{"ENABLE_PROMETHEUS", "not-a-bool"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			clearEnv(t)
			setMinimalEnv(t)
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load() with %s=%q should fail", tc.key, tc.value)
			}
		})
	}
}

// TestLoad_ValidationSentinels drives the Validate branches that only
// fire on values that parse cleanly but are semantically invalid.
func TestLoad_ValidationSentinels(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr error
	}{
		{
			name:    "negative request timeout",
			env:     map[string]string{"REQUEST_TIMEOUT": "-5s"},
			wantErr: ErrInvalidTimeout,
		},
		{
			name:    "negative rpc retries",
			env:     map[string]string{"RPC_MAX_RETRIES": "-1"},
			wantErr: ErrInvalidRetries,
		},
		{
			name:    "zero initial backoff",
			env:     map[string]string{"RPC_INITIAL_BACKOFF": "0s"},
			wantErr: ErrInvalidBackoff,
		},
		{
			name:    "zero rate burst",
			env:     map[string]string{"RPC_RATE_BURST": "0"},
			wantErr: ErrInvalidRateBurst,
		},
		{
			name:    "zero mcp rate limit",
			env:     map[string]string{"MCP_RATE_LIMIT": "0"},
			wantErr: ErrInvalidMCPRateLimit,
		},
		{
			name:    "zero mcp rate burst",
			env:     map[string]string{"MCP_RATE_BURST": "0"},
			wantErr: ErrInvalidMCPRateBurst,
		},
		{
			name:    "zero anon rate limit",
			env:     map[string]string{"MCP_ANON_RATE_LIMIT": "0"},
			wantErr: ErrInvalidAnonRateLimit,
		},
		{
			name:    "zero anon rate burst",
			env:     map[string]string{"MCP_ANON_RATE_BURST": "0"},
			wantErr: ErrInvalidAnonRateBurst,
		},
		{
			name:    "invalid key store backend",
			env:     map[string]string{"KEY_STORE_BACKEND": "etcd"},
			wantErr: ErrInvalidKeyStoreBackend,
		},
		{
			name: "key request without pending file",
			env: map[string]string{
				"NVNM_KEY_REQUEST_ENABLED": "true",
			},
			wantErr: ErrMissingKeyPendingFile,
		},
		{
			name: "key request rate limit zero",
			env: map[string]string{
				"NVNM_KEY_REQUEST_ENABLED":    "true",
				"NVNM_KEY_PENDING_FILE":       "/tmp/pending.json",
				"NVNM_KEY_REQUEST_RATE_LIMIT": "0",
			},
			wantErr: ErrInvalidKeyRequestRateLimit,
		},
		{
			name: "key request rate burst zero",
			env: map[string]string{
				"NVNM_KEY_REQUEST_ENABLED":    "true",
				"NVNM_KEY_PENDING_FILE":       "/tmp/pending.json",
				"NVNM_KEY_REQUEST_RATE_BURST": "0",
			},
			wantErr: ErrInvalidKeyRequestRateBurst,
		},
		{
			name: "key request max body zero",
			env: map[string]string{
				"NVNM_KEY_REQUEST_ENABLED":        "true",
				"NVNM_KEY_PENDING_FILE":           "/tmp/pending.json",
				"NVNM_KEY_REQUEST_MAX_BODY_BYTES": "0",
			},
			wantErr: ErrInvalidKeyRequestMaxBody,
		},
		{
			name: "key request enabled without smtp or opt-in",
			env: map[string]string{
				"NVNM_KEY_REQUEST_ENABLED": "true",
				"NVNM_KEY_PENDING_FILE":    "/tmp/pending.json",
			},
			wantErr: ErrKeyInLogsNotAllowed,
		},
		{
			name:    "smtp host without port",
			env:     map[string]string{"NVNM_SMTP_HOST": "smtp.example.com"},
			wantErr: ErrMissingSMTPPort,
		},
		{
			name: "smtp host without from",
			env: map[string]string{
				"NVNM_SMTP_HOST": "smtp.example.com",
				"NVNM_SMTP_PORT": "587",
			},
			wantErr: ErrMissingSMTPFrom,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("NVNM_ALLOW_KEY_IN_LOGS", "")
			setMinimalEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := Load()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Load() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestLoad_SMTPConfigured verifies the complete-SMTP happy path (port
// parse branch plus the "host set, everything present" acceptance).
func TestLoad_SMTPConfigured(t *testing.T) {
	clearEnv(t)
	setMinimalEnv(t)
	t.Setenv("NVNM_SMTP_HOST", "smtp.example.com")
	t.Setenv("NVNM_SMTP_PORT", "587")
	t.Setenv("NVNM_SMTP_FROM", "noreply@example.com")
	t.Setenv("NVNM_SMTP_FROM_NAME", "NVNM")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.SMTPHost != "smtp.example.com" || cfg.SMTPPort != 587 {
		t.Errorf("SMTP host/port = %q/%d, want smtp.example.com/587", cfg.SMTPHost, cfg.SMTPPort)
	}
	if cfg.SMTPFrom != "noreply@example.com" || cfg.SMTPFromName != "NVNM" {
		t.Errorf("SMTP from = %q (%q), want noreply@example.com (NVNM)", cfg.SMTPFrom, cfg.SMTPFromName)
	}
}

func TestParseCommaSeparated_OnlyEmptyEntries(t *testing.T) {
	if got := parseCommaSeparated(" , ,"); got != nil {
		t.Errorf("parseCommaSeparated(\" , ,\") = %v, want nil", got)
	}
}
