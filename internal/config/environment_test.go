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

func TestNamingFor(t *testing.T) {
	tests := []struct {
		env         ChainEnvironment
		wantNative  string
		wantWrapped string
	}{
		{EnvTestnet, "mantraUSD", "wmantraUSD"},
		{EnvMainnet, "mmUSD", "wmmUSD"},
		{"", "mantraUSD", "wmantraUSD"},        // empty defaults to testnet
		{"unknown", "mantraUSD", "wmantraUSD"}, // unrecognized defaults to testnet
	}
	for _, tc := range tests {
		t.Run(string(tc.env), func(t *testing.T) {
			n := NamingFor(tc.env)
			if n.Native != tc.wantNative {
				t.Errorf("Native = %q, want %q", n.Native, tc.wantNative)
			}
			if n.Wrapped != tc.wantWrapped {
				t.Errorf("Wrapped = %q, want %q", n.Wrapped, tc.wantWrapped)
			}
		})
	}
}

func TestInferEnvironmentFromChainID(t *testing.T) {
	tests := []struct {
		chainID int64
		want    ChainEnvironment
	}{
		{787111, EnvTestnet},
		{1611, EnvMainnet},
		{58887, ""}, // old manveniam-1 testnet -- not recognized by inference
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
