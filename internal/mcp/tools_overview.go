package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/config"
)

// nvnm_overview is the lobby tool an agent should call first. It
// returns the chain's identity, the privacy-by-design property that
// shapes everything else, and a 6-step canonical journey so the agent
// has a default plan even before it reads any other tool's
// description. Static text only; no chain calls.

type overviewInput struct{}

// canonicalJourneyStep is one step of the recommended-first-time-agent
// flow. The strings are surfaced verbatim to the agent so wording is
// load-bearing -- keep it concrete and tool-specific.
type canonicalJourneyStep struct {
	Step int    `json:"step"`
	Tool string `json:"tool,omitempty"`
	Hint string `json:"hint"`
}

type overviewOutput struct {
	ChainName        string                 `json:"chain_name"`
	ChainEnvironment string                 `json:"chain_environment"`
	ChainID          int64                  `json:"chain_id"`
	AnchorPrecompile string                 `json:"anchor_precompile"`
	ExplorerURL      string                 `json:"explorer_url,omitempty"`
	DocsURL          string                 `json:"docs_url,omitempty"`
	BridgeURL        string                 `json:"bridge_url,omitempty"`
	TokenNative      string                 `json:"token_native"`
	TokenWrapped     string                 `json:"token_wrapped"`
	WhatIsNVNMChain  string                 `json:"what_is_nvnm_chain"`
	PrivacyByDesign  string                 `json:"privacy_by_design"`
	Prereqs          []string               `json:"prereqs"`
	CanonicalJourney []canonicalJourneyStep `json:"canonical_journey"`
	NextActions      []NextAction           `json:"next_actions,omitempty"`
}

// whatIsNVNMChainText is the 2-3 sentence "what is this chain"
// explanation that EVERY consumer of the overview tool reads. It
// MUST include the privacy-by-design property because that property
// shapes what `wallet_status` and the wizard can and cannot tell the
// agent about the wallet's history on chain.
const whatIsNVNMChainText = "NVNM Chain is an EVM L2 on MANTRA Chain " +
	"used as a neutral notary for document anchoring. It stores only " +
	"one-way hash fingerprints (SHA-256) of documents, never the " +
	"documents themselves, and the anchoring precompile deliberately " +
	"emits no events -- so the chain is meaningless to passive " +
	"observers. Counterparties exchange the underlying file off-band " +
	"on a need-to-know basis and verify by recomputing the hash."

// privacyByDesignText restates the property with its consequence for
// what this server can and cannot do for an agent. The wizard message
// in funded_active state echoes the same constraint so a consumer
// reading either tool's output reaches the same conclusion.
const privacyByDesignText = "Because the precompile emits no events, " +
	"only you (or your user) know what your transactions on this chain " +
	"did. This server's onboarding wizard and wallet_status tool can " +
	"tell you whether a wallet has been funded or has sent any " +
	"transactions, but not whether it has anchored anything " +
	"specifically -- by design, that information lives off-chain. " +
	"Do not expect a tool that decodes RecordAdded events; that " +
	"feature would undermine the privacy property."

// canonicalJourney is the recommended-first-time-agent sequence.
// Listed verbatim in the overview response so an agent that calls
// nvnm_overview first has a default plan without needing to read
// every other tool's description.
//
//nolint:gochecknoglobals // immutable text; package-level by design
var canonicalJourney = []canonicalJourneyStep{
	{
		Step: 1, Tool: "nvnm_setup_wizard",
		Hint: "Find out what state your wallet is in (no wallet / unfunded / funded " +
			"but unused / has sent a transaction). The wizard tells you what to do next.",
	},
	{
		Step: 2, Tool: "wallet_status",
		Hint: "Once you have an address, get a one-shot snapshot: balance, nonce, " +
			"status. Equivalent to wizard's state derivation but without the prose guidance.",
	},
	{
		Step: 3, Tool: "anchor_get_registries",
		Hint: "List the registries on chain. A registry is a logical container for " +
			"records; each is owned by an admin who can grant editor roles.",
	},
	{
		Step: 4, Tool: "anchor_prepare_add_registry",
		Hint: "Create your own registry. You become its admin automatically. " +
			"Returns an unsigned transaction; sign and broadcast separately.",
	},
	{
		Step: 5, Tool: "anchor_prepare_add_record",
		Hint: "Anchor a SHA-256 checksum + URI into your registry. " +
			"Returns an unsigned transaction; sign and broadcast separately.",
	},
	{
		Step: 6, Tool: "evm_send_raw_transaction",
		Hint: "Submit the signed bytes from any anchor_prepare_* call. After this, " +
			"evm_get_transaction_receipt confirms inclusion.",
	},
}

// prereqsSummary lists the operator-facing prerequisites an agent or
// human should have in place before exercising write tools. The
// wizard's needs_wallet / unfunded states walk the agent through
// these; the overview lists them up front for self-aware consumers.
//
//nolint:gochecknoglobals // immutable text; package-level by design
var prereqsSummary = []string{
	"An EVM wallet you control. Server never holds private keys.",
	"That wallet funded with the gas token. See bridge_url for the funding flow.",
	"A signing path: either a browser wallet (MetaMask via the wallet_tx_request " +
		"field returned by anchor_prepare_*) or a local/headless signer " +
		"(sign raw_tx, broadcast via evm_send_raw_transaction).",
	"For write tools: an API key on this server with the writer or admin role " +
		"(per the deployment's auth policy).",
}

func registerOverviewTool(srv *mcp.Server, cfg *config.Config) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "nvnm_overview",
		Title: "NVNM Chain Overview",
		Description: "Lobby tool. Returns chain identity (env, chain ID, " +
			"precompile address, explorer/docs/bridge URLs), the chain's " +
			"privacy-by-design property, prerequisites for writes, and a " +
			"6-step canonical agent journey. Call this first if you have " +
			"never used this server before. No chain calls.",
		Annotations: newClosedWorldReadOnly(),
	}, makeOverviewHandler(cfg))
}

func makeOverviewHandler(cfg *config.Config) mcp.ToolHandlerFor[overviewInput, overviewOutput] {
	naming := config.NamingFor(cfg.ChainEnvironment)
	return func(
		_ context.Context, _ *mcp.CallToolRequest, _ overviewInput,
	) (*mcp.CallToolResult, overviewOutput, error) {
		out := overviewOutput{
			ChainName:        "NVNM Chain",
			ChainEnvironment: string(cfg.ChainEnvironment),
			ChainID:          cfg.ChainID,
			AnchorPrecompile: cfg.AnchorAddress,
			ExplorerURL:      cfg.ExplorerURL,
			DocsURL:          cfg.DocsURL,
			BridgeURL:        cfg.BridgeURL,
			TokenNative:      naming.Native,
			TokenWrapped:     naming.Wrapped,
			WhatIsNVNMChain:  whatIsNVNMChainText,
			PrivacyByDesign:  privacyByDesignText,
			Prereqs:          prereqsSummary,
			CanonicalJourney: canonicalJourney,
			NextActions: []NextAction{
				{
					Tool: "nvnm_setup_wizard",
					Hint: "Recommended first call. The wizard derives your wallet state " +
						"and tells you what to do next.",
				},
			},
		}
		return nil, out, nil
	}
}
