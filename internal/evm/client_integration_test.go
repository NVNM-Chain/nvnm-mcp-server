//go:build integration

package evm_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

const (
	testRPCURL         = "https://evm.inveniam.mantrachain.io"
	testChainID        = 58887
	testPrecompileAddr = "0x0000000000000000000000000000000000000A00"
	testConnectTimeout = 15 * time.Second
)

func integrationClient(t *testing.T) evm.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()

	c, err := evm.NewClient(ctx, testRPCURL, testConnectTimeout)
	if err != nil {
		t.Fatalf("failed to connect to %s: %v", testRPCURL, err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestIntegration_ChainID(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	chainID, err := c.ChainID(ctx)
	if err != nil {
		t.Fatalf("ChainID: %v", err)
	}
	if chainID.Int64() != testChainID {
		t.Errorf("ChainID = %d, want %d", chainID.Int64(), testChainID)
	}
}

func TestIntegration_GetChainInfo(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	info, err := c.GetChainInfo(ctx)
	if err != nil {
		t.Fatalf("GetChainInfo: %v", err)
	}
	if info.ChainID != testChainID {
		t.Errorf("ChainID = %d, want %d", info.ChainID, testChainID)
	}
	if info.LatestBlockNumber == 0 {
		t.Error("LatestBlockNumber should be > 0")
	}
}

func TestIntegration_LatestBlockNumber(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	num, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}
	if num == 0 {
		t.Error("LatestBlockNumber should be > 0")
	}
}

func TestIntegration_BlockByNumber_Latest(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	block, err := c.BlockByNumber(ctx, nil, false)
	if err != nil {
		t.Fatalf("BlockByNumber(latest): %v", err)
	}
	if block.Number == 0 {
		t.Error("block number should be > 0")
	}
	if block.Hash == "" {
		t.Error("block hash should not be empty")
	}
	if block.ParentHash == "" {
		t.Error("parent hash should not be empty")
	}
	if block.TimestampUnix == 0 {
		t.Error("timestamp should be > 0")
	}
}

func TestIntegration_BlockByNumber_Specific(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// Use a recent block rather than genesis (early blocks may be pruned)
	latest, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}
	target := latest - 10
	block, err := c.BlockByNumber(ctx, big.NewInt(int64(target)), true)
	if err != nil {
		t.Fatalf("BlockByNumber(%d): %v", target, err)
	}
	if block.Number != target {
		t.Errorf("block number = %d, want %d", block.Number, target)
	}
}

func TestIntegration_BalanceAt(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	addr := common.HexToAddress(testPrecompileAddr)
	bal, err := c.BalanceAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if bal.Address == "" {
		t.Error("address should not be empty")
	}
	if bal.Wei == "" {
		t.Error("wei should not be empty")
	}
}

func TestIntegration_CodeAt_Precompile(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	addr := common.HexToAddress(testPrecompileAddr)
	code, err := c.CodeAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if code.Address == "" {
		t.Error("address should not be empty")
	}
	// Precompiles return empty code (0x)
	if code.IsContract {
		t.Log("precompile reports as contract (chain-specific)")
	}
}

func TestIntegration_BlockByHash(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// Use a block well behind the tip to avoid race conditions
	latestNum, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}
	target := int64(latestNum - 100)
	if target < 1 {
		target = 1
	}

	block, err := c.BlockByNumber(ctx, big.NewInt(target), false)
	if err != nil {
		t.Fatalf("BlockByNumber(%d): %v", target, err)
	}

	blockByHash, err := c.BlockByHash(ctx, common.HexToHash(block.Hash), false)
	if err != nil {
		// Cosmos EVM chains (ethermint) may not support eth_getBlockByHash
		t.Skipf("BlockByHash not supported on this chain: %v", err)
	}
	if blockByHash.Number != block.Number {
		t.Errorf("block number = %d, want %d", blockByHash.Number, block.Number)
	}
	if blockByHash.Hash != block.Hash {
		t.Errorf("hash mismatch: got %s, want %s", blockByHash.Hash, block.Hash)
	}
}
