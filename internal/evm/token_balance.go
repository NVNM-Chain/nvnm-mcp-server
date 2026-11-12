// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// balanceOfSelector is the 4-byte selector of balanceOf(address), the
// ERC-20 read that TIP-20 fee tokens also implement.
var balanceOfSelector = []byte{0x70, 0xa0, 0x82, 0x31}

// abiWordLen is the length of one ABI word.
const abiWordLen = 32

// TokenBalanceAt reads owner's balance of an ERC-20-style token with
// balanceOf, and renders it at the token's own scale. It is how the server
// reads a fee balance: NVNM Chain charges fees in a token contract and has
// no native coin.
//
// The result reuses NormalizedBalance: Wei is the raw integer in the
// token's smallest unit and Ether is that integer divided by 10^decimals.
// Neither is wei or ether; callers label them.
func TokenBalanceAt(
	ctx context.Context, c Client, token, owner defitypes.Address, decimals int, block *big.Int,
) (*NormalizedBalance, error) {
	input := make([]byte, 0, len(balanceOfSelector)+abiWordLen)
	input = append(input, balanceOfSelector...)
	input = append(input, make([]byte, abiWordLen-len(owner))...)
	input = append(input, owner[:]...)

	output, err := c.CallContract(ctx, defitypes.Call{To: &token, Input: input}, block)
	if err != nil {
		// CallContract hands back the node's raw error. Curate the ones a
		// caller can act on, such as a balance at a block the node does not
		// have (observed 2026-11-12 on chain 787222 as "block not found:
		// 0x3b9ac9ff").
		if curated := classifyNodeRPCError(err); curated != nil {
			return nil, curated
		}
		return nil, fmt.Errorf("fee token balanceOf: %w", err)
	}
	if len(output) != abiWordLen {
		return nil, fmt.Errorf("fee token balanceOf returned %d bytes, want %d: %w",
			len(output), abiWordLen, apperrors.ErrNodeResponseDecode)
	}
	amount := new(big.Int).SetBytes(output)
	return &NormalizedBalance{
		Address: AddressHex(owner),
		Wei:     amount.String(),
		Ether:   FormatUnits(amount, decimals),
	}, nil
}

// FormatUnits renders a non-negative integer amount at a token's scale as
// an exact decimal string with all `decimals` fractional digits, e.g.
// FormatUnits(100000000, 6) = "100.000000". With decimals 0 the result has
// no fractional part.
func FormatUnits(amount *big.Int, decimals int) string {
	digits := amount.String()
	if decimals <= 0 {
		return digits
	}
	if len(digits) <= decimals {
		digits = strings.Repeat("0", decimals-len(digits)+1) + digits
	}
	split := len(digits) - decimals
	return digits[:split] + "." + digits[split:]
}
