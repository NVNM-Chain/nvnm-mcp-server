// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/defiweb/go-eth/rpc"
	"github.com/defiweb/go-eth/rpc/transport"
	defitypes "github.com/defiweb/go-eth/types"
)

// NewClient creates a new EVM client connected to the given RPC URL via
// the defiweb/go-eth HTTP transport.
func NewClient(ctx context.Context, rpcURL string, timeout time.Duration) (Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_ = dialCtx // reserved for future use (defiweb's HTTP transport does not currently take a connect-context)

	tport, err := transport.NewHTTP(transport.HTTPOptions{URL: rpcURL})
	if err != nil {
		return nil, fmt.Errorf("failed to build HTTP transport: %w", err)
	}
	rpcClient, err := rpc.NewClient(rpc.WithTransport(tport))
	if err != nil {
		return nil, fmt.Errorf("failed to build RPC client: %w", err)
	}
	return &client{rpc: rpcClient, timeout: timeout}, nil
}

// safeUnix coerces an int64 Unix timestamp into uint64, clamping
// negative values (pre-1970, impossible for an on-chain block) to 0.
// The clamp pattern lets gosec see the bounds check immediately
// before the conversion.
func safeUnix(t int64) uint64 {
	if t < 0 {
		return 0
	}
	return uint64(t)
}

func normalizeBlock(block *defitypes.Block, fullTx bool) *NormalizedBlock {
	nb := &NormalizedBlock{
		Number:           block.Number.Uint64(),
		Hash:             block.Hash.String(),
		ParentHash:       block.ParentHash.String(),
		TimestampUnix:    safeUnix(block.Timestamp.Unix()),
		GasLimit:         block.GasLimit,
		GasUsed:          block.GasUsed,
		Miner:            AddressHex(block.Miner),
		TransactionCount: len(block.Transactions) + len(block.TransactionHashes),
	}
	// defiweb does not surface a typed BaseFeePerGas yet; the field is
	// available on EIP-1559 blocks via the underlying JSON map but is
	// not exposed on Block. Leave BaseFeePerGas nil for now; callers
	// that need it should query the block by hash from a node that
	// supports the field and decode it directly.
	if fullTx && len(block.Transactions) > 0 {
		nb.Transactions = make([]NormalizedTxSummary, len(block.Transactions))
		for i := range block.Transactions {
			tx := &block.Transactions[i]
			summary := NormalizedTxSummary{
				Index: uint(i),
			}
			if tx.Hash != nil {
				summary.Hash = tx.Hash.String()
			}
			if tx.Value != nil {
				summary.Value = tx.Value.String()
			} else {
				summary.Value = "0"
			}
			if tx.To != nil {
				summary.To = AddressHex(*tx.To)
			}
			nb.Transactions[i] = summary
		}
	}
	if fullTx && len(block.TransactionHashes) > 0 && len(nb.Transactions) == 0 {
		nb.Transactions = make([]NormalizedTxSummary, len(block.TransactionHashes))
		for i, h := range block.TransactionHashes {
			nb.Transactions[i] = NormalizedTxSummary{Hash: h.String(), Index: uint(i)}
		}
	}
	return nb
}

// normalizeOnChainTransaction adapts defiweb's OnChainTransaction shape
// (which embeds Transaction and adds Hash/BlockHash/BlockNumber/idx)
// into the project's NormalizedTransaction.
func normalizeOnChainTransaction(tx *defitypes.OnChainTransaction, isPending bool) *NormalizedTransaction {
	nt := &NormalizedTransaction{
		IsPending: isPending,
	}
	if tx.Hash != nil {
		nt.Hash = tx.Hash.String()
	}
	if tx.Value != nil {
		nt.Value = tx.Value.String()
	} else {
		nt.Value = "0"
	}
	if tx.GasLimit != nil {
		nt.Gas = *tx.GasLimit
	}
	if tx.GasPrice != nil {
		nt.GasPrice = tx.GasPrice.String()
	} else if tx.MaxFeePerGas != nil {
		// EIP-1559 transactions surface MaxFeePerGas; populate GasPrice
		// so callers reading the legacy field see a sensible value.
		nt.GasPrice = tx.MaxFeePerGas.String()
	}
	if tx.Nonce != nil {
		nt.Nonce = *tx.Nonce
	}
	if len(tx.Input) > 0 {
		nt.Data = "0x" + hex.EncodeToString(tx.Input)
	} else {
		nt.Data = "0x"
	}
	if tx.To != nil {
		to := AddressHex(*tx.To)
		nt.To = &to
	}
	// The node recovers and returns the sender in eth_getTransactionByHash;
	// defiweb surfaces it as Transaction.From. Map it so callers see who
	// sent the tx instead of an empty "from".
	if tx.From != nil {
		nt.From = AddressHex(*tx.From)
	}
	return nt
}

func normalizeReceipt(receipt *defitypes.TransactionReceipt) *NormalizedReceipt {
	nr := &NormalizedReceipt{
		TxHash:         receipt.TransactionHash.String(),
		BlockHash:      receipt.BlockHash.String(),
		TransactionIdx: uint(receipt.TransactionIndex),
		GasUsed:        receipt.GasUsed,
		CumulativeGas:  receipt.CumulativeGasUsed,
		Logs:           make([]NormalizedLog, len(receipt.Logs)),
	}
	if receipt.BlockNumber != nil {
		nr.BlockNumber = receipt.BlockNumber.Uint64()
	}
	switch {
	case receipt.Status == nil:
		nr.Status = "unknown"
	case *receipt.Status == 1:
		nr.Status = "success"
	case *receipt.Status == 0:
		nr.Status = "reverted"
	default:
		nr.Status = fmt.Sprintf("unknown(%d)", *receipt.Status)
	}
	if receipt.ContractAddress != nil {
		addr := AddressHex(*receipt.ContractAddress)
		nr.ContractAddress = &addr
	}
	for i := range receipt.Logs {
		nr.Logs[i] = normalizeLog(&receipt.Logs[i])
	}
	return nr
}

func normalizeBalance(address defitypes.Address, balance *big.Int) *NormalizedBalance {
	ether := new(big.Float).Quo(
		new(big.Float).SetInt(balance),
		new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)),
	)
	return &NormalizedBalance{
		Address: AddressHex(address),
		Wei:     balance.String(),
		Ether:   ether.Text('f', 18),
	}
}

func normalizeLog(l *defitypes.Log) NormalizedLog {
	topics := make([]string, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = t.String()
	}
	nl := NormalizedLog{
		Address: AddressHex(l.Address),
		Topics:  topics,
		Data:    "0x" + hex.EncodeToString(l.Data),
		Removed: l.Removed,
	}
	if l.BlockNumber != nil {
		nl.BlockNumber = l.BlockNumber.Uint64()
	}
	if l.TransactionHash != nil {
		nl.TxHash = l.TransactionHash.String()
	}
	if l.TransactionIndex != nil {
		nl.TxIndex = uint(*l.TransactionIndex)
	}
	if l.LogIndex != nil {
		nl.LogIndex = uint(*l.LogIndex)
	}
	return nl
}
