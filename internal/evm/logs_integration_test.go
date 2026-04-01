//go:build integration

package evm_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

func TestIntegration_FilterLogs(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	precompile := common.HexToAddress(testPrecompileAddr)

	latestBlock, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}

	fromBlock := latestBlock - 1000
	if latestBlock < 1000 {
		fromBlock = 0
	}

	t.Logf("querying logs for precompile %s from block %d to %d", testPrecompileAddr, fromBlock, latestBlock)

	logs, err := c.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(latestBlock)),
		Addresses: []common.Address{precompile},
	})
	if err != nil {
		t.Fatalf("FilterLogs: %v", err)
	}

	t.Logf("  found %d logs", len(logs))

	for i, log := range logs {
		if i >= 3 {
			t.Logf("  ... and %d more", len(logs)-3)
			break
		}
		t.Logf("  log[%d]: block=%d txHash=%s topics=%d address=%s",
			i, log.BlockNumber, log.TxHash, len(log.Topics), log.Address)

		if log.Address == "" {
			t.Errorf("log[%d].Address is empty", i)
		}
		if log.TxHash == "" {
			t.Errorf("log[%d].TxHash is empty", i)
		}
		if log.BlockNumber == 0 {
			t.Errorf("log[%d].BlockNumber is zero", i)
		}
	}
}

func TestIntegration_FilterLogs_EmptyRange(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	latestBlock, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}

	from := int64(latestBlock - 5)
	to := int64(latestBlock - 4)

	logs, err := c.FilterLogs(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(from),
		ToBlock:   big.NewInt(to),
		Addresses: []common.Address{common.HexToAddress("0x0000000000000000000000000000000000000001")},
	})
	if err != nil {
		t.Fatalf("FilterLogs: %v", err)
	}

	if len(logs) != 0 {
		t.Errorf("expected 0 logs from dead address range, got %d", len(logs))
	}
}
