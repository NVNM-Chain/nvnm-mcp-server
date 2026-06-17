// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// ---------------------------------------------------------------------------
// Mock EVM client
// ---------------------------------------------------------------------------

type mockEVM struct {
	chainInfo   *evm.ChainInfo
	block       *evm.NormalizedBlock
	tx          *evm.NormalizedTransaction
	receipt     *evm.NormalizedReceipt
	balance     *evm.NormalizedBalance
	code        *evm.CodeResult
	logs        []evm.NormalizedLog
	callResult  []byte
	sendTxHash  string
	nonce       uint64 // returned by PendingNonceAt; default 0
	returnErr   error
	lastAddress defitypes.Address
	lastHash    defitypes.Hash
}

func (m *mockEVM) ChainID(_ context.Context) (*big.Int, error)         { return big.NewInt(58887), m.returnErr }
func (m *mockEVM) LatestBlockNumber(_ context.Context) (uint64, error) { return 100, m.returnErr }
func (m *mockEVM) GetChainInfo(_ context.Context) (*evm.ChainInfo, error) {
	return m.chainInfo, m.returnErr
}
func (m *mockEVM) BlockByNumber(_ context.Context, _ *big.Int, _ bool) (*evm.NormalizedBlock, error) {
	return m.block, m.returnErr
}
func (m *mockEVM) BlockByHash(_ context.Context, h defitypes.Hash, _ bool) (*evm.NormalizedBlock, error) {
	m.lastHash = h
	return m.block, m.returnErr
}
func (m *mockEVM) TransactionByHash(_ context.Context, h defitypes.Hash) (*evm.NormalizedTransaction, error) {
	m.lastHash = h
	return m.tx, m.returnErr
}
func (m *mockEVM) TransactionReceipt(_ context.Context, h defitypes.Hash) (*evm.NormalizedReceipt, error) {
	m.lastHash = h
	return m.receipt, m.returnErr
}
func (m *mockEVM) BalanceAt(_ context.Context, addr defitypes.Address, _ *big.Int) (*evm.NormalizedBalance, error) {
	m.lastAddress = addr
	return m.balance, m.returnErr
}
func (m *mockEVM) CodeAt(_ context.Context, addr defitypes.Address, _ *big.Int) (*evm.CodeResult, error) {
	m.lastAddress = addr
	return m.code, m.returnErr
}

//nolint:gocritic // hugeParam: matches go-ethereum's CallContract signature
func (m *mockEVM) CallContract(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
	return m.callResult, m.returnErr
}
func (m *mockEVM) FilterLogs(_ context.Context, _ defitypes.FilterLogsQuery) ([]evm.NormalizedLog, error) {
	return m.logs, m.returnErr
}
func (m *mockEVM) PendingNonceAt(_ context.Context, _ defitypes.Address) (uint64, error) {
	return m.nonce, m.returnErr
}
func (m *mockEVM) SuggestGasPrice(_ context.Context) (*big.Int, error) {
	return big.NewInt(0), m.returnErr
}
func (m *mockEVM) SuggestGasTipCap(_ context.Context) (*big.Int, error) {
	return big.NewInt(0), m.returnErr
}

//nolint:gocritic // hugeParam: matches go-ethereum's EstimateGas signature
func (m *mockEVM) EstimateGas(_ context.Context, _ defitypes.Call) (uint64, error) {
	return 0, m.returnErr
}
func (m *mockEVM) SendRawTransaction(_ context.Context, _ string) (string, error) {
	return m.sendTxHash, m.returnErr
}
func (m *mockEVM) Ping(_ context.Context) error { return m.returnErr }
func (m *mockEVM) Close()                       {}

// ---------------------------------------------------------------------------
// Mock Anchor client
// ---------------------------------------------------------------------------

type mockAnchor struct {
	info       anchor.PrecompileInfo
	registry   *anchor.Registry
	registries *anchor.GetRegistriesResponse
	records    *anchor.GetRecordsResponse
	unsignedTx *anchor.UnsignedTransaction
	returnErr  error
}

func (m *mockAnchor) Info() anchor.PrecompileInfo { return m.info }
func (m *mockAnchor) Available() bool             { return m.info.ABILoaded }
func (m *mockAnchor) GetRegistry(_ context.Context, _ anchor.GetRegistryRequest) (*anchor.Registry, error) {
	return m.registry, m.returnErr
}
func (m *mockAnchor) GetRegistries(_ context.Context, _ anchor.GetRegistriesRequest) (*anchor.GetRegistriesResponse, error) {
	return m.registries, m.returnErr
}
func (m *mockAnchor) GetRecords(_ context.Context, _ anchor.GetRecordsRequest) (*anchor.GetRecordsResponse, error) {
	return m.records, m.returnErr
}
func (m *mockAnchor) PrepareAddRegistry(_ context.Context, _ anchor.PrepareAddRegistryRequest) (*anchor.UnsignedTransaction, error) {
	return m.unsignedTx, m.returnErr
}
func (m *mockAnchor) PrepareAddRecord(_ context.Context, _ anchor.PrepareAddRecordRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
	return m.unsignedTx, m.returnErr
}
func (m *mockAnchor) PrepareGrantRole(_ context.Context, _ anchor.PrepareGrantRoleRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
	return m.unsignedTx, m.returnErr
}

// ---------------------------------------------------------------------------
// Test constants
// ---------------------------------------------------------------------------

const (
	testAddr    = "0x0000000000000000000000000000000000000A00"
	testTxHash  = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBadHash = "0xZZZZnotahash"
	testBadAddr = "not-an-address"
)

var ctx = context.Background()

// ---------------------------------------------------------------------------
// EVM read tool handler tests
// ---------------------------------------------------------------------------

func TestHandler_ChainID_Happy(t *testing.T) {
	m := &mockEVM{chainInfo: &evm.ChainInfo{ChainID: 58887, LatestBlockNumber: 100}}
	handler := makeChainIDHandler(m)

	_, out, err := handler(ctx, nil, chainIDInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ChainID != 58887 {
		t.Errorf("ChainID = %d, want 58887", out.ChainID)
	}
}

func TestHandler_ChainID_Error(t *testing.T) {
	m := &mockEVM{returnErr: errors.New("rpc down")}
	handler := makeChainIDHandler(m)

	_, _, err := handler(ctx, nil, chainIDInput{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestHandler_GetBlock_ByNumber(t *testing.T) {
	m := &mockEVM{block: &evm.NormalizedBlock{Number: 42, Hash: "0xabc"}}
	handler := makeGetBlockHandler(m)

	num := int64(42)
	_, out, err := handler(ctx, nil, getBlockInput{BlockNumber: &num})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Number != 42 {
		t.Errorf("Number = %d, want 42", out.Number)
	}
}

func TestHandler_GetBlock_ByHash(t *testing.T) {
	m := &mockEVM{block: &evm.NormalizedBlock{Number: 99, Hash: "0xdef"}}
	handler := makeGetBlockHandler(m)

	hash := testTxHash
	_, out, err := handler(ctx, nil, getBlockInput{BlockHash: &hash})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Number != 99 {
		t.Errorf("Number = %d, want 99", out.Number)
	}
}

func TestHandler_GetBlock_Latest(t *testing.T) {
	m := &mockEVM{block: &evm.NormalizedBlock{Number: 200}}
	handler := makeGetBlockHandler(m)

	_, out, err := handler(ctx, nil, getBlockInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Number != 200 {
		t.Errorf("Number = %d, want 200", out.Number)
	}
}

func TestHandler_GetBlock_InvalidHash(t *testing.T) {
	m := &mockEVM{}
	handler := makeGetBlockHandler(m)

	hash := testBadHash
	_, _, err := handler(ctx, nil, getBlockInput{BlockHash: &hash})
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
	if !errors.Is(err, apperrors.ErrInvalidTxHash) {
		t.Errorf("error = %v, want ErrInvalidTxHash", err)
	}
}

func TestHandler_GetTransaction_Happy(t *testing.T) {
	m := &mockEVM{tx: &evm.NormalizedTransaction{Hash: testTxHash, Gas: 21000}}
	handler := makeGetTransactionHandler(m)

	_, out, err := handler(ctx, nil, txHashInput{TxHash: testTxHash})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Hash != testTxHash {
		t.Errorf("Hash = %q, want %q", out.Hash, testTxHash)
	}
}

func TestHandler_GetTransaction_InvalidHash(t *testing.T) {
	handler := makeGetTransactionHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, txHashInput{TxHash: testBadHash})
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
}

func TestHandler_GetTransaction_MissingHash(t *testing.T) {
	handler := makeGetTransactionHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, txHashInput{TxHash: ""})
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestHandler_GetReceipt_Happy(t *testing.T) {
	m := &mockEVM{receipt: &evm.NormalizedReceipt{TxHash: testTxHash, Status: "success", GasUsed: 21000}}
	handler := makeGetReceiptHandler(m)

	_, out, err := handler(ctx, nil, txHashInput{TxHash: testTxHash})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != "success" {
		t.Errorf("Status = %q, want %q", out.Status, "success")
	}
}

func TestHandler_GetReceipt_InvalidHash(t *testing.T) {
	handler := makeGetReceiptHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, txHashInput{TxHash: "short"})
	if err == nil {
		t.Fatal("expected error for invalid hash")
	}
}

func TestHandler_GetBalance_Happy(t *testing.T) {
	m := &mockEVM{balance: &evm.NormalizedBalance{Address: testAddr, Wei: "1000", Ether: "0.000000000000001"}}
	handler := makeGetBalanceHandler(m)

	_, out, err := handler(ctx, nil, getBalanceInput{Address: testAddr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Wei != "1000" {
		t.Errorf("Wei = %q, want %q", out.Wei, "1000")
	}
}

func TestHandler_GetBalance_WithBlock(t *testing.T) {
	m := &mockEVM{balance: &evm.NormalizedBalance{Address: testAddr, Wei: "500", Ether: "0.0000000000000005"}}
	handler := makeGetBalanceHandler(m)

	block := int64(50)
	_, out, err := handler(ctx, nil, getBalanceInput{Address: testAddr, BlockNum: &block})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Wei != "500" {
		t.Errorf("Wei = %q, want %q", out.Wei, "500")
	}
}

func TestHandler_GetBalance_InvalidAddress(t *testing.T) {
	handler := makeGetBalanceHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, getBalanceInput{Address: testBadAddr})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("error = %v, want ErrInvalidAddress", err)
	}
}

func TestHandler_GetCode_Happy(t *testing.T) {
	m := &mockEVM{code: &evm.CodeResult{Address: testAddr, Bytecode: "0x6080", IsContract: true}}
	handler := makeGetCodeHandler(m)

	_, out, err := handler(ctx, nil, getCodeInput{Address: testAddr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsContract {
		t.Error("IsContract = false, want true")
	}
}

func TestHandler_GetCode_InvalidAddress(t *testing.T) {
	handler := makeGetCodeHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, getCodeInput{Address: testBadAddr})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestHandler_GetLogs_Happy(t *testing.T) {
	m := &mockEVM{logs: []evm.NormalizedLog{
		{Address: testAddr, BlockNumber: 10, TxHash: testTxHash},
	}}
	handler := makeGetLogsHandler(m)

	_, out, err := handler(ctx, nil, getLogsInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Count != 1 {
		t.Errorf("Count = %d, want 1", out.Count)
	}
}

func TestHandler_GetLogs_WithAddressAndTopics(t *testing.T) {
	m := &mockEVM{logs: []evm.NormalizedLog{}}
	handler := makeGetLogsHandler(m)

	addr := testAddr
	from := int64(1)
	to := int64(100)
	_, out, err := handler(ctx, nil, getLogsInput{
		Address:   &addr,
		FromBlock: &from,
		ToBlock:   &to,
		Topics:    []string{testTxHash},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Count != 0 {
		t.Errorf("Count = %d, want 0", out.Count)
	}
}

func TestHandler_GetLogs_InvalidAddress(t *testing.T) {
	handler := makeGetLogsHandler(&mockEVM{})

	bad := testBadAddr
	_, _, err := handler(ctx, nil, getLogsInput{Address: &bad})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestHandler_GetLogs_InvalidTopic(t *testing.T) {
	handler := makeGetLogsHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, getLogsInput{Topics: []string{"0xbadtopic"}})
	if err == nil {
		t.Fatal("expected error for invalid topic")
	}
}

func TestHandler_CallContract_Happy(t *testing.T) {
	m := &mockEVM{callResult: []byte{0xca, 0xfe}}
	handler := makeCallContractHandler(m)

	_, out, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xcafe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Result != "0xcafe" {
		t.Errorf("Result = %q, want %q", out.Result, "0xcafe")
	}
}

func TestHandler_CallContract_InvalidAddress(t *testing.T) {
	handler := makeCallContractHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, callContractInput{To: testBadAddr, Data: "0x00"})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestHandler_CallContract_BadHexData(t *testing.T) {
	handler := makeCallContractHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xGGGG"})
	if err == nil {
		t.Fatal("expected error for invalid hex data")
	}
}

func TestHandler_CallContract_WithBlock(t *testing.T) {
	m := &mockEVM{callResult: []byte{0xab}}
	handler := makeCallContractHandler(m)

	block := int64(42)
	_, out, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xab", BlockNum: &block})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Result != "0xab" {
		t.Errorf("Result = %q, want %q", out.Result, "0xab")
	}
}

// ---------------------------------------------------------------------------
// EVM write tool handler tests
// ---------------------------------------------------------------------------

func TestHandler_SendRawTx_Happy(t *testing.T) {
	m := &mockEVM{sendTxHash: "0xdeadbeef"}
	handler := makeSendRawTxHandler(m, testLogger())

	stubReq := &mcp.CallToolRequest{}
	_, out, err := handler(ctx, stubReq, sendRawTxInput{SignedTxHex: "0xf86c..."})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.TxHash != "0xdeadbeef" {
		t.Errorf("TxHash = %q, want %q", out.TxHash, "0xdeadbeef")
	}
}

func TestHandler_SendRawTx_Empty(t *testing.T) {
	handler := makeSendRawTxHandler(&mockEVM{}, testLogger())

	_, _, err := handler(ctx, nil, sendRawTxInput{SignedTxHex: ""})
	if err == nil {
		t.Fatal("expected error for empty signed_tx")
	}
	if !errors.Is(err, apperrors.ErrMissingRequired) {
		t.Errorf("error = %v, want ErrMissingRequired", err)
	}
}

// ---------------------------------------------------------------------------
// Anchor read tool handler tests
// ---------------------------------------------------------------------------

func TestHandler_AnchorInfo_Happy(t *testing.T) {
	m := &mockAnchor{info: anchor.PrecompileInfo{
		Address:     testAddr,
		ChainID:     58887,
		ABILoaded:   true,
		MethodCount: 5,
	}}
	handler := makeAnchorInfoHandler(m)

	_, out, err := handler(ctx, nil, anchorInfoInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ChainID != 58887 {
		t.Errorf("ChainID = %d, want 58887", out.ChainID)
	}
	if out.MethodCount != 5 {
		t.Errorf("MethodCount = %d, want 5", out.MethodCount)
	}
}

func TestHandler_GetRegistry_ByName(t *testing.T) {
	m := &mockAnchor{registry: &anchor.Registry{ID: 1, Name: "test-reg", Creator: "someone"}}
	handler := makeGetRegistryHandler(m)

	name := "test-reg"
	_, out, err := handler(ctx, nil, getRegistryInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Name != "test-reg" {
		t.Errorf("Name = %q, want %q", out.Name, "test-reg")
	}
}

func TestHandler_GetRegistry_ByID(t *testing.T) {
	m := &mockAnchor{registry: &anchor.Registry{ID: 42, Name: "by-id"}}
	handler := makeGetRegistryHandler(m)

	id := uint64(42)
	_, out, err := handler(ctx, nil, getRegistryInput{ID: &id})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != 42 {
		t.Errorf("ID = %d, want 42", out.ID)
	}
}

func TestHandler_GetRegistry_NeitherIDNorName(t *testing.T) {
	handler := makeGetRegistryHandler(&mockAnchor{})

	_, _, err := handler(ctx, nil, getRegistryInput{})
	if err == nil {
		t.Fatal("expected error when neither id nor name provided")
	}
	if !errors.Is(err, apperrors.ErrMissingRequired) {
		t.Errorf("error = %v, want ErrMissingRequired", err)
	}
}

func TestHandler_GetRegistries_NoFilter(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{{ID: 1}, {ID: 2}},
		Pagination: &anchor.PageResponse{Total: 2},
	}}
	handler := makeGetRegistriesHandler(m)

	_, out, err := handler(ctx, nil, getRegistriesInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 2 {
		t.Errorf("len(Registries) = %d, want 2", len(out.Registries))
	}
}

func TestHandler_GetRegistries_WithPagination(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{{ID: 5}},
		Pagination: &anchor.PageResponse{Total: 100},
	}}
	handler := makeGetRegistriesHandler(m)

	offset := uint64(4)
	limit := uint64(1)
	_, out, err := handler(ctx, nil, getRegistriesInput{Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pagination.Total != 100 {
		t.Errorf("Total = %d, want 100", out.Pagination.Total)
	}
}

func TestHandler_GetRecords_ByRegistry(t *testing.T) {
	m := &mockAnchor{records: &anchor.GetRecordsResponse{
		Records:    []anchor.Record{{RecordID: 1, Checksum: "abc123"}},
		Pagination: &anchor.PageResponse{Total: 1},
	}}
	handler := makeGetRecordsHandler(m)

	reg := "test-reg"
	_, out, err := handler(ctx, nil, getRecordsInput{Registry: &reg})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Records) != 1 {
		t.Errorf("len(Records) = %d, want 1", len(out.Records))
	}
}

func TestHandler_GetRecords_WithPagination(t *testing.T) {
	m := &mockAnchor{records: &anchor.GetRecordsResponse{
		Records:    []anchor.Record{},
		Pagination: &anchor.PageResponse{Total: 0},
	}}
	handler := makeGetRecordsHandler(m)

	offset := uint64(0)
	limit := uint64(10)
	_, out, err := handler(ctx, nil, getRecordsInput{Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pagination.Total != 0 {
		t.Errorf("Total = %d, want 0", out.Pagination.Total)
	}
}

// ---------------------------------------------------------------------------
// Anchor write tool handler tests
// ---------------------------------------------------------------------------

var sampleUnsignedTx = &anchor.UnsignedTransaction{
	RawTx:    "0xdeadbeef",
	To:       testAddr,
	Data:     "0xcafebabe",
	Nonce:    5,
	Gas:      63000,
	GasPrice: "45000000000",
	Value:    "0",
	ChainID:  58887,
}

func TestHandler_PrepareAddRegistry_Happy(t *testing.T) {
	m := &mockAnchor{unsignedTx: sampleUnsignedTx}
	handler := makePrepareAddRegistryHandler(m, testLogger())

	_, out, err := handler(ctx, nil, prepareAddRegistryInput{
		From:        testAddr,
		Name:        "my-reg",
		Description: "desc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ChainID != 58887 {
		t.Errorf("ChainID = %d, want 58887", out.ChainID)
	}
	if out.Nonce != 5 {
		t.Errorf("Nonce = %d, want 5", out.Nonce)
	}
}

func TestHandler_PrepareAddRegistry_Error(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("missing from")}
	handler := makePrepareAddRegistryHandler(m, testLogger())

	_, _, err := handler(ctx, nil, prepareAddRegistryInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandler_PrepareAddRecord_Happy(t *testing.T) {
	m := &mockAnchor{unsignedTx: sampleUnsignedTx}
	handler := makePrepareAddRecordHandler(m, testLogger())

	_, out, err := handler(ctx, nil, prepareAddRecordInput{
		From:     testAddr,
		Registry: "test-reg",
		Checksum: "abc123",
		URI:      "https://example.com/doc.pdf",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.To != testAddr {
		t.Errorf("To = %q, want %q", out.To, testAddr)
	}
}

func TestHandler_PrepareAddRecord_Error(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("checksum required")}
	handler := makePrepareAddRecordHandler(m, testLogger())

	_, _, err := handler(ctx, nil, prepareAddRecordInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandler_PrepareGrantRole_Happy(t *testing.T) {
	m := &mockAnchor{unsignedTx: sampleUnsignedTx}
	handler := makePrepareGrantRoleHandler(m, testLogger())

	_, out, err := handler(ctx, nil, prepareGrantRoleInput{
		From:       testAddr,
		RegistryID: 1,
		Account:    testAddr,
		Role:       "editor",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Gas != 63000 {
		t.Errorf("Gas = %d, want 63000", out.Gas)
	}
}

func TestHandler_PrepareGrantRole_Error(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("role required")}
	handler := makePrepareGrantRoleHandler(m, testLogger())

	_, _, err := handler(ctx, nil, prepareGrantRoleInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ---------------------------------------------------------------------------
// Validation helper tests
// ---------------------------------------------------------------------------

func TestParseAddress_Valid(t *testing.T) {
	addr, err := parseAddress(testAddr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != defitypes.MustAddressFromHex(testAddr) {
		t.Errorf("address mismatch")
	}
}

func TestParseAddress_Invalid(t *testing.T) {
	_, err := parseAddress(testBadAddr)
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
	if !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("error = %v, want ErrInvalidAddress", err)
	}
}

func TestParseHash_Valid(t *testing.T) {
	_, err := parseHash(testTxHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseHash_TooShort(t *testing.T) {
	_, err := parseHash("0xabcd")
	if err == nil {
		t.Fatal("expected error for short hash")
	}
}

func TestParseHash_InvalidHex(t *testing.T) {
	_, err := parseHash("0x" + "GG" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa0000")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestParseHexData_Valid(t *testing.T) {
	data, err := parseHexData("0xcafe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("len = %d, want 2", len(data))
	}
}

func TestParseHexData_NoPrefixValid(t *testing.T) {
	data, err := parseHexData("cafe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("len = %d, want 2", len(data))
	}
}

func TestParseHexData_Invalid(t *testing.T) {
	_, err := parseHexData("0xZZZZ")
	if err == nil {
		t.Fatal("expected error for invalid hex data")
	}
}
