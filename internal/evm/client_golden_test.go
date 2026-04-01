package evm

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func goldenPath(name string) string {
	return filepath.Join("testdata", name+".golden.json")
}

func assertGolden(t *testing.T, name string, v interface{}) {
	t.Helper()

	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	got = append(got, '\n')

	path := goldenPath(name)

	if *update {
		if writeErr := os.WriteFile(path, got, 0o644); writeErr != nil {
			t.Fatalf("update golden %s: %v", path, writeErr)
		}
		t.Logf("updated %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s golden mismatch.\nGot:\n%s\nWant:\n%s\n"+
			"Run: go test ./internal/evm/ -run %s -update",
			name, got, want, t.Name())
	}
}

func TestGolden_ChainInfo(t *testing.T) {
	info := ChainInfo{
		ChainID:           58887,
		LatestBlockNumber: 1234567,
	}
	assertGolden(t, "chain_info", info)
}

func TestGolden_NormalizedBlock(t *testing.T) {
	baseFee := "1000000000"
	block := NormalizedBlock{
		Number:           1000,
		Hash:             "0xabc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		ParentHash:       "0xdef456abc123def456abc123def456abc123def456abc123def456abc123def4",
		TimestampUnix:    1709300000,
		GasLimit:         30000000,
		GasUsed:          15000000,
		Miner:            "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD00",
		BaseFeePerGas:    &baseFee,
		TransactionCount: 2,
		Transactions: []NormalizedTxSummary{
			{
				Hash:  "0x1111111111111111111111111111111111111111111111111111111111111111",
				Index: 0,
				To:    "0x0000000000000000000000000000000000000A00",
				Value: "0",
			},
			{
				Hash:  "0x2222222222222222222222222222222222222222222222222222222222222222",
				Index: 1,
				To:    "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD00",
				Value: "1000000000000000000",
			},
		},
	}
	assertGolden(t, "normalized_block", block)
}

func TestGolden_NormalizedTransaction(t *testing.T) {
	to := "0x0000000000000000000000000000000000000A00"
	tx := NormalizedTransaction{
		Hash:      "0x1111111111111111111111111111111111111111111111111111111111111111",
		To:        &to,
		Value:     "0",
		Gas:       120000,
		GasPrice:  "8000000000",
		Nonce:     42,
		Data:      "0xcafebabe",
		IsPending: false,
	}
	assertGolden(t, "normalized_transaction", tx)
}

func TestGolden_NormalizedReceipt(t *testing.T) {
	contractAddr := "0x9876543210abcdef9876543210abcdef98765432"
	receipt := NormalizedReceipt{
		TxHash:          "0x1111111111111111111111111111111111111111111111111111111111111111",
		BlockNumber:     1000,
		BlockHash:       "0xabc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		TransactionIdx:  0,
		Status:          "success",
		GasUsed:         21000,
		CumulativeGas:   21000,
		ContractAddress: &contractAddr,
		Logs: []NormalizedLog{
			{
				Address:     "0x0000000000000000000000000000000000000A00",
				Topics:      []string{"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"},
				Data:        "0x0000000000000000000000000000000000000000000000000000000000000001",
				BlockNumber: 1000,
				TxHash:      "0x1111111111111111111111111111111111111111111111111111111111111111",
				TxIndex:     0,
				LogIndex:    0,
				Removed:     false,
			},
		},
	}
	assertGolden(t, "normalized_receipt", receipt)
}

func TestGolden_NormalizedBalance(t *testing.T) {
	balance := NormalizedBalance{
		Address: "0x742d35Cc6634C0532925a3b844Bc9e7595f2bD00",
		Wei:     "1000000000000000000",
		Ether:   "1.000000000000000000",
	}
	assertGolden(t, "normalized_balance", balance)
}

func TestGolden_CodeResult(t *testing.T) {
	code := CodeResult{
		Address:    "0x0000000000000000000000000000000000000A00",
		Bytecode:   "0x",
		IsContract: false,
	}
	assertGolden(t, "code_result", code)
}
