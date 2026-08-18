// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// evm_get_code -- bytecode at an address, and the is_contract verdict
// derived from it.
//
// The signing wallet is an externally owned account, so the expected
// answer is unambiguous: no code, not a contract. That is the assertion
// that would catch is_contract being inverted or defaulted to true.

func phaseGetCode(t *testing.T, f *flow) {
	var eoa struct {
		Address    string `json:"address"`
		Bytecode   string `json:"bytecode"`
		IsContract bool   `json:"is_contract"`
	}
	f.callOK(t, "evm_get_code", map[string]any{"address": f.address}, &eoa)

	if !strings.EqualFold(eoa.Address, f.address) {
		t.Errorf("address = %s, want %s", eoa.Address, f.address)
	}
	if eoa.IsContract {
		t.Errorf("is_contract = true for the signing EOA %s", f.address)
	}
	if eoa.Bytecode != "" && eoa.Bytecode != "0x" {
		t.Errorf("bytecode = %q for an EOA, want empty", eoa.Bytecode)
	}

	// The anchor precompile is the interesting counterpart: it is a
	// precompile rather than deployed bytecode, so whatever it reports
	// here is worth recording -- a caller deciding "is this address
	// callable" from is_contract would be misled if it reports false.
	var precompile struct {
		Bytecode   string `json:"bytecode"`
		IsContract bool   `json:"is_contract"`
	}
	f.callOK(t, "evm_get_code", map[string]any{"address": f.anchorAddress}, &precompile)
	t.Logf("code at the anchor precompile %s: is_contract=%v bytecode_len=%d",
		f.anchorAddress, precompile.IsContract, len(precompile.Bytecode))

	t.Logf("code at %s: is_contract=%v bytecode_len=%d",
		eoa.Address, eoa.IsContract, len(eoa.Bytecode))
}
