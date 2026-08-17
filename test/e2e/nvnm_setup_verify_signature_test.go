// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
)

// nvnm_setup_verify_signature -- the signing half of the onboarding
// self-check. It proves the EIP-191 path an agent is told to verify
// before it broadcasts anything, using the same key that signs this
// suite's transactions.

type verifySignatureResponse struct {
	OK               bool   `json:"ok"`
	Address          string `json:"address"`
	Challenge        string `json:"challenge"`
	RecoveredAddress string `json:"recovered_address"`
	Message          string `json:"message"`
}

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

	// A signature over a different message must not verify against this
	// challenge. Without this, a tool that returned ok=true unconditionally
	// would pass the check above.
	wrongSig, err := f.key.SignMessage(context.Background(), []byte(challenge+"-tampered"))
	if err != nil {
		t.Fatalf("sign tampered challenge: %v", err)
	}
	var wrong verifySignatureResponse
	f.callOK(t, "nvnm_setup_verify_signature", map[string]any{
		"address":   f.address,
		"signature": wrongSig.String(),
	}, &wrong)

	if wrong.OK {
		t.Errorf("a signature over a different message verified as valid (recovered=%s)",
			wrong.RecoveredAddress)
	}
}
