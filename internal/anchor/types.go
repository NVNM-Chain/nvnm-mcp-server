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

// --- Write request types ---

type AddRegistryRequest struct {
	Sender      string `json:"sender"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AddRegistryResponse struct {
	RegistryID uint64 `json:"registry_id"`
	TxHash     string `json:"tx_hash"`
}

type AddRecordRequest struct {
	Sender       string `json:"sender"`
	Registry     string `json:"registry"`
	URI          string `json:"uri"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	Metadata     string `json:"metadata"`
}

type AddRecordResponse struct {
	TxHash string `json:"tx_hash"`
}

type UpdateRecordStatusRequest struct {
	Editor     string `json:"editor"`
	RegistryID uint64 `json:"registry_id"`
	RecordID   uint64 `json:"record_id"`
	Index      uint64 `json:"index"`
	Status     string `json:"status"`
}

type UpdateRecordStatusResponse struct {
	TxHash string `json:"tx_hash"`
}

type GrantRoleRequest struct {
	Admin      string `json:"admin"`
	Address    string `json:"address"`
	RegistryID uint64 `json:"registry_id"`
	Checksum   string `json:"checksum,omitempty"`
	Role       string `json:"role"`
}

type RevokeRoleRequest struct {
	Admin      string `json:"admin"`
	Address    string `json:"address"`
	RegistryID uint64 `json:"registry_id"`
	Checksum   string `json:"checksum,omitempty"`
	Role       string `json:"role"`
}

// PrecompileInfo describes the anchoring precompile configuration.
type PrecompileInfo struct {
	Address     string `json:"address"`
	ChainID     int64  `json:"chain_id"`
	ABILoaded   bool   `json:"abi_loaded"`
	MethodCount int    `json:"method_count,omitempty"`
}
