// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// evm_get_block -- a block by number or by hash.
//
// Both lookup modes are driven against the block the fixture's addRecord
// landed in, and then checked against each other: two ways of naming the
// same block must return the same block.

type blockResponse struct {
	Number           uint64 `json:"number"`
	Hash             string `json:"hash"`
	ParentHash       string `json:"parent_hash"`
	TimestampUnix    uint64 `json:"timestamp_unix"`
	GasUsed          uint64 `json:"gas_used"`
	TransactionCount int    `json:"transaction_count"`
}

func phaseGetBlock(t *testing.T, f *flow) {
	var byNumber blockResponse
	f.callOK(t, "evm_get_block", map[string]any{
		"block_number": int64(f.recordBlockNum), //nolint:gosec // testnet block heights are far below int64 max
	}, &byNumber)

	if byNumber.Number != f.recordBlockNum {
		t.Errorf("number = %d, want %d", byNumber.Number, f.recordBlockNum)
	}
	if !strings.EqualFold(byNumber.Hash, f.recordBlockHash) {
		t.Errorf("hash = %s, want %s (from the receipt)", byNumber.Hash, f.recordBlockHash)
	}
	if byNumber.TransactionCount == 0 {
		t.Errorf("block %d reports 0 transactions, but this run's addRecord was mined there",
			byNumber.Number)
	}
	if byNumber.TimestampUnix == 0 {
		t.Error("timestamp_unix is 0")
	}
	if byNumber.ParentHash == "" {
		t.Error("parent_hash is empty")
	}

	var byHash blockResponse
	f.callOK(t, "evm_get_block", map[string]any{"block_hash": f.recordBlockHash}, &byHash)

	if byHash.Number != byNumber.Number {
		t.Errorf("by-hash lookup returned block %d, by-number returned %d; they must be the same block",
			byHash.Number, byNumber.Number)
	}
	if byHash.TransactionCount != byNumber.TransactionCount {
		t.Errorf("the same block reports %d transactions by hash and %d by number",
			byHash.TransactionCount, byNumber.TransactionCount)
	}

	t.Logf("block %d: hash=%s txs=%d gas_used=%d",
		byNumber.Number, byNumber.Hash, byNumber.TransactionCount, byNumber.GasUsed)
}
