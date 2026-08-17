// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// nvnm_setup_verify_hash -- the hashing half of the onboarding
// self-check. An agent hashes a challenge derived from its address and
// asks the server to confirm it got the derivation right.
//
// The mismatch path matters as much as the match: the tool contract is
// that a wrong answer comes back as ok=false *with remediation*, not as
// an error, so an agent can correct itself without a human.

type verifyHashResponse struct {
	OK        bool   `json:"ok"`
	Address   string `json:"address"`
	Challenge string `json:"challenge"`
	Expected  string `json:"expected"`
	Got       string `json:"got"`
	Message   string `json:"message"`
}

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

	// The challenge derivation is published in the tool description.
	// Recomputing it locally asserts the server implements what it
	// documents, rather than echoing whatever it happens to compute.
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
	if !strings.EqualFold(match.Address, f.address) {
		t.Errorf("address = %s, want %s", match.Address, f.address)
	}
}
