// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"strings"
	"testing"
)

// TestPrereqsFor_BroadcastLineTracksAuthPosture pins the broadcast
// prerequisite to the deployment's actual auth posture. The overview tool's
// prereqs are model-visible: every connecting agent reads them. A keyless
// deployment that tells callers they need an API key is asserting an
// authentication control the server does not apply (RequiresAuth exempts
// evm_send_raw_transaction when KeylessWrites is set), which is exactly the
// drift this derivation exists to prevent.
func TestPrereqsFor_BroadcastLineTracksAuthPosture(t *testing.T) {
	tests := []struct {
		name          string
		keylessWrites bool
		wantContains  []string
		wantAbsent    []string
	}{
		{
			name:          "authed deployment states the API-key requirement",
			keylessWrites: false,
			wantContains:  []string{"an API key on this server", "writer or admin role"},
		},
		{
			name:          "keyless deployment states no credential is needed",
			keylessWrites: true,
			wantContains:  []string{"no credential of any kind", "anchor precompile"},
			// The regression guard: a keyless deployment must never tell an
			// agent that a credential is *required* for a broadcast. Note we
			// ban the requirement phrasings, not the bare token "API key" --
			// the keyless text legitimately says it issues *no* API keys, and
			// a substring ban cannot tell an assertion from a negation.
			wantAbsent: []string{"an API key on this server", "writer or admin role"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testServerConfig(false)
			cfg.KeylessWrites = tt.keylessWrites
			got := prereqsFor(cfg)

			// wallet, funding, signing path, then the broadcast line.
			if len(got) != 4 {
				t.Fatalf("prereqsFor() returned %d entries, want 4", len(got))
			}
			if got[0] != walletPrereq || got[1] != fundingPrereq(cfg) || got[2] != signingPrereq {
				t.Errorf("prereqsFor() fixed entries = %q", got[:3])
			}

			broadcast := got[len(got)-1]
			for _, want := range tt.wantContains {
				if !strings.Contains(broadcast, want) {
					t.Errorf("broadcast prereq %q does not contain %q", broadcast, want)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(broadcast, absent) {
					t.Errorf("keyless broadcast prereq must not mention %q, got %q",
						absent, broadcast)
				}
			}
		})
	}
}

// TestPrereqsFor_CallsAreIndependent guards against one call's
// deployment-specific lines leaking into the next call's result.
func TestPrereqsFor_CallsAreIndependent(t *testing.T) {
	authedCfg, keylessCfg := testServerConfig(false), testServerConfig(false)
	keylessCfg.KeylessWrites = true

	authed := prereqsFor(authedCfg)
	keyless := prereqsFor(keylessCfg)
	authedAgain := prereqsFor(authedCfg)

	if authed[len(authed)-1] == keyless[len(keyless)-1] {
		t.Error("authed and keyless deployments returned the same broadcast prereq")
	}
	if strings.Join(authed, "|") != strings.Join(authedAgain, "|") {
		t.Error("a keyless call changed the authed deployment's prerequisites")
	}
}
