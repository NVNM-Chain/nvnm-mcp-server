// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "github.com/NVNM-Chain/nvnm-mcp-server/internal/config"

// testServerConfig builds a minimal *config.Config suitable for
// in-process server tests. Only the fields the tools actually read
// are populated; everything else stays zero-valued.
func testServerConfig(enableWrite bool) *config.Config {
	return &config.Config{
		ChainID:          58887,
		ChainEnvironment: config.EnvTestnet,
		AnchorAddress:    "0x0000000000000000000000000000000000000A00",
		EnableWriteTools: enableWrite,
		// Onboarding URLs intentionally empty: the overview / wizard
		// tools tolerate empty strings and tests don't need to assert
		// on their concrete values.
	}
}
