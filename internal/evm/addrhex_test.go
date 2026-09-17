// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"errors"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// TestAddressHex_EIP55 pins the checksum casing against the reference
// vectors from EIP-55 so the output keeps matching go-ethereum's
// common.Address.Hex() format.
func TestAddressHex_EIP55(t *testing.T) {
	cases := []struct {
		lower string
		want  string
	}{
		{"0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed", "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed"},
		{"0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359", "0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359"},
		{"0xdbf03b407c01e7cd3cbea99509d93f8dddc8c6fb", "0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB"},
		{"0xd1220a0cf47c7b9be7a2e6ba89f429762e7b9adb", "0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb"},
	}
	for _, tc := range cases {
		got := AddressHex(defitypes.MustAddressFromHex(tc.lower))
		if got != tc.want {
			t.Errorf("AddressHex(%s) = %s, want %s", tc.lower, got, tc.want)
		}
	}
}

func TestHashHex(t *testing.T) {
	const raw = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	h := defitypes.MustHashFromHex(raw, defitypes.PadNone)
	if got := HashHex(h); got != raw {
		t.Errorf("HashHex = %s, want %s", got, raw)
	}
}

// TestParseAddress_EIP55Checksum pins the strict parser: lower/upper-case
// hex carries no checksum and passes; correctly checksummed mixed case
// passes; mixed case with the wrong checksum is rejected and the message
// tells the caller the expected spelling (directory review 2026-09-15, M-2).
func TestParseAddress_EIP55Checksum(t *testing.T) {
	const good = "0x57EB2e9ee9345ce3dD4063E130D58EC79aba7207" // pragma: allowlist secret -- EIP-55 test vector
	accept := []string{
		good,
		"0x57eb2e9ee9345ce3dd4063e130d58ec79aba7207", // pragma: allowlist secret -- same vector, lower case
		"0x57EB2E9EE9345CE3DD4063E130D58EC79ABA7207", // pragma: allowlist secret -- same vector, upper case
		"57EB2e9ee9345ce3dD4063E130D58EC79aba7207",   // pragma: allowlist secret -- same vector, no 0x
		"0X57eb2e9ee9345ce3dd4063e130d58ec79aba7207", // pragma: allowlist secret -- same vector, 0X prefix
	}
	for _, in := range accept {
		addr, err := ParseAddress(in)
		if err != nil {
			t.Errorf("ParseAddress(%q): %v", in, err)
			continue
		}
		if AddressHex(addr) != good {
			t.Errorf("ParseAddress(%q) = %s, want %s", in, AddressHex(addr), good)
		}
	}
	reject := map[string]string{
		"0x57eb2E9EE9345CE3DD4063E130D58EC79ABA7207": "EIP-55", // pragma: allowlist secret -- wrong checksum on purpose
		"0x1234":         "invalid Ethereum address",
		"not-an-address": "invalid Ethereum address",
		"":               "invalid Ethereum address",
	}
	for in, want := range reject {
		_, err := ParseAddress(in)
		if !errors.Is(err, apperrors.ErrInvalidAddress) {
			t.Errorf("ParseAddress(%q): err = %v, want ErrInvalidAddress", in, err)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ParseAddress(%q): message %q lacks %q", in, err.Error(), want)
		}
	}
}
