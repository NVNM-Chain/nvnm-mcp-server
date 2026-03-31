package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Client wraps go-ethereum's ethclient and returns normalized response types.
type Client interface {
	ChainID(ctx context.Context) (*big.Int, error)
	LatestBlockNumber(ctx context.Context) (uint64, error)
	GetChainInfo(ctx context.Context) (*ChainInfo, error)
	BlockByNumber(ctx context.Context, number *big.Int, fullTx bool) (*NormalizedBlock, error)
	BlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (*NormalizedBlock, error)
	TransactionByHash(ctx context.Context, hash common.Hash) (*NormalizedTransaction, error)
	TransactionReceipt(ctx context.Context, hash common.Hash) (*NormalizedReceipt, error)
	BalanceAt(ctx context.Context, address common.Address, block *big.Int) (*NormalizedBalance, error)
	CodeAt(ctx context.Context, address common.Address, block *big.Int) (*CodeResult, error)
	CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]NormalizedLog, error)
	Close()
}

type client struct {
	eth     *ethclient.Client
	timeout time.Duration
}

// NewClient creates a new EVM client connected to the given RPC URL.
func NewClient(ctx context.Context, rpcURL string, timeout time.Duration) (Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	eth, err := ethclient.DialContext(dialCtx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to EVM RPC at %s: %w", rpcURL, err)
	}
	return &client{eth: eth, timeout: timeout}, nil
}

func (c *client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, c.timeout)
}

// ChainID returns the chain identifier.
func (c *client) ChainID(ctx context.Context) (*big.Int, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.eth.ChainID(ctx)
}

// LatestBlockNumber returns the most recent block number.
func (c *client) LatestBlockNumber(ctx context.Context) (uint64, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.eth.BlockNumber(ctx)
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

	block, err := c.eth.BlockByNumber(ctx, number)
	if err != nil {
		return nil, fmt.Errorf("failed to get block by number: %w", err)
	}
	return normalizeBlock(block, fullTx), nil
}

// BlockByHash returns a normalized block by hash.
func (c *client) BlockByHash(ctx context.Context, hash common.Hash, fullTx bool) (*NormalizedBlock, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	block, err := c.eth.BlockByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get block by hash: %w", err)
	}
	return normalizeBlock(block, fullTx), nil
}

// TransactionByHash returns a normalized transaction by hash.
func (c *client) TransactionByHash(ctx context.Context, hash common.Hash) (*NormalizedTransaction, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	tx, isPending, err := c.eth.TransactionByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}
	return normalizeTransaction(tx, isPending), nil
}

// TransactionReceipt returns a normalized receipt by transaction hash.
func (c *client) TransactionReceipt(ctx context.Context, hash common.Hash) (*NormalizedReceipt, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	receipt, err := c.eth.TransactionReceipt(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction receipt: %w", err)
	}
	return normalizeReceipt(receipt), nil
}

// BalanceAt returns a normalized balance for an address at a given block.
func (c *client) BalanceAt(ctx context.Context, address common.Address, block *big.Int) (*NormalizedBalance, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	balance, err := c.eth.BalanceAt(ctx, address, block)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	return normalizeBalance(address, balance), nil
}

// CodeAt returns the contract bytecode at an address.
func (c *client) CodeAt(ctx context.Context, address common.Address, block *big.Int) (*CodeResult, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	code, err := c.eth.CodeAt(ctx, address, block)
	if err != nil {
		return nil, fmt.Errorf("failed to get code: %w", err)
	}
	return &CodeResult{
		Address:    address.Hex(),
		Bytecode:   "0x" + hex.EncodeToString(code),
		IsContract: len(code) > 0,
	}, nil
}

// CallContract executes a read-only contract call.
//
//nolint:gocritic // hugeParam: msg matches go-ethereum's CallContract signature
func (c *client) CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()
	return c.eth.CallContract(ctx, msg, block)
}

// FilterLogs returns normalized logs matching the filter query.
func (c *client) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]NormalizedLog, error) {
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	logs, err := c.eth.FilterLogs(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to filter logs: %w", err)
	}
	result := make([]NormalizedLog, len(logs))
	for i := range logs {
		result[i] = normalizeLog(&logs[i])
	}
	return result, nil
}

// Close closes the underlying RPC connection.
func (c *client) Close() {
	c.eth.Close()
}

func normalizeBlock(block *types.Block, fullTx bool) *NormalizedBlock {
	nb := &NormalizedBlock{
		Number:           block.NumberU64(),
		Hash:             block.Hash().Hex(),
		ParentHash:       block.ParentHash().Hex(),
		TimestampUnix:    block.Time(),
		GasLimit:         block.GasLimit(),
		GasUsed:          block.GasUsed(),
		Miner:            block.Coinbase().Hex(),
		TransactionCount: len(block.Transactions()),
	}
	if block.BaseFee() != nil {
		s := block.BaseFee().String()
		nb.BaseFeePerGas = &s
	}
	if fullTx {
		txs := block.Transactions()
		nb.Transactions = make([]NormalizedTxSummary, len(txs))
		for i, tx := range txs {
			summary := NormalizedTxSummary{
				Hash:  tx.Hash().Hex(),
				Index: uint(i),
				Value: tx.Value().String(),
			}
			if tx.To() != nil {
				to := tx.To().Hex()
				summary.To = to
			}
			nb.Transactions[i] = summary
		}
	}
	return nb
}

func normalizeTransaction(tx *types.Transaction, isPending bool) *NormalizedTransaction {
	nt := &NormalizedTransaction{
		Hash:      tx.Hash().Hex(),
		Value:     tx.Value().String(),
		Gas:       tx.Gas(),
		GasPrice:  tx.GasPrice().String(),
		Nonce:     tx.Nonce(),
		Data:      "0x" + hex.EncodeToString(tx.Data()),
		IsPending: isPending,
	}
	if tx.To() != nil {
		to := tx.To().Hex()
		nt.To = &to
	}
	return nt
}

func normalizeReceipt(receipt *types.Receipt) *NormalizedReceipt {
	nr := &NormalizedReceipt{
		TxHash:         receipt.TxHash.Hex(),
		BlockNumber:    receipt.BlockNumber.Uint64(),
		BlockHash:      receipt.BlockHash.Hex(),
		TransactionIdx: receipt.TransactionIndex,
		GasUsed:        receipt.GasUsed,
		CumulativeGas:  receipt.CumulativeGasUsed,
		Logs:           make([]NormalizedLog, len(receipt.Logs)),
	}
	switch receipt.Status {
	case types.ReceiptStatusSuccessful:
		nr.Status = "success"
	case types.ReceiptStatusFailed:
		nr.Status = "reverted"
	default:
		nr.Status = fmt.Sprintf("unknown(%d)", receipt.Status)
	}
	if receipt.ContractAddress != (common.Address{}) {
		addr := receipt.ContractAddress.Hex()
		nr.ContractAddress = &addr
	}
	for i, l := range receipt.Logs {
		nr.Logs[i] = normalizeLog(l)
	}
	return nr
}

func normalizeBalance(address common.Address, balance *big.Int) *NormalizedBalance {
	ether := new(big.Float).Quo(
		new(big.Float).SetInt(balance),
		new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)),
	)
	return &NormalizedBalance{
		Address: address.Hex(),
		Wei:     balance.String(),
		Ether:   ether.Text('f', 18),
	}
}

func normalizeLog(l *types.Log) NormalizedLog {
	topics := make([]string, len(l.Topics))
	for i, t := range l.Topics {
		topics[i] = t.Hex()
	}
	return NormalizedLog{
		Address:     l.Address.Hex(),
		Topics:      topics,
		Data:        "0x" + hex.EncodeToString(l.Data),
		BlockNumber: l.BlockNumber,
		TxHash:      l.TxHash.Hex(),
		TxIndex:     l.TxIndex,
		LogIndex:    l.Index,
		Removed:     l.Removed,
	}
}
