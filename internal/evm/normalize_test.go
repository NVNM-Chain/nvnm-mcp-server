// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"math/big"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
)

func TestSafeUnix(t *testing.T) {
	if got := safeUnix(-1); got != 0 {
		t.Errorf("safeUnix(-1) = %d, want 0 (pre-1970 clamp)", got)
	}
	if got := safeUnix(1709300000); got != 1709300000 {
		t.Errorf("safeUnix(1709300000) = %d, want 1709300000", got)
	}
}

func TestNormalizeBlock_FullTxFieldFallbacks(t *testing.T) {
	// One tx with every optional field nil: the summary must fall back
	// to zero values instead of dereferencing nil pointers.
	block := &defitypes.Block{
		Number:       big.NewInt(2000),
		Timestamp:    time.Unix(1709300000, 0),
		Transactions: []defitypes.OnChainTransaction{{}},
	}
	nb := normalizeBlock(block, true)
	if len(nb.Transactions) != 1 {
		t.Fatalf("transactions = %d, want 1", len(nb.Transactions))
	}
	sum := nb.Transactions[0]
	if sum.Hash != "" || sum.To != "" || sum.Value != "0" {
		t.Errorf("summary = %+v, want empty hash/to and value 0", sum)
	}
	if nb.TransactionCount != 1 {
		t.Errorf("transaction count = %d, want 1", nb.TransactionCount)
	}
}

func TestNormalizeBlock_HashesOnly(t *testing.T) {
	h1 := defitypes.MustHashFromHex(
		"0x1111111111111111111111111111111111111111111111111111111111111111",
		defitypes.PadNone,
	)
	h2 := defitypes.MustHashFromHex(
		"0x2222222222222222222222222222222222222222222222222222222222222222",
		defitypes.PadNone,
	)
	block := &defitypes.Block{
		Number:            big.NewInt(3000),
		Timestamp:         time.Unix(1709300000, 0),
		TransactionHashes: []defitypes.Hash{h1, h2},
	}
	nb := normalizeBlock(block, true)
	if nb.TransactionCount != 2 {
		t.Errorf("transaction count = %d, want 2", nb.TransactionCount)
	}
	if len(nb.Transactions) != 2 {
		t.Fatalf("transactions = %d, want 2 (from hashes)", len(nb.Transactions))
	}
	if nb.Transactions[0].Hash != h1.String() || nb.Transactions[1].Index != 1 {
		t.Errorf("summaries = %+v, want hash-only entries in order", nb.Transactions)
	}
}

func TestNormalizeBlock_NoFullTx(t *testing.T) {
	block := &defitypes.Block{
		Number:       big.NewInt(4000),
		Timestamp:    time.Unix(1709300000, 0),
		Transactions: []defitypes.OnChainTransaction{{}},
	}
	nb := normalizeBlock(block, false)
	if len(nb.Transactions) != 0 {
		t.Errorf("fullTx=false must not populate transactions, got %d", len(nb.Transactions))
	}
}

func TestNormalizeOnChainTransaction_AllFields(t *testing.T) {
	from := defitypes.MustAddressFromHex("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	to := defitypes.MustAddressFromHex("0xfb6916095ca1df60bb79ce92ce3ea74c37c5d359")
	gasLimit := uint64(120000)
	nonce := uint64(42)
	tx := &defitypes.OnChainTransaction{
		Hash: defitypes.MustHashFromHexPtr(
			"0x1111111111111111111111111111111111111111111111111111111111111111",
			defitypes.PadNone,
		),
	}
	tx.From = &from
	tx.To = &to
	tx.Value = big.NewInt(5)
	tx.GasLimit = &gasLimit
	tx.GasPrice = big.NewInt(8_000_000_000)
	tx.Nonce = &nonce
	tx.Input = []byte{0xca, 0xfe}

	nt := normalizeOnChainTransaction(tx, false)
	if nt.From != "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed" {
		t.Errorf("from = %q, want checksummed sender", nt.From)
	}
	if nt.To == nil || *nt.To != "0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359" {
		t.Errorf("to = %v, want checksummed recipient", nt.To)
	}
	if nt.Value != "5" || nt.Gas != 120000 || nt.GasPrice != "8000000000" || nt.Nonce != 42 {
		t.Errorf("tx = %+v, want value=5 gas=120000 gasPrice=8000000000 nonce=42", nt)
	}
	if nt.Data != "0xcafe" {
		t.Errorf("data = %q, want 0xcafe", nt.Data)
	}
	if nt.IsPending {
		t.Error("IsPending = true, want false")
	}
}

func TestNormalizeOnChainTransaction_Defaults(t *testing.T) {
	// EIP-1559 tx: no GasPrice, MaxFeePerGas populates the legacy field.
	tx := &defitypes.OnChainTransaction{}
	tx.MaxFeePerGas = big.NewInt(2_000_000_000)

	nt := normalizeOnChainTransaction(tx, true)
	if nt.GasPrice != "2000000000" {
		t.Errorf("gas price = %q, want MaxFeePerGas fallback 2000000000", nt.GasPrice)
	}
	if nt.Value != "0" {
		t.Errorf("value = %q, want 0 for nil value", nt.Value)
	}
	if nt.Data != "0x" {
		t.Errorf("data = %q, want 0x for empty input", nt.Data)
	}
	if !nt.IsPending {
		t.Error("IsPending = false, want true")
	}
	if nt.Hash != "" || nt.From != "" || nt.To != nil {
		t.Errorf("tx = %+v, want empty hash/from/to", nt)
	}
}

func TestNormalizeReceipt_StatusVariants(t *testing.T) {
	success := uint64(1)
	reverted := uint64(0)
	weird := uint64(7)
	cases := []struct {
		name   string
		status *uint64
		want   string
	}{
		{"nil status", nil, "unknown"},
		{"success", &success, "success"},
		{"reverted", &reverted, "reverted"},
		{"unexpected code", &weird, "unknown(7)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nr := normalizeReceipt(&defitypes.TransactionReceipt{Status: tc.status})
			if nr.Status != tc.want {
				t.Errorf("status = %q, want %q", nr.Status, tc.want)
			}
		})
	}
}

func TestNormalizeReceipt_ContractCreationWithLogs(t *testing.T) {
	status := uint64(1)
	contract := defitypes.MustAddressFromHex("0x9876543210abcdef9876543210abcdef98765432")
	receipt := &defitypes.TransactionReceipt{
		TransactionHash: defitypes.MustHashFromHex(
			"0x1111111111111111111111111111111111111111111111111111111111111111",
			defitypes.PadNone,
		),
		TransactionIndex:  3,
		BlockNumber:       big.NewInt(1000),
		GasUsed:           21000,
		CumulativeGasUsed: 42000,
		Status:            &status,
		ContractAddress:   &contract,
		Logs: []defitypes.Log{
			{Address: contract, Data: []byte{0x01}},
		},
	}
	nr := normalizeReceipt(receipt)
	if nr.BlockNumber != 1000 || nr.TransactionIdx != 3 || nr.CumulativeGas != 42000 {
		t.Errorf("receipt = %+v, want block=1000 idx=3 cumulative=42000", nr)
	}
	if nr.ContractAddress == nil || *nr.ContractAddress != AddressHex(contract) {
		t.Errorf("contract address = %v, want %q", nr.ContractAddress, AddressHex(contract))
	}
	if len(nr.Logs) != 1 || nr.Logs[0].Data != "0x01" {
		t.Errorf("logs = %+v, want one log with data 0x01", nr.Logs)
	}
}

func TestNormalizeReceipt_NilBlockNumber(t *testing.T) {
	nr := normalizeReceipt(&defitypes.TransactionReceipt{})
	if nr.BlockNumber != 0 {
		t.Errorf("block number = %d, want 0 for nil BlockNumber", nr.BlockNumber)
	}
}

func TestNormalizeBalance(t *testing.T) {
	addr := defitypes.MustAddressFromHex("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	nb := normalizeBalance(addr, big.NewInt(1_500_000_000_000_000_000))
	if nb.Wei != "1500000000000000000" {
		t.Errorf("wei = %q, want 1500000000000000000", nb.Wei)
	}
	if nb.Ether != "1.500000000000000000" {
		t.Errorf("ether = %q, want 1.500000000000000000", nb.Ether)
	}
	if nb.Address != "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed" {
		t.Errorf("address = %q, want checksummed form", nb.Address)
	}
}

func TestNormalizeLog_PendingFieldsNil(t *testing.T) {
	// A pending log has all positional pointers nil; the normalizer
	// must leave the zero values instead of dereferencing.
	nl := normalizeLog(&defitypes.Log{Removed: true})
	if nl.BlockNumber != 0 || nl.TxHash != "" || nl.TxIndex != 0 || nl.LogIndex != 0 {
		t.Errorf("log = %+v, want zero positional fields", nl)
	}
	if !nl.Removed {
		t.Error("removed flag was not carried over")
	}
	if nl.Data != "0x" {
		t.Errorf("data = %q, want 0x for empty data", nl.Data)
	}
	if len(nl.Topics) != 0 {
		t.Errorf("topics = %v, want empty", nl.Topics)
	}
}
