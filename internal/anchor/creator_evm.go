// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"encoding/hex"
	"strings"

	"github.com/btcsuite/btcd/btcutil/bech32"
)

// evmAddressLen is the byte length of an EVM account address.
const evmAddressLen = 20

// creatorEVM derives the 0x-hex EVM form of a registry creator address.
//
// The chain reports `creator` as a Cosmos bech32 string (nvnm1...), which no
// EVM tool accepts (finding P1). On this chain the bech32 payload IS the
// 20-byte EVM account, so both representations are returned: `creator` stays
// exactly what the chain said, `creator_evm` is the derived hex form usable
// with wallet_status / evm_get_balance / evm_get_code.
//
// Returns "" when the value cannot be derived (not bech32, wrong payload
// length): the response then simply omits creator_evm rather than inventing
// an address. A 0x-prefixed 20-byte hex creator passes through lowercased.
func creatorEVM(creator string) string {
	if strings.HasPrefix(creator, "0x") {
		raw := creator[2:]
		if len(raw) == evmAddressLen*2 {
			if _, err := hex.DecodeString(raw); err == nil {
				return strings.ToLower(creator)
			}
		}
		return ""
	}
	_, data, err := bech32.DecodeToBase256(creator)
	if err != nil || len(data) != evmAddressLen {
		return ""
	}
	return "0x" + hex.EncodeToString(data)
}
