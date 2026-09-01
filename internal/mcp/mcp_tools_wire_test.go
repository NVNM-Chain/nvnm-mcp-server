// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"strings"
	"testing"
)

// Published-contract shapes for TestMCP_Tools.
// These are independent of the server's envelope types so a json-tag
// rename or type change fails here instead of unmarshaling into the
// same struct the handler uses.

type wireOverview struct {
	ChainName        string `json:"chain_name"`
	ChainEnvironment string `json:"chain_environment"`
	ChainID          int64  `json:"chain_id"`
	AnchorPrecompile string `json:"anchor_precompile"`
	TokenNative      string `json:"token_native"`
}

type wireWizardWallet struct {
	Address          string `json:"address"`
	BalanceWei       string `json:"balance_wei"`
	Nonce            uint64 `json:"nonce"`
	ChainID          int64  `json:"chain_id"`
	ChainEnvironment string `json:"chain_environment"`
}

type wireWizard struct {
	State   string            `json:"state"`
	Message string            `json:"message"`
	Wallet  *wireWizardWallet `json:"wallet"`
}

type wireVerifyHash struct {
	OK       bool   `json:"ok"`
	Address  string `json:"address"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

type wireVerifySignature struct {
	OK        bool   `json:"ok"`
	Address   string `json:"address"`
	Recovered string `json:"recovered_address"`
}

type wireWalletStatus struct {
	Address          string `json:"address"`
	BalanceWei       string `json:"balance_wei"`
	Status           string `json:"status"`
	ChainID          int64  `json:"chain_id"`
	ChainEnvironment string `json:"chain_environment"`
	Nonce            uint64 `json:"nonce"`
}

type wireChainID struct {
	ChainID           int64  `json:"chain_id"`
	LatestBlockNumber uint64 `json:"latest_block_number"`
}

type wireBlock struct {
	Number        uint64 `json:"number"`
	Hash          string `json:"hash"`
	ParentHash    string `json:"parent_hash"`
	TimestampUnix uint64 `json:"timestamp_unix"`
	Miner         string `json:"miner"`
}

type wireTransaction struct {
	Hash        string  `json:"hash"`
	From        string  `json:"from"`
	To          *string `json:"to"`
	Value       string  `json:"value"`
	Gas         uint64  `json:"gas"`
	Nonce       uint64  `json:"nonce"`
	BlockNumber *uint64 `json:"block_number"`
	BlockHash   *string `json:"block_hash"`
}

type wireReceipt struct {
	TxHash      string    `json:"tx_hash"`
	BlockNumber uint64    `json:"block_number"`
	BlockHash   string    `json:"block_hash"`
	Status      string    `json:"status"`
	Logs        []wireLog `json:"logs"`
}

type wireBalance struct {
	Address string `json:"address"`
	Wei     string `json:"wei"`
	Ether   string `json:"ether"`
}

type wireCode struct {
	Address string `json:"address"`
}

type wireLogs struct {
	Logs  []wireLog `json:"logs"`
	Count int       `json:"count"`
}

type wireLog struct {
	Address string   `json:"address"`
	TxHash  string   `json:"tx_hash"`
	Data    string   `json:"data"`
	Topics  []string `json:"topics"`
}

type wireCall struct {
	Result string `json:"result"`
}

type wireSendTx struct {
	TxHash string `json:"tx_hash"`
}

type wireAnchorInfo struct {
	Address     string `json:"address"`
	ChainID     int64  `json:"chain_id"`
	ABILoaded   bool   `json:"abi_loaded"`
	MethodCount int    `json:"method_count"`
}

type wireRegistry struct {
	ID           uint64 `json:"id"`
	Name         string `json:"name"`
	Creator      string `json:"creator"`
	ContentTrust string `json:"content_trust"`
}

type wirePage struct {
	Total   uint64 `json:"total"`
	NextKey string `json:"next_key"`
}

type wireRegistries struct {
	Registries   []wireRegistry `json:"registries"`
	Pagination   *wirePage      `json:"pagination"`
	ContentTrust string         `json:"content_trust"`
}

type wireRecord struct {
	RegistryID   uint64 `json:"registry_id"`
	RecordID     uint64 `json:"record_id"`
	Index        uint64 `json:"index"`
	Checksum     string `json:"checksum"`
	ChecksumAlgo string `json:"checksum_algo"`
	URI          string `json:"uri"`
	Status       string `json:"status"`
	IsLatest     bool   `json:"is_latest"`
}

type wireRecords struct {
	Records      []wireRecord `json:"records"`
	Pagination   *wirePage    `json:"pagination"`
	ContentTrust string       `json:"content_trust"`
}

type wireWalletTx struct {
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

type wireUnsignedTx struct {
	RawTx                string        `json:"raw_tx"`
	Type                 uint8         `json:"type"`
	To                   string        `json:"to"`
	Data                 string        `json:"data"`
	Nonce                uint64        `json:"nonce"`
	Gas                  uint64        `json:"gas"`
	GasPrice             string        `json:"gas_price"`
	MaxFeePerGas         string        `json:"max_fee_per_gas"`
	MaxPriorityFeePerGas string        `json:"max_priority_fee_per_gas"`
	Value                string        `json:"value"`
	ChainID              int64         `json:"chain_id"`
	WalletTxRequest      *wireWalletTx `json:"wallet_tx_request"`
}

func decodeWire[T any](t *testing.T, raw []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("published contract decode failed: %v\njson: %s", err, raw)
	}
	return out
}

func assert0xAddress(t *testing.T, field, got string) {
	t.Helper()
	if !is0xHex(got, 20) {
		t.Errorf("%s = %q, want 0x + 40 hex chars", field, got)
	}
}

func assert0xHash(t *testing.T, field, got string) {
	t.Helper()
	if !is0xHex(got, 32) {
		t.Errorf("%s = %q, want 0x + 64 hex chars", field, got)
	}
}

func assertChecksum64(t *testing.T, field, got string) {
	t.Helper()
	s := strings.TrimPrefix(strings.ToLower(got), "0x")
	if len(s) != 64 {
		t.Errorf("%s = %q, want 64 hex chars", field, got)
		return
	}
	if _, err := hex.DecodeString(s); err != nil {
		t.Errorf("%s = %q, not hex: %v", field, got, err)
	}
}

func assertRecordStatus(t *testing.T, got string) {
	t.Helper()
	switch got {
	case "Active", "Superseded", "Revoked":
	default:
		t.Errorf("status = %q, want Active, Superseded, or Revoked", got)
	}
}

func is0xHex(s string, byteLen int) bool {
	if !strings.HasPrefix(s, "0x") && !strings.HasPrefix(s, "0X") {
		return false
	}
	body := s[2:]
	if len(body) != byteLen*2 {
		return false
	}
	_, err := hex.DecodeString(body)
	return err == nil
}

func uint64Hex(n uint64) string {
	return "0x" + new(big.Int).SetUint64(n).Text(16)
}

func int64Hex(n int64) string {
	return "0x" + big.NewInt(n).Text(16)
}

func decimalHex(t *testing.T, decimal string) string {
	t.Helper()
	n, ok := new(big.Int).SetString(decimal, 10)
	if !ok {
		t.Fatalf("not a decimal quantity: %q", decimal)
	}
	return "0x" + n.Text(16)
}

func assertDecimalWei(t *testing.T, field, got string) {
	t.Helper()
	n, ok := new(big.Int).SetString(got, 10)
	if !ok || n.Sign() < 0 {
		t.Errorf("%s = %q, want a non-negative decimal integer", field, got)
	}
}
