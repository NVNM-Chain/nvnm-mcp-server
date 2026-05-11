package mcp

import (
	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
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
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type registriesOutput struct {
	anchor.GetRegistriesResponse
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type recordsOutput struct {
	anchor.GetRecordsResponse
	NextActions []NextAction `json:"next_actions,omitempty"`
}

type unsignedTxOutput struct {
	anchor.UnsignedTransaction
	NextActions []NextAction `json:"next_actions,omitempty"`
}
