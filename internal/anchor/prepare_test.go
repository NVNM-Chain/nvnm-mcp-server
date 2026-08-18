// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// TestPrepareAddRecord_EmptyJSONMetadata_CuratedMessageOnly verifies the "{}"
// rejection surfaces exactly the curated client-facing text. Wrapping it with
// ErrMissingRequired appended the classifier's own text, so clients saw the
// message end in ": missing required parameter" -- wrong category (the value
// is present but invalid) and machinery leaking into user-facing output.
func TestPrepareAddRecord_EmptyJSONMetadata_CuratedMessageOnly(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.PrepareAddRecord(context.Background(), PrepareAddRecordRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID:   1,
		Checksum:     "abc123",
		ChecksumAlgo: "sha256",
		Metadata:     "{}",
	})
	if err == nil {
		t.Fatal("expected error for empty JSON object metadata")
	}
	if !errors.Is(err, apperrors.ErrEmptyMetadataObject) {
		t.Errorf("error should be ErrEmptyMetadataObject; got %v", err)
	}
	if !apperrors.IsInputError(err) {
		t.Errorf("error must classify as input error so SafeForClient surfaces it; got %v", err)
	}
	if strings.Contains(err.Error(), apperrors.ErrMissingRequired.Error()) {
		t.Errorf("curated message must not carry the classifier suffix %q; got %q",
			apperrors.ErrMissingRequired.Error(), err.Error())
	}
}

func TestApplyGasBuffer(t *testing.T) {
	tests := []struct {
		name     string
		estimate uint64
		want     uint64
	}{
		{"100k estimate", 100000, 120000},
		{"50k estimate", 50000, 60000},
		{"zero", 0, 0},
		{"1 gas", 1, 1},
		{"5 gas rounds down", 5, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyGasBuffer(tt.estimate)
			if got != tt.want {
				t.Errorf("applyGasBuffer(%d) = %d, want %d", tt.estimate, got, tt.want)
			}
		})
	}
}

func TestPrepareAddRegistry_Validation(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tests := []struct {
		name    string
		req     PrepareAddRegistryRequest
		wantErr string
	}{
		{
			name:    "missing from",
			req:     PrepareAddRegistryRequest{Name: "test"},
			wantErr: "from address",
		},
		{
			name:    "missing name",
			req:     PrepareAddRegistryRequest{From: "0x1234567890abcdef1234567890abcdef12345678"},
			wantErr: "name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.PrepareAddRegistry(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPrepareAddRecord_Validation(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tests := []struct {
		name    string
		req     PrepareAddRecordRequest
		wantErr string
	}{
		{
			name: "missing from",
			req: PrepareAddRecordRequest{
				RegistryID: 1,
				Checksum:   "0xabc",
			},
			wantErr: "from address",
		},
		{
			name: "missing registry_id",
			req: PrepareAddRecordRequest{
				From:     "0x1234567890abcdef1234567890abcdef12345678",
				Checksum: "0xabc",
			},
			wantErr: "registry_id must be > 0",
		},
		{
			name: "missing checksum",
			req: PrepareAddRecordRequest{
				From:       "0x1234567890abcdef1234567890abcdef12345678",
				RegistryID: 1,
			},
			wantErr: "checksum is required",
		},
		{
			// The anchoring precompile rejects an empty checksum_algo
			// during gas estimation; fail loud client-side instead with
			// a precise message (E2E rc8 finding).
			name: "missing checksum_algo",
			req: PrepareAddRecordRequest{
				From:       "0x1234567890abcdef1234567890abcdef12345678",
				RegistryID: 1,
				Checksum:   "abc123",
				Metadata:   "{}",
			},
			wantErr: "checksum_algo is required",
		},
		{
			// The precompile likewise rejects empty metadata; surface it
			// before the opaque on-chain gas-estimation error.
			name: "missing metadata",
			req: PrepareAddRecordRequest{
				From:         "0x1234567890abcdef1234567890abcdef12345678",
				RegistryID:   1,
				Checksum:     "abc123",
				ChecksumAlgo: "sha256",
			},
			wantErr: "metadata is required",
		},
		{
			// F3a: the precompile rejects the literal empty JSON object
			// "{}" ("metadata cannot be empty"), yet older schema guidance
			// told callers to "pass {} if you have none". The server's own
			// check only tested == "", so "{}" passed here and died on-chain
			// behind an opaque "upstream operation failed". Reject it loudly
			// client-side with an actionable message instead.
			name: "metadata is empty JSON object",
			req: PrepareAddRecordRequest{
				From:         "0x1234567890abcdef1234567890abcdef12345678",
				RegistryID:   1,
				Checksum:     "abc123",
				ChecksumAlgo: "sha256",
				Metadata:     "{}",
			},
			wantErr: "{}",
		},
		{
			// Whitespace around the empty object is rejected the same way.
			name: "metadata is empty JSON object with whitespace",
			req: PrepareAddRecordRequest{
				From:         "0x1234567890abcdef1234567890abcdef12345678",
				RegistryID:   1,
				Checksum:     "abc123",
				ChecksumAlgo: "sha256",
				Metadata:     "  {}  ",
			},
			wantErr: "{}",
		},
		{
			// A bare "0x" normalizes to an empty checksum and must be
			// rejected as missing, not passed through.
			name: "checksum is only 0x prefix",
			req: PrepareAddRecordRequest{
				From:         "0x1234567890abcdef1234567890abcdef12345678",
				RegistryID:   1,
				Checksum:     "0x",
				ChecksumAlgo: "sha256",
				Metadata:     "{}",
			},
			wantErr: "checksum is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.PrepareAddRecord(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPrepareGrantRole_Validation(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tests := []struct {
		name    string
		req     PrepareGrantRoleRequest
		wantErr string
	}{
		{
			name: "missing from",
			req: PrepareGrantRoleRequest{
				Account: "0x1234567890abcdef1234567890abcdef12345678",
				Role:    "admin",
			},
			wantErr: "from address",
		},
		{
			name: "missing account",
			req: PrepareGrantRoleRequest{
				From: "0x1234567890abcdef1234567890abcdef12345678",
				Role: "admin",
			},
			wantErr: "account address",
		},
		{
			name: "missing role",
			req: PrepareGrantRoleRequest{
				From:    "0x1234567890abcdef1234567890abcdef12345678",
				Account: "0x1234567890abcdef1234567890abcdef12345678",
			},
			wantErr: "role is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.PrepareGrantRole(context.Background(), tt.req)
			if err == nil {
				t.Fatal("expected error")
			}
			if !containsSubstring(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestPrepareAddRegistry_BuildsUnsignedTx exercises the legacy (type-0)
// path. Phase 8.4 made EIP-1559 (type-2) the default; this test keeps
// the legacy path covered by setting PreferLegacy=true on the request.
func TestPrepareAddRegistry_BuildsUnsignedTx(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{
		pendingNonceFn: func(_ context.Context, _ defitypes.Address) (uint64, error) {
			return 42, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(5000000000), nil
		},
		estimateGasFn: func(_ context.Context, _ defitypes.Call) (uint64, error) {
			return 100000, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tx, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		Name:         "test-registry",
		Description:  "A test registry",
		Metadata:     "{\"env\":\"test\"}",
		PreferLegacy: true,
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}

	if tx.To != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("To = %q", tx.To)
	}
	if tx.Nonce != 42 {
		t.Errorf("Nonce = %d, want 42", tx.Nonce)
	}
	if tx.Gas != 120000 {
		t.Errorf("Gas = %d, want 120000 (100000 + 20%% buffer)", tx.Gas)
	}
	if tx.GasPrice != "5000000000" {
		t.Errorf("GasPrice = %q, want 5000000000", tx.GasPrice)
	}
	if tx.Value != "0" {
		t.Errorf("Value = %q, want 0", tx.Value)
	}
	if tx.ChainID != 58887 {
		t.Errorf("ChainID = %d, want 58887", tx.ChainID)
	}
	if tx.RawTx == "" {
		t.Error("RawTx should not be empty")
	}
	if tx.Data == "" || tx.Data == "0x" {
		t.Error("Data should contain ABI-encoded calldata")
	}
}

func TestPrepareAddRecord_BuildsUnsignedTx(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tx, err := c.PrepareAddRecord(context.Background(), PrepareAddRecordRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID:   1,
		URI:          "https://example.com/doc",
		Checksum:     "abc123",
		ChecksumAlgo: "sha256",
		Metadata:     "{\"file\":\"test.pdf\"}",
	})
	if err != nil {
		t.Fatalf("PrepareAddRecord: %v", err)
	}

	if tx.To != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("To = %q", tx.To)
	}
	if tx.RawTx == "" {
		t.Error("RawTx should not be empty")
	}
	if tx.Data == "" || tx.Data == "0x" {
		t.Error("Data should contain ABI-encoded calldata")
	}
}

func TestNormalizeChecksum(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bare hex unchanged", "abc123", "abc123"},
		{"lowercase 0x stripped", "0xabc123", "abc123"},
		{"uppercase 0X stripped", "0Xabc123", "abc123"},
		{"only prefix becomes empty", "0x", ""},
		{"empty stays empty", "", ""},
		{"0x in the middle untouched", "ab0xcd", "ab0xcd"},
		{"64-hex digest unchanged", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeChecksum(tt.in); got != tt.want {
				t.Errorf("normalizeChecksum(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestPrepareAddRecord_StripsChecksumPrefix proves the 0x-prefixed and
// bare-hex forms of the same digest produce byte-identical calldata --
// i.e. the prefix is stripped before ABI encoding, so a caller passing
// the natural 0x form gets the same on-chain record (E2E rc8 finding:
// the precompile caps checksum at 64 chars, so 0x+64-hex would fail).
func TestPrepareAddRecord_StripsChecksumPrefix(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, abiPath, logger)

	const digest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	base := PrepareAddRecordRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID:   1,
		URI:          "https://example.com/doc",
		ChecksumAlgo: "sha256",
		Metadata:     "{\"file\":\"test.pdf\"}",
	}

	bareReq := base
	bareReq.Checksum = digest
	bare, err := c.PrepareAddRecord(context.Background(), bareReq)
	if err != nil {
		t.Fatalf("PrepareAddRecord (bare): %v", err)
	}

	prefixedReq := base
	prefixedReq.Checksum = "0x" + digest
	prefixed, err := c.PrepareAddRecord(context.Background(), prefixedReq)
	if err != nil {
		t.Fatalf("PrepareAddRecord (0x-prefixed): %v", err)
	}

	if bare.Data != prefixed.Data {
		t.Errorf("calldata differs after prefix strip:\n bare     = %s\n prefixed = %s", bare.Data, prefixed.Data)
	}
}

func TestPrepareGrantRole_BuildsUnsignedTx(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tx, err := c.PrepareGrantRole(context.Background(), PrepareGrantRoleRequest{
		From:       "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID: 1,
		Account:    "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Role:       "editor",
	})
	if err != nil {
		t.Fatalf("PrepareGrantRole: %v", err)
	}

	if tx.To != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("To = %q", tx.To)
	}
	if tx.RawTx == "" {
		t.Error("RawTx should not be empty")
	}
}

func TestPrepareWithoutABI_ReturnsError(t *testing.T) {
	logger := logging.New("error")
	mock := &mockEVMClient{}
	c := NewClient(mock, PrecompileAddress, 58887, "", logger)

	_, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "test",
	})
	if err == nil {
		t.Fatal("expected error when ABI not loaded")
	}

	_, err = c.PrepareAddRecord(context.Background(), PrepareAddRecordRequest{
		From:       "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID: 1,
		Checksum:   "c",
	})
	if err == nil {
		t.Fatal("expected error when ABI not loaded")
	}

	_, err = c.PrepareGrantRole(context.Background(), PrepareGrantRoleRequest{
		From:    "0x1234567890abcdef1234567890abcdef12345678",
		Account: "0xabcdef",
		Role:    "admin",
	})
	if err == nil {
		t.Fatal("expected error when ABI not loaded")
	}
}

// TestPrepareAddRegistry_WalletTxRequest exercises the legacy (type-0)
// EIP-1193 path. The wallet_tx_request shape for type-2 transactions
// is covered separately in TestPrepareAddRegistry_BuildsEIP1559Tx_ByDefault.
func TestPrepareAddRegistry_WalletTxRequest(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	const chainID = int64(58887)
	mock := &mockEVMClient{
		pendingNonceFn: func(_ context.Context, _ defitypes.Address) (uint64, error) {
			return 7, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(1_000_000_000), nil
		},
		estimateGasFn: func(_ context.Context, _ defitypes.Call) (uint64, error) {
			return 50000, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, chainID, abiPath, logger)

	tx, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		Name:         "wallet-test",
		PreferLegacy: true,
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}

	w := tx.WalletTxRequest
	if w == nil {
		t.Fatal("WalletTxRequest must not be nil")
		return // unreachable; makes the nil guard explicit for staticcheck SA5011
	}

	// from is checksummed address
	if w.From == "" {
		t.Error("WalletTxRequest.From must not be empty")
	}

	// to is the precompile
	if w.To != PrecompileAddress {
		t.Errorf("WalletTxRequest.To = %q, want %q", w.To, PrecompileAddress)
	}

	// data matches the unsigned tx data field
	if w.Data != tx.Data {
		t.Errorf("WalletTxRequest.Data = %q, want same as tx.Data", w.Data)
	}

	// value is 0x0
	if w.Value != "0x0" {
		t.Errorf("WalletTxRequest.Value = %q, want 0x0", w.Value)
	}

	// chainId is 0xe607 (58887 in hex)
	if w.ChainID != "0xe607" {
		t.Errorf("WalletTxRequest.ChainID = %q, want 0xe607", w.ChainID)
	}

	// gas is 0x-prefixed hex: 60000 (50000 + 20%) = 0xea60
	if w.Gas != "0xea60" {
		t.Errorf("WalletTxRequest.Gas = %q, want 0xea60", w.Gas)
	}

	// gasPrice is 0x-prefixed hex: 1_000_000_000 = 0x3b9aca00
	if w.GasPrice != "0x3b9aca00" {
		t.Errorf("WalletTxRequest.GasPrice = %q, want 0x3b9aca00", w.GasPrice)
	}

	// data must be 0x-prefixed non-empty calldata
	if len(w.Data) < 3 || w.Data[:2] != "0x" {
		t.Errorf("WalletTxRequest.Data must be 0x-prefixed hex, got %q", w.Data)
	}
}

// TestPrepareAddRegistry_BuildsEIP1559Tx_ByDefault verifies the default
// (Phase 8.4+) path produces a type-2 DynamicFeeTx with the EIP-1559
// fields populated, GasPrice dual-populated for legacy signers, and a
// WalletTxRequest that carries the type-2 fee fields.
func TestPrepareAddRegistry_BuildsEIP1559Tx_ByDefault(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	const chainID = int64(58887)
	mock := &mockEVMClient{
		pendingNonceFn: func(_ context.Context, _ defitypes.Address) (uint64, error) {
			return 13, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(20_000_000_000), nil // 20 gwei
		},
		suggestGasTipFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(2_000_000_000), nil // 2 gwei tip
		},
		estimateGasFn: func(_ context.Context, _ defitypes.Call) (uint64, error) {
			return 80000, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, chainID, abiPath, logger)

	tx, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From:        "0x1234567890abcdef1234567890abcdef12345678",
		Name:        "eip1559-test",
		Description: "default type-2 path",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry: %v", err)
	}

	if tx.Type != 2 {
		t.Errorf("Type = %d, want 2", tx.Type)
	}
	// maxFeePerGas = SuggestGasPrice * 2 = 40 gwei
	if tx.MaxFeePerGas != "40000000000" {
		t.Errorf("MaxFeePerGas = %q, want 40000000000", tx.MaxFeePerGas)
	}
	// maxPriorityFeePerGas = SuggestGasTipCap = 2 gwei
	if tx.MaxPriorityFeePerGas != "2000000000" {
		t.Errorf("MaxPriorityFeePerGas = %q, want 2000000000", tx.MaxPriorityFeePerGas)
	}
	// Dual-populate: GasPrice == MaxFeePerGas so legacy signers have a usable value
	if tx.GasPrice != tx.MaxFeePerGas {
		t.Errorf("GasPrice = %q, MaxFeePerGas = %q -- dual-populate violated", tx.GasPrice, tx.MaxFeePerGas)
	}
	if tx.Nonce != 13 {
		t.Errorf("Nonce = %d, want 13", tx.Nonce)
	}
	if tx.Gas != 96000 { // 80000 + 20%
		t.Errorf("Gas = %d, want 96000 (80000 + 20%% buffer)", tx.Gas)
	}
	if tx.ChainID != chainID {
		t.Errorf("ChainID = %d, want %d", tx.ChainID, chainID)
	}

	// WalletTxRequest carries type-2 fields, NOT GasPrice
	w := tx.WalletTxRequest
	if w == nil {
		t.Fatal("WalletTxRequest must not be nil")
		return // unreachable; makes the nil guard explicit for staticcheck SA5011
	}
	if w.MaxFeePerGas != "0x"+big.NewInt(40_000_000_000).Text(16) {
		t.Errorf("WalletTxRequest.MaxFeePerGas = %q", w.MaxFeePerGas)
	}
	if w.MaxPriorityFeePerGas != "0x"+big.NewInt(2_000_000_000).Text(16) {
		t.Errorf("WalletTxRequest.MaxPriorityFeePerGas = %q", w.MaxPriorityFeePerGas)
	}
	if w.GasPrice != "" {
		t.Errorf("WalletTxRequest.GasPrice = %q, want empty (omitted for type-2)", w.GasPrice)
	}
}

// TestPrepareAddRegistry_FallsBackToDefaultTipCap_WhenSuggestGasTipCapErrors
// verifies that a chain that does not expose eth_maxPriorityFeePerGas
// (or transiently errors) still produces a valid type-2 transaction
// by falling back to defaultPriorityFeeWei.
func TestPrepareAddRegistry_FallsBackToDefaultTipCap_WhenSuggestGasTipCapErrors(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{
		pendingNonceFn: func(_ context.Context, _ defitypes.Address) (uint64, error) {
			return 0, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(5_000_000_000), nil
		},
		suggestGasTipFn: func(_ context.Context) (*big.Int, error) {
			return nil, errors.New("eth_maxPriorityFeePerGas not supported")
		},
		estimateGasFn: func(_ context.Context, _ defitypes.Call) (uint64, error) {
			return 50000, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	tx, err := c.PrepareAddRegistry(context.Background(), PrepareAddRegistryRequest{
		From: "0x1234567890abcdef1234567890abcdef12345678",
		Name: "fallback-test",
	})
	if err != nil {
		t.Fatalf("PrepareAddRegistry should succeed despite SuggestGasTipCap error, got: %v", err)
	}

	if tx.Type != 2 {
		t.Errorf("Type = %d, want 2 (default type-2 path)", tx.Type)
	}
	// Default fallback = 1 gwei
	if tx.MaxPriorityFeePerGas != "1000000000" {
		t.Errorf("MaxPriorityFeePerGas = %q, want 1000000000 (default fallback)", tx.MaxPriorityFeePerGas)
	}
}

func TestWalletTransactionRequest_JSON(t *testing.T) {
	req := WalletTransactionRequest{
		From:     "0x1234567890AbcdEF1234567890aBcdef12345678",
		To:       PrecompileAddress,
		Data:     "0xcafebabe",
		Value:    "0x0",
		ChainID:  "0xe607",
		Gas:      "0xea60",
		GasPrice: "0x3b9aca00",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded WalletTransactionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	checks := map[string][2]string{
		"From":     {decoded.From, req.From},
		"To":       {decoded.To, req.To},
		"Data":     {decoded.Data, req.Data},
		"Value":    {decoded.Value, req.Value},
		"ChainID":  {decoded.ChainID, req.ChainID},
		"Gas":      {decoded.Gas, req.Gas},
		"GasPrice": {decoded.GasPrice, req.GasPrice},
	}
	for field, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s = %q, want %q", field, pair[0], pair[1])
		}
	}
}

func TestUnsignedTransaction_WalletTxRequestOmittedWhenNil(t *testing.T) {
	tx := UnsignedTransaction{
		RawTx:   "0xdeadbeef",
		ChainID: 58887,
	}
	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsSubstring(string(data), "wallet_tx_request") {
		t.Error("wallet_tx_request should be omitted when nil")
	}
}

func TestUnsignedTransaction_WalletTxRequestIncludedWhenSet(t *testing.T) {
	tx := UnsignedTransaction{
		RawTx:   "0xdeadbeef",
		ChainID: 58887,
		WalletTxRequest: &WalletTransactionRequest{
			From:     "0xabc",
			To:       PrecompileAddress,
			Data:     "0x1234",
			Value:    "0x0",
			ChainID:  "0xe607",
			Gas:      "0x1",
			GasPrice: "0x1",
		},
	}
	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !containsSubstring(string(data), "wallet_tx_request") {
		t.Error("wallet_tx_request should be present when set")
	}
	if !containsSubstring(string(data), "0xe607") {
		t.Error("wallet_tx_request should contain chain ID hex")
	}
}

func TestUnsignedTransaction_JSON(t *testing.T) {
	tx := UnsignedTransaction{
		RawTx:    "0xdeadbeef",
		To:       "0x0000000000000000000000000000000000000A00",
		Data:     "0xcafebabe",
		Nonce:    42,
		Gas:      120000,
		GasPrice: "5000000000",
		Value:    "0",
		ChainID:  58887,
	}

	data, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded UnsignedTransaction
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.RawTx != tx.RawTx {
		t.Errorf("RawTx = %q", decoded.RawTx)
	}
	if decoded.To != tx.To {
		t.Errorf("To = %q", decoded.To)
	}
	if decoded.Data != tx.Data {
		t.Errorf("Data = %q", decoded.Data)
	}
	if decoded.Nonce != tx.Nonce {
		t.Errorf("Nonce = %d", decoded.Nonce)
	}
	if decoded.Gas != tx.Gas {
		t.Errorf("Gas = %d", decoded.Gas)
	}
	if decoded.GasPrice != tx.GasPrice {
		t.Errorf("GasPrice = %q", decoded.GasPrice)
	}
	if decoded.Value != tx.Value {
		t.Errorf("Value = %q", decoded.Value)
	}
	if decoded.ChainID != tx.ChainID {
		t.Errorf("ChainID = %d", decoded.ChainID)
	}
}

// --- PrepareUpdateRecordStatus ---
//
// `index` selects which version of a record to update. It is documented as
// optional, "default: latest", but the ABI declares a plain uint64 -- there is
// no "absent" to encode -- and version indexes are 1-based, so the 0 an
// omitted index would otherwise encode as names no version at all. The client
// resolves nil and 0 alike to a concrete index via the `records` view before
// packing the call. These tests pin that resolution by decoding the calldata
// the client actually built, not by trusting the request it was handed.

// updateStatusProbe wires a client whose `records` view returns fixed rows and
// whose gas estimate captures the updateRecordStatus calldata, so a test can
// assert both which lookup was made and which index was encoded.
type updateStatusProbe struct {
	client       Client
	recordsCalls int
	recordsInput []byte
	txCalldata   []byte
}

func newUpdateStatusProbe(t *testing.T, rows []abiRecordRow, recordsErr error) *updateStatusProbe {
	t.Helper()
	p := &updateStatusProbe{}
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			p.recordsCalls++
			p.recordsInput = msg.Input
			if recordsErr != nil {
				return nil, recordsErr
			}
			return encodeRecordsOutput(t, rows, abiPaginationOutput{Total: uint64(len(rows))}), nil
		},
		estimateGasFn: func(_ context.Context, msg defitypes.Call) (uint64, error) {
			p.txCalldata = msg.Input
			return 100000, nil
		},
	}
	p.client = NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
	return p
}

// encodedIndex decodes the index argument out of the updateRecordStatus
// calldata the client built.
func (p *updateStatusProbe) encodedIndex(t *testing.T) uint64 {
	t.Helper()
	var registryID, recordID, index uint64
	var status string
	m := parsedTestABI(t).Methods["updateRecordStatus"]
	if err := m.DecodeArgs(p.txCalldata, &registryID, &recordID, &index, &status); err != nil {
		t.Fatalf("decode updateRecordStatus calldata: %v", err)
	}
	return index
}

// recordsLookup decodes the (registryId, checksum, recordId, index) filter the
// client used for its latest-version lookup.
func (p *updateStatusProbe) recordsLookup(t *testing.T) (registryID uint64, checksum string, recordID, index uint64) {
	t.Helper()
	var page abiPaginationInput
	m := parsedTestABI(t).Methods["records"]
	if err := m.DecodeArgs(p.recordsInput, &registryID, &checksum, &recordID, &index, &page); err != nil {
		t.Fatalf("decode records calldata: %v", err)
	}
	return registryID, checksum, recordID, index
}

func versionRow(recordID, index uint64, isLatest bool) abiRecordRow {
	return abiRecordRow{
		URI:          "https://example.invalid/doc",
		Checksum:     "abc123",
		ChecksumAlgo: "sha256",
		Metadata:     "{\"v\":1}",
		Timestamp:    "2026-08-17T00:00:00Z",
		Status:       "Active",
		RecordID:     recordID,
		Index:        index,
		IsLatest:     isLatest,
		RegistryID:   7,
	}
}

func validUpdateRequest(index *uint64) PrepareUpdateRecordStatusRequest {
	return PrepareUpdateRecordStatusRequest{
		From:       "0x1234567890abcdef1234567890abcdef12345678",
		RegistryID: 7,
		RecordID:   42,
		Index:      index,
		Status:     "Revoked",
	}
}

func TestPrepareUpdateRecordStatus_ExplicitIndexEncodedVerbatim(t *testing.T) {
	p := newUpdateStatusProbe(t, nil, nil)

	index := uint64(3)
	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(&index)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 3 {
		t.Errorf("encoded index = %d, want 3", got)
	}
	// An explicit index needs no lookup: spending an eth_call on a question
	// the caller already answered would be waste on every write.
	if p.recordsCalls != 0 {
		t.Errorf("records was called %d times for an explicit index; want 0", p.recordsCalls)
	}
}

func TestPrepareUpdateRecordStatus_OmittedIndexResolvesLatest(t *testing.T) {
	rows := []abiRecordRow{
		versionRow(42, 1, false),
		versionRow(42, 2, false),
		versionRow(42, 3, true),
	}
	p := newUpdateStatusProbe(t, rows, nil)

	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 3 {
		t.Errorf("encoded index = %d, want 3 (the latest version)", got)
	}
	if p.recordsCalls != 1 {
		t.Errorf("records was called %d times; want exactly 1", p.recordsCalls)
	}
	registryID, checksum, recordID, index := p.recordsLookup(t)
	if registryID != 7 || checksum != "" || recordID != 42 || index != 0 {
		t.Errorf("lookup = (%d, %q, %d, %d), want (7, \"\", 42, 0)", registryID, checksum, recordID, index)
	}
}

// A zero index is how the read path spells "latest" (see
// anchor_get_records). The write path must not disagree with it: both forms
// have to produce the same transaction, byte for byte.
func TestPrepareUpdateRecordStatus_ZeroIndexMatchesOmitted(t *testing.T) {
	rows := []abiRecordRow{versionRow(42, 1, false), versionRow(42, 2, true)}

	omitted := newUpdateStatusProbe(t, rows, nil)
	if _, err := omitted.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("omitted index: %v", err)
	}

	zero := uint64(0)
	explicit := newUpdateStatusProbe(t, rows, nil)
	if _, err := explicit.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(&zero)); err != nil {
		t.Fatalf("index=0: %v", err)
	}

	if !bytes.Equal(omitted.txCalldata, explicit.txCalldata) {
		t.Errorf("index=0 built different calldata than omitting index:\n omitted:  %x\n index=0: %x",
			omitted.txCalldata, explicit.txCalldata)
	}
	if got := explicit.encodedIndex(t); got != 2 {
		t.Errorf("encoded index = %d, want 2 (the latest version)", got)
	}
}

// is_latest is the chain's own answer, so it wins over row order and over a
// higher index appearing earlier in the page.
func TestPrepareUpdateRecordStatus_PrefersIsLatestRow(t *testing.T) {
	rows := []abiRecordRow{versionRow(42, 5, false), versionRow(42, 2, true)}
	p := newUpdateStatusProbe(t, rows, nil)

	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 2 {
		t.Errorf("encoded index = %d, want 2 (the row flagged is_latest)", got)
	}
}

// If the chain ever stops flagging is_latest on a zero-index lookup, the
// highest version is still the latest one -- but rows for another record are
// never a candidate.
func TestPrepareUpdateRecordStatus_FallsBackToHighestIndex(t *testing.T) {
	rows := []abiRecordRow{
		versionRow(42, 1, false),
		versionRow(99, 7, false),
		versionRow(42, 4, false),
	}
	p := newUpdateStatusProbe(t, rows, nil)

	if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil)); err != nil {
		t.Fatalf("PrepareUpdateRecordStatus: %v", err)
	}

	if got := p.encodedIndex(t); got != 4 {
		t.Errorf("encoded index = %d, want 4 (highest index for record 42)", got)
	}
}

func TestPrepareUpdateRecordStatus_NoVersionsIsRecordNotFound(t *testing.T) {
	tests := []struct {
		name string
		rows []abiRecordRow
	}{
		{"empty result", nil},
		{"only other records", []abiRecordRow{versionRow(99, 1, true)}},
		{"zero index row", []abiRecordRow{versionRow(42, 0, true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newUpdateStatusProbe(t, tc.rows, nil)

			tx, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil))
			if err == nil {
				t.Fatalf("expected an error; got tx %+v", tx)
			}
			if !errors.Is(err, apperrors.ErrRecordNotFound) {
				t.Errorf("error should be ErrRecordNotFound; got %v", err)
			}
			if !apperrors.IsNotFound(err) {
				t.Errorf("error must classify as not-found so SafeForClient surfaces it; got %v", err)
			}
			if p.txCalldata != nil {
				t.Error("no transaction should be built when the record has no version to update")
			}
		})
	}
}

func TestPrepareUpdateRecordStatus_LookupFailurePropagates(t *testing.T) {
	p := newUpdateStatusProbe(t, nil, errors.New("rpc down"))

	tx, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil))
	if err == nil {
		t.Fatalf("expected an error; got tx %+v", tx)
	}
	if !strings.Contains(err.Error(), "resolve latest version index") {
		t.Errorf("error should name the step that failed; got %v", err)
	}
	if p.txCalldata != nil {
		t.Error("no transaction should be built when the latest-version lookup fails")
	}
}

// The precompile reverts on a key it does not hold rather than returning zero
// rows, so an unknown record_id arrives as a call failure, not an empty
// result. It must still reach the caller as "record not found" -- collapsed to
// a generic upstream failure, it tells them nothing they can act on.
func TestPrepareUpdateRecordStatus_UnknownRecordRevertIsNotFound(t *testing.T) {
	revert := errors.New(
		"RPC error: -32000 rpc error: code = Internal desc = collections: " +
			"not found: key '(\"7\", \"42\")' of type uint64",
	)
	p := newUpdateStatusProbe(t, nil, revert)

	tx, err := p.client.PrepareUpdateRecordStatus(context.Background(), validUpdateRequest(nil))
	if err == nil {
		t.Fatalf("expected an error; got tx %+v", tx)
	}
	if !errors.Is(err, apperrors.ErrRecordNotFound) {
		t.Errorf("error should be ErrRecordNotFound; got %v", err)
	}
	if !apperrors.IsNotFound(err) {
		t.Errorf("error must classify as not-found so SafeForClient surfaces it; got %v", err)
	}
	// The chain's raw revert text names internal storage keys and types;
	// SafeForClient passes not-found errors through verbatim, so the message
	// the client sees must not carry it.
	if strings.Contains(apperrors.SafeForClient(err).Error(), "collections") {
		t.Errorf("client-facing message leaks the raw revert: %v", apperrors.SafeForClient(err))
	}
}

func TestPrepareUpdateRecordStatus_Validation(t *testing.T) {
	index := uint64(1)
	tests := []struct {
		name    string
		mutate  func(*PrepareUpdateRecordStatusRequest)
		wantErr error
	}{
		{"no from", func(r *PrepareUpdateRecordStatusRequest) { r.From = "" }, apperrors.ErrMissingRequired},
		{"registry_id 0", func(r *PrepareUpdateRecordStatusRequest) { r.RegistryID = 0 }, apperrors.ErrInvalidRegistryID},
		{"record_id 0", func(r *PrepareUpdateRecordStatusRequest) { r.RecordID = 0 }, apperrors.ErrInvalidRecordID},
		{"no status", func(r *PrepareUpdateRecordStatusRequest) { r.Status = "" }, apperrors.ErrMissingRequired},
		{"bad from", func(r *PrepareUpdateRecordStatusRequest) { r.From = "not-an-address" }, apperrors.ErrInvalidAddress},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newUpdateStatusProbe(t, nil, nil)
			req := validUpdateRequest(&index)
			tc.mutate(&req)

			if _, err := p.client.PrepareUpdateRecordStatus(context.Background(), req); !errors.Is(err, tc.wantErr) {
				t.Errorf("error = %v, want %v", err, tc.wantErr)
			}
			if p.recordsCalls != 0 {
				t.Errorf("rejected input still cost %d chain reads; want 0", p.recordsCalls)
			}
		})
	}
}
