// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"math/big"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// nvnm_setup_wizard is the prose-guided onboarding flow. It derives
// one of four states from the (optional) input address and the
// wallet's on-chain snapshot, then returns concrete next-step
// guidance. The state names are intentionally honest -- in particular
// the `funded_active` state name resists the (incorrect) reading
// that the wizard could tell the agent "you have anchored." The
// wizard reads only balance and nonce, never transaction contents;
// only the agent's user knows what their transactions on chain did.

const (
	// WizardStateNeedsWallet means the caller did not provide an
	// address. The response contains language-specific sample code
	// for generating a wallet via a secrets-mechanism path that
	// never prints the private key.
	WizardStateNeedsWallet = "needs_wallet"
	// WizardStateUnfunded means an address was provided and its
	// on-chain balance is 0.
	WizardStateUnfunded = "unfunded"
	// WizardStateFundedUnused means balance > 0 but nonce == 0.
	WizardStateFundedUnused = "funded_unused"
	// WizardStateFundedActive means balance > 0 and nonce > 0.
	// This says "has sent any tx," NOT "has anchored," because the
	// wizard reads only balance and nonce, never transaction
	// contents. The response text repeats this caveat verbatim.
	WizardStateFundedActive = "funded_active"
)

type walletSnapshot struct {
	Address          string `json:"address"`
	BalanceWei       string `json:"balance_wei"`
	BalanceHuman     string `json:"balance_human"`
	Nonce            uint64 `json:"nonce"`
	HasSentTx        bool   `json:"has_sent_tx"`
	ChainID          int64  `json:"chain_id"`
	ChainEnvironment string `json:"chain_environment"`
	TokenNative      string `json:"token_native"`
	TokenWrapped     string `json:"token_wrapped"`
}

type setupWizardInput struct {
	//nolint:lll // schema docstring
	Address string `json:"address,omitempty" jsonschema:"Optional EVM address (0x...). Omit to receive wallet-generation guidance for the needs_wallet state."`
}

type sampleCode struct {
	Language string `json:"language"`
	Title    string `json:"title"`
	Code     string `json:"code"`
}

type setupWizardOutput struct {
	State              string          `json:"state"`
	Message            string          `json:"message"`
	Wallet             *walletSnapshot `json:"wallet,omitempty"`
	SampleCode         []sampleCode    `json:"sample_code,omitempty"`
	BridgeURL          string          `json:"bridge_url,omitempty"`
	WalletGeneratorURL string          `json:"wallet_generator_url,omitempty"`
	NextActions        []NextAction    `json:"next_actions,omitempty"`
}

func registerSetupWizardTool(srv *mcp.Server, evmClient evm.Client, cfg *config.Config) {
	addTool(srv, &mcp.Tool{
		Name:  "nvnm_setup_wizard",
		Title: "Setup Wizard",
		Description: "Walks through the four onboarding states " +
			"(`needs_wallet` / `unfunded` / `funded_unused` / " +
			"`funded_active`) for the supplied (optional) address. " +
			"Returns prose guidance + a wallet snapshot. Important: " +
			"`funded_active` means \"has sent any transaction,\" not " +
			"\"has anchored\" -- the wizard reads only balance and nonce, " +
			"never transaction contents.",
		Annotations: newOpenWorldReadOnly(),
	}, makeSetupWizardHandler(evmClient, cfg))
}

func makeSetupWizardHandler(
	c evm.Client,
	cfg *config.Config,
) mcp.ToolHandlerFor[setupWizardInput, setupWizardOutput] {
	naming := config.NamingFor(cfg.ChainEnvironment)
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input setupWizardInput,
	) (*mcp.CallToolResult, setupWizardOutput, error) {
		if err := requireRole(ctx, readRoleSet...); err != nil {
			return nil, setupWizardOutput{}, err
		}

		if input.Address == "" {
			return nil, needsWalletResponse(cfg, naming), nil
		}

		addr, err := parseAddress(input.Address)
		if err != nil {
			return nil, setupWizardOutput{}, err
		}

		balance, err := c.BalanceAt(ctx, addr, nil)
		if err != nil {
			return nil, setupWizardOutput{}, fmt.Errorf("get balance: %w", err)
		}
		nonce, err := c.PendingNonceAt(ctx, addr)
		if err != nil {
			return nil, setupWizardOutput{}, fmt.Errorf("get nonce: %w", err)
		}

		hasFunds := false
		if balance != nil {
			b, ok := new(big.Int).SetString(balance.Wei, 10)
			if ok && b.Sign() > 0 {
				hasFunds = true
			}
		}
		hasSentTx := nonce > 0

		snap := &walletSnapshot{
			Address:          evm.AddressHex(addr),
			Nonce:            nonce,
			HasSentTx:        hasSentTx,
			ChainID:          cfg.ChainID,
			ChainEnvironment: string(cfg.ChainEnvironment),
			TokenNative:      naming.Native,
			TokenWrapped:     naming.Wrapped,
		}
		if balance != nil {
			snap.BalanceWei = balance.Wei
			snap.BalanceHuman = balance.Ether + " " + naming.Wrapped
		} else {
			snap.BalanceWei = "0"
			snap.BalanceHuman = "0 " + naming.Wrapped
		}

		switch {
		case !hasFunds:
			return nil, unfundedResponse(cfg, snap), nil
		case hasFunds && !hasSentTx:
			return nil, fundedUnusedResponse(snap), nil
		default:
			return nil, fundedActiveResponse(snap), nil
		}
	}
}

// --- Per-state response builders ---

func needsWalletResponse(cfg *config.Config, naming config.TokenNaming) setupWizardOutput {
	msg := "No address provided. Two ways to get one: " +
		"(1) the browser-hosted wallet generator at `wallet_generator_url` " +
		"(opens in a tab, creates a key entirely in the browser, hands you " +
		"the artifacts to self-custody -- nothing leaves the page); " +
		"(2) the language-specific samples below, which generate a key in " +
		"a local runtime and IMMEDIATELY hand it to a secrets mechanism -- " +
		"they never print the private key to stdout. Once you have an " +
		"address, fund it with " + naming.Wrapped + " (see `bridge_url`) " +
		"and call this wizard again with the address."
	return setupWizardOutput{
		State:              WizardStateNeedsWallet,
		Message:            msg,
		SampleCode:         needsWalletSamples(),
		BridgeURL:          cfg.BridgeURL,
		WalletGeneratorURL: cfg.WalletGeneratorURL,
		NextActions: []NextAction{
			{
				Tool: "nvnm_setup_wizard",
				Hint: "Re-call with the generated address to derive the next state " +
					"(likely `unfunded`).",
			},
			{
				Tool: "nvnm_setup_verify_hash",
				Hint: "Optional: prove off-chain hashing before funding the wallet. " +
					"Surfaced here so an agent that never funds the wallet still " +
					"knows the verify step exists.",
			},
		},
	}
}

func unfundedResponse(cfg *config.Config, snap *walletSnapshot) setupWizardOutput {
	msg := "Wallet has no balance. Fund it with " + snap.TokenWrapped +
		" via the bridge, then re-call this wizard."
	if cfg.BridgeURL != "" {
		msg += " Bridge: " + cfg.BridgeURL
	}
	return setupWizardOutput{
		State:     WizardStateUnfunded,
		Message:   msg,
		Wallet:    snap,
		BridgeURL: cfg.BridgeURL,
		NextActions: []NextAction{
			{Tool: "nvnm_setup_wizard", Hint: "Re-call after funding; the wizard will detect the new balance automatically."},
			{Tool: "wallet_status", Hint: "Equivalent one-shot snapshot if you don't need the prose guidance."},
		},
	}
}

func fundedUnusedResponse(snap *walletSnapshot) setupWizardOutput {
	msg := "Wallet has " + snap.BalanceHuman + " but has never sent a " +
		"transaction. Optional: prove your hashing and signing paths " +
		"work via the verify_hash and verify_signature tools below. " +
		"When you are ready, browse registries or create one of your own."
	return setupWizardOutput{
		State:   WizardStateFundedUnused,
		Message: msg,
		Wallet:  snap,
		NextActions: []NextAction{
			{Tool: "nvnm_setup_verify_hash", Hint: "Optional: prove off-chain hashing before broadcasting."},
			{Tool: "nvnm_setup_verify_signature", Hint: "Optional: prove off-chain signing before broadcasting."},
			{Tool: "anchor_get_registries", Hint: "Browse registries before creating your own."},
		},
	}
}

func fundedActiveResponse(snap *walletSnapshot) setupWizardOutput {
	msg := "Wallet has " + snap.BalanceHuman + " and has sent " +
		"transactions (nonce > 0). Important: this state means \"has " +
		"sent any transaction,\" NOT \"has anchored.\" This wizard reads " +
		"only balance and nonce, never transaction contents, so only you " +
		"(or your user) know what those transactions did. Use the " +
		"anchor_prepare_* tools to build new writes, or anchor_get_records " +
		"to browse on-chain records."
	return setupWizardOutput{
		State:   WizardStateFundedActive,
		Message: msg,
		Wallet:  snap,
		NextActions: []NextAction{
			{Tool: "anchor_get_registries", Hint: "Discover registries you may want to write into."},
			{Tool: "anchor_prepare_add_registry", Hint: "Create your own registry (you become admin)."},
			{Tool: "anchor_prepare_add_record", Hint: "Anchor a checksum + URI into a registry you have editor on."},
		},
	}
}

// needsWalletSamples returns language-specific snippets that
// generate a wallet and immediately hand the private key to a
// secrets mechanism. None of the snippets print the private key.
// These are surfaced verbatim to consuming agents.
func needsWalletSamples() []sampleCode {
	return []sampleCode{
		{
			Language: "python",
			Title:    "Generate an EVM wallet and store the key via the `keyring` library",
			Code: `# Recent Linux distros mark system Python as PEP 668 externally-
# managed, so "pip install" outside a venv errors with
# "error: externally-managed-environment" unless --break-system-packages
# is passed. The supported path is a per-project venv:
#
#   python3 -m venv .venv
#   source .venv/bin/activate    # Windows: .venv\Scripts\activate
#   pip install eth-account keyring
#
from eth_account import Account
import keyring

# Generates a new wallet. The private key bytes live only in
# the local 'acct' object; we hand them straight to keyring and
# do not print them.
acct = Account.create()
keyring.set_password("nvnm-wallet", "default", acct.key.hex())

# Pass ONLY the address to your code that talks to the MCP server.
print(f"Address: {acct.address}")
# Retrieve the key later with:
#   keyring.get_password("nvnm-wallet", "default")
`,
		},
		{
			Language: "javascript",
			Title:    "Generate an EVM wallet and store the key via env var (e.g. .env file, never committed)",
			Code: `// npm install ethers dotenv
import { Wallet } from "ethers";
import fs from "fs";

// Generates a new wallet. Write the private key into a .env file
// excluded from version control; do not print it to stdout.
const w = Wallet.createRandom();
// The "mode" option to appendFileSync is only honored when the file
// is created -- on an existing .env it is silently a no-op. Set 0o600
// explicitly when we created the file; leave a pre-existing .env's
// mode alone (the user may have intentionally chosen different perms).
const existed = fs.existsSync(".env");
fs.appendFileSync(".env", ` + "`NVNM_PRIVATE_KEY=${w.privateKey}\n`" + `);
if (!existed) {
    fs.chmodSync(".env", 0o600);
}
console.log("Address:", w.address);
// In your app, load via process.env.NVNM_PRIVATE_KEY (dotenv).
`,
		},
		{
			Language: "go",
			Title:    "Generate an EVM wallet and store the key as a 0600 file",
			Code: `// go get github.com/defiweb/go-eth/wallet
package main

import (
    "fmt"
    "os"

    defiwallet "github.com/defiweb/go-eth/wallet"
)

func main() {
    priv := defiwallet.NewRandomKey()
    if err := os.WriteFile(
        ".nvnm-key", []byte(fmt.Sprintf("%x", priv.PrivateKey().D.Bytes())), 0o600,
    ); err != nil {
        panic(err)
    }
    fmt.Println("Address:", priv.Address().String())
    // Load later: read .nvnm-key, hex-decode, defiwallet.NewKeyFromBytes(...)
}
`,
		},
	}
}
