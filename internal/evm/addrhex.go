// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"fmt"
	"strings"

	deficrypto "github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// AddressHex returns the EIP-55 checksummed hex form of an address.
// Matches the format previously produced by go-ethereum's
// common.Address.Hex() so existing API responses keep their casing.
func AddressHex(a defitypes.Address) string {
	return a.Checksum(checksumHash)
}

// ParseAddress parses a 0x-prefixed (or bare) 20-byte hex address and
// enforces the EIP-55 checksum when the input is mixed-case. All-lowercase
// and all-uppercase hex carry no checksum and are accepted as-is; a
// mixed-case string whose casing does not match the checksum is rejected,
// because that is how a mistyped or truncated-and-repaired address surfaces
// (directory review 2026-09-15, M-2). Every error wraps
// apperrors.ErrInvalidAddress; the message says which check failed.
func ParseAddress(s string) (defitypes.Address, error) {
	addr, err := defitypes.AddressFromHex(s)
	if err != nil {
		return defitypes.Address{}, fmt.Errorf("%q: %w", s, apperrors.ErrInvalidAddress)
	}
	hexPart := strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if hexPart == strings.ToLower(hexPart) || hexPart == strings.ToUpper(hexPart) {
		return addr, nil
	}
	if want := AddressHex(addr); s != want && "0x"+hexPart != want {
		return defitypes.Address{}, fmt.Errorf(
			"%q: mixed-case address fails the EIP-55 checksum (expected %s; "+
				"use that spelling or all-lowercase hex): %w", s, want, apperrors.ErrInvalidAddress)
	}
	return addr, nil
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
