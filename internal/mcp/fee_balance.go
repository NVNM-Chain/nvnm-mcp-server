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
// token's balanceOf. The chain has no native coin, and eth_getBalance
// answers a placeholder that is the same for every address, so it must
// not be what decides whether a wallet is funded.
func feeBalanceAt(
	ctx context.Context, c evm.Client, cfg *config.Config, addr defitypes.Address, block *big.Int,
) (*evm.NormalizedBalance, error) {
	tokenAddr, err := evm.ParseAddress(cfg.FeeToken.Address)
	if err != nil {
		return nil, fmt.Errorf("fee token address: %w", err)
	}
	return evm.TokenBalanceAt(ctx, c, tokenAddr, addr, cfg.FeeToken.Decimals, block)
}

// balanceIsPositive reports whether a balance read holds a non-zero
// amount. A balance whose integer does not parse is not funded.
func balanceIsPositive(balance *evm.NormalizedBalance) bool {
	amount, ok := new(big.Int).SetString(balance.Wei, 10)
	return ok && amount.Sign() > 0
}

// fundingHint is the clause that tells a reader where to get the fee
// token: the response field that carries this deployment's faucet or
// bridge URL. With neither configured no response field carries one, so
// the hint sends the reader to the operator instead of at a field that is
// not in the response.
func fundingHint(cfg *config.Config) string {
	switch {
	case cfg.FaucetURL != "" && cfg.BridgeURL != "":
		return "see `faucet_url` or `bridge_url`"
	case cfg.FaucetURL != "":
		return "see `faucet_url`"
	case cfg.BridgeURL != "":
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
