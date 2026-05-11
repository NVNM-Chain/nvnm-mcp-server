package anchor

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/inveniam/nvnm-mcp-server/internal/logging"
)

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
				Registry: "test-reg",
				Checksum: "0xabc",
			},
			wantErr: "from address",
		},
		{
			name: "missing registry",
			req: PrepareAddRecordRequest{
				From:     "0x1234567890abcdef1234567890abcdef12345678",
				Checksum: "0xabc",
			},
			wantErr: "registry is required",
		},
		{
			name: "missing checksum",
			req: PrepareAddRecordRequest{
				From:     "0x1234567890abcdef1234567890abcdef12345678",
				Registry: "test-reg",
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
		pendingNonceFn: func(_ context.Context, _ common.Address) (uint64, error) {
			return 42, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(5000000000), nil
		},
		estimateGasFn: func(_ context.Context, _ ethereum.CallMsg) (uint64, error) {
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
		Registry:     "test-registry",
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
		From:     "0x1234567890abcdef1234567890abcdef12345678",
		Registry: "r",
		Checksum: "c",
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
		pendingNonceFn: func(_ context.Context, _ common.Address) (uint64, error) {
			return 7, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(1_000_000_000), nil
		},
		estimateGasFn: func(_ context.Context, _ ethereum.CallMsg) (uint64, error) {
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
		pendingNonceFn: func(_ context.Context, _ common.Address) (uint64, error) {
			return 13, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(20_000_000_000), nil // 20 gwei
		},
		suggestGasTipFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(2_000_000_000), nil // 2 gwei tip
		},
		estimateGasFn: func(_ context.Context, _ ethereum.CallMsg) (uint64, error) {
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
		pendingNonceFn: func(_ context.Context, _ common.Address) (uint64, error) {
			return 0, nil
		},
		suggestGasFn: func(_ context.Context) (*big.Int, error) {
			return big.NewInt(5_000_000_000), nil
		},
		suggestGasTipFn: func(_ context.Context) (*big.Int, error) {
			return nil, errors.New("eth_maxPriorityFeePerGas not supported")
		},
		estimateGasFn: func(_ context.Context, _ ethereum.CallMsg) (uint64, error) {
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
