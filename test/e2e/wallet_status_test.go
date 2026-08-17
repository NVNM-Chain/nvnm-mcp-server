// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// wallet_status -- a one-shot snapshot of an EVM address: balance, nonce,
// and a three-state verdict.
//
// The status field is deliberately narrow, and this phase holds it to
// that: funded_active means "has sent a transaction", NOT "has anchored".
// wallet_status reads balance and nonce only, never transaction contents,
// which is the privacy property nvnm_overview advertises.

type walletStatusResponse struct {
	Address          string `json:"address"`
	BalanceWei       string `json:"balance_wei"`
	BalanceHuman     string `json:"balance_human"`
	Nonce            uint64 `json:"nonce"`
	HasSentTx        bool   `json:"has_sent_tx"`
	Status           string `json:"status"`
	ChainID          int64  `json:"chain_id"`
	ChainEnvironment string `json:"chain_environment"`
	TokenWrapped     string `json:"token_wrapped"`
}

func phaseWalletStatus(t *testing.T, f *flow) {
	var out walletStatusResponse
	f.callOK(t, "wallet_status", map[string]any{"address": f.address}, &out)

	if !strings.EqualFold(out.Address, f.address) {
		t.Errorf("address = %s, want %s", out.Address, f.address)
	}
	if out.ChainID != f.chainID {
		t.Errorf("chain_id = %d, want %d", out.ChainID, f.chainID)
	}
	if out.BalanceHuman == "" || !strings.HasSuffix(out.BalanceHuman, out.TokenWrapped) {
		t.Errorf("balance_human = %q, want it denominated in %q", out.BalanceHuman, out.TokenWrapped)
	}

	// The three states are a function of balance and nonce, and nothing
	// else. Checking that relation is what stops the field drifting into
	// "has anchored" territory.
	switch out.Status {
	case "unfunded":
		if out.BalanceWei != "0" {
			t.Errorf("status = unfunded but balance_wei = %s", out.BalanceWei)
		}
	case "funded_unused":
		if out.HasSentTx {
			t.Error("status = funded_unused but has_sent_tx is true")
		}
	case "funded_active":
		if !out.HasSentTx {
			t.Error("status = funded_active but has_sent_tx is false")
		}
	default:
		t.Errorf("unexpected status %q", out.Status)
	}
	if out.HasSentTx != (out.Nonce > 0) {
		t.Errorf("has_sent_tx = %v but nonce = %d; has_sent_tx is defined as nonce > 0",
			out.HasSentTx, out.Nonce)
	}

	t.Logf("wallet %s: status=%s nonce=%d balance=%s", out.Address, out.Status, out.Nonce, out.BalanceHuman)
}
