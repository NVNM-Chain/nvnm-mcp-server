package evm

// NormalizedBlock is the stable response shape for block data.
type NormalizedBlock struct {
	Number           uint64                `json:"number"`
	Hash             string                `json:"hash"`
	ParentHash       string                `json:"parent_hash"`
	TimestampUnix    uint64                `json:"timestamp_unix"`
	GasLimit         uint64                `json:"gas_limit"`
	GasUsed          uint64                `json:"gas_used"`
	BaseFeePerGas    *string               `json:"base_fee_per_gas,omitempty"`
	Miner            string                `json:"miner"`
	TransactionCount int                   `json:"transaction_count"`
	Transactions     []NormalizedTxSummary `json:"transactions,omitempty"`
}

// NormalizedTxSummary is a minimal transaction reference within a block.
type NormalizedTxSummary struct {
	Hash  string `json:"hash"`
	Index uint   `json:"index"`
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Value string `json:"value"`
}

// NormalizedTransaction is the stable response shape for transaction data.
type NormalizedTransaction struct {
	Hash        string  `json:"hash"`
	BlockNumber *uint64 `json:"block_number,omitempty"`
	BlockHash   *string `json:"block_hash,omitempty"`
	Index       *uint64 `json:"index,omitempty"`
	From        string  `json:"from"`
	To          *string `json:"to,omitempty"`
	Value       string  `json:"value"`
	Gas         uint64  `json:"gas"`
	GasPrice    string  `json:"gas_price"`
	Nonce       uint64  `json:"nonce"`
	Data        string  `json:"data"`
	IsPending   bool    `json:"is_pending"`
}

// NormalizedReceipt is the stable response shape for transaction receipts.
type NormalizedReceipt struct {
	TxHash          string          `json:"tx_hash"`
	BlockNumber     uint64          `json:"block_number"`
	BlockHash       string          `json:"block_hash"`
	TransactionIdx  uint            `json:"transaction_index"`
	Status          string          `json:"status"`
	GasUsed         uint64          `json:"gas_used"`
	CumulativeGas   uint64          `json:"cumulative_gas_used"`
	ContractAddress *string         `json:"contract_address,omitempty"`
	Logs            []NormalizedLog `json:"logs"`
}

// NormalizedLog is the stable response shape for event logs.
type NormalizedLog struct {
	Address     string   `json:"address"`
	Topics      []string `json:"topics"`
	Data        string   `json:"data"`
	BlockNumber uint64   `json:"block_number"`
	TxHash      string   `json:"tx_hash"`
	TxIndex     uint     `json:"tx_index"`
	LogIndex    uint     `json:"log_index"`
	Removed     bool     `json:"removed"`
}

// NormalizedBalance is the stable response shape for balance queries.
type NormalizedBalance struct {
	Address string `json:"address"`
	Wei     string `json:"wei"`
	Ether   string `json:"ether"`
}

// ChainInfo is the response shape for chain identification.
type ChainInfo struct {
	ChainID           int64  `json:"chain_id"`
	LatestBlockNumber uint64 `json:"latest_block_number"`
}

// CodeResult is the response shape for contract code queries.
type CodeResult struct {
	Address    string `json:"address"`
	Bytecode   string `json:"bytecode"`
	IsContract bool   `json:"is_contract"`
}
