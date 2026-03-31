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

type PageResponse struct {
	Total uint64 `json:"total"`
}

// --- Query request/response types ---

type GetRegistryRequest struct {
	ID   *uint64 `json:"id,omitempty"`
	Name *string `json:"name,omitempty"`
}

type GetRegistriesRequest struct {
	RegistryID *uint64      `json:"registry_id,omitempty"`
	Name       *string      `json:"name,omitempty"`
	Pagination *PageRequest `json:"pagination,omitempty"`
}

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
type GetRecordsRequest struct {
	RegistryID *uint64      `json:"registry_id,omitempty"`
	RecordID   *uint64      `json:"record_id,omitempty"`
	Index      *uint64      `json:"index,omitempty"`
	Checksum   *string      `json:"checksum,omitempty"`
	Registry   *string      `json:"registry,omitempty"`
	Pagination *PageRequest `json:"pagination,omitempty"`
}

type GetRecordsResponse struct {
	Records    []Record      `json:"records"`
	Pagination *PageResponse `json:"pagination,omitempty"`
}

// --- Unsigned transaction (prepare-sign-submit) ---

// UnsignedTransaction contains a fully constructed but unsigned EVM transaction.
// The caller signs raw_tx with their private key, then submits via
// evm_send_raw_transaction. The other fields are provided for transparency
// so the caller can verify what they're signing.
type UnsignedTransaction struct {
	RawTx    string `json:"raw_tx"`    // RLP-encoded unsigned tx (hex, 0x-prefixed)
	To       string `json:"to"`        // Target address (precompile)
	Data     string `json:"data"`      // ABI-encoded calldata (hex, 0x-prefixed)
	Nonce    uint64 `json:"nonce"`     // Sender's pending nonce
	Gas      uint64 `json:"gas"`       // Estimated gas limit (with buffer)
	GasPrice string `json:"gas_price"` // Current gas price (wei, decimal string)
	Value    string `json:"value"`     // Always "0" for precompile calls
	ChainID  int64  `json:"chain_id"`  // EIP-155 chain ID
}

// --- Prepare request types (write operations) ---

// PrepareAddRegistryRequest contains the parameters for preparing an
// addRegistry transaction. From is the sender's EVM address (0x...).
type PrepareAddRegistryRequest struct {
	From        string `json:"from"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Metadata    string `json:"metadata,omitempty"`
}

// PrepareAddRecordRequest contains the parameters for preparing an
// addRecord transaction. From is the sender's EVM address (0x...).
type PrepareAddRecordRequest struct {
	From         string `json:"from"`
	Registry     string `json:"registry"`
	URI          string `json:"uri"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	Metadata     string `json:"metadata,omitempty"`
}

// PrepareGrantRoleRequest contains the parameters for preparing a
// grantRole transaction. From is the admin's EVM address (0x...).
type PrepareGrantRoleRequest struct {
	From       string `json:"from"`
	RegistryID uint64 `json:"registry_id"`
	Checksum   string `json:"checksum,omitempty"`
	Account    string `json:"account"` // Address receiving the role (0x...)
	Role       string `json:"role"`    // "admin" or "editor"
}

// PrecompileInfo describes the anchoring precompile configuration.
type PrecompileInfo struct {
	Address     string `json:"address"`
	ChainID     int64  `json:"chain_id"`
	ABILoaded   bool   `json:"abi_loaded"`
	MethodCount int    `json:"method_count,omitempty"`
}
