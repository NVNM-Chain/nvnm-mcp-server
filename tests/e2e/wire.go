// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"unicode"
)

// Wire shapes decoded from tool JSON. Local structs on purpose: they
// assert the published contract, not the server's unexported types.
// A renamed or mistyped field fails Unmarshal or the format checks.

type Registry struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Creator     string `json:"creator"`
	CreatedAt   string `json:"created_at"`
}

type Record struct {
	RegistryID   uint64 `json:"registry_id"`
	RecordID     uint64 `json:"record_id"`
	Index        uint64 `json:"index"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	URI          string `json:"uri"`
	Status       string `json:"status"`
	IsLatest     bool   `json:"is_latest"`
}

type PageResponse struct {
	Total   uint64 `json:"total"`
	NextKey string `json:"next_key"`
}

type RegistriesResponse struct {
	Registries        []Registry    `json:"registries"`
	Pagination        *PageResponse `json:"pagination"`
	ContentTrust      string        `json:"content_trust"`
	TotalIsLowerBound bool          `json:"total_is_lower_bound"`
}

type RegistryResponse struct {
	Registry
	ContentTrust string `json:"content_trust"`
}

type RecordsResponse struct {
	Records      []Record      `json:"records"`
	Pagination   *PageResponse `json:"pagination"`
	ContentTrust string        `json:"content_trust"`
}

type UnsignedTx struct {
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
	WalletTxRequest      *WalletTxRequest `json:"wallet_tx_request"`
}

type WalletTxRequest struct {
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

type Receipt struct {
	TxHash      string `json:"tx_hash"`
	BlockNumber uint64 `json:"block_number"`
	BlockHash   string `json:"block_hash"`
	Status      string `json:"status"`
	GasUsed     uint64 `json:"gas_used"`
	Logs        []Log  `json:"logs"`
}

type Log struct {
	Address string   `json:"address"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
	TxHash  string   `json:"tx_hash"`
}

type OverviewResponse struct {
	ChainName        string `json:"chain_name"`
	ChainEnvironment string `json:"chain_environment"`
	ChainID          int64  `json:"chain_id"`
	AnchorPrecompile string `json:"anchor_precompile"`
	TokenNative      string `json:"token_native"`
}

type WalletStatusResponse struct {
	Address      string `json:"address"`
	Status       string `json:"status"`
	Nonce        uint64 `json:"nonce"`
	BalanceWei   string `json:"balance_wei"`
	BalanceHuman string `json:"balance_human"`
}

type ChainIDResponse struct {
	ChainID           int64  `json:"chain_id"`
	LatestBlockNumber uint64 `json:"latest_block_number"`
}

type BalanceResponse struct {
	Address string `json:"address"`
	Wei     string `json:"wei"`
	Ether   string `json:"ether"`
}

type AnchorInfoResponse struct {
	Address     string `json:"address"`
	ChainID     int64  `json:"chain_id"`
	ABILoaded   bool   `json:"abi_loaded"`
	MethodCount int    `json:"method_count"`
}

type WizardResponse struct {
	State   string `json:"state"`
	Message string `json:"message"`
	Wallet  *struct {
		Address          string `json:"address"`
		BalanceWei       string `json:"balance_wei"`
		Nonce            uint64 `json:"nonce"`
		ChainID          int64  `json:"chain_id"`
		ChainEnvironment string `json:"chain_environment"`
	} `json:"wallet"`
}

type VerifyHashResponse struct {
	OK        bool   `json:"ok"`
	Address   string `json:"address"`
	Challenge string `json:"challenge"`
	Expected  string `json:"expected"`
	Got       string `json:"got"`
}

type VerifySignatureResponse struct {
	OK        bool   `json:"ok"`
	Address   string `json:"address"`
	Challenge string `json:"challenge"`
	Recovered string `json:"recovered_address"`
}

type TransactionResponse struct {
	Hash        string  `json:"hash"`
	From        string  `json:"from"`
	To          *string `json:"to"`
	Value       string  `json:"value"`
	Nonce       uint64  `json:"nonce"`
	BlockNumber *uint64 `json:"block_number"`
	BlockHash   *string `json:"block_hash"`
}

type LogsResponse struct {
	Logs  []Log `json:"logs"`
	Count int   `json:"count"`
}

type CodeResponse struct {
	Address string `json:"address"`
}

type CallContractResponse struct {
	Result string `json:"result"`
}

type BlockResponse struct {
	Number        uint64 `json:"number"`
	Hash          string `json:"hash"`
	TimestampUnix uint64 `json:"timestamp_unix"`
}

func Contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func AssertRegistryContract(t *testing.T, reg Registry, signerHex string) {
	t.Helper()
	if reg.ID == 0 {
		t.Error("registry id is 0")
	}
	if reg.Name == "" {
		t.Error("registry name empty")
	}
	assertCreatorFormat(t, reg.Creator, signerHex)
}

func AssertRecordContract(t *testing.T, rec Record, wantChecksum string) {
	t.Helper()
	if rec.RegistryID == 0 || rec.RecordID == 0 || rec.Index == 0 {
		t.Errorf("record ids registry=%d record=%d index=%d, want all > 0",
			rec.RegistryID, rec.RecordID, rec.Index)
	}
	body := strings.TrimPrefix(strings.ToLower(rec.Checksum), "0x")
	if len(body) != 64 {
		t.Errorf("checksum = %q, want 64 hex chars", rec.Checksum)
	} else if _, err := hex.DecodeString(body); err != nil {
		t.Errorf("checksum = %q, not hex: %v", rec.Checksum, err)
	}
	if wantChecksum != "" && !strings.EqualFold(body, strings.TrimPrefix(strings.ToLower(wantChecksum), "0x")) {
		t.Errorf("checksum = %q, want %q", rec.Checksum, wantChecksum)
	}
	if rec.ChecksumAlgo != "sha256" {
		t.Errorf("checksum_algo = %q, want sha256", rec.ChecksumAlgo)
	}
	switch rec.Status {
	case "Active", "Superseded", "Revoked":
	default:
		t.Errorf("status = %q, want Active, Superseded, or Revoked", rec.Status)
	}
}

func AssertRecordReadBack(t *testing.T, rec Record, registryID uint64, checksum, uri string) {
	t.Helper()
	AssertRecordContract(t, rec, checksum)
	if rec.RegistryID != registryID {
		t.Errorf("registry_id = %d, want %d", rec.RegistryID, registryID)
	}
	if rec.URI != uri {
		t.Errorf("uri = %q, want %q", rec.URI, uri)
	}
	if !rec.IsLatest {
		t.Error("is_latest = false, want true for the version just written")
	}
}

func AssertReceiptContract(t *testing.T, r *Receipt) {
	t.Helper()
	if r == nil {
		t.Fatal("receipt is nil")
	}
	if r.Status != "success" {
		t.Errorf("status = %q, want success", r.Status)
	}
	if !strings.HasPrefix(r.TxHash, "0x") || len(r.TxHash) != 66 {
		t.Errorf("tx_hash = %q, want 0x + 64 hex chars", r.TxHash)
	}
	if r.BlockNumber == 0 {
		t.Error("block_number is 0")
	}
	if !strings.HasPrefix(r.BlockHash, "0x") || len(r.BlockHash) != 66 {
		t.Errorf("block_hash = %q, want 0x + 64 hex chars", r.BlockHash)
	}
	if r.Logs == nil {
		t.Error("logs field missing from receipt")
	}
}

func assertCreatorFormat(t *testing.T, creator, signerHex string) {
	t.Helper()
	if creator == "" {
		t.Error("creator is empty")
		return
	}
	if strings.HasPrefix(strings.ToLower(creator), "0x") {
		if len(creator) != 42 {
			t.Errorf("creator = %q, want 0x + 40 hex chars", creator)
		}
		if signerHex != "" && !strings.EqualFold(creator, signerHex) {
			t.Errorf("creator = %s, want signing wallet %s", creator, signerHex)
		}
		return
	}
	if !looksLikeBech32(creator) {
		t.Errorf("creator = %q, want 0x-hex or bech32", creator)
	}
}

func looksLikeBech32(s string) bool {
	sep := strings.LastIndexByte(s, '1')
	if sep < 1 || sep >= len(s)-6 {
		return false
	}
	for _, c := range s {
		if c > unicode.MaxASCII || (!unicode.IsLower(c) && !unicode.IsDigit(c)) {
			return false
		}
	}
	return true
}

func hexQuantityInt64(n int64) string {
	return "0x" + big.NewInt(n).Text(16)
}

func hexQuantityUint(n uint64) string {
	return "0x" + new(big.Int).SetUint64(n).Text(16)
}

func hexQuantityDecimal(t *testing.T, decimal string) string {
	t.Helper()
	n, ok := new(big.Int).SetString(decimal, 10)
	if !ok {
		t.Fatalf("not a decimal quantity: %q", decimal)
	}
	return "0x" + n.Text(16)
}
