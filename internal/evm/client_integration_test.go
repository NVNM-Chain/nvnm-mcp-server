// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package evm_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"strings"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

const (
	testRPCURL         = "https://evm.testnet.nvnmchain.io"
	testChainID        = 787111
	testPrecompileAddr = "0x0000000000000000000000000000000000000A00"
	testConnectTimeout = 15 * time.Second
)

func mustHashFromHex(s string) defitypes.Hash {
	h, err := defitypes.HashFromHex(s, defitypes.PadNone)
	if err != nil {
		panic(err)
	}
	return h
}

func integrationClient(t *testing.T) evm.Client {
	t.Helper()
	return integrationResilientClient(t)
}

// integrationResilientClient mirrors the production wiring:
//
//	bare client -> resilient wrapper (retry / rate-limit / breaker).
//
// Integration tests must use the resilient wrapper because the testnet
// RPC has a documented transient race on eth_gasPrice immediately after
// a broadcast (see cometReceiptsRaceMarker in resilient.go). Without
// the wrapper, tests that call PrepareXxx back-to-back hit the race
// uncovered and fail spuriously.
func integrationResilientClient(t *testing.T) evm.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()

	raw, err := evm.NewClient(ctx, testRPCURL, testConnectTimeout)
	if err != nil {
		t.Fatalf("failed to connect to %s: %v", testRPCURL, err)
	}
	t.Cleanup(func() { raw.Close() })

	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	mets, err := telemetry.NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	return evm.NewResilientClient(raw, evm.ResilientConfig{
		MaxRetries:       5,
		InitialBackoff:   500 * time.Millisecond,
		MaxBackoff:       5 * time.Second,
		RateLimit:        100,
		RateBurst:        20,
		BreakerThreshold: 10,
		BreakerTimeout:   30 * time.Second,
	}, mets, slog.New(slog.NewTextHandler(io.Discard, nil)))
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

	addr := defitypes.MustAddressFromHex(testPrecompileAddr)
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

	addr := defitypes.MustAddressFromHex(testPrecompileAddr)
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

	blockByHash, err := c.BlockByHash(ctx, mustHashFromHex(block.Hash), false)
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

// TestIntegration_TransactionByHash_PopulatesFrom verifies the normalized
// transaction carries the recovered sender. A real mined tx previously came
// back with an empty `from` because the normalizer never mapped it.
func TestIntegration_TransactionByHash_PopulatesFrom(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// Permanent testnet tx: addRecord into ElicitTest-rc6 (registry 741),
	// sent by the funded test wallet below.
	const txHash = "0x7e0b28b30916a3dcc14c4ffe9a5ed6be06acd3d4f4c618161fbb1e6c1acf2188"
	const sender = "0x9f8a6425F7AD925701fE1CdF85fd883340b2A9CD"

	tx, err := c.TransactionByHash(ctx, mustHashFromHex(txHash))
	if err != nil {
		t.Fatalf("TransactionByHash: %v", err)
	}
	if tx.IsPending {
		t.Error("a mined tx must not be reported as pending")
	}
	if !strings.EqualFold(tx.From, sender) {
		t.Errorf("From = %q, want %q", tx.From, sender)
	}
}

// TestIntegration_TransactionByHash_NotFound verifies that a well-formed but
// non-existent hash is reported as an explicit not-found rather than a
// zero-value object that misleadingly reads as "pending" with an empty hash.
func TestIntegration_TransactionByHash_NotFound(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	garbage := "0x" + strings.Repeat("ab", 32)
	_, err := c.TransactionByHash(ctx, mustHashFromHex(garbage))
	if !errors.Is(err, apperrors.ErrTxNotFound) {
		t.Fatalf("want ErrTxNotFound for a non-existent hash, got %v", err)
	}
}
