// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"math/big"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// wallet_status is the one-shot wallet snapshot tool. It is the
// programmatic equivalent of the setup wizard's state derivation,
// without the prose guidance. Returned `status` values are honest
// (see the type-level comment) and tied to the chain's
// privacy-by-design property: this server cannot tell an agent "this
// wallet has anchored a record" because the chain emits no events --
// only that the wallet has been funded or has sent transactions.

const (
	// WalletStatusUnfunded means balance == 0. The wallet exists
	// on-chain only in the sense that nodes will accept queries
	// against the address; nothing has been deposited.
	WalletStatusUnfunded = "unfunded"
	// WalletStatusFundedUnused means balance > 0 and nonce == 0.
	// The wallet has gas but has never sent a transaction.
	WalletStatusFundedUnused = "funded_unused"
	// WalletStatusFundedActive means balance > 0 and nonce > 0.
	// The wallet has sent at least one transaction. This says
	// nothing about WHAT those transactions did -- per the
	// privacy-by-design property the server cannot detect
	// anchoring specifically.
	WalletStatusFundedActive = "funded_active"
)

type walletStatusInput struct {
	Address string `json:"address" jsonschema:"EVM address (0x...) to inspect"`
}

type walletStatusOutput struct {
	Address          string       `json:"address"`
	BalanceWei       string       `json:"balance_wei"`
	BalanceHuman     string       `json:"balance_human"`
	Nonce            uint64       `json:"nonce"`
	HasSentTx        bool         `json:"has_sent_tx"`
	Status           string       `json:"status"`
	ChainID          int64        `json:"chain_id"`
	ChainEnvironment string       `json:"chain_environment"`
	TokenNative      string       `json:"token_native"`
	TokenWrapped     string       `json:"token_wrapped"`
	NextActions      []NextAction `json:"next_actions,omitempty"`
}

func registerWalletTool(srv *mcp.Server, evmClient evm.Client, cfg *config.Config) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "wallet_status",
		Title: "Wallet Status",
		Description: "Returns a one-shot snapshot of an EVM address: " +
			"balance (wei + human-readable in the env-aware gas token), " +
			"nonce, whether any transaction has been sent, and a " +
			"three-state status (`unfunded` / `funded_unused` / " +
			"`funded_active`). Status `funded_active` means \"has sent " +
			"any transaction,\" not \"has anchored\" -- by design this " +
			"chain emits no events, so the server cannot detect " +
			"anchoring specifically.",
		Annotations: newOpenWorldReadOnly(),
	}, makeWalletStatusHandler(evmClient, cfg))
}

func makeWalletStatusHandler(
	c evm.Client,
	cfg *config.Config,
) mcp.ToolHandlerFor[walletStatusInput, walletStatusOutput] {
	naming := config.NamingFor(cfg.ChainEnvironment)
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input walletStatusInput,
	) (*mcp.CallToolResult, walletStatusOutput, error) {
		if err := requireRole(ctx, readRoleSet...); err != nil {
			return nil, walletStatusOutput{}, err
		}
		addr, err := parseAddress(input.Address)
		if err != nil {
			return nil, walletStatusOutput{}, err
		}

		balance, err := c.BalanceAt(ctx, addr, nil)
		if err != nil {
			return nil, walletStatusOutput{}, fmt.Errorf("get balance: %w", err)
		}
		nonce, err := c.PendingNonceAt(ctx, addr)
		if err != nil {
			return nil, walletStatusOutput{}, fmt.Errorf("get nonce: %w", err)
		}

		// classify balance==0 vs balance>0 by parsing the wei string.
		// BalanceAt returns a *NormalizedBalance with Wei as decimal
		// string; we don't have direct access to the *big.Int, so
		// re-parse here. An empty or "0" string is unfunded.
		hasFunds := false
		if balance != nil {
			b, ok := new(big.Int).SetString(balance.Wei, 10)
			if ok && b.Sign() > 0 {
				hasFunds = true
			}
		}
		hasSentTx := nonce > 0

		status := WalletStatusUnfunded
		switch {
		case hasFunds && hasSentTx:
			status = WalletStatusFundedActive
		case hasFunds && !hasSentTx:
			status = WalletStatusFundedUnused
		}

		balanceWei := "0"
		balanceHuman := "0 " + naming.Wrapped
		if balance != nil {
			balanceWei = balance.Wei
			balanceHuman = balance.Ether + " " + naming.Wrapped
		}

		return nil, walletStatusOutput{
			Address:          evm.AddressHex(addr),
			BalanceWei:       balanceWei,
			BalanceHuman:     balanceHuman,
			Nonce:            nonce,
			HasSentTx:        hasSentTx,
			Status:           status,
			ChainID:          cfg.ChainID,
			ChainEnvironment: string(cfg.ChainEnvironment),
			TokenNative:      naming.Native,
			TokenWrapped:     naming.Wrapped,
			NextActions:      walletStatusNextActions(status),
		}, nil
	}
}

// walletStatusNextActions returns the agent's recommended next step
// for each status value. The hints are concrete (named tool + one-line
// reason) so an agent can chain calls without reading every tool's
// description.
func walletStatusNextActions(status string) []NextAction {
	switch status {
	case WalletStatusUnfunded:
		return []NextAction{
			{
				Tool: "nvnm_setup_wizard",
				Hint: "Walk through the funding flow. The wizard's `unfunded` state " +
					"points at the bridge.",
			},
		}
	case WalletStatusFundedUnused:
		return []NextAction{
			{Tool: "nvnm_setup_verify_hash", Hint: "Optional: prove off-chain hash computation works."},
			{
				Tool: "nvnm_setup_verify_signature",
				Hint: "Optional: prove off-chain signing works before sending an on-chain tx.",
			},
			{Tool: "anchor_get_registries", Hint: "Browse existing registries before creating your own."},
		}
	case WalletStatusFundedActive:
		return []NextAction{
			{Tool: "anchor_get_registries", Hint: "Discover registries you may want to write into."},
			{Tool: "anchor_prepare_add_registry", Hint: "Create your own registry (you become admin)."},
			{Tool: "anchor_prepare_add_record", Hint: "Anchor a checksum + URI into a registry you have editor on."},
		}
	}
	return nil
}

// avoid unused-package error when apperrors is referenced only via
// other files in the package; this exists so a future edit needing
// the alias doesn't have to re-import.
var _ = apperrors.ErrInvalidAddress
