// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// TestEVMGetCodeNext_PrecompileIsNotAnEmptyAccount pins the precompile branch.
// The anchoring precompile is implemented in the node, so empty bytecode is
// expected rather than a sign the address is wrong; steering the caller to its
// always-zero balance was actively misleading.
func TestEVMGetCodeNext_PrecompileIsNotAnEmptyAccount(t *testing.T) {
	got := evmGetCodeNext(false, true)
	if len(got) == 0 {
		t.Fatal("no next_actions for a precompile")
	}
	if got[0].Tool == "evm_get_balance" {
		t.Errorf("precompile still steered at its balance: %+v", got[0])
	}
	if !strings.Contains(strings.ToLower(got[0].Hint), "precompile") {
		t.Errorf("hint does not explain the precompile case: %q", got[0].Hint)
	}

	// An ordinary bytecode-less account keeps the balance hint.
	plain := evmGetCodeNext(false, false)
	if len(plain) == 0 || plain[0].Tool != "evm_get_balance" {
		t.Errorf("non-precompile empty account lost its balance hint: %+v", plain)
	}
}

// TestGetBalance_ReportsChainGasToken guards the unit label. The chain's gas
// token is wmantraUSD / wmmUSD, never ether, so an agent reading the legacy
// `ether` field alone would report the wrong unit to a user.
func TestGetBalance_ReportsChainGasToken(t *testing.T) {
	m := &mockEVM{balance: &evm.NormalizedBalance{
		Address: testAddr, Wei: "1000000000000000000", Ether: "1.000000000000000000",
	}}
	handler := makeGetBalanceHandler(m, testServerConfig(false))

	_, out, err := handler(ctx, nil, getBalanceInput{Address: testAddr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TokenWrapped == "" || strings.EqualFold(out.TokenWrapped, "ether") {
		t.Errorf("token_wrapped = %q, want the chain's gas token", out.TokenWrapped)
	}
	if !strings.Contains(out.BalanceHuman, out.TokenWrapped) {
		t.Errorf("balance_human %q does not carry the token symbol %q",
			out.BalanceHuman, out.TokenWrapped)
	}
	// Legacy field retained for wire compatibility.
	if out.Ether != "1.000000000000000000" {
		t.Errorf("legacy ether field changed: %q", out.Ether)
	}
}
