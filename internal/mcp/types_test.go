// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"testing"
)

func TestNextAction_JSON_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		na   NextAction
		want string
	}{
		{
			name: "tool and hint only",
			na: NextAction{
				Tool: "wallet_status",
				Hint: "Check whether your wallet has balance and history.",
			},
			want: `{"tool":"wallet_status","hint":"Check whether your wallet has balance and history."}`,
		},
		{
			name: "with when precondition",
			na: NextAction{
				Tool: "nvnm_setup_wizard",
				Hint: "Call again after funding lands.",
				When: "after wmmUSD lands in your wallet on chain",
			},
			want: `{"tool":"nvnm_setup_wizard","hint":"Call again after funding lands.","when":"after wmmUSD lands in your wallet on chain"}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.na)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Marshal:\n  got  %s\n  want %s", got, tc.want)
			}

			var back NextAction
			if err := json.Unmarshal(got, &back); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if back != tc.na {
				t.Errorf("round-trip: got %+v, want %+v", back, tc.na)
			}
		})
	}
}

func TestNextAction_When_OmittedWhenEmpty(t *testing.T) {
	na := NextAction{Tool: "x", Hint: "y"}
	got, err := json.Marshal(na)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(got); got != `{"tool":"x","hint":"y"}` {
		t.Errorf("expected when omitted; got %s", got)
	}
}
