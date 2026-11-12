// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import "testing"

func TestChainEnvironment_IsValid(t *testing.T) {
	tests := []struct {
		env  ChainEnvironment
		want bool
	}{
		{EnvTestnet, true},
		{EnvMainnet, true},
		{"", false},
		{"prod", false},
		{"TESTNET", false}, // case-sensitive on purpose
	}
	for _, tc := range tests {
		t.Run(string(tc.env)+"/want="+boolString(tc.want), func(t *testing.T) {
			if got := tc.env.IsValid(); got != tc.want {
				t.Errorf("IsValid(%q) = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}

func TestInferEnvironmentFromChainID(t *testing.T) {
	tests := []struct {
		chainID int64
		want    ChainEnvironment
	}{
		{787222, EnvTestnet}, // devnet (nvnm-tempo-devnet-1)
		{787111, ""},         // retired chain ID: no longer recognized
		{1611, ""},           // retired chain ID: no longer recognized
		{58887, ""},
		{0, ""},
		{1, ""},
	}
	for _, tc := range tests {
		t.Run(boolString(tc.want != "")+"_"+string(tc.want), func(t *testing.T) {
			if got := InferEnvironmentFromChainID(tc.chainID); got != tc.want {
				t.Errorf("InferEnvironmentFromChainID(%d) = %q, want %q", tc.chainID, got, tc.want)
			}
		})
	}
}

func TestChainEnvironment_String(t *testing.T) {
	if got := EnvTestnet.String(); got != "testnet" {
		t.Errorf("EnvTestnet.String() = %q, want %q", got, "testnet")
	}
	if got := EnvMainnet.String(); got != "mainnet" {
		t.Errorf("EnvMainnet.String() = %q, want %q", got, "mainnet")
	}
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
