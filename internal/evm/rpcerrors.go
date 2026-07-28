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
