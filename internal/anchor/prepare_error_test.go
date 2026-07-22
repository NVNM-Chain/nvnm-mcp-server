// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

func TestPrepareGrantRole_InvalidAccountAddress(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.PrepareGrantRole(context.Background(), PrepareGrantRoleRequest{
		From:       "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID: 1,
		Account:    "not-an-address",
		Role:       "admin",
	})
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Fatalf("want ErrInvalidAddress for malformed account, got %v", err)
	}
}

func TestBuildUnsignedTx_InvalidFromAddress(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "not-an-address",
		Name: "test",
	})
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Fatalf("want ErrInvalidAddress for malformed from, got %v", err)
	}
}

func TestBuildUnsignedTx_PendingNonceError(t *testing.T) {
	mock := &mockEVMClient{
		pendingNonceFn: func(_ context.Context, _ defitypes.Address) (uint64, error) {
			return 0, errors.New("nonce backend down")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "get pending nonce") {
		t.Fatalf("want pending-nonce error, got %v", err)
	}
}

func TestBuildUnsignedTx_SuggestGasPriceError(t *testing.T) {
	mock := &mockEVMClient{
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return nil, errors.New("gas oracle down")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "get gas price") {
		t.Fatalf("want gas-price error, got %v", err)
	}
}

// TestBuildUnsignedTx_EstimateGasGenericError: an EstimateGas failure that
// is NOT an allowlisted precompile validation reason falls through to the
// generic "estimate gas" wrap (no ErrPrecompileValidation).
func TestBuildUnsignedTx_EstimateGasGenericError(t *testing.T) {
	mock := &mockEVMClient{
		estimateGasFn: func(_ context.Context, _ defitypes.Call) (uint64, error) {
			return 0, errors.New("dial tcp 10.0.0.1:8545: connect: connection refused")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "estimate gas") {
		t.Fatalf("want generic estimate-gas error, got %v", err)
	}
	if errors.Is(err, apperrors.ErrPrecompileValidation) {
		t.Errorf("generic RPC failure must not be classified as precompile validation: %v", err)
	}
}

// TestBuildDynamicFeeUnsignedTx_NegativeChainID: the default type-2 path
// rejects a negative chain ID before serialization.
func TestBuildDynamicFeeUnsignedTx_NegativeChainID(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, -1, testABIPath(t), logging.New("error"))

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if !errors.Is(err, apperrors.ErrInvalidChainID) {
		t.Fatalf("want ErrInvalidChainID for chainID -1, got %v", err)
	}
}

// TestBuildDynamicFeeUnsignedTx_MaxFeeClampedToTipCap covers the
// pathological case where 2x SuggestGasPrice is below the suggested tip:
// maxFee is raised to tipCap so the transaction stays well-formed.
func TestBuildDynamicFeeUnsignedTx_MaxFeeClampedToTipCap(t *testing.T) {
	mock := &mockEVMClient{
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(1), nil // 2x = 2 wei, far below the tip
		},
		suggestGasTipFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(10_000_000_000), nil // 10 gwei
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	tx, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}
	if tx.MaxFeePerGas != "10000000000" {
		t.Errorf("MaxFeePerGas = %q, want clamped to tip 10000000000", tx.MaxFeePerGas)
	}
	if tx.MaxPriorityFeePerGas != "10000000000" {
		t.Errorf("MaxPriorityFeePerGas = %q, want 10000000000", tx.MaxPriorityFeePerGas)
	}
}

// TestBuildLegacyUnsignedTx_BadCalldataHex calls the internal builder
// directly: the dataHex parameter is produced from bytes in production, so
// the decode-failure guard is only reachable in-package.
func TestBuildLegacyUnsignedTx_BadCalldataHex(t *testing.T) {
	raw := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
	c, ok := raw.(*client)
	if !ok {
		t.Fatalf("NewClient returned %T, want *client", raw)
	}

	_, err := c.buildLegacyUnsignedTx(0, 21000, big.NewInt(1), "0xzz-not-hex", PrecompileAddress, PrecompileAddress)
	if err == nil || !strings.Contains(err.Error(), "decode calldata hex") {
		t.Fatalf("want calldata decode error, got %v", err)
	}
}

// TestPrepare_PackErrors exercises the EncodeArgs failure branch of each
// Prepare* method via an ABI whose write methods take argument lists that
// do not match what the client encodes.
func TestPrepare_PackErrors(t *testing.T) {
	abiPath := writeTempABI(t, `[
	  {"type":"function","name":"addRegistry","stateMutability":"nonpayable",
	   "inputs":[{"name":"name","type":"string"}],"outputs":[]},
	  {"type":"function","name":"addRecord","stateMutability":"nonpayable",
	   "inputs":[{"name":"a","type":"uint64"},{"name":"b","type":"uint64"}],"outputs":[]},
	  {"type":"function","name":"grantRole","stateMutability":"nonpayable",
	   "inputs":[{"name":"registryId","type":"uint64"}],"outputs":[]}
	]`)
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, abiPath, logging.New("error"))

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "pack addRegistry") {
		t.Errorf("want pack addRegistry error, got %v", err)
	}

	_, err = c.PrepareAddRecord(context.Background(), PrepareAddRecordRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		Registry:     "reg",
		Checksum:     "abc123",
		ChecksumAlgo: "sha256",
		Metadata:     "smoke",
	})
	if err == nil || !strings.Contains(err.Error(), "pack addRecord") {
		t.Errorf("want pack addRecord error, got %v", err)
	}

	_, err = c.PrepareGrantRole(context.Background(), PrepareGrantRoleRequest{
		From:       "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID: 1,
		Account:    "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Role:       "admin",
	})
	if err == nil || !strings.Contains(err.Error(), "pack grantRole") {
		t.Errorf("want pack grantRole error, got %v", err)
	}
}
