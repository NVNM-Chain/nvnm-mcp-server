// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package config

import (
	"errors"
	"testing"
)

func liveGetenv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLiveTargetFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    LiveTarget
		wantErr error
	}{
		{
			name: "defaults when unset",
			want: LiveTarget{RPCURL: LiveDefaultRPCURL, ChainID: LiveDefaultChainID},
		},
		{
			name: "overrides from env",
			env: map[string]string{
				LiveEnvRPCURL:  "  https://rpc.example.com ",
				LiveEnvChainID: " 262144 ",
			},
			want: LiveTarget{RPCURL: "https://rpc.example.com", ChainID: 262144},
		},
		{
			name: "blank values count as unset",
			env:  map[string]string{LiveEnvRPCURL: "  ", LiveEnvChainID: ""},
			want: LiveTarget{RPCURL: LiveDefaultRPCURL, ChainID: LiveDefaultChainID},
		},
		{
			name:    "non-numeric chain id",
			env:     map[string]string{LiveEnvChainID: "abc"},
			wantErr: ErrLiveInvalidChainID,
		},
		{
			name:    "non-positive chain id",
			env:     map[string]string{LiveEnvChainID: "0"},
			wantErr: ErrLiveInvalidChainID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LiveTargetFromEnv(liveGetenv(tt.env))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("target = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMustLiveTarget(t *testing.T) {
	t.Run("process env", func(t *testing.T) {
		t.Setenv(LiveEnvRPCURL, "https://rpc.example.com")
		t.Setenv(LiveEnvChainID, "1611")
		got := MustLiveTarget()
		if got.RPCURL != "https://rpc.example.com" || got.ChainID != 1611 {
			t.Errorf("target = %+v", got)
		}
	})
	t.Run("panics on invalid chain id", func(t *testing.T) {
		t.Setenv(LiveEnvChainID, "nope")
		defer func() {
			if recover() == nil {
				t.Error("expected panic for invalid chain id")
			}
		}()
		MustLiveTarget()
	})
}

func TestLiveEnvironment(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		chainID int64
		want    ChainEnvironment
		wantErr error
	}{
		{name: "inferred testnet", chainID: 787111, want: EnvTestnet},
		{name: "inferred mainnet", chainID: 1611, want: EnvMainnet},
		{
			name:    "explicit wins for fork chain id",
			env:     map[string]string{LiveEnvChainEnvironment: "testnet"},
			chainID: 262144,
			want:    EnvTestnet,
		},
		{
			name:    "invalid explicit",
			env:     map[string]string{LiveEnvChainEnvironment: "staging"},
			chainID: 787111,
			wantErr: ErrLiveInvalidChainEnvironment,
		},
		{name: "unknown chain without explicit", chainID: 262144, wantErr: ErrLiveUnknownEnvironment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LiveEnvironment(liveGetenv(tt.env), tt.chainID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("env = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLiveMinedTx(t *testing.T) {
	override := map[string]string{LiveEnvTxHash: "0xabc", LiveEnvTxSender: "0xdef"}
	tests := []struct {
		name    string
		env     map[string]string
		chainID int64
		wantOK  bool
		wantFx  *LiveTxFixture
		wantErr error
	}{
		{name: "built-in on default testnet", chainID: LiveDefaultChainID, wantOK: true},
		{
			name: "override on any chain", env: override, chainID: 262144, wantOK: true,
			wantFx: &LiveTxFixture{Hash: "0xabc", Sender: "0xdef"},
		},
		{name: "none on other chain", chainID: 262144, wantOK: false},
		{
			name:    "hash without sender",
			env:     map[string]string{LiveEnvTxHash: "0xabc"},
			chainID: 262144,
			wantErr: ErrLivePartialTxFixture,
		},
		{
			name:    "sender without hash",
			env:     map[string]string{LiveEnvTxSender: "0xdef"},
			chainID: LiveDefaultChainID,
			wantErr: ErrLivePartialTxFixture,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx, ok, err := LiveMinedTx(liveGetenv(tt.env), tt.chainID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantFx != nil && fx != *tt.wantFx {
				t.Errorf("fixture = %+v, want %+v", fx, *tt.wantFx)
			}
			if ok && (fx.Hash == "" || fx.Sender == "") {
				t.Errorf("ok fixture has empty fields: %+v", fx)
			}
		})
	}
}
