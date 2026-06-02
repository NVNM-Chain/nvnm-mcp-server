// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	deficrypto "github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"
	defiwallet "github.com/defiweb/go-eth/wallet"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// --- nvnm_overview ---

func TestOverviewTool_ReturnsChainIdentityAndJourney(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	cfg.ExplorerURL = "https://explorer.example"
	cfg.DocsURL = "https://docs.example"
	cfg.BridgeURL = "https://bridge.example"

	h := makeOverviewHandler(cfg)
	_, out, err := h(context.Background(), nil, overviewInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ChainName != "NVNM Chain" {
		t.Errorf("ChainName = %q", out.ChainName)
	}
	if out.ChainID != cfg.ChainID {
		t.Errorf("ChainID = %d, want %d", out.ChainID, cfg.ChainID)
	}
	if out.ExplorerURL != cfg.ExplorerURL {
		t.Errorf("ExplorerURL not threaded through")
	}
	if out.WhatIsNVNMChain == "" {
		t.Error("WhatIsNVNMChain must be populated")
	}
	if !strings.Contains(out.WhatIsNVNMChain, "hash") {
		t.Errorf("WhatIsNVNMChain must mention hashing; got %q", out.WhatIsNVNMChain)
	}
	if out.PrivacyByDesign == "" {
		t.Error("PrivacyByDesign must be populated")
	}
	if len(out.CanonicalJourney) == 0 {
		t.Error("CanonicalJourney must list at least one step")
	}
	if len(out.NextActions) == 0 || out.NextActions[0].Tool != "nvnm_setup_wizard" {
		t.Errorf("first next action should point at nvnm_setup_wizard; got %+v", out.NextActions)
	}
}

// --- wallet_status state matrix ---

func TestWalletStatusTool_StatesDerivedFromBalanceAndNonce(t *testing.T) {
	cases := []struct {
		name       string
		balanceWei string
		nonce      uint64
		want       string
		hasSentTx  bool
	}{
		{"unfunded", "0", 0, WalletStatusUnfunded, false},
		{"funded-but-unused", "1000000000000000000", 0, WalletStatusFundedUnused, false},
		{"funded-active", "1000000000000000000", 7, WalletStatusFundedActive, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testServerConfig(false, ApprovalRequired)
			m := &mockEVM{
				balance: &evm.NormalizedBalance{
					Address: "0x" + strings.Repeat("a", 40),
					Wei:     tc.balanceWei,
					Ether:   "1.0",
				},
				nonce: tc.nonce,
			}
			h := makeWalletStatusHandler(m, cfg)
			_, out, err := h(context.Background(), nil, walletStatusInput{
				Address: "0x" + strings.Repeat("a", 40),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Status != tc.want {
				t.Errorf("Status = %q, want %q", out.Status, tc.want)
			}
			if out.HasSentTx != tc.hasSentTx {
				t.Errorf("HasSentTx = %v, want %v", out.HasSentTx, tc.hasSentTx)
			}
			if len(out.NextActions) == 0 {
				t.Errorf("expected next_actions to be populated for status %q", out.Status)
			}
		})
	}
}

func TestWalletStatusTool_InvalidAddressRejected(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	h := makeWalletStatusHandler(&mockEVM{}, cfg)
	_, _, err := h(context.Background(), nil, walletStatusInput{Address: "not-an-address"})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("error should wrap ErrInvalidAddress; got %v", err)
	}
}

// --- verify_hash ---

func TestVerifyHash_DeterministicChallenge_SameAddressSameValue(t *testing.T) {
	addr := defitypes.MustAddressFromHex("0x" + strings.Repeat("1", 40))
	// Call twice across separate variables so staticcheck does not
	// flag the comparison as SA4000 ("identical expressions on both
	// sides"); the test is intentionally asserting determinism.
	first := challengeForAddress(addr)
	second := challengeForAddress(addr)
	if first != second {
		t.Errorf("challenge is non-deterministic: %q vs %q", first, second)
	}
}

func TestVerifyHash_DifferentAddressesYieldDifferentChallenges(t *testing.T) {
	a := defitypes.MustAddressFromHex("0x" + strings.Repeat("1", 40))
	b := defitypes.MustAddressFromHex("0x" + strings.Repeat("2", 40))
	if challengeForAddress(a) == challengeForAddress(b) {
		t.Error("different addresses produced the same challenge")
	}
}

func TestVerifyHash_CaseInsensitiveAddress(t *testing.T) {
	lower := defitypes.MustAddressFromHex("0xabcdef0000000000000000000000000000000000")
	mixed := defitypes.MustAddressFromHex("0xABCDEF0000000000000000000000000000000000")
	if challengeForAddress(lower) != challengeForAddress(mixed) {
		t.Error("challenge changed across address casing variants")
	}
}

func TestVerifyHash_SuccessAcceptsExpectedDigest(t *testing.T) {
	addr := "0x" + strings.Repeat("a", 40)
	parsed, _ := defitypes.AddressFromHex(addr)
	challenge := challengeForAddress(parsed)
	digest := sha256.Sum256([]byte(challenge))
	hashed := "0x" + hex.EncodeToString(digest[:])

	h := makeVerifyHashHandler()
	_, out, err := h(context.Background(), nil, verifyHashInput{
		Address: addr,
		Hash:    hashed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.OK {
		t.Errorf("expected ok=true; got %+v", out)
	}
}

func TestVerifyHash_FailureRejectsWrongDigest(t *testing.T) {
	h := makeVerifyHashHandler()
	_, out, err := h(context.Background(), nil, verifyHashInput{
		Address: "0x" + strings.Repeat("a", 40),
		Hash:    "0x" + strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatal("expected error for wrong digest")
	}
	if !errors.Is(err, apperrors.ErrInvalidHash) {
		t.Errorf("error should wrap ErrInvalidHash; got %v", err)
	}
	if out.OK {
		t.Error("OK should be false on mismatch")
	}
}

func TestVerifyHash_MissingHashRejected(t *testing.T) {
	h := makeVerifyHashHandler()
	_, _, err := h(context.Background(), nil, verifyHashInput{
		Address: "0x" + strings.Repeat("a", 40),
	})
	if err == nil {
		t.Fatal("expected error for missing hash")
	}
	if !errors.Is(err, apperrors.ErrMissingRequired) {
		t.Errorf("error should wrap ErrMissingRequired; got %v", err)
	}
}

func TestVerifyHash_InvalidAddressRejected(t *testing.T) {
	h := makeVerifyHashHandler()
	_, _, err := h(context.Background(), nil, verifyHashInput{
		Address: "not-an-address",
		Hash:    "0x" + strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("error should wrap ErrInvalidAddress; got %v", err)
	}
}

// --- verify_signature ---

func TestVerifySignature_RoundTripWithFreshKey(t *testing.T) {
	key := defiwallet.NewRandomKey()
	addr := key.Address()
	challenge := challengeForAddress(addr)

	sig, err := key.SignMessage(context.Background(), []byte(challenge))
	if err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	// Sanity-check the signer outside the handler.
	recovered, recErr := deficrypto.ECRecoverer.RecoverMessage([]byte(challenge), *sig)
	if recErr != nil || *recovered != addr {
		t.Fatalf("round-trip recover failed: got %v err=%v", recovered, recErr)
	}

	h := makeVerifySignatureHandler()
	_, out, err := h(context.Background(), nil, verifySignatureInput{
		Address:   addr.String(),
		Signature: sig.String(),
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !out.OK {
		t.Errorf("expected ok=true; got %+v", out)
	}
}

func TestVerifySignature_WrongSignerRejected(t *testing.T) {
	signingKey := defiwallet.NewRandomKey()
	otherKey := defiwallet.NewRandomKey()
	// Sign with signingKey but claim the signature was for otherKey.
	challenge := challengeForAddress(otherKey.Address())
	sig, err := signingKey.SignMessage(context.Background(), []byte(challenge))
	if err != nil {
		t.Fatalf("SignMessage: %v", err)
	}

	h := makeVerifySignatureHandler()
	_, out, retErr := h(context.Background(), nil, verifySignatureInput{
		Address:   otherKey.Address().String(),
		Signature: sig.String(),
	})
	if retErr == nil {
		t.Fatal("expected error for mismatched signer")
	}
	if !errors.Is(retErr, apperrors.ErrInvalidSignature) {
		t.Errorf("error should wrap ErrInvalidSignature; got %v", retErr)
	}
	if out.OK {
		t.Error("OK should be false on mismatch")
	}
}

func TestVerifySignature_MalformedSignatureRejected(t *testing.T) {
	h := makeVerifySignatureHandler()
	_, _, err := h(context.Background(), nil, verifySignatureInput{
		Address:   "0x" + strings.Repeat("a", 40),
		Signature: "0xnotreallyahexsignature",
	})
	if err == nil {
		t.Fatal("expected error for malformed signature")
	}
	if !errors.Is(err, apperrors.ErrInvalidSignature) {
		t.Errorf("error should wrap ErrInvalidSignature; got %v", err)
	}
}

// --- wizard ---

func TestWizard_NoAddressReturnsNeedsWalletWithSamples(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	cfg.BridgeURL = "https://bridge.example"
	cfg.WalletGeneratorURL = "https://wallet.example"
	h := makeSetupWizardHandler(&mockEVM{}, cfg)
	_, out, err := h(context.Background(), nil, setupWizardInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.State != WizardStateNeedsWallet {
		t.Errorf("State = %q, want %q", out.State, WizardStateNeedsWallet)
	}
	if len(out.SampleCode) == 0 {
		t.Error("needs_wallet state should include sample code")
	}
	// Phase 11 D-L8-2: needs_wallet response must surface the
	// browser-hosted wallet generator URL alongside the snippet flow.
	if out.WalletGeneratorURL != cfg.WalletGeneratorURL {
		t.Errorf("WalletGeneratorURL = %q, want %q",
			out.WalletGeneratorURL, cfg.WalletGeneratorURL)
	}
	if !strings.Contains(out.Message, "wallet_generator_url") {
		t.Errorf("needs_wallet message should mention wallet_generator_url; got: %q", out.Message)
	}
	// Critical safety property: each sample must demonstrate storing
	// the private key via a real secrets mechanism, not via stdout.
	// Positive assertion: every sample contains at least one of the
	// allowlisted storage primitives. Trying to enumerate the
	// disallowed shape (print calls that name a key) was too brittle
	// because legitimate samples reference the key by name when
	// handing it to the storage primitive.
	safePrimitives := map[string][]string{
		"python":     {"keyring.set_password", "keyring.set_password(", "secretstorage", "os.environ"},
		"javascript": {"fs.appendFileSync(\".env", "fs.writeFileSync(\".env", "writeFileSync('.env"},
		"go":         {"os.WriteFile(", "os.OpenFile("},
	}
	for _, sample := range out.SampleCode {
		needles, ok := safePrimitives[sample.Language]
		if !ok {
			t.Errorf("sample language %q has no safety policy registered", sample.Language)
			continue
		}
		found := false
		for _, n := range needles {
			if strings.Contains(sample.Code, n) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("sample (%s) does not use any allowlisted safe-storage primitive %v:\n%s",
				sample.Language, needles, sample.Code)
		}
	}
}

func TestWizard_UnfundedAddress(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	cfg.BridgeURL = "https://bridge.example"
	m := &mockEVM{
		balance: &evm.NormalizedBalance{Address: "0x", Wei: "0", Ether: "0"},
		nonce:   0,
	}
	h := makeSetupWizardHandler(m, cfg)
	_, out, err := h(context.Background(), nil, setupWizardInput{
		Address: "0x" + strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.State != WizardStateUnfunded {
		t.Errorf("State = %q, want %q", out.State, WizardStateUnfunded)
	}
	if out.BridgeURL != cfg.BridgeURL {
		t.Errorf("BridgeURL = %q, want %q", out.BridgeURL, cfg.BridgeURL)
	}
}

func TestWizard_FundedUnused(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	m := &mockEVM{
		balance: &evm.NormalizedBalance{Address: "0x", Wei: "1000", Ether: "0.000000000000001"},
		nonce:   0,
	}
	h := makeSetupWizardHandler(m, cfg)
	_, out, err := h(context.Background(), nil, setupWizardInput{
		Address: "0x" + strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.State != WizardStateFundedUnused {
		t.Errorf("State = %q, want %q", out.State, WizardStateFundedUnused)
	}
}

func TestWizard_FundedActiveSurfacesPrivacyCaveat(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	m := &mockEVM{
		balance: &evm.NormalizedBalance{Address: "0x", Wei: "1000", Ether: "0.000000000000001"},
		nonce:   3,
	}
	h := makeSetupWizardHandler(m, cfg)
	_, out, err := h(context.Background(), nil, setupWizardInput{
		Address: "0x" + strings.Repeat("a", 40),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.State != WizardStateFundedActive {
		t.Errorf("State = %q, want %q", out.State, WizardStateFundedActive)
	}
	// The funded_active message must explicitly disclaim that "sent
	// any tx" != "anchored." The privacy-by-design property depends
	// on this caveat being part of every consuming agent's context.
	if !strings.Contains(out.Message, "anchored") {
		t.Errorf("funded_active message should disclaim has-sent-any-tx != has-anchored; got:\n%s", out.Message)
	}
}

func TestWizard_InvalidAddressRejected(t *testing.T) {
	cfg := testServerConfig(false, ApprovalRequired)
	h := makeSetupWizardHandler(&mockEVM{}, cfg)
	_, _, err := h(context.Background(), nil, setupWizardInput{
		Address: "not-an-address",
	})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("error should wrap ErrInvalidAddress; got %v", err)
	}
}
