// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"math/big"

	defitypes "github.com/defiweb/go-eth/types"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// feeBalanceAt reads the balance a wallet can pay fees with: the fee
// token's balanceOf on a chain that charges fees in a token contract, the
// native coin otherwise. On the token chains eth_getBalance answers a
// placeholder that is the same for every address, so it must not be what
// decides whether a wallet is funded.
func feeBalanceAt(
	ctx context.Context, c evm.Client, cfg *config.Config, addr defitypes.Address, block *big.Int,
) (*evm.NormalizedBalance, error) {
	token := cfg.GasToken()
	if !token.IsContract() {
		return c.BalanceAt(ctx, addr, block)
	}
	tokenAddr, err := evm.ParseAddress(token.Address)
	if err != nil {
		return nil, fmt.Errorf("fee token address: %w", err)
	}
	return evm.TokenBalanceAt(ctx, c, tokenAddr, addr, token.Decimals, block)
}

// balanceIsPositive reports whether a balance read holds a non-zero
// amount. A nil balance, or one whose integer does not parse, is not
// funded.
func balanceIsPositive(balance *evm.NormalizedBalance) bool {
	if balance == nil {
		return false
	}
	amount, ok := new(big.Int).SetString(balance.Wei, 10)
	return ok && amount.Sign() > 0
}

// fundingHint is the clause that tells a reader where to fund a wallet:
// the response field that carries this deployment's faucet or bridge URL.
//
// A native-coin chain is funded through the bridge, so with nothing
// configured the hint still names bridge_url (the long-standing L2 text).
// A fee-token chain has no bridge. With neither URL configured there, no
// response field carries one, so the hint sends the reader to the
// operator instead of at a field that is not in the response.
func fundingHint(cfg *config.Config) string {
	switch {
	case cfg.FaucetURL != "" && cfg.BridgeURL != "":
		return "see `faucet_url` or `bridge_url`"
	case cfg.FaucetURL != "":
		return "see `faucet_url`"
	case cfg.BridgeURL != "" || !cfg.FeeToken.IsContract():
		return "see `bridge_url`"
	default:
		return "this deployment lists no funding URL; ask its operator where to get " +
			cfg.FeeToken.Symbol
	}
}

// fundingURLs is the " Faucet: … Bridge: …" tail of a funding message,
// naming whichever of the two URLs this deployment configures.
func fundingURLs(cfg *config.Config) string {
	out := ""
	if cfg.FaucetURL != "" {
		out += " Faucet: " + cfg.FaucetURL
	}
	if cfg.BridgeURL != "" {
		out += " Bridge: " + cfg.BridgeURL
	}
	return out
}
