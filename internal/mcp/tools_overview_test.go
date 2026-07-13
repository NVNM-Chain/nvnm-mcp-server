// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
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
			got := prereqsFor(&config.Config{KeylessWrites: tt.keylessWrites})

			if len(got) != len(basePrereqs)+1 {
				t.Fatalf("prereqsFor() returned %d entries, want %d (base + broadcast)",
					len(got), len(basePrereqs)+1)
			}
			for i, want := range basePrereqs {
				if got[i] != want {
					t.Errorf("prereqsFor()[%d] = %q, want base prereq %q", i, got[i], want)
				}
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

// TestPrereqsFor_DoesNotMutateBasePrereqs guards the append-to-shared-slice
// footgun: prereqsFor appends the broadcast line to basePrereqs, and appending
// to a package-level slice without copying would let one call's broadcast line
// leak into the next call's result.
func TestPrereqsFor_DoesNotMutateBasePrereqs(t *testing.T) {
	before := len(basePrereqs)

	authed := prereqsFor(&config.Config{KeylessWrites: false})
	keyless := prereqsFor(&config.Config{KeylessWrites: true})

	if len(basePrereqs) != before {
		t.Errorf("prereqsFor mutated basePrereqs: len %d, want %d", len(basePrereqs), before)
	}
	if authed[len(authed)-1] == keyless[len(keyless)-1] {
		t.Error("authed and keyless deployments returned the same broadcast prereq")
	}
}
