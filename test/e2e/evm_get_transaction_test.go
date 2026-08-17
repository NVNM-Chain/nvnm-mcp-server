// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// evm_get_transaction -- one transaction by hash.
//
// It runs against the fixture's addRecord transaction, so every field
// has a known expected value: this run signed it, broadcast it, and holds
// its receipt. That is what makes the assertions below sharp rather than
// shape-checks.

// transactionResponse mirrors evm_get_transaction's JSON. The optional
// fields are pointers because they are absent while a transaction is
// still pending; ours is mined, so they must be present.
type transactionResponse struct {
	Hash        string  `json:"hash"`
	From        string  `json:"from"`
	To          *string `json:"to"`
	Nonce       uint64  `json:"nonce"`
	BlockNumber *uint64 `json:"block_number"`
	BlockHash   *string `json:"block_hash"`
	Data        string  `json:"data"`
	IsPending   bool    `json:"is_pending"`
}

func phaseGetTransaction(t *testing.T, f *flow) {
	var out transactionResponse
	f.callOK(t, "evm_get_transaction", map[string]any{"tx_hash": f.recordTxHash}, &out)

	if !strings.EqualFold(out.Hash, f.recordTxHash) {
		t.Errorf("hash = %s, want %s", out.Hash, f.recordTxHash)
	}
	if !strings.EqualFold(out.From, f.address) {
		t.Errorf("from = %s, want the signing wallet %s", out.From, f.address)
	}
	if out.To == nil {
		t.Error("to is absent; an anchor call always has the precompile as its destination")
	} else if !strings.EqualFold(*out.To, f.anchorAddress) {
		t.Errorf("to = %s, want the anchor precompile %s", *out.To, f.anchorAddress)
	}
	if out.Data == "" {
		t.Error("data is empty; an anchor call carries ABI-encoded calldata")
	}

	// Pending-ness and block placement are one statement, not two: this
	// transaction has a receipt, so is_pending must be false AND the
	// block fields must be populated with what that receipt reported.
	if out.IsPending {
		t.Error("is_pending = true for a transaction we already have a receipt for")
	}
	if out.BlockNumber == nil {
		t.Errorf("block_number is absent for a transaction that is not pending (receipt puts it "+
			"in block %d); a caller cannot locate a mined transaction from this response",
			f.recordBlockNum)
	} else if *out.BlockNumber != f.recordBlockNum {
		t.Errorf("block_number = %d, want %d (from the receipt)", *out.BlockNumber, f.recordBlockNum)
	}
	if out.BlockHash == nil {
		t.Error("block_hash is absent for a transaction that is not pending")
	} else if !strings.EqualFold(*out.BlockHash, f.recordBlockHash) {
		t.Errorf("block_hash = %s, want %s (from the receipt)", *out.BlockHash, f.recordBlockHash)
	}

	// An unknown hash is a not-found, not a crash and not a zero-valued
	// transaction that a caller might mistake for a real one.
	unknown := "0x" + strings.Repeat("11", 32)
	if got := f.call(t, "evm_get_transaction", map[string]any{"tx_hash": unknown}); !got.IsError {
		t.Errorf("a transaction hash that does not exist (%s) returned a result instead of an error", unknown)
	}
}
