// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// nvnm_overview -- the lobby tool. Chain identity, the privacy-by-design
// caveat, and the canonical journey an agent should follow. Pure compute
// from operator config: no chain calls, so its answers must be internally
// consistent rather than merely plausible.

type overviewResponse struct {
	ChainName        string   `json:"chain_name"`
	ChainEnvironment string   `json:"chain_environment"`
	ChainID          int64    `json:"chain_id"`
	AnchorPrecompile string   `json:"anchor_precompile"`
	TokenNative      string   `json:"token_native"`
	TokenWrapped     string   `json:"token_wrapped"`
	WhatIsNVNMChain  string   `json:"what_is_nvnm_chain"`
	PrivacyByDesign  string   `json:"privacy_by_design"`
	Prereqs          []string `json:"prereqs"`
	CanonicalJourney []struct {
		Step int    `json:"step"`
		Tool string `json:"tool"`
		Hint string `json:"hint"`
	} `json:"canonical_journey"`
	NextActions []struct {
		Tool string `json:"tool"`
		Hint string `json:"hint"`
	} `json:"next_actions"`
}

func phaseOverview(t *testing.T, f *flow) {
	var out overviewResponse
	f.callOK(t, "nvnm_overview", map[string]any{}, &out)

	if out.ChainID != f.chainID {
		t.Errorf("chain_id = %d, want %d (the value the fixture read from this same tool)",
			out.ChainID, f.chainID)
	}
	if !strings.EqualFold(out.AnchorPrecompile, f.anchorAddress) {
		t.Errorf("anchor_precompile = %s, want %s", out.AnchorPrecompile, f.anchorAddress)
	}
	if out.ChainEnvironment == "" {
		t.Error("chain_environment is empty; a caller cannot tell mainnet from testnet")
	}
	if out.TokenWrapped == "" {
		t.Error("token_wrapped is empty; wallet_status denominates balances in it")
	}

	// The prose fields are the tool's reason to exist: an agent reads
	// them before it does anything else.
	if out.WhatIsNVNMChain == "" || out.PrivacyByDesign == "" {
		t.Error("the overview must carry its what-is / privacy-by-design prose")
	}
	if len(out.Prereqs) == 0 {
		t.Error("prereqs is empty")
	}

	// The journey is a promise about other tools. Every tool it names must
	// exist, or an agent following it walks into a dead end.
	if len(out.CanonicalJourney) == 0 {
		t.Fatal("canonical_journey is empty; the tool's whole purpose is to publish that order")
	}
	for _, step := range out.CanonicalJourney {
		if !contains(f.advertisedTools, step.Tool) {
			t.Errorf("canonical_journey step %d names %q, which the server does not advertise",
				step.Step, step.Tool)
		}
		if step.Hint == "" {
			t.Errorf("canonical_journey step %d (%s) has no hint", step.Step, step.Tool)
		}
	}
	for _, next := range out.NextActions {
		if !contains(f.advertisedTools, next.Tool) {
			t.Errorf("next_actions names %q, which the server does not advertise", next.Tool)
		}
	}

	t.Logf("chain=%s env=%s chain_id=%d precompile=%s token=%s journey=%d steps",
		out.ChainName, out.ChainEnvironment, out.ChainID, out.AnchorPrecompile,
		out.TokenWrapped, len(out.CanonicalJourney))
}
