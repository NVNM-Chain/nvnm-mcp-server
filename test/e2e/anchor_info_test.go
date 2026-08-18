// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// anchor_info -- what the server is wired to. It reports the precompile
// address, chain ID, and whether the ABI loaded.
//
// The cross-check against nvnm_overview is the point: those two answers
// come from different places in the server, and a disagreement means it
// is describing a different precompile than the one its write tools will
// encode calldata for.

func phaseAnchorInfo(t *testing.T, f *flow) {
	var out struct {
		Address     string `json:"address"`
		ChainID     int64  `json:"chain_id"`
		ABILoaded   bool   `json:"abi_loaded"`
		MethodCount int    `json:"method_count"`
	}
	f.callOK(t, "anchor_info", map[string]any{}, &out)

	if !out.ABILoaded {
		t.Error("abi_loaded is false; the anchor write tools cannot encode calldata without it")
	}
	if out.MethodCount == 0 {
		t.Error("method_count is 0")
	}
	if !strings.EqualFold(out.Address, f.anchorAddress) {
		t.Errorf("anchor_info address = %s, but nvnm_overview reported %s", out.Address, f.anchorAddress)
	}
	if out.ChainID != f.chainID {
		t.Errorf("anchor_info chain_id = %d, want %d", out.ChainID, f.chainID)
	}

	t.Logf("precompile %s: abi_loaded=%v methods=%d", out.Address, out.ABILoaded, out.MethodCount)
}
