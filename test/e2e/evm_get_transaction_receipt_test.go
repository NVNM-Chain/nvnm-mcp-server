// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// evm_get_transaction_receipt -- inclusion, outcome, gas, and decoded
// logs for a broadcast transaction.
//
// The harness leans on this tool for every write in the suite:
// signBroadcastConfirm polls it until a transaction is mined and refuses
// to continue unless status is "success". So by the time this phase runs
// the tool has been called dozens of times -- but always for one field.
// This is where the rest of the response is actually checked.

func phaseGetTransactionReceipt(t *testing.T, f *flow) {
	var out receipt
	f.callOK(t, "evm_get_transaction_receipt", map[string]any{"tx_hash": f.recordTxHash}, &out)

	if !strings.EqualFold(out.TxHash, f.recordTxHash) {
		t.Errorf("tx_hash = %s, want %s", out.TxHash, f.recordTxHash)
	}
	if out.Status != "success" {
		t.Errorf("status = %q for the fixture's addRecord, which this run confirmed as mined", out.Status)
	}
	if out.BlockNumber != f.recordBlockNum {
		t.Errorf("block_number = %d, want %d", out.BlockNumber, f.recordBlockNum)
	}
	if !strings.EqualFold(out.BlockHash, f.recordBlockHash) {
		t.Errorf("block_hash = %s, want %s", out.BlockHash, f.recordBlockHash)
	}
	if out.GasUsed == 0 {
		t.Error("gas_used = 0 for a transaction that ran precompile code")
	}

	// Anchoring is publicly observable by design: the write emits a log
	// from the precompile. A receipt with no logs would mean the anchor
	// landed silently, which is the failure this checks for.
	if len(out.Logs) == 0 {
		t.Error("the addRecord receipt carries no logs; anchor writes are observable by design")
	}
	for i, l := range out.Logs {
		if !strings.EqualFold(l.Address, f.anchorAddress) {
			t.Errorf("log %d came from %s, not the anchor precompile %s", i, l.Address, f.anchorAddress)
		}
		if !strings.EqualFold(l.TxHash, f.recordTxHash) {
			t.Errorf("log %d carries tx_hash %s, not %s", i, l.TxHash, f.recordTxHash)
		}
		if len(l.Topics) == 0 {
			t.Errorf("log %d has no topics; an anonymous event cannot be filtered for", i)
		}
	}
	t.Logf("receipt: status=%s block=%d gas_used=%d logs=%d",
		out.Status, out.BlockNumber, out.GasUsed, len(out.Logs))

	// A hash that was never broadcast must read as not-found, promptly.
	// Both halves matter, and both are load-bearing for the harness: its
	// polling loop treats a tool error as "not mined yet" and calls again
	// every couple of seconds, so a tool that answered with an empty
	// receipt would make every write in this suite return a bogus
	// success, and one that takes longer than the client timeout to say
	// "no" would stall the loop instead of advancing it.
	//
	// The transport error is caught rather than fatal: a request that
	// never comes back is the finding, and reporting it beats dying on it.
	unknown := "0x" + strings.Repeat("22", 32)
	got, err := f.tryCall(t, "evm_get_transaction_receipt", map[string]any{"tx_hash": unknown})
	switch {
	case err != nil:
		t.Errorf("a receipt lookup for a hash that was never broadcast (%s) did not answer within "+
			"the %s client timeout: %v. A not-found receipt has nothing to wait for, and the "+
			"harness polls this tool on every write expecting a prompt error",
			unknown, httpTimeout, err)
	case !got.IsError:
		t.Errorf("a receipt lookup for a hash that was never broadcast (%s) returned a result "+
			"instead of an error; the harness's mined-yet polling depends on that error", unknown)
	default:
		t.Logf("unknown tx hash: rejected -- %s", resultText(got))
	}
}
