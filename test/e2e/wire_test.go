// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

// Response shapes shared by more than one tool's test file.
//
// These are hand-written rather than imported from the server: the
// envelope types are unexported, but reusing them would be wrong here
// anyway. Decoding into local structs asserts the *published JSON
// contract* -- which is what caught five field-name mismatches on the
// first draft of this suite (timestamp_unix not timestamp, bytecode not
// code, tx_hash not transaction_hash, data not input, and to /
// block_number being pointers).
//
// A shape used by exactly one tool's file stays in that file. Only what
// crosses files lives here.

// registry is one row of the registry table.
type registry struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Creator     string `json:"creator"`
	CreatedAt   string `json:"created_at"`
	Metadata    string `json:"metadata"`
}

// record is one version of one anchored document. Records are versioned:
// several rows share a record_id and differ by index, and exactly one
// carries is_latest.
type record struct {
	RegistryID   uint64 `json:"registry_id"`
	RecordID     uint64 `json:"record_id"`
	Index        uint64 `json:"index"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	URI          string `json:"uri"`
	Status       string `json:"status"`
	IsLatest     bool   `json:"is_latest"`
	Timestamp    string `json:"timestamp"`
	Metadata     string `json:"metadata"`
}

// pageResponse mirrors the Cosmos-style pagination envelope. This chain's
// precompile always reports total=0; the server substitutes its own scan
// count on the listing paths, which is why total is logged rather than
// asserted on.
type pageResponse struct {
	Total uint64 `json:"total"`
}

type registriesResponse struct {
	Registries        []registry    `json:"registries"`
	Pagination        *pageResponse `json:"pagination"`
	ContentTrust      string        `json:"content_trust"`
	TotalIsLowerBound bool          `json:"total_is_lower_bound"`
}

type registryResponse struct {
	registry
	ContentTrust string `json:"content_trust"`
}

type recordsResponse struct {
	Records      []record      `json:"records"`
	Pagination   *pageResponse `json:"pagination"`
	ContentTrust string        `json:"content_trust"`
}

// unsignedTx is what every anchor_prepare_* tool returns. Only the fields
// this suite asserts on are declared.
type unsignedTx struct {
	RawTx                string           `json:"raw_tx"`
	Type                 uint8            `json:"type"`
	To                   string           `json:"to"`
	Data                 string           `json:"data"`
	Nonce                uint64           `json:"nonce"`
	Gas                  uint64           `json:"gas"`
	GasPrice             string           `json:"gas_price"`
	MaxFeePerGas         string           `json:"max_fee_per_gas"`
	MaxPriorityFeePerGas string           `json:"max_priority_fee_per_gas"`
	Value                string           `json:"value"`
	ChainID              int64            `json:"chain_id"`
	WalletTxRequest      *walletTxRequest `json:"wallet_tx_request"`
}

// walletTxRequest is the EIP-1193 form of the same transaction, for
// callers who hand it to a browser wallet instead of signing locally.
//
// Two things differ from unsignedTx and both are deliberate: the field
// names are camelCase, because that is what eth_sendTransaction takes,
// and the values are 0x-hex quantities rather than decimal strings. The
// gas-pricing fields are mutually exclusive -- gasPrice for type-0,
// maxFeePerGas/maxPriorityFeePerGas for type-2 -- so whichever set does
// not apply is omitted.
type walletTxRequest struct {
	From                 string `json:"from"`
	To                   string `json:"to"`
	Data                 string `json:"data"`
	Value                string `json:"value"`
	ChainID              string `json:"chainId"`
	Gas                  string `json:"gas"`
	GasPrice             string `json:"gasPrice"`
	MaxFeePerGas         string `json:"maxFeePerGas"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas"`
}

// receipt is what evm_get_transaction_receipt returns.
type receipt struct {
	TxHash      string `json:"tx_hash"`
	BlockNumber uint64 `json:"block_number"`
	BlockHash   string `json:"block_hash"`
	Status      string `json:"status"`
	GasUsed     uint64 `json:"gas_used"`
	From        string `json:"from"`
	To          string `json:"to"`
	Logs        []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
		TxHash  string   `json:"tx_hash"`
	} `json:"logs"`
}
