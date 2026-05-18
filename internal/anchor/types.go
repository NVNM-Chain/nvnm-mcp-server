// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

// Registry is a logical container for records, analogous to a database table.
// Each registry is created by a user who automatically becomes its admin.
type Registry struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Creator     string `json:"creator"`
	CreatedAt   string `json:"created_at"`
	Metadata    string `json:"metadata,omitempty"`
}

// abiPaginationInput matches the ABI pagination request tuple for Pack calls.
type abiPaginationInput struct {
	Key        []byte `abi:"key"`
	Offset     uint64 `abi:"offset"`
	Limit      uint64 `abi:"limit"`
	CountTotal bool   `abi:"countTotal"`
	Reverse    bool   `abi:"reverse"`
}

// Record represents a single anchored data entry within a Registry.
// Records support versioning: multiple records can share the same RecordID
// but differ by Index (version number).
type Record struct {
	Registry     string `json:"registry"`
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

// Pagination mirrors the Cosmos SDK PageRequest/PageResponse pattern.
type PageRequest struct {
	Offset uint64 `json:"offset,omitempty"`
	Limit  uint64 `json:"limit,omitempty"`
}

// PageResponse holds the total count returned from paginated precompile queries.
type PageResponse struct {
	Total uint64 `json:"total"`
}

// --- Query request/response types ---

// GetRegistryRequest specifies filters for fetching a single registry.
type GetRegistryRequest struct {
	ID   *uint64 `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`
}

// GetRegistriesRequest specifies filters and pagination for listing registries.
type GetRegistriesRequest struct {
	RegistryID *uint64      `json:"registry_id,omitempty"`
	Name       *string      `json:"name,omitempty"`
	Pagination *PageRequest `json:"pagination,omitempty"`
}

// GetRegistriesResponse contains the registries and pagination info.
type GetRegistriesResponse struct {
	Registries []Registry    `json:"registries"`
	Pagination *PageResponse `json:"pagination,omitempty"`
}

// GetRecordsRequest supports flexible querying:
//   - By specific version: (RegistryID, RecordID, Index)
//   - Latest version of a record: (RegistryID, RecordID)
//   - Latest by content hash: (RegistryID, Checksum)
//   - All latest in a registry: (RegistryID) with pagination
//   - All matching a checksum across registries: (Checksum)
//   - Filter by registry name: (Registry)
type GetRecordsRequest struct {
	RegistryID *uint64      `json:"registry_id,omitempty"`
	RecordID   *uint64      `json:"record_id,omitempty"`
	Index      *uint64      `json:"index,omitempty"`
	Checksum   *string      `json:"checksum,omitempty"`
	Registry   *string      `json:"registry,omitempty"`
	Pagination *PageRequest `json:"pagination,omitempty"`
}

// GetRecordsResponse contains the matched records and pagination info.
type GetRecordsResponse struct {
	Records    []Record      `json:"records"`
	Pagination *PageResponse `json:"pagination,omitempty"`
}

// --- Unsigned transaction (prepare-sign-submit) ---

// WalletTransactionRequest contains the transaction fields in the format
// expected by EIP-1193 browser wallets such as MetaMask. Pass this object
// directly to eth_sendTransaction:
//
//	await window.ethereum.request({
//	  method: "eth_sendTransaction",
//	  params: [wallet_tx_request],
//	})
//
// All numeric fields are 0x-prefixed hexadecimal strings so the wallet can
// interpret them without conversion. The wallet signs the transaction locally
// and broadcasts it directly to the chain; the MCP server never holds the key.
type WalletTransactionRequest struct {
	From    string `json:"from"`    // Sender address (0x-prefixed, checksummed)
	To      string `json:"to"`      // Target address (precompile)
	Data    string `json:"data"`    // ABI-encoded calldata (0x-prefixed hex)
	Value   string `json:"value"`   // Always "0x0" for precompile calls
	ChainID string `json:"chainId"` // EIP-155 chain ID as 0x-prefixed hex
	Gas     string `json:"gas"`     // Estimated gas limit as 0x-prefixed hex
	// Type-0 (legacy) gas pricing. Omitted when the prepared transaction
	// is EIP-1559 (type 2). EIP-1193 wallets fall back to the
	// maxFeePerGas / maxPriorityFeePerGas fields below.
	GasPrice string `json:"gasPrice,omitempty"`
	// EIP-1559 (type-2) gas pricing. Populated when the prepared
	// transaction is type 2. MetaMask et al. prefer these over GasPrice
	// when both are present.
	MaxFeePerGas         string `json:"maxFeePerGas,omitempty"`
	MaxPriorityFeePerGas string `json:"maxPriorityFeePerGas,omitempty"`
}

// UnsignedTransaction contains a fully constructed but unsigned EVM transaction.
// Two signing paths are provided:
//
//   - wallet_tx_request: pass directly to MetaMask/EIP-1193 eth_sendTransaction.
//     The wallet signs and broadcasts; use evm_get_transaction_receipt for the result.
//
//   - raw_tx: RLP-encoded unsigned bytes for local/headless signers.
//     Sign externally, then broadcast via evm_send_raw_transaction.
//
// The MCP server never receives or stores private keys in either path.
type UnsignedTransaction struct {
	// RLP-encoded unsigned tx (hex, 0x-prefixed) for local/headless signers.
	RawTx string `json:"raw_tx"`
	// EIP-2718 transaction type. 0 for legacy (LegacyTx); 2 for EIP-1559
	// (DynamicFeeTx). Phase 8.4 makes type 2 the default; callers can
	// opt back into type 0 via the prefer_legacy_tx parameter on
	// anchor_prepare_* tools. Omitted from JSON when 0 (legacy default)
	// so existing type-0 consumers see no shape change.
	Type uint8 `json:"type,omitempty"`
	// Target address (anchor precompile).
	To string `json:"to"`
	// ABI-encoded calldata (hex, 0x-prefixed).
	Data string `json:"data"`
	// Sender's pending nonce.
	Nonce uint64 `json:"nonce"`
	// Estimated gas limit (with 20% buffer).
	Gas uint64 `json:"gas"`
	// GasPrice is the type-0 gas price (wei, decimal string). Always
	// populated: for type-2 transactions it equals MaxFeePerGas so a
	// legacy signer that ignores the EIP-1559 fields still has a usable
	// value to sign with.
	GasPrice string `json:"gas_price"`
	// MaxFeePerGas is the EIP-1559 fee cap (wei, decimal string).
	// Populated only on type-2 transactions.
	MaxFeePerGas string `json:"max_fee_per_gas,omitempty"`
	// MaxPriorityFeePerGas is the EIP-1559 miner tip (wei, decimal
	// string). Populated only on type-2 transactions.
	MaxPriorityFeePerGas string `json:"max_priority_fee_per_gas,omitempty"`
	// Always "0" for precompile calls.
	Value string `json:"value"`
	// EIP-155 chain ID.
	ChainID int64 `json:"chain_id"`
	// MetaMask / EIP-1193 compatible request; omitted for backwards compatibility.
	WalletTxRequest *WalletTransactionRequest `json:"wallet_tx_request,omitempty"`
}

// --- Prepare request types (write operations) ---

// PrepareAddRegistryRequest contains the parameters for preparing an
// addRegistry transaction. From is the sender's EVM address (0x...).
type PrepareAddRegistryRequest struct {
	From        string `json:"from"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Metadata    string `json:"metadata,omitempty"`
	// PreferLegacy opts the caller back into a type-0 LegacyTx instead
	// of the EIP-1559 default. Useful for signers that cannot produce
	// type-2 signatures. Defaults to false (type-2 default).
	PreferLegacy bool `json:"prefer_legacy,omitempty"`
}

// PrepareAddRecordRequest contains the parameters for preparing an
// addRecord transaction. From is the sender's EVM address (0x...).
type PrepareAddRecordRequest struct {
	From         string `json:"from"`
	Registry     string `json:"registry"`
	URI          string `json:"uri"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	Status       string `json:"status,omitempty"`
	Metadata     string `json:"metadata,omitempty"`
	// PreferLegacy: see PrepareAddRegistryRequest.PreferLegacy.
	PreferLegacy bool `json:"prefer_legacy,omitempty"`
}

// PrepareGrantRoleRequest contains the parameters for preparing a
// grantRole transaction. From is the admin's EVM address (0x...).
type PrepareGrantRoleRequest struct {
	From       string `json:"from"`
	RegistryID uint64 `json:"registry_id"`
	Checksum   string `json:"checksum,omitempty"`
	Account    string `json:"account"` // Address receiving the role (0x...)
	Role       string `json:"role"`    // "admin" or "editor"
	// PreferLegacy: see PrepareAddRegistryRequest.PreferLegacy.
	PreferLegacy bool `json:"prefer_legacy,omitempty"`
}

// PrecompileInfo describes the anchoring precompile configuration.
type PrecompileInfo struct {
	Address     string `json:"address"`
	ChainID     int64  `json:"chain_id"`
	ABILoaded   bool   `json:"abi_loaded"`
	MethodCount int    `json:"method_count,omitempty"`
}
