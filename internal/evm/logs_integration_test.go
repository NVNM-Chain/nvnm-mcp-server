// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package evm_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// TestIntegration_FilterLogs_RangeRejectionsCurated pins the two distinct
// node rejections for an over-large eth_getLogs window against the live
// node, so a change in the node's error text fails here instead of
// degrading to "upstream operation failed" in production (rc21 ticket 20).
// Probed 2026-09-10: 1..head -> "maximum [from, to] blocks distance";
// 1..9999999 (past head) -> "invalid block range params".
func TestIntegration_FilterLogs_RangeRejectionsCurated(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	head, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}
	query := func(from, to uint64) error {
		fromBN := defitypes.BlockNumberFromUint64(from)
		toBN := defitypes.BlockNumberFromUint64(to)
		_, err := c.FilterLogs(ctx, defitypes.FilterLogsQuery{FromBlock: &fromBN, ToBlock: &toBN})
		return err
	}

	if err := query(1, head); !errors.Is(err, apperrors.ErrLogRangeTooWide) {
		t.Errorf("1..head: want ErrLogRangeTooWide (width cap), got %v", err)
	}
	if err := query(1, head+10_000_000); !errors.Is(err, apperrors.ErrLogRangeInvalid) {
		t.Errorf("1..past-head: want ErrLogRangeInvalid, got %v", err)
	}
}

func TestIntegration_FilterLogs(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	precompile := defitypes.MustAddressFromHex(testPrecompileAddr)

	latestBlock, err := c.LatestBlockNumber(ctx)
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}

	fromBlock := latestBlock - 1000
	if latestBlock < 1000 {
		fromBlock = 0
	}

	t.Logf("querying logs for precompile %s from block %d to %d", testPrecompileAddr, fromBlock, latestBlock)

	fromBN := defitypes.BlockNumberFromUint64(fromBlock)
	toBN := defitypes.BlockNumberFromUint64(latestBlock)
	logs, err := c.FilterLogs(ctx, defitypes.FilterLogsQuery{
		FromBlock: &fromBN,
		ToBlock:   &toBN,
		Address:   []defitypes.Address{precompile},
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

	fromBN := defitypes.BlockNumberFromBigInt(big.NewInt(from))
	toBN := defitypes.BlockNumberFromBigInt(big.NewInt(to))
	logs, err := c.FilterLogs(ctx, defitypes.FilterLogsQuery{
		FromBlock: &fromBN,
		ToBlock:   &toBN,
		Address:   []defitypes.Address{defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000001")},
	})
	if err != nil {
		t.Fatalf("FilterLogs: %v", err)
	}

	if len(logs) != 0 {
		t.Errorf("expected 0 logs from dead address range, got %d", len(logs))
	}
}
