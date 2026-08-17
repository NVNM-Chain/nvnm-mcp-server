// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// evm_get_logs -- event log queries, filtered by address and block range.
//
// The query is deliberately narrow: the anchor precompile's logs over the
// single block this run's addRecord landed in. That block must contain at
// least one precompile log, because anchoring is publicly observable by
// design -- this is the phase that would catch an anchor write emitting
// nothing at all.

func phaseGetLogs(t *testing.T, f *flow) {
	type logsResponse struct {
		Logs []struct {
			Address     string   `json:"address"`
			Topics      []string `json:"topics"`
			Data        string   `json:"data"`
			BlockNumber uint64   `json:"block_number"`
			TxHash      string   `json:"tx_hash"`
		} `json:"logs"`
		Count int `json:"count"`
	}

	//nolint:gosec // testnet block heights are far below int64 max
	blockNum := int64(f.recordBlockNum)

	var out logsResponse
	f.callOK(t, "evm_get_logs", map[string]any{
		"address":    f.anchorAddress,
		"from_block": blockNum,
		"to_block":   blockNum,
	}, &out)

	if out.Count == 0 {
		t.Errorf("no anchor precompile logs in block %d, but this run's addRecord was mined there",
			f.recordBlockNum)
	}
	if out.Count != len(out.Logs) {
		t.Errorf("count = %d but logs carries %d entries", out.Count, len(out.Logs))
	}
	for _, l := range out.Logs {
		if !strings.EqualFold(l.Address, f.anchorAddress) {
			t.Errorf("log from %s leaked into an address-filtered query for %s", l.Address, f.anchorAddress)
		}
		if l.BlockNumber != f.recordBlockNum {
			t.Errorf("log from block %d leaked into a query bounded to block %d",
				l.BlockNumber, f.recordBlockNum)
		}
	}
	t.Logf("block %d has %d anchor precompile logs", f.recordBlockNum, out.Count)

	// The same range with no address filter must be a superset: dropping
	// a filter cannot drop results.
	var unfiltered logsResponse
	f.callOK(t, "evm_get_logs", map[string]any{
		"from_block": blockNum,
		"to_block":   blockNum,
	}, &unfiltered)
	if unfiltered.Count < out.Count {
		t.Errorf("an unfiltered query over block %d returned %d logs, fewer than the %d returned "+
			"when filtering by address", f.recordBlockNum, unfiltered.Count, out.Count)
	}

	// A range that ends before it starts has no valid answer and must be
	// refused rather than silently normalized.
	if got := f.call(t, "evm_get_logs", map[string]any{
		"address":    f.anchorAddress,
		"from_block": blockNum,
		"to_block":   blockNum - 10,
	}); !got.IsError {
		t.Error("an inverted block range (to_block before from_block) was accepted")
	}
}
