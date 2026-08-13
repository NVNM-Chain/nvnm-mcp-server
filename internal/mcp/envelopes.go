// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// Per-tool envelope structs that embed the underlying response type and
// add a next_actions field. Embedding keeps the JSON shape backwards-
// compatible: the embedded type's fields are promoted to the top level
// alongside next_actions, so existing clients that ignore unknown fields
// see no change.
//
// Tools whose response type is local to this package (sendRawTxOutput,
// getLogsOutput, callContractOutput) gain the NextActions field directly
// on the existing struct rather than via a wrapper.

type chainIDOutput struct {
	evm.ChainInfo
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type blockOutput struct {
	evm.NormalizedBlock
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type transactionOutput struct {
	evm.NormalizedTransaction
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type receiptOutput struct {
	evm.NormalizedReceipt
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type balanceOutput struct {
	evm.NormalizedBalance
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type codeOutput struct {
	evm.CodeResult
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type anchorInfoOutput struct {
	anchor.PrecompileInfo
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type registryOutput struct {
	anchor.Registry
	ContentTrust string       `json:"content_trust" jsonschema:"Which fields are untrusted user content"`
	NextActions  []NextAction `json:"next_actions,omitempty"`
}

type registriesOutput struct {
	anchor.GetRegistriesResponse
	ContentTrust string `json:"content_trust" jsonschema:"Which fields are untrusted user content"`
	// NameMatchTruncated is true when a name-filtered scan hit the client-side
	// page cap before reaching the end of the registry table (see
	// maxNameScanPages in tools_anchor.go). It signals that Registries may be
	// an incomplete match set, rather than silently returning a partial result
	// indistinguishable from a complete one.
	NameMatchTruncated bool `json:"name_match_truncated,omitempty"`
	// TotalIsLowerBound is true when the full-table scan was truncated by the
	// client-side page cap or an ID-gap heuristic before reaching the natural
	// end of the registry table. In that case pagination.total is the number
	// of rows actually scanned -- a floor, not an exact count. Absent (false)
	// means the scan completed normally and total is exact.
	TotalIsLowerBound bool         `json:"total_is_lower_bound,omitempty"`
	NextActions       []NextAction `json:"next_actions,omitempty"`
}

type recordsOutput struct {
	anchor.GetRecordsResponse
	ContentTrust string       `json:"content_trust" jsonschema:"Which fields are untrusted user content"`
	NextActions  []NextAction `json:"next_actions,omitempty"`
}

type unsignedTxOutput struct {
	anchor.UnsignedTransaction
	NextActions []NextAction `json:"next_actions,omitempty"`
}
