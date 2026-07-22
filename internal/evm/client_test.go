// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// stubRPCClient is a configurable test double for defiRPCClient. Only the
// fields a given test needs are set; unset methods return zero values. It
// exists to feed hostile/malformed node responses into the client's decode
// paths without a live node.
type stubRPCClient struct {
	block   *defitypes.Block
	balance *big.Int
}

func (s *stubRPCClient) ChainID(context.Context) (uint64, error) { return 0, nil }
func (s *stubRPCClient) BlockNumber(context.Context) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (s *stubRPCClient) BlockByNumber(context.Context, defitypes.BlockNumber, bool) (*defitypes.Block, error) {
	return s.block, nil
}
func (s *stubRPCClient) BlockByHash(context.Context, defitypes.Hash, bool) (*defitypes.Block, error) {
	return s.block, nil
}
func (s *stubRPCClient) GetTransactionByHash(context.Context, defitypes.Hash) (*defitypes.OnChainTransaction, error) {
	return nil, nil
}
func (s *stubRPCClient) GetTransactionReceipt(context.Context, defitypes.Hash) (*defitypes.TransactionReceipt, error) {
	return nil, nil
}
func (s *stubRPCClient) GetBalance(context.Context, defitypes.Address, defitypes.BlockNumber) (*big.Int, error) {
	return s.balance, nil
}
func (s *stubRPCClient) GetCode(context.Context, defitypes.Address, defitypes.BlockNumber) ([]byte, error) {
	return nil, nil
}
func (s *stubRPCClient) GetTransactionCount(context.Context, defitypes.Address, defitypes.BlockNumber) (uint64, error) {
	return 0, nil
}
func (s *stubRPCClient) GetLogs(context.Context, *defitypes.FilterLogsQuery) ([]defitypes.Log, error) {
	return nil, nil
}
func (s *stubRPCClient) GasPrice(context.Context) (*big.Int, error) { return big.NewInt(0), nil }
func (s *stubRPCClient) MaxPriorityFeePerGas(context.Context) (*big.Int, error) {
	return big.NewInt(0), nil
}
func (s *stubRPCClient) Call(context.Context, *defitypes.Call, defitypes.BlockNumber) ([]byte, *defitypes.Call, error) {
	return nil, nil, nil
}
func (s *stubRPCClient) EstimateGas(context.Context, *defitypes.Call, defitypes.BlockNumber) (uint64, *defitypes.Call, error) {
	return 0, nil, nil
}
func (s *stubRPCClient) SendRawTransaction(context.Context, []byte) (*defitypes.Hash, error) {
	return nil, nil
}

// TestClient_BlockByNumber_HostileNilNumberReturnsError verifies that a
// malformed node response (a block with a null number, which defiweb decodes
// as a nil *big.Int) is converted to an error rather than crashing the process
// with a nil-pointer panic. On the stdio transport an unrecovered panic here is
// a denial of service triggerable by a hostile or MITM'd RPC node (EV-2).
func TestClient_BlockByNumber_HostileNilNumberReturnsError(t *testing.T) {
	c := &client{
		rpc:     &stubRPCClient{block: &defitypes.Block{ /* Number: nil */ }},
		timeout: time.Second,
	}

	_, err := c.BlockByNumber(context.Background(), nil, false)

	if err == nil {
		t.Fatal("expected an error for a hostile nil-number block, got nil")
	}
	if !errors.Is(err, apperrors.ErrNodeResponseDecode) {
		t.Fatalf("expected ErrNodeResponseDecode, got %v", err)
	}
}
