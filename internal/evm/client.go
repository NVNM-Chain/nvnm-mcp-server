// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// Client wraps defiweb/go-eth's JSON-RPC client and returns normalized
// response types. The interface is intentionally narrower than the
// underlying SDK: it exposes only the methods the MCP tools consume.
type Client interface {
	// Read methods
	ChainID(ctx context.Context) (*big.Int, error)
	LatestBlockNumber(ctx context.Context) (uint64, error)
	GetChainInfo(ctx context.Context) (*ChainInfo, error)
	BlockByNumber(ctx context.Context, number *big.Int, fullTx bool) (*NormalizedBlock, error)
	BlockByHash(ctx context.Context, hash defitypes.Hash, fullTx bool) (*NormalizedBlock, error)
	TransactionByHash(ctx context.Context, hash defitypes.Hash) (*NormalizedTransaction, error)
	TransactionReceipt(ctx context.Context, hash defitypes.Hash) (*NormalizedReceipt, error)
	BalanceAt(ctx context.Context, address defitypes.Address, block *big.Int) (*NormalizedBalance, error)
	CodeAt(ctx context.Context, address defitypes.Address, block *big.Int) (*CodeResult, error)
	CallContract(ctx context.Context, msg defitypes.Call, block *big.Int) ([]byte, error)
	FilterLogs(ctx context.Context, q defitypes.FilterLogsQuery) ([]NormalizedLog, error)

	// Write support methods
	PendingNonceAt(ctx context.Context, address defitypes.Address) (uint64, error)
	SuggestGasPrice(ctx context.Context) (*big.Int, error)
	SuggestGasTipCap(ctx context.Context) (*big.Int, error)
	EstimateGas(ctx context.Context, msg defitypes.Call) (uint64, error)
	SendRawTransaction(ctx context.Context, signedTxHex string) (string, error)

	// Ping checks that the RPC connection is alive (used by readiness probes).
	Ping(ctx context.Context) error

	// Close releases the underlying RPC connection.
	Close()
}

type client struct {
	rpc     defiRPCClient
	timeout time.Duration
}

// defiRPCClient is the subset of methods we need from defiweb's
// *rpc.Client. Declared as an interface in the file scope only to keep
// the wiring testable; in production the concrete *rpc.Client satisfies
// it.
type defiRPCClient interface {
	ChainID(ctx context.Context) (uint64, error)
	BlockNumber(ctx context.Context) (*big.Int, error)
	BlockByNumber(ctx context.Context, number defitypes.BlockNumber, full bool) (*defitypes.Block, error)
	BlockByHash(ctx context.Context, hash defitypes.Hash, full bool) (*defitypes.Block, error)
	GetTransactionByHash(ctx context.Context, hash defitypes.Hash) (*defitypes.OnChainTransaction, error)
	GetTransactionReceipt(ctx context.Context, hash defitypes.Hash) (*defitypes.TransactionReceipt, error)
	GetBalance(ctx context.Context, address defitypes.Address, block defitypes.BlockNumber) (*big.Int, error)
	GetCode(ctx context.Context, account defitypes.Address, block defitypes.BlockNumber) ([]byte, error)
	GetTransactionCount(ctx context.Context, account defitypes.Address, block defitypes.BlockNumber) (uint64, error)
	GetLogs(ctx context.Context, query *defitypes.FilterLogsQuery) ([]defitypes.Log, error)
	GasPrice(ctx context.Context) (*big.Int, error)
	MaxPriorityFeePerGas(ctx context.Context) (*big.Int, error)
	Call(ctx context.Context, call *defitypes.Call, block defitypes.BlockNumber) ([]byte, *defitypes.Call, error)
	EstimateGas(ctx context.Context, call *defitypes.Call, block defitypes.BlockNumber) (uint64, *defitypes.Call, error)
	SendRawTransaction(ctx context.Context, raw []byte) (*defitypes.Hash, error)
}

func blockNumOrLatest(b *big.Int) defitypes.BlockNumber {
	if b == nil {
		return defitypes.LatestBlockNumber
	}
	return defitypes.BlockNumberFromBigInt(b)
}

func (c *client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

// guardNodeDecode runs fn -- which reads and normalizes an untrusted node/RPC
// response -- and converts any panic into an ErrNodeResponseDecode error.
// defiweb/go-eth can panic (nil *big.Int dereference, slice bounds) when
// decoding malformed input, and node responses are untrusted: config permits a
// plaintext http:// endpoint, so a hostile or MITM'd node controls these bytes.
// On the stdio transport an unrecovered panic would crash the process, a
// denial of service. This mirrors the recover() on the caller-input decode
// path (decode.go) for the node-response direction (EV-2). Deliberately narrow
// -- it wraps only node-response decode/normalize, never our own trusted logic.
func guardNodeDecode(op string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: %w", op, apperrors.ErrNodeResponseDecode)
		}
	}()
	return fn()
}

// ChainID returns the chain identifier.
func (c *client) ChainID(ctx context.Context) (*big.Int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	id, err := c.rpc.ChainID(ctx)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetUint64(id), nil
}

// LatestBlockNumber returns the most recent block number.
func (c *client) LatestBlockNumber(ctx context.Context) (uint64, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var out uint64
	err := guardNodeDecode("get latest block number", func() error {
		n, err := c.rpc.BlockNumber(ctx)
		if err != nil {
			return err
		}
		out = n.Uint64()
		return nil
	})
	return out, err
}

// GetChainInfo returns chain ID and latest block number.
func (c *client) GetChainInfo(ctx context.Context) (*ChainInfo, error) {
	chainID, err := c.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}
	blockNum, err := c.LatestBlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest block number: %w", err)
	}
	return &ChainInfo{
		ChainID:           chainID.Int64(),
		LatestBlockNumber: blockNum,
	}, nil
}

// BlockByNumber returns a normalized block by number. Pass nil for latest.
func (c *client) BlockByNumber(ctx context.Context, number *big.Int, fullTx bool) (*NormalizedBlock, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var out *NormalizedBlock
	err := guardNodeDecode("get block by number", func() error {
		block, err := c.rpc.BlockByNumber(ctx, blockNumOrLatest(number), fullTx)
		if err != nil {
			return fmt.Errorf("failed to get block by number: %w", err)
		}
		out = normalizeBlock(block, fullTx)
		return nil
	})
	return out, err
}

// BlockByHash returns a normalized block by hash.
func (c *client) BlockByHash(ctx context.Context, hash defitypes.Hash, fullTx bool) (*NormalizedBlock, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var out *NormalizedBlock
	err := guardNodeDecode("get block by hash", func() error {
		block, err := c.rpc.BlockByHash(ctx, hash, fullTx)
		if err != nil {
			return fmt.Errorf("failed to get block by hash: %w", err)
		}
		out = normalizeBlock(block, fullTx)
		return nil
	})
	return out, err
}

// TransactionByHash returns a normalized transaction by hash.
func (c *client) TransactionByHash(ctx context.Context, hash defitypes.Hash) (*NormalizedTransaction, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var out *NormalizedTransaction
	err := guardNodeDecode("get transaction by hash", func() error {
		tx, err := c.rpc.GetTransactionByHash(ctx, hash)
		if err != nil {
			return fmt.Errorf("failed to get transaction: %w", err)
		}
		// A missing hash yields either a nil tx or (with some RPC/decoder
		// combinations) a non-nil zero-value struct with no Hash. Treat both as
		// not-found: a real transaction -- pending or mined -- always carries a
		// hash. Without the Hash check a garbage hash decoded into an empty
		// struct, whose nil BlockNumber then read as "pending" with an empty hash.
		if tx == nil || tx.Hash == nil {
			return apperrors.ErrTxNotFound
		}
		// defiweb's OnChainTransaction surfaces BlockNumber/BlockHash; a
		// pending transaction has them as nil/zero.
		isPending := tx.BlockNumber == nil
		out = normalizeOnChainTransaction(tx, isPending)
		return nil
	})
	return out, err
}

// TransactionReceipt returns a normalized receipt by transaction hash.
func (c *client) TransactionReceipt(ctx context.Context, hash defitypes.Hash) (*NormalizedReceipt, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var out *NormalizedReceipt
	err := guardNodeDecode("get transaction receipt", func() error {
		receipt, err := c.rpc.GetTransactionReceipt(ctx, hash)
		if err != nil {
			return fmt.Errorf("failed to get transaction receipt: %w", err)
		}
		if receipt == nil {
			return apperrors.ErrTxNotFound
		}
		out = normalizeReceipt(receipt)
		return nil
	})
	return out, err
}

// BalanceAt returns a normalized balance for an address at a given block.
func (c *client) BalanceAt(ctx context.Context, address defitypes.Address, block *big.Int) (*NormalizedBalance, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var out *NormalizedBalance
	err := guardNodeDecode("get balance", func() error {
		balance, err := c.rpc.GetBalance(ctx, address, blockNumOrLatest(block))
		if err != nil {
			return fmt.Errorf("failed to get balance: %w", err)
		}
		out = normalizeBalance(address, balance)
		return nil
	})
	return out, err
}

// CodeAt returns the contract bytecode at an address.
func (c *client) CodeAt(ctx context.Context, address defitypes.Address, block *big.Int) (*CodeResult, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	code, err := c.rpc.GetCode(ctx, address, blockNumOrLatest(block))
	if err != nil {
		return nil, fmt.Errorf("failed to get code: %w", err)
	}
	return &CodeResult{
		Address:    AddressHex(address),
		Bytecode:   "0x" + hex.EncodeToString(code),
		IsContract: len(code) > 0,
	}, nil
}

// CallContract executes a read-only contract call.
//
//nolint:gocritic // hugeParam: msg matches go-ethereum's CallContract signature
func (c *client) CallContract(ctx context.Context, msg defitypes.Call, block *big.Int) ([]byte, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	out, _, err := c.rpc.Call(ctx, &msg, blockNumOrLatest(block))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FilterLogs returns normalized logs matching the filter query.
func (c *client) FilterLogs(ctx context.Context, q defitypes.FilterLogsQuery) ([]NormalizedLog, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	var result []NormalizedLog
	err := guardNodeDecode("filter logs", func() error {
		logs, err := c.rpc.GetLogs(ctx, &q)
		if err != nil {
			return fmt.Errorf("failed to filter logs: %w", err)
		}
		result = make([]NormalizedLog, len(logs))
		for i := range logs {
			result[i] = normalizeLog(&logs[i])
		}
		return nil
	})
	return result, err
}

// PendingNonceAt returns the pending nonce for an address (for transaction construction).
func (c *client) PendingNonceAt(ctx context.Context, address defitypes.Address) (uint64, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.rpc.GetTransactionCount(ctx, address, defitypes.PendingBlockNumber)
}

// SuggestGasPrice returns the current suggested gas price.
func (c *client) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.rpc.GasPrice(ctx)
}

// SuggestGasTipCap returns the current suggested miner tip (EIP-1559
// priority fee). Used alongside baseFee from the latest block to derive
// MaxFeePerGas and MaxPriorityFeePerGas for type-2 transactions.
func (c *client) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.rpc.MaxPriorityFeePerGas(ctx)
}

// EstimateGas estimates the gas needed for a transaction.
//
// interface; passing by value is consistent with the rest of the
// write-support surface (PendingNonceAt, SuggestGasPrice...).
//
//nolint:gocritic // hugeParam: Call is large but Client is a public
func (c *client) EstimateGas(ctx context.Context, msg defitypes.Call) (uint64, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	gas, _, err := c.rpc.EstimateGas(ctx, &msg, defitypes.LatestBlockNumber)
	return gas, err
}

// maxSignedTxHexLen caps signed transaction hex at 2 MB (1 MB decoded).
const maxSignedTxHexLen = 2 * 1024 * 1024

// SendRawTransaction broadcasts a signed transaction and returns the tx hash.
func (c *client) SendRawTransaction(ctx context.Context, signedTxHex string) (string, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	raw := signedTxHex
	if len(raw) >= 2 && raw[:2] == "0x" {
		raw = raw[2:]
	}

	if len(raw) > maxSignedTxHexLen {
		return "", fmt.Errorf(
			"signed tx hex too large (%d chars, max %d): %w",
			len(raw), maxSignedTxHexLen, apperrors.ErrInputTooLarge,
		)
	}

	txBytes, err := hex.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("decode signed tx hex: %w", err)
	}

	hashPtr, err := c.rpc.SendRawTransaction(ctx, txBytes)
	if err != nil {
		return "", fmt.Errorf("send transaction: %w", err)
	}
	if hashPtr == nil {
		return "", fmt.Errorf("send transaction: %w", apperrors.ErrEmptyTxHash)
	}
	return hashPtr.String(), nil
}

// Ping verifies the RPC connection by requesting the chain ID.
func (c *client) Ping(ctx context.Context) error {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	_, err := c.rpc.ChainID(ctx)
	return err
}

// Close is a no-op for defiweb's HTTP-transport client; included to
// satisfy the Client interface and to match the previous ethclient
// shape so callers don't change their shutdown sequencing.
func (c *client) Close() {}
