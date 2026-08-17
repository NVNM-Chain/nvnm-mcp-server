// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// evm_get_balance -- native balance for an address, in wei and in a
// human-readable form.
//
// It runs against the signing wallet, which by this point in the run has
// been paying gas for real transactions, so "non-zero" is a fact about
// this run rather than an assumption about the chain.

func phaseGetBalance(t *testing.T, f *flow) {
	var out struct {
		Address string `json:"address"`
		Wei     string `json:"wei"`
		Ether   string `json:"ether"`
	}
	f.callOK(t, "evm_get_balance", map[string]any{"address": f.address}, &out)

	if !strings.EqualFold(out.Address, f.address) {
		t.Errorf("address = %s, want %s", out.Address, f.address)
	}
	if out.Wei == "" || out.Wei == "0" {
		t.Errorf("wei = %q, but this wallet just paid gas for several transactions", out.Wei)
	}
	if out.Ether == "" {
		t.Error("ether is empty")
	}

	// The zero address is a valid query and a useful control: it exists
	// on every chain and the tool must answer rather than error.
	var zero struct {
		Wei string `json:"wei"`
	}
	f.callOK(t, "evm_get_balance", map[string]any{
		"address": "0x0000000000000000000000000000000000000000",
	}, &zero)
	if zero.Wei == "" {
		t.Error("querying the zero address returned an empty balance rather than a number")
	}

	if got := f.call(t, "evm_get_balance", map[string]any{"address": "not-an-address"}); !got.IsError {
		t.Error("a malformed address was accepted")
	}

	t.Logf("balance: %s wei (%s ether)", out.Wei, out.Ether)
}
