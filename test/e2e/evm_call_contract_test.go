// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// evm_call_contract -- the arbitrary read path (eth_call).
//
// It targets the signing EOA with empty calldata, which is the one call
// whose result is deterministic on any chain: no code to run, so empty
// return data and no revert. Pointing it at the precompile with real
// calldata would instead simulate a state-changing method, whose outcome
// depends on chain state and could change under us mid-run.

func phaseCallContract(t *testing.T, f *flow) {
	var out struct {
		Result string `json:"result"`
	}
	f.callOK(t, "evm_call_contract", map[string]any{
		"to":   f.address,
		"data": "0x",
	}, &out)

	if out.Result != "0x" && out.Result != "" {
		t.Errorf("result = %q, want empty return data from a call to an account with no code", out.Result)
	}

	if got := f.call(t, "evm_call_contract", map[string]any{
		"to":   "not-an-address",
		"data": "0x",
	}); !got.IsError {
		t.Error("a malformed destination was accepted")
	}
}
