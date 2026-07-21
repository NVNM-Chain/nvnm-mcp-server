// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
)

func TestLoadKeylessConfig_KeylessWrites(t *testing.T) {
	t.Setenv("MCP_KEYLESS_WRITES", "true")
	c := &Config{}
	if err := c.loadKeylessConfig(); err != nil {
		t.Fatalf("loadKeylessConfig: %v", err)
	}
	if !c.KeylessWrites {
		t.Error("KeylessWrites = false, want true")
	}
}

func TestLoadKeylessConfig_KeylessWritesDefaultsFalse(t *testing.T) {
	c := &Config{}
	if err := c.loadKeylessConfig(); err != nil {
		t.Fatalf("loadKeylessConfig: %v", err)
	}
	if c.KeylessWrites {
		t.Error("KeylessWrites = true, want false by default")
	}
}

func TestLoadKeylessConfig_RelayAllowAny(t *testing.T) {
	t.Setenv("MCP_RELAY_ALLOW_ANY", "true")
	c := &Config{}
	if err := c.loadKeylessConfig(); err != nil {
		t.Fatalf("loadKeylessConfig: %v", err)
	}
	if !c.RelayAllowAny {
		t.Error("RelayAllowAny = false, want true")
	}
}

func TestLoadKeylessConfig_RelayAllowAnyDefaultsFalse(t *testing.T) {
	c := &Config{}
	if err := c.loadKeylessConfig(); err != nil {
		t.Fatalf("loadKeylessConfig: %v", err)
	}
	if c.RelayAllowAny {
		t.Error("RelayAllowAny = true, want false by default")
	}
}

// TestLoad_RelayAllowAnyRejectedUnderKeylessWrites asserts the fail-closed
// boot guard: the authenticated-path escape hatch must never coexist with
// anonymous keyless writes, which stay pinned to the anchor precompile.
func TestLoad_RelayAllowAnyRejectedUnderKeylessWrites(t *testing.T) {
	setValidKeylessEnv(t) // keyless reads+writes+dsn+anchor+http
	t.Setenv("MCP_RELAY_ALLOW_ANY", "true")
	_, err := Load()
	if !errors.Is(err, ErrRelayAllowAnyWithKeyless) {
		t.Fatalf("err = %v; want ErrRelayAllowAnyWithKeyless", err)
	}
}

// TestLoad_RelayAllowAnyAllowedWithoutKeylessWrites confirms the escape hatch
// is accepted on the authenticated (non-keyless) path it is meant for.
func TestLoad_RelayAllowAnyAllowedWithoutKeylessWrites(t *testing.T) {
	t.Setenv("NVNM_EVM_RPC_URL", "https://evm.inveniam.mantrachain.io")
	t.Setenv("NVNM_CHAIN_ID", "787111")
	t.Setenv("MCP_TRANSPORT", "http")
	t.Setenv("ANCHOR_ADDRESS", "0x0000000000000000000000000000000000000A00")
	t.Setenv("MCP_RELAY_ALLOW_ANY", "true")
	// keyless reads/writes unset (false) -> authenticated path
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.RelayAllowAny || c.KeylessWrites {
		t.Fatalf("RelayAllowAny=%v KeylessWrites=%v; want true/false", c.RelayAllowAny, c.KeylessWrites)
	}
}
