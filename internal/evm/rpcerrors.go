// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"strings"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// safeNodeRPCErrors maps a lowercase substring of an upstream node RPC error
// to a curated, input-class sentinel whose text is safe to surface to
// clients. The node reports these rejections as opaque strings with no Go
// sentinel to match on, so recognizing the safe, actionable ones requires
// substring matching at this boundary -- the one place that knows the error
// came from the node RPC (same rationale as anchor.safePrecompileReverts).
//
// Two safety rules make this acceptable despite the project's general
// "errors.Is, not string matching" preference:
//  1. Matching is on EXTERNAL node output, which carries no Go sentinel.
//  2. Only the curated sentinel is ever surfaced -- never the raw node
//     error -- so node internals (URLs, configured limits) cannot leak.
//
// Keep this list to rejections that are (a) caller-input validation, (b) safe
// to disclose, and (c) observed from a live node (do not speculate). Anything
// not listed falls through to the generic upstream-failure collapse.
var safeNodeRPCErrors = []struct {
	match    string
	sentinel error
}{
	// Observed from the testnet node for an over-wide eth_getLogs query:
	// "maximum [from, to] blocks distance: 10000".
	{"maximum [from, to] blocks distance", apperrors.ErrLogRangeTooWide},
	// Observed from the testnet node (2026-09-10) for eth_getLogs with
	// to_block beyond the chain head (from=1, to=9999999, head=3876423):
	// "RPC error: -32000 invalid block range params". A different rejection
	// from the width cap -- "narrow the range" would not fix it.
	{"invalid block range params", apperrors.ErrLogRangeInvalid},
	// Observed 2026-09-15 for eth_getBalance / eth_getCode at a block past
	// the head: "height 999999999 must be less than or equal to the current
	// blockchain height 4005936".
	{"must be less than or equal to the current blockchain height", apperrors.ErrBlockBeyondHead},
	// Same condition, different phrasing on eth_getCode (observed
	// 2026-09-15): "codespace sdk code 26: invalid height: cannot query with
	// height in the future; please provide a valid height".
	{"cannot query with height in the future", apperrors.ErrBlockBeyondHead},
	// Observed 2026-09-15 for eth_getBlockByHash with an unknown hash:
	// "block not found for hash 0x…" (the node errors instead of returning
	// null). eth_getBlockByNumber returns null for an unknown number; that
	// path is handled by the zero-block check in BlockByNumber.
	{"block not found", apperrors.ErrBlockNotFound},
	// Broadcast rejections, all observed live 2026-09-15 from the testnet
	// node via eth_sendRawTransaction. Order matters: the mempool duplicate
	// is checked before the nonce phrases because a re-broadcast of a still-
	// pending tx can mention both.
	{"already in mempool", apperrors.ErrTxAlreadyKnown},
	{"already known", apperrors.ErrTxAlreadyKnown},
	{"nonce is lower than account nonce", apperrors.ErrTxNonceConflict},
	{"invalid nonce", apperrors.ErrTxNonceConflict},
	{"nonce too low", apperrors.ErrTxNonceConflict},
	{"nonce too high", apperrors.ErrTxNonceConflict},
	{"invalid chain id for signer", apperrors.ErrTxChainIDMismatch},
	{"insufficient funds", apperrors.ErrInsufficientFunds},
}

// safeCallReverts maps eth_call rejections to ErrCallReverted. Kept separate
// from safeNodeRPCErrors and applied ONLY by ClassifyCallRevert (the
// evm_call_contract handler): the anchor client also goes through
// CallContract and must keep the raw precompile reason so its own, more
// specific classifier (anchor.classifyPrecompileRevert) can see it.
//
// Observed live 2026-09-15 against the anchoring precompile: "unknown method
// id: 3735928559" (unknown selector) and "invalid input length" (calldata
// shorter than a selector). "execution reverted" is the generic EVM revert
// prefix every Solidity contract produces.
var safeCallReverts = []string{
	"execution reverted",
	"unknown method id",
	"invalid input length",
}

// ClassifyCallRevert returns ErrCallReverted when err's text matches a known
// eth_call rejection, or nil otherwise. The raw reason (which on this chain
// carries Cosmos-side detail) is never surfaced.
func ClassifyCallRevert(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	for _, m := range safeCallReverts {
		if strings.Contains(msg, m) {
			return apperrors.ErrCallReverted
		}
	}
	return nil
}

// classifyNodeRPCError returns the curated sentinel for a known, safe
// node-side input-validation rejection contained in err's text, or nil when
// nothing matches. The sentinel is drawn solely from safeNodeRPCErrors, so
// raw node detail never escapes through it.
func classifyNodeRPCError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	for _, e := range safeNodeRPCErrors {
		if strings.Contains(msg, e.match) {
			return e.sentinel
		}
	}
	return nil
}
