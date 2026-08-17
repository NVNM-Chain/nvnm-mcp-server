// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// EVM tools: evm_get_chain_id, evm_get_block, evm_get_transaction,
// evm_get_transaction_receipt, evm_get_balance, evm_get_code,
// evm_get_logs, evm_call_contract, evm_send_raw_transaction.
//
// evm_send_raw_transaction and evm_get_transaction_receipt are exercised
// by the harness on every anchor write, so they have no phase of their
// own -- the coverage phase confirms they were reached. The phases below
// target the transaction and block that this run's addRecord produced, so
// they assert against data with a known expected shape rather than
// whatever happens to be on chain.

// phaseChainID reads chain identity from the EVM side and cross-checks it
// against what nvnm_overview reported from config. A mismatch means the
// server is describing a different chain than it is talking to.
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
	if f.chainID != 0 && out.ChainID != f.chainID {
		t.Errorf("evm_get_chain_id reports %d but nvnm_overview reports %d; "+
			"the server's configured chain and its RPC disagree", out.ChainID, f.chainID)
	}
	t.Logf("chain_id=%d latest_block=%d", out.ChainID, out.LatestBlockNumber)
}

// transactionResponse mirrors evm_get_transaction's JSON. The optional
// fields are pointers on the wire because they are absent while a
// transaction is still pending; ours is mined, so they must be present.
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

// phaseGetTransaction fetches this run's addRecord transaction and checks
// it is the one we broadcast: our sender, the anchor precompile as
// destination.
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
	if out.IsPending {
		t.Error("is_pending = true for a transaction we already have a receipt for")
	}
	if out.BlockNumber == nil {
		t.Error("block_number is absent for a mined transaction")
	} else if *out.BlockNumber != f.recordBlockNum {
		t.Errorf("block_number = %d, want %d (from the receipt)", *out.BlockNumber, f.recordBlockNum)
	}
	if out.Data == "" {
		t.Error("data is empty; an anchor call carries ABI-encoded calldata")
	}
}

type blockResponse struct {
	Number           uint64 `json:"number"`
	Hash             string `json:"hash"`
	ParentHash       string `json:"parent_hash"`
	TimestampUnix    uint64 `json:"timestamp_unix"`
	GasUsed          uint64 `json:"gas_used"`
	TransactionCount int    `json:"transaction_count"`
}

// phaseGetBlock exercises both lookup modes -- by number and by hash --
// against the block this run's record landed in, and checks they agree.
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

	var byHash blockResponse
	f.callOK(t, "evm_get_block", map[string]any{"block_hash": f.recordBlockHash}, &byHash)

	if byHash.Number != byNumber.Number {
		t.Errorf("by-hash lookup returned block %d, by-number returned %d; they must be the same block",
			byHash.Number, byNumber.Number)
	}
	if byNumber.TransactionCount == 0 {
		t.Errorf("block %d reports 0 transactions, but this run's addRecord was mined there", byNumber.Number)
	}
	t.Logf("block %d: hash=%s txs=%d", byNumber.Number, byNumber.Hash, byNumber.TransactionCount)
}

// phaseGetBalance reads the signing wallet's balance. It must be
// non-empty: this wallet has been paying for the writes above.
func phaseGetBalance(t *testing.T, f *flow) {
	var out struct {
		Address string `json:"address"`
		Wei     string `json:"wei"`
		Ether   string `json:"ether"`
	}
	f.callOK(t, "evm_get_balance", map[string]any{"address": f.address}, &out)

	if !strings.EqualFold(out.Address, f.address) {
		t.Errorf("address = %s, want %s", out.Address, f.address)
	}
	if out.Wei == "" || out.Wei == "0" {
		t.Errorf("wei = %q, but this wallet just paid gas for several transactions", out.Wei)
	}
	t.Logf("balance: %s wei (%s ether)", out.Wei, out.Ether)
}

// phaseGetCode reads code at the signing wallet, an externally owned
// account, so the expected answer is unambiguous: no code, not a
// contract.
func phaseGetCode(t *testing.T, f *flow) {
	var out struct {
		Address    string `json:"address"`
		Bytecode   string `json:"bytecode"`
		IsContract bool   `json:"is_contract"`
	}
	f.callOK(t, "evm_get_code", map[string]any{"address": f.address}, &out)

	if out.IsContract {
		t.Errorf("is_contract = true for the signing EOA %s", f.address)
	}
	t.Logf("code at %s: is_contract=%v bytecode_len=%d", out.Address, out.IsContract, len(out.Bytecode))
}

// phaseGetLogs filters the anchor precompile's logs over the single block
// this run's addRecord landed in. Anchor writes are publicly observable
// by design, so that block must contain at least one precompile log --
// this is the phase that would catch anchoring silently emitting nothing.
func phaseGetLogs(t *testing.T, f *flow) {
	var out struct {
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
	f.callOK(t, "evm_get_logs", map[string]any{
		"address":    f.anchorAddress,
		"from_block": blockNum,
		"to_block":   blockNum,
	}, &out)

	if out.Count == 0 {
		t.Errorf("no anchor precompile logs in block %d, but this run's addRecord was mined there",
			f.recordBlockNum)
	}
	for _, l := range out.Logs {
		if !strings.EqualFold(l.Address, f.anchorAddress) {
			t.Errorf("log from %s leaked into an address-filtered query for %s", l.Address, f.anchorAddress)
		}
	}
	t.Logf("block %d has %d anchor precompile logs", f.recordBlockNum, out.Count)
}

// phaseCallContract exercises the arbitrary-read path. It targets the
// signing EOA with empty calldata, which is the one eth_call whose result
// is deterministic on any chain: no code to run, so empty return data and
// no revert. Calling the precompile with real calldata would instead
// simulate a state-changing method, whose outcome depends on chain state.
func phaseCallContract(t *testing.T, f *flow) {
	var out struct {
		Result string `json:"result"`
	}
	f.callOK(t, "evm_call_contract", map[string]any{
		"to":   f.address,
		"data": "0x",
	}, &out)

	if out.Result != "0x" && out.Result != "" {
		t.Errorf("result = %q, want empty return data from a call to an account with no code", out.Result)
	}
}
