// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	deficrypto "github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"
)

// AddressHex returns the EIP-55 checksummed hex form of an address.
// Matches the format previously produced by go-ethereum's
// common.Address.Hex() so existing API responses keep their casing.
func AddressHex(a defitypes.Address) string {
	return a.Checksum(checksumHash)
}

// HashHex returns the 0x-prefixed hex form of a hash. defiweb's
// types.Hash.String() is already lowercase 0x-prefixed; hash strings
// have no checksum convention so a direct passthrough is fine.
func HashHex(h defitypes.Hash) string {
	return h.String()
}

// checksumHash is a types.HashFunc-compatible Keccak256 wrapper.
// Used by Address.Checksum to derive the EIP-55 checksum.
func checksumHash(data ...[]byte) defitypes.Hash {
	return deficrypto.Keccak256(data...)
}
