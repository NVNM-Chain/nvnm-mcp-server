// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/config"
)

func TestRuntimeInfoFromConfig_CopiesFields(t *testing.T) {
	cfg := &config.Config{
		ChainEnvironment: config.EnvMainnet,
		ChainID:          1611,
		AnchorAddress:    "0x0000000000000000000000000000000000000A00",
		DocsURL:          "https://docs.nvnmchain.io",
		ExplorerURL:      "https://explorer.nvnmchain.io",
		BridgeURL:        "https://bridge.nvnmchain.io",
	}

	info := RuntimeInfoFromConfig(cfg)

	if info.ChainEnvironment != config.EnvMainnet {
		t.Errorf("ChainEnvironment = %q, want %q", info.ChainEnvironment, config.EnvMainnet)
	}
	if info.ChainID != 1611 {
		t.Errorf("ChainID = %d, want 1611", info.ChainID)
	}
	if info.AnchorAddress != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("AnchorAddress = %q, want anchor precompile", info.AnchorAddress)
	}
	if info.DocsURL != "https://docs.nvnmchain.io" {
		t.Errorf("DocsURL = %q", info.DocsURL)
	}
	if info.ExplorerURL != "https://explorer.nvnmchain.io" {
		t.Errorf("ExplorerURL = %q", info.ExplorerURL)
	}
	if info.BridgeURL != "https://bridge.nvnmchain.io" {
		t.Errorf("BridgeURL = %q", info.BridgeURL)
	}
}

func TestRuntimeInfo_TokenNaming_Testnet(t *testing.T) {
	info := RuntimeInfo{ChainEnvironment: config.EnvTestnet}
	n := info.TokenNaming()
	if n.Native != "mantraUSD" || n.Wrapped != "wmantraUSD" {
		t.Errorf("testnet naming = {%q, %q}, want {mantraUSD, wmantraUSD}", n.Native, n.Wrapped)
	}
}

func TestRuntimeInfo_TokenNaming_Mainnet(t *testing.T) {
	info := RuntimeInfo{ChainEnvironment: config.EnvMainnet}
	n := info.TokenNaming()
	if n.Native != "mmUSD" || n.Wrapped != "wmmUSD" {
		t.Errorf("mainnet naming = {%q, %q}, want {mmUSD, wmmUSD}", n.Native, n.Wrapped)
	}
}

func TestRuntimeInfo_TokenNaming_EmptyEnvDefaultsToTestnet(t *testing.T) {
	info := RuntimeInfo{} // zero value -- empty environment
	n := info.TokenNaming()
	if n.Native != "mantraUSD" || n.Wrapped != "wmantraUSD" {
		t.Errorf("empty-env naming = {%q, %q}, want testnet defaults", n.Native, n.Wrapped)
	}
}
