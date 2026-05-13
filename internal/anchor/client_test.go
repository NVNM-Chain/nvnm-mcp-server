package anchor

import (
	"context"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	"github.com/inveniam/nvnm-mcp-server/internal/evm"
	"github.com/inveniam/nvnm-mcp-server/internal/logging"
)

// mockEVMClient implements evm.Client for testing without a live RPC.
type mockEVMClient struct {
	callContractFn  func(ctx context.Context, msg defitypes.Call, block *big.Int) ([]byte, error)
	pendingNonceFn  func(ctx context.Context, addr defitypes.Address) (uint64, error)
	suggestGasFn    func(ctx context.Context) (*big.Int, error)
	suggestGasTipFn func(ctx context.Context) (*big.Int, error)
	estimateGasFn   func(ctx context.Context, msg defitypes.Call) (uint64, error)
	sendRawTxFn     func(ctx context.Context, hex string) (string, error)
}

func (m *mockEVMClient) ChainID(context.Context) (*big.Int, error) {
	return big.NewInt(58887), nil
}

func (m *mockEVMClient) LatestBlockNumber(context.Context) (uint64, error) { return 0, nil }

func (m *mockEVMClient) GetChainInfo(context.Context) (*evm.ChainInfo, error) { return nil, nil }

func (m *mockEVMClient) BlockByNumber(context.Context, *big.Int, bool) (*evm.NormalizedBlock, error) {
	return nil, nil
}

func (m *mockEVMClient) BlockByHash(context.Context, defitypes.Hash, bool) (*evm.NormalizedBlock, error) {
	return nil, nil
}

func (m *mockEVMClient) TransactionByHash(context.Context, defitypes.Hash) (*evm.NormalizedTransaction, error) {
	return nil, nil
}

func (m *mockEVMClient) TransactionReceipt(context.Context, defitypes.Hash) (*evm.NormalizedReceipt, error) {
	return nil, nil
}

func (m *mockEVMClient) BalanceAt(context.Context, defitypes.Address, *big.Int) (*evm.NormalizedBalance, error) {
	return nil, nil
}

func (m *mockEVMClient) CodeAt(context.Context, defitypes.Address, *big.Int) (*evm.CodeResult, error) {
	return nil, nil
}

func (m *mockEVMClient) CallContract(
	ctx context.Context,
	msg defitypes.Call, //nolint:gocritic // matches evm.Client interface
	block *big.Int,
) ([]byte, error) {
	if m.callContractFn != nil {
		return m.callContractFn(ctx, msg, block)
	}
	return nil, nil
}

func (m *mockEVMClient) FilterLogs(context.Context, defitypes.FilterLogsQuery) ([]evm.NormalizedLog, error) {
	return nil, nil
}

func (m *mockEVMClient) PendingNonceAt(ctx context.Context, addr defitypes.Address) (uint64, error) {
	if m.pendingNonceFn != nil {
		return m.pendingNonceFn(ctx, addr)
	}
	return 0, nil
}

func (m *mockEVMClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	if m.suggestGasFn != nil {
		return m.suggestGasFn(ctx)
	}
	return big.NewInt(1000000000), nil
}

func (m *mockEVMClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	if m.suggestGasTipFn != nil {
		return m.suggestGasTipFn(ctx)
	}
	return big.NewInt(1000000000), nil
}

//nolint:gocritic // hugeParam: msg matches evm.Client interface
func (m *mockEVMClient) EstimateGas(ctx context.Context, msg defitypes.Call) (uint64, error) {
	if m.estimateGasFn != nil {
		return m.estimateGasFn(ctx, msg)
	}
	return 100000, nil
}

func (m *mockEVMClient) SendRawTransaction(ctx context.Context, hex string) (string, error) {
	if m.sendRawTxFn != nil {
		return m.sendRawTxFn(ctx, hex)
	}
	return "0x0000000000000000000000000000000000000000000000000000000000000000", nil
}

func (m *mockEVMClient) Ping(context.Context) error { return nil }

func (m *mockEVMClient) Close() {}

func testABIPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "abi", "anchoring.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("ABI file not found at %s: %v", path, err)
	}
	return path
}

func TestNewClient_WithoutABI(t *testing.T) {
	logger := logging.New("error")
	mock := &mockEVMClient{}

	c := NewClient(mock, PrecompileAddress, 58887, "", logger)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}

	if c.Available() {
		t.Error("client without ABI should not be available")
	}

	info := c.Info()
	if info.Address != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("Address = %q", info.Address)
	}
	if info.ChainID != 58887 {
		t.Errorf("ChainID = %d", info.ChainID)
	}
	if info.ABILoaded {
		t.Error("ABILoaded should be false")
	}
	if info.MethodCount != 0 {
		t.Errorf("MethodCount = %d, want 0", info.MethodCount)
	}
}

func TestNewClient_WithABI(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{}

	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}

	if !c.Available() {
		t.Error("client with ABI should be available")
	}

	info := c.Info()
	if !info.ABILoaded {
		t.Error("ABILoaded should be true")
	}
	if info.MethodCount != 5 {
		t.Errorf("MethodCount = %d, want 5", info.MethodCount)
	}
}

func TestNewClient_InvalidABIPath(t *testing.T) {
	logger := logging.New("error")
	mock := &mockEVMClient{}

	c := NewClient(mock, PrecompileAddress, 58887, "/nonexistent/path.json", logger)
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.Available() {
		t.Error("client with invalid ABI path should not be available")
	}
}

func TestNewClient_InvalidABIContent(t *testing.T) {
	dir := t.TempDir()
	badABI := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badABI, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	logger := logging.New("error")
	mock := &mockEVMClient{}

	c := NewClient(mock, PrecompileAddress, 58887, badABI, logger)
	if c.Available() {
		t.Error("client with invalid ABI content should not be available")
	}
}

func TestRequireABI_ReturnsError(t *testing.T) {
	logger := logging.New("error")
	mock := &mockEVMClient{}

	c := NewClient(mock, PrecompileAddress, 58887, "", logger)

	_, err := c.GetRegistry(context.Background(), GetRegistryRequest{})
	if err == nil {
		t.Fatal("expected error when ABI not loaded")
	}
	if !containsSubstring(err.Error(), "ABI not loaded") {
		t.Errorf("error = %q, want substring about ABI not loaded", err.Error())
	}

	_, err = c.GetRegistries(context.Background(), GetRegistriesRequest{})
	if err == nil {
		t.Fatal("expected error from GetRegistries when ABI not loaded")
	}

	_, err = c.GetRecords(context.Background(), GetRecordsRequest{})
	if err == nil {
		t.Fatal("expected error from GetRecords when ABI not loaded")
	}
}

func TestLoadABI_RawArray(t *testing.T) {
	abiPath := testABIPath(t)

	parsed, err := loadABI(abiPath)
	if err != nil {
		t.Fatalf("loadABI failed: %v", err)
	}
	if len(parsed.Methods) != 5 {
		t.Errorf("method count = %d, want 5", len(parsed.Methods))
	}

	expectedMethods := []string{
		"addRecord", "addRegistry", "grantRole", "records", "registries",
	}
	for _, name := range expectedMethods {
		if _, ok := parsed.Methods[name]; !ok {
			t.Errorf("missing method %q in parsed ABI", name)
		}
	}
}

func TestLoadABI_WrappedObject(t *testing.T) {
	abiPath := testABIPath(t)
	raw, err := os.ReadFile(abiPath)
	if err != nil {
		t.Fatal(err)
	}

	wrapper := struct {
		ABI json.RawMessage `json:"abi"`
	}{
		ABI: json.RawMessage(raw),
	}
	wrappedJSON, err := json.Marshal(wrapper)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	wrappedPath := filepath.Join(dir, "wrapped.json")
	if writeErr := os.WriteFile(wrappedPath, wrappedJSON, 0o644); writeErr != nil {
		t.Fatal(writeErr)
	}

	parsed, err := loadABI(wrappedPath)
	if err != nil {
		t.Fatalf("loadABI with wrapper failed: %v", err)
	}
	if len(parsed.Methods) != 5 {
		t.Errorf("method count = %d, want 5", len(parsed.Methods))
	}
}

func TestLoadABI_EmptyMethods(t *testing.T) {
	dir := t.TempDir()
	emptyABI := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(emptyABI, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := loadABI(emptyABI)
	if err == nil {
		t.Fatal("expected error for empty ABI")
	}
}

func TestLoadABI_FileNotFound(t *testing.T) {
	_, err := loadABI("/nonexistent/path.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestPrecompileInfo_JSON(t *testing.T) {
	info := PrecompileInfo{
		Address:     "0x0000000000000000000000000000000000000A00",
		ChainID:     58887,
		ABILoaded:   true,
		MethodCount: 5,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded PrecompileInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Address != info.Address {
		t.Errorf("Address = %q", decoded.Address)
	}
	if decoded.ChainID != info.ChainID {
		t.Errorf("ChainID = %d", decoded.ChainID)
	}
	if decoded.ABILoaded != info.ABILoaded {
		t.Errorf("ABILoaded = %v", decoded.ABILoaded)
	}
	if decoded.MethodCount != info.MethodCount {
		t.Errorf("MethodCount = %d", decoded.MethodCount)
	}
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
