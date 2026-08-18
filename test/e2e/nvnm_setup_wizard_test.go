// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// nvnm_setup_wizard -- the branching onboarding tool. Called without an
// address it hands out wallet-generation guidance; called with one it
// derives that wallet's on-chain state. Both branches are exercised here,
// because the address-less one is the branch a brand-new agent hits first.

type setupWizardResponse struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Wallet  *struct {
		Address    string `json:"address"`
		BalanceWei string `json:"balance_wei"`
		Nonce      uint64 `json:"nonce"`
		HasSentTx  bool   `json:"has_sent_tx"`
		ChainID    int64  `json:"chain_id"`
	} `json:"wallet"`
	SampleCode []struct {
		Language string `json:"language"`
		Title    string `json:"title"`
		Code     string `json:"code"`
	} `json:"sample_code"`
	NextActions []struct {
		Tool string `json:"tool"`
	} `json:"next_actions"`
}

func phaseSetupWizard(t *testing.T, f *flow) {
	// Branch 1: no address.
	var noAddr setupWizardResponse
	f.callOK(t, "nvnm_setup_wizard", map[string]any{}, &noAddr)

	if noAddr.State != "needs_wallet" {
		t.Errorf("wizard with no address: state = %q, want %q", noAddr.State, "needs_wallet")
	}
	if len(noAddr.SampleCode) == 0 {
		t.Error("needs_wallet state must carry wallet-generation sample code; it is the whole point of that state")
	}
	for _, sample := range noAddr.SampleCode {
		if sample.Code == "" {
			t.Errorf("sample_code %q (%s) has an empty code body", sample.Title, sample.Language)
		}
	}
	if noAddr.Wallet != nil {
		t.Error("wizard with no address returned a wallet snapshot; there is no wallet to snapshot")
	}

	// Branch 2: the signing wallet, which has been paying for this run.
	var withAddr setupWizardResponse
	f.callOK(t, "nvnm_setup_wizard", map[string]any{"address": f.address}, &withAddr)

	if withAddr.Wallet == nil {
		t.Fatal("wizard with an address returned no wallet snapshot")
	}
	switch withAddr.State {
	case "unfunded", "funded_unused", "funded_active":
	default:
		t.Errorf("unexpected wizard state %q for a supplied address", withAddr.State)
	}
	if !strings.EqualFold(withAddr.Wallet.Address, f.address) {
		t.Errorf("wizard wallet address = %s, want %s", withAddr.Wallet.Address, f.address)
	}
	if withAddr.Wallet.ChainID != f.chainID {
		t.Errorf("wizard wallet chain_id = %d, want %d", withAddr.Wallet.ChainID, f.chainID)
	}
	for _, next := range withAddr.NextActions {
		if !contains(f.advertisedTools, next.Tool) {
			t.Errorf("next_actions names %q, which the server does not advertise", next.Tool)
		}
	}

	t.Logf("wizard state=%s nonce=%d balance_wei=%s",
		withAddr.State, withAddr.Wallet.Nonce, withAddr.Wallet.BalanceWei)
}
