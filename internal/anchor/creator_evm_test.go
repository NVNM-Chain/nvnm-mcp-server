// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil/bech32"
)

// TestCreatorEVM pins the bech32 -> 0x-hex derivation contract (P1 / ADR
// 0001): a valid chain-native bech32 creator yields its 20-byte payload as
// an EVM address, hex input passes through lowercased, and anything
// non-derivable yields "" so creator_evm is omitted rather than invented.
func TestCreatorEVM(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "live chain creator",
			in:   "nvnm139t3cwka9ps4sm5dtnuv2kf28t83n065s57gcm",
			want: "0x89571c3add2861586e8d5cf8c5592a3acf19bf54",
		},
		{
			name: "pre-rename HRP still derives (payload is HRP-independent)",
			in:   "inveniam12r28dewjcpzfnrkpshvx5rh4eve08685xyya3f", // pragma: allowlist secret -- public on-chain address, test fixture
			want: "0x50d476e5d2c044998ec185d86a0ef5cb32f3e8f4",
		},
		{
			name: "hex passthrough lowercased",
			in:   "0x89571C3ADD2861586E8D5CF8C5592A3ACF19BF54",
			want: "0x89571c3add2861586e8d5cf8c5592a3acf19bf54",
		},
		{name: "invalid bech32 checksum", in: "nvnm1abc123def456", want: ""},
		{name: "hex wrong length", in: "0x89571c", want: ""},
		{name: "hex bad digits", in: "0x" + strings.Repeat("z", 40), want: ""},
		{name: "empty", in: "", want: ""},
		{name: "arbitrary text", in: "not-an-address", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := creatorEVM(tt.in); got != tt.want {
				t.Errorf("creatorEVM(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestCreatorEVM_WrongPayloadLength rejects a valid bech32 string whose
// payload is not 20 bytes (not an account address).
func TestCreatorEVM_WrongPayloadLength(t *testing.T) {
	short, err := bech32.EncodeFromBase256("nvnm", []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("encode short payload: %v", err)
	}
	if got := creatorEVM(short); got != "" {
		t.Errorf("creatorEVM(%q) = %q, want empty", short, got)
	}
}
