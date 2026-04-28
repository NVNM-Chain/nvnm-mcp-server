package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

func registerEVMTools(srv *mcp.Server, evmClient evm.Client, _ *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_chain_id",
		Title: "Get Chain ID",
		Description: "Returns the chain ID and latest block number " +
			"for the connected EVM network.",
	}, makeChainIDHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_block",
		Title: "Get Block",
		Description: "Returns a block by number or hash. " +
			"Use block_number for numeric lookup, block_hash for hash lookup. " +
			"Set full_transactions to true to include transaction details.",
	}, makeGetBlockHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_transaction",
		Title: "Get Transaction",
		Description: "Returns transaction details by hash, " +
			"including block inclusion info if mined.",
	}, makeGetTransactionHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_transaction_receipt",
		Title: "Get Transaction Receipt",
		Description: "Returns the receipt for a mined transaction, " +
			"including status, gas used, logs, and created contract address. " +
			"Always call this after submitting a transaction to confirm the outcome. " +
			"status='success' means the transaction executed correctly on-chain. " +
			"status='reverted' means the transaction was included but the contract " +
			"rejected it (e.g. permission denied, bad input) -- the write did NOT occur " +
			"and gas was still consumed. " +
			"If the receipt is not yet available, the transaction is still pending -- " +
			"wait a few seconds and retry.",
	}, makeGetReceiptHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_balance",
		Title: "Get Balance",
		Description: "Returns the balance of an address in both wei and ether. " +
			"Optionally specify a block number.",
	}, makeGetBalanceHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_code",
		Title: "Get Code",
		Description: "Returns the contract bytecode at an address, " +
			"and whether a contract is deployed there.",
	}, makeGetCodeHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_get_logs",
		Title: "Get Logs",
		Description: "Returns event logs emitted by smart contracts matching a filter. " +
			"Specify address to filter by contract, from_block/to_block for a block range, " +
			"and topics as keccak256 hashes of event signatures " +
			"(e.g. keccak256('Transfer(address,address,uint256)') = 0xddf252...). " +
			"All filters are optional -- omitting all returns all logs in the block range. " +
			"Useful for watching for on-chain events or auditing contract activity.",
	}, makeGetLogsHandler(evmClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_call_contract",
		Title: "Call Contract",
		Description: "Execute a read-only (eth_call) call to any smart contract. " +
			"Provide the contract address and ABI-encoded calldata as a 0x-prefixed hex string. " +
			"Returns the raw hex output -- you must ABI-decode it to get structured data. " +
			"For anchor precompile reads, prefer the anchor_get_* tools which handle ABI " +
			"encoding and decoding automatically. " +
			"Use this tool for arbitrary contract reads not covered by specific tools.",
	}, makeCallContractHandler(evmClient))
}

// --- Input types ---

type chainIDInput struct{}

type getBlockInput struct {
	BlockNumber *int64  `json:"block_number,omitempty" jsonschema:"Block number (omit for latest)"`
	BlockHash   *string `json:"block_hash,omitempty" jsonschema:"Block hash (0x-prefixed, 32 bytes)"`
	FullTx      bool    `json:"full_transactions,omitempty" jsonschema:"Include full transaction details"`
}

type txHashInput struct {
	TxHash string `json:"tx_hash" jsonschema:"Transaction hash (0x-prefixed, 32 bytes),required"`
}

type getBalanceInput struct {
	Address  string `json:"address" jsonschema:"Ethereum address (0x-prefixed, 20 bytes),required"`
	BlockNum *int64 `json:"block_number,omitempty" jsonschema:"Block number (omit for latest)"`
}

type getCodeInput struct {
	Address  string `json:"address" jsonschema:"Ethereum address (0x-prefixed, 20 bytes),required"`
	BlockNum *int64 `json:"block_number,omitempty" jsonschema:"Block number (omit for latest)"`
}

type getLogsInput struct {
	Address   *string  `json:"address,omitempty" jsonschema:"Contract address to filter"`
	FromBlock *int64   `json:"from_block,omitempty" jsonschema:"Start block number"`
	ToBlock   *int64   `json:"to_block,omitempty" jsonschema:"End block number"`
	Topics    []string `json:"topics,omitempty" jsonschema:"Event topics (0x-prefixed hashes)"`
}

type callContractInput struct {
	To       string `json:"to" jsonschema:"Contract address (0x-prefixed),required"`
	Data     string `json:"data" jsonschema:"Hex-encoded calldata (0x-prefixed),required"`
	BlockNum *int64 `json:"block_number,omitempty" jsonschema:"Block number (omit for latest)"`
}

// --- Handlers ---

func makeChainIDHandler(
	c evm.Client,
) mcp.ToolHandlerFor[chainIDInput, evm.ChainInfo] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, _ chainIDInput,
	) (*mcp.CallToolResult, evm.ChainInfo, error) {
		info, err := c.GetChainInfo(ctx)
		if err != nil {
			return nil, evm.ChainInfo{},
				fmt.Errorf("failed to get chain info: %w", err)
		}
		return nil, *info, nil
	}
}

func makeGetBlockHandler(
	c evm.Client,
) mcp.ToolHandlerFor[getBlockInput, evm.NormalizedBlock] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getBlockInput,
	) (*mcp.CallToolResult, evm.NormalizedBlock, error) {
		if input.BlockHash != nil {
			hash, err := parseHash(*input.BlockHash)
			if err != nil {
				return nil, evm.NormalizedBlock{},
					fmt.Errorf("invalid block_hash: %w", err)
			}
			block, err := c.BlockByHash(ctx, hash, input.FullTx)
			if err != nil {
				return nil, evm.NormalizedBlock{},
					fmt.Errorf("block not found: %w", err)
			}
			return nil, *block, nil
		}

		var num *big.Int
		if input.BlockNumber != nil {
			num = big.NewInt(*input.BlockNumber)
		}
		block, err := c.BlockByNumber(ctx, num, input.FullTx)
		if err != nil {
			return nil, evm.NormalizedBlock{},
				fmt.Errorf("block not found: %w", err)
		}
		return nil, *block, nil
	}
}

func makeGetTransactionHandler(
	c evm.Client,
) mcp.ToolHandlerFor[txHashInput, evm.NormalizedTransaction] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input txHashInput,
	) (*mcp.CallToolResult, evm.NormalizedTransaction, error) {
		hash, err := parseHash(input.TxHash)
		if err != nil {
			return nil, evm.NormalizedTransaction{},
				fmt.Errorf("invalid tx_hash: %w", err)
		}
		tx, err := c.TransactionByHash(ctx, hash)
		if err != nil {
			return nil, evm.NormalizedTransaction{},
				fmt.Errorf("transaction not found: %w", err)
		}
		return nil, *tx, nil
	}
}

func makeGetReceiptHandler(
	c evm.Client,
) mcp.ToolHandlerFor[txHashInput, evm.NormalizedReceipt] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input txHashInput,
	) (*mcp.CallToolResult, evm.NormalizedReceipt, error) {
		hash, err := parseHash(input.TxHash)
		if err != nil {
			return nil, evm.NormalizedReceipt{},
				fmt.Errorf("invalid tx_hash: %w", err)
		}
		receipt, err := c.TransactionReceipt(ctx, hash)
		if err != nil {
			return nil, evm.NormalizedReceipt{},
				fmt.Errorf("receipt not found: %w", err)
		}
		return nil, *receipt, nil
	}
}

func makeGetBalanceHandler(
	c evm.Client,
) mcp.ToolHandlerFor[getBalanceInput, evm.NormalizedBalance] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getBalanceInput,
	) (*mcp.CallToolResult, evm.NormalizedBalance, error) {
		addr, err := parseAddress(input.Address)
		if err != nil {
			return nil, evm.NormalizedBalance{}, err
		}
		var blockNum *big.Int
		if input.BlockNum != nil {
			blockNum = big.NewInt(*input.BlockNum)
		}
		balance, err := c.BalanceAt(ctx, addr, blockNum)
		if err != nil {
			return nil, evm.NormalizedBalance{},
				fmt.Errorf("failed to get balance: %w", err)
		}
		return nil, *balance, nil
	}
}

func makeGetCodeHandler(
	c evm.Client,
) mcp.ToolHandlerFor[getCodeInput, evm.CodeResult] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getCodeInput,
	) (*mcp.CallToolResult, evm.CodeResult, error) {
		addr, err := parseAddress(input.Address)
		if err != nil {
			return nil, evm.CodeResult{}, err
		}
		var blockNum *big.Int
		if input.BlockNum != nil {
			blockNum = big.NewInt(*input.BlockNum)
		}
		code, err := c.CodeAt(ctx, addr, blockNum)
		if err != nil {
			return nil, evm.CodeResult{},
				fmt.Errorf("failed to get code: %w", err)
		}
		return nil, *code, nil
	}
}

func makeGetLogsHandler(
	c evm.Client,
) mcp.ToolHandlerFor[getLogsInput, getLogsOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getLogsInput,
	) (*mcp.CallToolResult, getLogsOutput, error) {
		q := ethereum.FilterQuery{}
		if input.Address != nil {
			addr, err := parseAddress(*input.Address)
			if err != nil {
				return nil, getLogsOutput{}, err
			}
			q.Addresses = []common.Address{addr}
		}
		if input.FromBlock != nil {
			q.FromBlock = big.NewInt(*input.FromBlock)
		}
		if input.ToBlock != nil {
			q.ToBlock = big.NewInt(*input.ToBlock)
		}
		if len(input.Topics) > 0 {
			topicSet := make([]common.Hash, len(input.Topics))
			for i, t := range input.Topics {
				hash, err := parseHash(t)
				if err != nil {
					return nil, getLogsOutput{},
						fmt.Errorf("invalid topic at index %d: %w", i, err)
				}
				topicSet[i] = hash
			}
			q.Topics = [][]common.Hash{topicSet}
		}

		logs, err := c.FilterLogs(ctx, q)
		if err != nil {
			return nil, getLogsOutput{}, fmt.Errorf("failed to filter logs: %w", err)
		}
		return nil, getLogsOutput{Logs: logs, Count: len(logs)}, nil
	}
}

type getLogsOutput struct {
	Logs  []evm.NormalizedLog `json:"logs"`
	Count int                 `json:"count"`
}

type callContractOutput struct {
	Result string `json:"result" jsonschema:"Hex-encoded return data"`
}

func makeCallContractHandler(
	c evm.Client,
) mcp.ToolHandlerFor[callContractInput, callContractOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input callContractInput,
	) (*mcp.CallToolResult, callContractOutput, error) {
		toAddr, err := parseAddress(input.To)
		if err != nil {
			return nil, callContractOutput{}, err
		}
		data, err := parseHexData(input.Data)
		if err != nil {
			return nil, callContractOutput{},
				fmt.Errorf("invalid calldata: %w", err)
		}

		msg := ethereum.CallMsg{
			To:   &toAddr,
			Data: data,
		}
		var blockNum *big.Int
		if input.BlockNum != nil {
			blockNum = big.NewInt(*input.BlockNum)
		}
		result, err := c.CallContract(ctx, msg, blockNum)
		if err != nil {
			return nil, callContractOutput{},
				fmt.Errorf("contract call failed: %w", err)
		}
		return nil, callContractOutput{
			Result: "0x" + hex.EncodeToString(result),
		}, nil
	}
}

// --- Validation helpers ---

func parseAddress(s string) (common.Address, error) {
	if !common.IsHexAddress(s) {
		return common.Address{},
			fmt.Errorf("%q: %w", s, apperrors.ErrInvalidAddress)
	}
	return common.HexToAddress(s), nil
}

func parseHash(s string) (common.Hash, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) != 64 {
		return common.Hash{},
			fmt.Errorf("%q: %w", s, apperrors.ErrInvalidTxHash)
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return common.Hash{}, fmt.Errorf("invalid hash hex: %w", err)
	}
	return common.BytesToHash(b), nil
}

// maxHexDataLen caps hex input strings at 2 MB (1 MB decoded).
const maxHexDataLen = 2 * 1024 * 1024

func parseHexData(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) > maxHexDataLen {
		return nil, fmt.Errorf("hex data too large (%d chars, max %d): %w",
			len(s), maxHexDataLen, apperrors.ErrInvalidABI)
	}
	return hex.DecodeString(s)
}
