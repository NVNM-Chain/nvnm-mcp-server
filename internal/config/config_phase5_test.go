// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
	"time"
)

// setValidKeylessEnv sets a complete, valid keyless (anonymous reads +
// anonymous writes) environment so Load() succeeds. Tests that exercise a
// single Phase 5 guard start from this baseline and override just the one
// variable under test.
func setValidKeylessEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NVNM_EVM_RPC_URL", "https://evm.inveniam.mantrachain.io")
	t.Setenv("NVNM_CHAIN_ID", "787111") // recognized testnet chain ID; no NVNM_CHAIN_ENVIRONMENT needed
	t.Setenv("MCP_TRANSPORT", "http")
	t.Setenv("MCP_KEYLESS_READS", "true")
	t.Setenv("MCP_KEYLESS_WRITES", "true")
	t.Setenv("MCP_KEYLESS_PG_DSN", "postgres://x/y")
	t.Setenv("ANCHOR_ADDRESS", "0x0000000000000000000000000000000000000A00")
}

func TestLoad_KeylessWritesRequiresReads(t *testing.T) {
	t.Setenv("NVNM_EVM_RPC_URL", "https://evm.inveniam.mantrachain.io")
	t.Setenv("NVNM_CHAIN_ID", "787111")
	t.Setenv("MCP_TRANSPORT", "http")
	t.Setenv("MCP_KEYLESS_WRITES", "true")
	t.Setenv("MCP_KEYLESS_PG_DSN", "postgres://x/y")
	t.Setenv("ANCHOR_ADDRESS", "0x0000000000000000000000000000000000000A00")
	// MCP_KEYLESS_READS unset (false)
	_, err := Load()
	if !errors.Is(err, ErrKeylessWritesRequiresReads) {
		t.Fatalf("err = %v; want ErrKeylessWritesRequiresReads", err)
	}
}

func TestLoad_Phase5Defaults(t *testing.T) {
	setValidKeylessEnv(t) // reads+writes+dsn+anchor+http; helper in this file
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SignerWriteRate != 500 || c.SignerWriteWindow != 24*time.Hour {
		t.Fatalf("defaults = %d/%v; want 500/24h", c.SignerWriteRate, c.SignerWriteWindow)
	}
	if c.SignerQuotaFailOpen || c.SignerBlacklistFailOpen {
		t.Fatal("fail-open knobs must default false (fail-closed)")
	}
}

func TestLoad_AnchorAddressInvalid(t *testing.T) {
	setValidKeylessEnv(t)
	t.Setenv("ANCHOR_ADDRESS", "not-an-address")
	_, err := Load()
	if !errors.Is(err, ErrAnchorAddressInvalid) {
		t.Fatalf("err = %v; want ErrAnchorAddressInvalid", err)
	}
}

func TestLoad_SignerWriteRateInvalid(t *testing.T) {
	setValidKeylessEnv(t)
	t.Setenv("MCP_SIGNER_WRITE_RATE", "0")
	_, err := Load()
	if !errors.Is(err, ErrSignerWriteRateInvalid) {
		t.Fatalf("err = %v; want ErrSignerWriteRateInvalid", err)
	}
}

func TestLoad_SignerWriteWindowInvalid(t *testing.T) {
	setValidKeylessEnv(t)
	t.Setenv("MCP_SIGNER_WRITE_WINDOW", "0s")
	_, err := Load()
	if !errors.Is(err, ErrSignerWriteWindowInvalid) {
		t.Fatalf("err = %v; want ErrSignerWriteWindowInvalid", err)
	}
}

func TestLoad_Phase5Overrides(t *testing.T) {
	setValidKeylessEnv(t)
	t.Setenv("MCP_SIGNER_WRITE_RATE", "10")
	t.Setenv("MCP_SIGNER_WRITE_WINDOW", "1h")
	t.Setenv("MCP_SIGNER_QUOTA_FAIL_OPEN", "true")
	t.Setenv("MCP_SIGNER_BLACKLIST_FAIL_OPEN", "true")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SignerWriteRate != 10 {
		t.Errorf("SignerWriteRate = %d, want 10", c.SignerWriteRate)
	}
	if c.SignerWriteWindow != time.Hour {
		t.Errorf("SignerWriteWindow = %v, want 1h", c.SignerWriteWindow)
	}
	if !c.SignerQuotaFailOpen || !c.SignerBlacklistFailOpen {
		t.Error("fail-open knobs did not honor MCP_SIGNER_*_FAIL_OPEN=true")
	}
}
