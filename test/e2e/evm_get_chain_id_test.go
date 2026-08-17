// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"testing"
)

// evm_get_chain_id -- chain identity straight from the RPC, plus the
// latest block height.
//
// The cross-check against nvnm_overview is what makes this more than a
// one-field read: overview reports the chain the server is *configured*
// for, this reports the chain it is actually *talking to*. A mismatch
// means every prepared transaction is signed for the wrong chain.

func phaseChainID(t *testing.T, f *flow) {
	var out struct {
		ChainID           int64  `json:"chain_id"`
		LatestBlockNumber uint64 `json:"latest_block_number"`
	}
	f.callOK(t, "evm_get_chain_id", map[string]any{}, &out)

	if out.ChainID == 0 {
		t.Fatal("chain_id is 0")
	}
	if out.LatestBlockNumber == 0 {
		t.Error("latest_block_number is 0; the RPC is not following the chain")
	}
	if out.ChainID != f.chainID {
		t.Errorf("evm_get_chain_id reports %d but nvnm_overview reports %d; "+
			"the server's configured chain and its RPC disagree", out.ChainID, f.chainID)
	}

	t.Logf("chain_id=%d latest_block=%d", out.ChainID, out.LatestBlockNumber)
}
