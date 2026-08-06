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
	lastCall    defitypes.Call
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
func (m *mockEVM) CallContract(_ context.Context, call defitypes.Call, _ *big.Int) ([]byte, error) {
	m.lastCall = call
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
	// registriesFn, when non-nil, generates a response per call index --
	// used to test the by-name scan's multi-page walk without building
	// large fixed slices. Takes priority over registriesPages.
	registriesFn func(callIndex int) (*anchor.GetRegistriesResponse, error)
	// registriesPages, when non-nil, makes GetRegistries return successive
	// pages in order (one per call) instead of the fixed registries
	// response -- used to test the by-name scan's multi-page walk. A call
	// past the end of the slice returns an empty page (end of table).
	registriesPages     []*anchor.GetRegistriesResponse
	registriesCallCount int
	records             *anchor.GetRecordsResponse
	unsignedTx          *anchor.UnsignedTransaction
	returnErr           error
}

func (m *mockAnchor) Info() anchor.PrecompileInfo { return m.info }
func (m *mockAnchor) Available() bool             { return m.info.ABILoaded }

// MethodSelector reports no ABI: these tests never assert on selectors.
func (m *mockAnchor) MethodSelector(string) (string, bool) { return "", false }
func (m *mockAnchor) GetRegistry(_ context.Context, _ anchor.GetRegistryRequest) (*anchor.Registry, error) {
	return m.registry, m.returnErr
}
func (m *mockAnchor) GetRegistries(_ context.Context, _ anchor.GetRegistriesRequest) (*anchor.GetRegistriesResponse, error) {
	idx := m.registriesCallCount
	m.registriesCallCount++
	if m.registriesFn != nil {
		return m.registriesFn(idx)
	}
	if m.registriesPages != nil {
		if idx < len(m.registriesPages) {
			return m.registriesPages[idx], m.returnErr
		}
		return &anchor.GetRegistriesResponse{}, m.returnErr
	}
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
func (m *mockAnchor) PrepareUpdateRecordStatus(
	_ context.Context, _ anchor.PrepareUpdateRecordStatusRequest,
) (*anchor.UnsignedTransaction, error) {
	return m.unsignedTx, m.returnErr
}
func (m *mockAnchor) PrepareGrantRole(_ context.Context, _ anchor.PrepareGrantRoleRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
	return m.unsignedTx, m.returnErr
}
func (m *mockAnchor) PrepareRevokeRole(_ context.Context, _ anchor.PrepareRevokeRoleRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
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

// TestHandler_GetTransaction_NotFound_MessageNotDoubled guards the
// client-visible not-found text. The evm client already returns the
// ErrTxNotFound sentinel ("transaction not found"); the handler re-wrapping it
// with the same prefix produced "transaction not found: transaction not
// found" on the wire, and mislabeled genuine upstream failures as not-found.
func TestHandler_GetTransaction_NotFound_MessageNotDoubled(t *testing.T) {
	handler := makeGetTransactionHandler(&mockEVM{returnErr: apperrors.ErrTxNotFound})

	_, _, err := handler(ctx, nil, txHashInput{TxHash: testTxHash})
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !errors.Is(err, apperrors.ErrTxNotFound) {
		t.Fatalf("error should wrap ErrTxNotFound; got %v", err)
	}
	if got, want := err.Error(), apperrors.ErrTxNotFound.Error(); got != want {
		t.Errorf("client-visible message = %q, want %q", got, want)
	}
}

// TestHandler_GetLogs_ClientErrorNotRePrefixed verifies the handler passes
// FilterLogs errors through untouched: the evm client already contextualizes
// its errors ("failed to filter logs: ..."), and curated input-class sentinels
// (e.g. the log range cap) are client-facing text that must surface verbatim.
func TestHandler_GetLogs_ClientErrorNotRePrefixed(t *testing.T) {
	handler := makeGetLogsHandler(&mockEVM{returnErr: apperrors.ErrLogRangeTooWide})

	_, _, err := handler(ctx, nil, getLogsInput{})
	if err == nil {
		t.Fatal("expected error from FilterLogs")
	}
	if got, want := err.Error(), apperrors.ErrLogRangeTooWide.Error(); got != want {
		t.Errorf("client-visible message = %q, want %q", got, want)
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

// F6: evm_call_contract exposes an optional `from`. Without it eth_call runs
// as the zero address, so permissioned-function simulations always revert.
// When supplied, the address must be forwarded to the eth_call message.
func TestHandler_CallContract_PassesFromAddress(t *testing.T) {
	m := &mockEVM{callResult: []byte{0x01}}
	handler := makeCallContractHandler(m)

	const fromHex = "0x1111111111111111111111111111111111111111"
	_, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xab", From: fromHex})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.lastCall.From == nil {
		t.Fatal("From must be set on the eth_call message when supplied")
	}
	want, _ := defitypes.AddressFromHex(fromHex)
	if *m.lastCall.From != want {
		t.Errorf("From = %v, want %v", m.lastCall.From, want)
	}
}

// When `from` is omitted the sender stays nil (zero-address eth_call), so the
// existing behavior is preserved for callers that don't need it.
func TestHandler_CallContract_OmittedFromLeavesNilSender(t *testing.T) {
	m := &mockEVM{callResult: []byte{0x01}}
	handler := makeCallContractHandler(m)

	if _, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xab"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.lastCall.From != nil {
		t.Errorf("From should stay nil when not supplied; got %v", m.lastCall.From)
	}
}

// A malformed `from` is a caller-input error, surfaced as such.
func TestHandler_CallContract_InvalidFromRejected(t *testing.T) {
	handler := makeCallContractHandler(&mockEVM{})

	_, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xab", From: testBadAddr})
	if err == nil {
		t.Fatal("expected error for invalid from address")
	}
}

// ---------------------------------------------------------------------------
// EVM write tool handler tests
// ---------------------------------------------------------------------------

func TestHandler_SendRawTx_Happy(t *testing.T) {
	m := &mockEVM{sendTxHash: "0xdeadbeef"}
	// relayAllowAny=true: this test broadcasts a placeholder hex that does not
	// decode, exercising the escape-hatch best-effort passthrough. The
	// scoped-default path is covered in tools_evm_write_test.go.
	handler := makeSendRawTxHandler(m, testAddr, false, true, nil, nil, signerGates{}, testLogger())

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
	handler := makeSendRawTxHandler(&mockEVM{}, testAddr, false, false, nil, nil, signerGates{}, testLogger())

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

func TestHandler_GetRegistry_ByID(t *testing.T) {
	m := &mockAnchor{registry: &anchor.Registry{ID: 42, Name: "by-id"}}
	handler := makeGetRegistryHandler(m)

	_, out, err := handler(ctx, nil, getRegistryInput{ID: 42})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.ID != 42 {
		t.Errorf("ID = %d, want 42", out.ID)
	}
}

func TestHandler_GetRegistry_MissingID(t *testing.T) {
	handler := makeGetRegistryHandler(&mockAnchor{})

	_, _, err := handler(ctx, nil, getRegistryInput{})
	if err == nil {
		t.Fatal("expected error when id not provided")
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
	handler := makeGetRegistriesHandler(m, testLogger())

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
	handler := makeGetRegistriesHandler(m, testLogger())

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

// TestHandler_GetRegistries_ByName_ReturnsAllMatches guards W3's core
// requirement: registry names are caller-supplied and not unique, so a
// by-name lookup must return every match, never auto-select one.
func TestHandler_GetRegistries_ByName_ReturnsAllMatches(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{
			{ID: 1, Name: "fund-documents", Creator: "0xaaa"},
			{ID: 2, Name: "audit-reports", Creator: "0xbbb"},
			{ID: 3, Name: "fund-documents", Creator: "0xccc"},
		},
	}}
	handler := makeGetRegistriesHandler(m, testLogger())

	name := "fund-documents"
	_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 2 {
		t.Fatalf("len(Registries) = %d, want 2 (both fund-documents registries)", len(out.Registries))
	}
	gotIDs := map[uint64]bool{}
	for _, r := range out.Registries {
		gotIDs[r.ID] = true
	}
	if !gotIDs[1] || !gotIDs[3] {
		t.Errorf("expected registries 1 and 3, got %+v", out.Registries)
	}
}

func TestHandler_GetRegistries_ByName_MatchModesCaseInsensitive(t *testing.T) {
	registries := []anchor.Registry{
		{ID: 1, Name: "Fund-Documents"},
		{ID: 2, Name: "audit-reports"},
	}
	tests := []struct {
		name    string
		query   string
		match   string
		wantIDs []uint64
	}{
		{"exact default", "fund-documents", "", []uint64{1}},
		{"exact explicit", "FUND-DOCUMENTS", "exact", []uint64{1}},
		{"prefix", "fund", "prefix", []uint64{1}},
		{"suffix", "documents", "suffix", []uint64{1}},
		{"contains", "doc", "contains", []uint64{1}},
		{"no match", "nonexistent", "exact", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAnchor{registries: &anchor.GetRegistriesResponse{Registries: registries}}
			handler := makeGetRegistriesHandler(m, testLogger())

			_, out, err := handler(ctx, nil, getRegistriesInput{Name: &tc.query, Match: tc.match})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out.Registries) != len(tc.wantIDs) {
				t.Fatalf("len(Registries) = %d, want %d", len(out.Registries), len(tc.wantIDs))
			}
			for i, id := range tc.wantIDs {
				if out.Registries[i].ID != id {
					t.Errorf("Registries[%d].ID = %d, want %d", i, out.Registries[i].ID, id)
				}
			}
		})
	}
}

func TestHandler_GetRegistries_ByName_InvalidMatchMode(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}
	handler := makeGetRegistriesHandler(m, testLogger())

	name := "anything"
	_, _, err := handler(ctx, nil, getRegistriesInput{Name: &name, Match: "fuzzy"})
	if !errors.Is(err, apperrors.ErrInvalidMatchMode) {
		t.Errorf("error = %v, want ErrInvalidMatchMode", err)
	}
}

// TestHandler_GetRegistries_ByName_CombinedWithOtherFiltersErrors covers all
// three fields the name filter is mutually exclusive with -- one table
// instead of one function per field, since each case exercises the same
// guard (input.RegistryID != nil || input.Offset != nil || input.Limit !=
// nil), not distinct behavior.
func TestHandler_GetRegistries_ByName_CombinedWithOtherFiltersErrors(t *testing.T) {
	regID := uint64(1)
	offset := uint64(10)
	limit := uint64(10)
	tests := []struct {
		name  string
		input getRegistriesInput
	}{
		{"registry_id", getRegistriesInput{RegistryID: &regID}},
		{"offset", getRegistriesInput{Offset: &offset}},
		{"limit", getRegistriesInput{Limit: &limit}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}
			handler := makeGetRegistriesHandler(m, testLogger())

			name := "anything"
			tc.input.Name = &name
			_, _, err := handler(ctx, nil, tc.input)
			if !errors.Is(err, apperrors.ErrInvalidFilterCombination) {
				t.Errorf("error = %v, want ErrInvalidFilterCombination", err)
			}
		})
	}
}

// TestHandler_GetRegistries_MatchWithoutNameRejected proves a match mode
// with no name to match against is rejected outright rather than silently
// ignored -- silently dropping a supplied parameter would leave the caller
// believing a filter was applied when none was.
func TestHandler_GetRegistries_MatchWithoutNameRejected(t *testing.T) {
	empty := ""
	tests := []struct {
		name  string
		input getRegistriesInput
	}{
		{"match alone", getRegistriesInput{Match: "prefix"}},
		{"match with empty name", getRegistriesInput{Name: &empty, Match: "prefix"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}
			handler := makeGetRegistriesHandler(m, testLogger())

			_, _, err := handler(ctx, nil, tc.input)
			if !errors.Is(err, apperrors.ErrMatchWithoutName) {
				t.Errorf("error = %v, want ErrMatchWithoutName", err)
			}
		})
	}
}

// TestScanRegistriesByName_PagesUntilShortPage proves the walk crosses
// multiple chain pages -- pagination.total is unreliable on this chain, so
// the scan must keep paging until it sees a page shorter than requested,
// not stop after one call.
func TestScanRegistriesByName_PagesUntilShortPage(t *testing.T) {
	full := make([]anchor.Registry, nameScanPageSize)
	for i := range full {
		full[i] = anchor.Registry{ID: uint64(i), Name: "filler"}
	}
	pages := []*anchor.GetRegistriesResponse{
		// Call 0: the latestRegistryID peek. 201 reconciles exactly with
		// the 201 rows the walk below will scan (200 filler + 1 target),
		// so the peek doesn't itself flag the result as truncated.
		{Registries: []anchor.Registry{{ID: 201}}},
		// Full page: NextKey non-empty means the SDK saw at least one more
		// entry past this page, so the walk must keep paging via Key, not
		// stop just because the page happened to be full.
		{Registries: full, Pagination: &anchor.PageResponse{NextKey: []byte("cursor-1")}},
		{Registries: []anchor.Registry{{ID: 9999, Name: "target-registry"}}}, // short page: stop here
	}
	m := &mockAnchor{registriesPages: pages}

	matches, truncated, err := scanRegistriesByName(ctx, m, "target-registry", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Error("expected truncated=false; the walk ended on a short page and reconciled with the peek")
	}
	if len(matches) != 1 || matches[0].ID != 9999 {
		t.Errorf("matches = %+v, want a single match with ID 9999", matches)
	}
	if m.registriesCallCount != 3 {
		t.Errorf("registriesCallCount = %d, want 3 (peek, one full page, one short page)", m.registriesCallCount)
	}
}

// TestScanRegistriesByName_StopsOnExactlyFullLastPage guards the boundary
// case the SDK's own pagination has: a page can come back exactly full
// (len == nameScanPageSize) and still be the true last page, signaled only
// by an empty NextKey, not by a short row count. The walk must stop there,
// not issue a wasted extra call.
func TestScanRegistriesByName_StopsOnExactlyFullLastPage(t *testing.T) {
	full := make([]anchor.Registry, nameScanPageSize)
	for i := range full {
		full[i] = anchor.Registry{ID: uint64(i), Name: "target-registry"}
	}
	pages := []*anchor.GetRegistriesResponse{
		{Registries: []anchor.Registry{{ID: uint64(nameScanPageSize)}}},    // peek: reconciles with totalScanned
		{Registries: full, Pagination: &anchor.PageResponse{NextKey: nil}}, // exactly full, but NextKey empty: done
	}
	m := &mockAnchor{registriesPages: pages}

	matches, truncated, err := scanRegistriesByName(ctx, m, "target-registry", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Error("expected truncated=false; empty NextKey on an exactly-full page still means done")
	}
	if len(matches) != nameScanPageSize {
		t.Errorf("len(matches) = %d, want %d", len(matches), nameScanPageSize)
	}
	if m.registriesCallCount != 2 {
		t.Errorf("registriesCallCount = %d, want 2 (peek + the one exactly-full page, no wasted extra call)", m.registriesCallCount)
	}
}

// TestScanRegistriesByName_ShortPageWithNextKeyContinues guards the
// cap-drift case: if the chain's own page cap ever drops below
// nameScanPageSize, every page comes back short (fewer rows than requested)
// with NextKey still set. The walk must continue on NextKey -- row count is
// not a termination signal -- or a by-name scan would silently cover only
// the first page of the table.
func TestScanRegistriesByName_ShortPageWithNextKeyContinues(t *testing.T) {
	shortPage := make([]anchor.Registry, 100) // chain cap 100 < nameScanPageSize
	for i := range shortPage {
		shortPage[i] = anchor.Registry{ID: uint64(i + 1), Name: "filler"}
	}
	pages := []*anchor.GetRegistriesResponse{
		// Peek: 101 reconciles with the 101 rows the walk scans below.
		{Registries: []anchor.Registry{{ID: 101}}},
		// Short page (100 < nameScanPageSize) with NextKey set: keep going.
		{Registries: shortPage, Pagination: &anchor.PageResponse{NextKey: []byte("cursor-1")}},
		// Final page: no NextKey, walk ends.
		{Registries: []anchor.Registry{{ID: 101, Name: "target-registry"}}},
	}
	m := &mockAnchor{registriesPages: pages}

	matches, truncated, err := scanRegistriesByName(ctx, m, "target-registry", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Error("expected truncated=false; the walk ended on an empty NextKey and reconciled with the peek")
	}
	if len(matches) != 1 || matches[0].ID != 101 {
		t.Errorf("matches = %+v, want a single match with ID 101", matches)
	}
	if m.registriesCallCount != 3 {
		t.Errorf("registriesCallCount = %d, want 3 (peek + short-but-continuing page + final page)", m.registriesCallCount)
	}
}

// TestScanRegistriesByName_TruncatesAtPageCap guards the safety backstop:
// a chain that never returns a short page (e.g. pagination bug, or a
// pathologically large registry table) must not hang the request forever.
// The walk stops at maxNameScanPages and reports truncated=true rather than
// silently returning a partial match set indistinguishable from a complete
// one.
func TestScanRegistriesByName_TruncatesAtPageCap(t *testing.T) {
	// Every call, including the leading latestRegistryID peek, sees the same
	// never-ending full page with a NextKey always set -- simulating a
	// chain that never signals completion (pagination bug, or a
	// pathologically large table).
	m := &mockAnchor{
		registriesFn: func(_ int) (*anchor.GetRegistriesResponse, error) {
			full := make([]anchor.Registry, nameScanPageSize)
			for i := range full {
				full[i] = anchor.Registry{ID: uint64(i), Name: "never-matches"}
			}
			return &anchor.GetRegistriesResponse{
				Registries: full,
				Pagination: &anchor.PageResponse{NextKey: []byte("always-more")},
			}, nil
		},
	}

	matches, truncated, err := scanRegistriesByName(ctx, m, "target-registry", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !truncated {
		t.Error("expected truncated=true after exhausting maxNameScanPages full pages")
	}
	if len(matches) != 0 {
		t.Errorf("len(matches) = %d, want 0", len(matches))
	}
	// +1 for the leading latestRegistryID peek.
	if m.registriesCallCount != maxNameScanPages+1 {
		t.Errorf("registriesCallCount = %d, want %d", m.registriesCallCount, maxNameScanPages+1)
	}
}

func TestLatestRegistryID_ReturnsHighestID(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{{ID: 42, Name: "most-recent"}},
	}}

	id, found, err := latestRegistryID(ctx, m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}

func TestLatestRegistryID_EmptyTable(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}

	id, found, err := latestRegistryID(ctx, m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("found = true, want false (no registries)")
	}
	if id != 0 {
		t.Errorf("id = %d, want 0", id)
	}
}

func TestLatestRegistryID_PropagatesError(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("rpc down")}

	_, _, err := latestRegistryID(ctx, m)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestScanRegistriesByName_EmptyTableSkipsWalk proves the peek short-
// circuits an empty registry table without ever calling GetRegistries a
// second time.
func TestScanRegistriesByName_EmptyTableSkipsWalk(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}

	matches, truncated, err := scanRegistriesByName(ctx, m, "anything", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Error("expected truncated=false for a definitively empty table")
	}
	if len(matches) != 0 {
		t.Errorf("len(matches) = %d, want 0", len(matches))
	}
	if m.registriesCallCount != 1 {
		t.Errorf("registriesCallCount = %d, want 1 (peek only, walk skipped)", m.registriesCallCount)
	}
}

// TestScanRegistriesByName_PeekFailureFallsBackToPlainWalk proves a failed
// peek doesn't fail the whole scan -- it's a best-effort optimization, and
// the walk's own short-page termination is sufficient on its own.
func TestScanRegistriesByName_PeekFailureFallsBackToPlainWalk(t *testing.T) {
	callIndex := 0
	m := &mockAnchor{
		registriesFn: func(_ int) (*anchor.GetRegistriesResponse, error) {
			callIndex++
			if callIndex == 1 {
				return nil, errors.New("rpc down") // the peek fails
			}
			return &anchor.GetRegistriesResponse{
				Registries: []anchor.Registry{{ID: 7, Name: "target-registry"}},
			}, nil
		},
	}

	matches, truncated, err := scanRegistriesByName(ctx, m, "target-registry", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if truncated {
		t.Error("expected truncated=false; the walk itself ended on a short page")
	}
	if len(matches) != 1 || matches[0].ID != 7 {
		t.Errorf("matches = %+v, want a single match with ID 7", matches)
	}
}

func TestHandler_GetRecords_ByRegistryID(t *testing.T) {
	m := &mockAnchor{records: &anchor.GetRecordsResponse{
		Records:    []anchor.Record{{RecordID: 1, Checksum: "abc123"}},
		Pagination: &anchor.PageResponse{Total: 1},
	}}
	handler := makeGetRecordsHandler(m)

	regID := uint64(1)
	_, out, err := handler(ctx, nil, getRecordsInput{RegistryID: &regID})
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
		From:       testAddr,
		RegistryID: 1,
		Checksum:   "abc123",
		URI:        "https://example.com/doc.pdf",
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

func TestHandler_PrepareUpdateRecordStatus_Happy(t *testing.T) {
	m := &mockAnchor{unsignedTx: sampleUnsignedTx}
	handler := makePrepareUpdateRecordStatusHandler(m, testLogger())

	_, out, err := handler(ctx, nil, prepareUpdateRecordStatusInput{
		From:       testAddr,
		RegistryID: 1,
		RecordID:   7,
		Status:     "Superseded",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Gas != 63000 {
		t.Errorf("Gas = %d, want 63000", out.Gas)
	}
}

func TestHandler_PrepareUpdateRecordStatus_Error(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("status required")}
	handler := makePrepareUpdateRecordStatusHandler(m, testLogger())

	_, _, err := handler(ctx, nil, prepareUpdateRecordStatusInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHandler_PrepareRevokeRole_Happy(t *testing.T) {
	m := &mockAnchor{unsignedTx: sampleUnsignedTx}
	handler := makePrepareRevokeRoleHandler(m, testLogger())

	_, out, err := handler(ctx, nil, prepareRevokeRoleInput{
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

func TestHandler_PrepareRevokeRole_Error(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("role required")}
	handler := makePrepareRevokeRoleHandler(m, testLogger())

	_, _, err := handler(ctx, nil, prepareRevokeRoleInput{})
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
