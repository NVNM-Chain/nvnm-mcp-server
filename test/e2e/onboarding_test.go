// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
)

// Onboarding tools: nvnm_overview, nvnm_setup_wizard,
// nvnm_setup_verify_hash, nvnm_setup_verify_signature, wallet_status.
//
// These are the first five steps of the canonical agent journey the
// overview tool itself publishes, exercised in that order.

type overviewResponse struct {
	ChainName        string `json:"chain_name"`
	ChainEnvironment string `json:"chain_environment"`
	ChainID          int64  `json:"chain_id"`
	AnchorPrecompile string `json:"anchor_precompile"`
	TokenNative      string `json:"token_native"`
	TokenWrapped     string `json:"token_wrapped"`
	WhatIsNVNMChain  string `json:"what_is_nvnm_chain"`
	PrivacyByDesign  string `json:"privacy_by_design"`
	Prereqs          []string
	CanonicalJourney []struct {
		Step int    `json:"step"`
		Tool string `json:"tool"`
		Hint string `json:"hint"`
	} `json:"canonical_journey"`
}

// phaseOverview calls the lobby tool and records the chain identity the
// rest of the suite builds on. Taking chain_id and the precompile address
// from the server rather than a constant means this suite follows
// whichever deployment it is pointed at.
func phaseOverview(t *testing.T, f *flow) {
	var out overviewResponse
	f.callOK(t, "nvnm_overview", map[string]any{}, &out)

	if out.ChainID == 0 {
		t.Fatal("chain_id is 0")
	}
	if out.AnchorPrecompile == "" {
		t.Fatal("anchor_precompile is empty")
	}
	if out.WhatIsNVNMChain == "" || out.PrivacyByDesign == "" {
		t.Error("the overview must carry its what-is / privacy-by-design prose; an agent reads this before anything else")
	}
	if len(out.CanonicalJourney) == 0 {
		t.Error("canonical_journey is empty")
	}
	if len(out.Prereqs) == 0 {
		t.Error("prereqs is empty")
	}

	f.chainID = out.ChainID
	f.anchorAddress = out.AnchorPrecompile
	t.Logf("chain=%s env=%s chain_id=%d precompile=%s token=%s",
		out.ChainName, out.ChainEnvironment, out.ChainID, out.AnchorPrecompile, out.TokenWrapped)
}

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
}

// phaseSetupWizard exercises both branches of the wizard: the
// address-less needs_wallet branch that hands out wallet-generation
// guidance, and the address-bearing branch that derives on-chain state.
func phaseSetupWizard(t *testing.T, f *flow) {
	var noAddr setupWizardResponse
	f.callOK(t, "nvnm_setup_wizard", map[string]any{}, &noAddr)
	if noAddr.State != "needs_wallet" {
		t.Errorf("wizard with no address: state = %q, want %q", noAddr.State, "needs_wallet")
	}
	if len(noAddr.SampleCode) == 0 {
		t.Error("needs_wallet state must carry wallet-generation sample code; it is the whole point of that state")
	}

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
	if withAddr.Wallet.ChainID != f.chainID {
		t.Errorf("wizard wallet chain_id = %d, want %d", withAddr.Wallet.ChainID, f.chainID)
	}
	t.Logf("wizard state=%s nonce=%d balance_wei=%s",
		withAddr.State, withAddr.Wallet.Nonce, withAddr.Wallet.BalanceWei)
}

type verifyHashResponse struct {
	OK        bool   `json:"ok"`
	Address   string `json:"address"`
	Challenge string `json:"challenge"`
	Expected  string `json:"expected"`
	Got       string `json:"got"`
}

// phaseVerifyHash drives both outcomes of the hash challenge. The
// mismatch path matters as much as the match: the tool contract is that a
// wrong answer comes back as ok=false with remediation, not as an error,
// so an agent can self-correct.
func phaseVerifyHash(t *testing.T, f *flow) {
	// A deliberately wrong digest must be reported as a verdict, not a failure.
	var mismatch verifyHashResponse
	f.callOK(t, "nvnm_setup_verify_hash", map[string]any{
		"address": f.address,
		"hash":    sha256Hex("not the challenge"),
	}, &mismatch)
	if mismatch.OK {
		t.Error("a wrong hash was accepted")
	}
	if mismatch.Challenge == "" || mismatch.Expected == "" {
		t.Fatal("a mismatch must still echo challenge and expected, or the caller cannot self-correct")
	}

	// The challenge derivation is documented in the tool description;
	// recomputing it locally asserts the server implements what it documents.
	wantChallenge := challengeFor(f.address)
	if !strings.EqualFold(mismatch.Challenge, wantChallenge) {
		t.Errorf("challenge = %s, want %s (per the derivation published in the tool description)",
			mismatch.Challenge, wantChallenge)
	}
	if want := sha256Hex(mismatch.Challenge); !strings.EqualFold(mismatch.Expected, want) {
		t.Errorf("expected = %s, want sha256(challenge) = %s", mismatch.Expected, want)
	}

	// Now the correct digest, computed independently of the echoed value.
	var match verifyHashResponse
	f.callOK(t, "nvnm_setup_verify_hash", map[string]any{
		"address": f.address,
		"hash":    sha256Hex(wantChallenge),
	}, &match)
	if !match.OK {
		t.Errorf("correct hash rejected: expected=%s got=%s", match.Expected, match.Got)
	}
}

type verifySignatureResponse struct {
	OK               bool   `json:"ok"`
	Address          string `json:"address"`
	Challenge        string `json:"challenge"`
	RecoveredAddress string `json:"recovered_address"`
}

// phaseVerifySignature signs the challenge with the same key that signs
// this suite's transactions, proving the EIP-191 path the onboarding flow
// tells agents to verify before they broadcast anything.
func phaseVerifySignature(t *testing.T, f *flow) {
	challenge := challengeFor(f.address)

	sig, err := f.key.SignMessage(context.Background(), []byte(challenge))
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}

	var out verifySignatureResponse
	f.callOK(t, "nvnm_setup_verify_signature", map[string]any{
		"address":   f.address,
		"signature": sig.String(),
	}, &out)

	if !out.OK {
		t.Errorf("valid signature rejected: recovered=%s address=%s", out.RecoveredAddress, out.Address)
	}
	if !strings.EqualFold(out.Challenge, challenge) {
		t.Errorf("challenge = %s, want %s", out.Challenge, challenge)
	}
	if out.RecoveredAddress != "" && !strings.EqualFold(out.RecoveredAddress, f.address) {
		t.Errorf("recovered_address = %s, want %s", out.RecoveredAddress, f.address)
	}
}

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

// phaseWalletStatus snapshots the signing wallet and records whether it
// can pay for the writes ahead. The parent acts on that: an unfunded
// wallet would fail every remaining phase for one boring reason.
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

	t.Logf("wallet %s: status=%s nonce=%d balance=%s", out.Address, out.Status, out.Nonce, out.BalanceHuman)
	f.walletFunded = out.Status != "unfunded"
}
