// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// blockNumArg builds a numeric block-number input the way JSON decoding
// would, so handler tests stay terse.
func blockNumArg(n int64) blockNumberArg {
	return blockNumberArg{num: &n}
}

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
	// lastRegistriesReq records the most recent GetRegistries request so
	// tests can assert how tool inputs map onto the precompile call;
	// registriesReqs keeps every request in order for multi-call paths.
	lastRegistriesReq anchor.GetRegistriesRequest
	registriesReqs    []anchor.GetRegistriesRequest
	// byNameFn, when non-nil, serves GetRegistriesByName; otherwise the
	// mock answers like the chain does over the fixed `registries` rows
	// (see chainByName). byNameReqs keeps every request in order.
	byNameFn   func(req anchor.GetRegistriesByNameRequest) (*anchor.GetRegistriesResponse, error)
	byNameReqs []anchor.GetRegistriesByNameRequest
	records    *anchor.GetRecordsResponse
	unsignedTx *anchor.UnsignedTransaction
	// unsignedTxByMethod, when set, returns a distinct unsigned tx per
	// prepare method so a handler wired to the wrong Prepare* still fails.
	unsignedTxByMethod map[string]*anchor.UnsignedTransaction
	returnErr          error
}

func (m *mockAnchor) Info() anchor.PrecompileInfo { return m.info }
func (m *mockAnchor) Available() bool             { return m.info.ABILoaded }

// MethodSelector reports no ABI: these tests never assert on selectors.
func (m *mockAnchor) MethodSelector(string) (string, bool) { return "", false }
func (m *mockAnchor) GetRegistry(_ context.Context, _ anchor.GetRegistryRequest) (*anchor.Registry, error) {
	return m.registry, m.returnErr
}
func (m *mockAnchor) GetRegistries(_ context.Context, req anchor.GetRegistriesRequest) (*anchor.GetRegistriesResponse, error) {
	m.lastRegistriesReq = req
	m.registriesReqs = append(m.registriesReqs, req)
	m.registriesCallCount++
	// registry_id > 0 is the deprecated single-registry lookup. Keep it on
	// the fixed `registries` payload so listing mocks (pages / fn) can
	// coexist with a cursor-bearing by-id response in the same server.
	if req.RegistryID != nil && *req.RegistryID > 0 {
		return m.registries, m.returnErr
	}
	idx := m.registriesCallCount - 1
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

func (m *mockAnchor) GetRegistriesByName(
	_ context.Context, req anchor.GetRegistriesByNameRequest,
) (*anchor.GetRegistriesResponse, error) {
	m.byNameReqs = append(m.byNameReqs, req)
	if m.byNameFn != nil {
		return m.byNameFn(req)
	}
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	var regs []anchor.Registry
	if m.registries != nil {
		regs = m.registries.Registries
	}
	return chainByName(regs, req), nil
}

// chainByName answers a registriesByName request over regs the way the live
// precompile does (probed 2026-10-06, chain 262144): case-insensitive match,
// Offset/Limit window over the matches, at most registriesChainPageSize rows
// per call, and an empty pagination (no cursor, no count).
func chainByName(regs []anchor.Registry, req anchor.GetRegistriesByNameRequest) *anchor.GetRegistriesResponse {
	target := strings.ToLower(req.Name)
	var matches []anchor.Registry
	for _, r := range regs {
		name := strings.ToLower(r.Name)
		var ok bool
		switch req.Match {
		case anchor.RegistryNameExact:
			ok = name == target
		case anchor.RegistryNamePrefix:
			ok = strings.HasPrefix(name, target)
		case anchor.RegistryNameSuffix:
			ok = strings.HasSuffix(name, target)
		case anchor.RegistryNameContains:
			ok = strings.Contains(name, target)
		}
		if ok {
			matches = append(matches, r)
		}
	}
	var offset, limit uint64 = 0, 50
	if p := req.Pagination; p != nil {
		offset = p.Offset
		if p.Limit > 0 {
			limit = min(p.Limit, registriesChainPageSize)
		}
	}
	n := uint64(len(matches))
	start := min(offset, n)
	end := min(start+limit, n)
	return &anchor.GetRegistriesResponse{
		Registries: matches[start:end],
		Pagination: &anchor.PageResponse{},
	}
}

// newChainMock returns a mockAnchor that serves regs the way the live
// precompile does for registryId=0 (probed 2026-09-14): Offset or Key
// positions the page, Limit bounds it, Reverse walks from the end, the
// cursor is the 8-byte big-endian index of the next row and is set only
// when rows remain, and pagination.total is whatever chainTotal says (the
// live chain reports 0). Use it for tests of the direct listing path,
// where the request parameters are the behavior under test.
func newChainMock(regs []anchor.Registry) *mockAnchor {
	return newChainMockWithTotal(regs, 0)
}

// newChainMockWithTotal is newChainMock for a chain whose countTotal works.
func newChainMockWithTotal(regs []anchor.Registry, chainTotal uint64) *mockAnchor {
	m := &mockAnchor{}
	m.registriesFn = func(int) (*anchor.GetRegistriesResponse, error) {
		p := m.lastRegistriesReq.Pagination
		if p == nil {
			p = &anchor.PageRequest{Limit: 100}
		}
		n := uint64(len(regs))
		resp := &anchor.GetRegistriesResponse{Pagination: &anchor.PageResponse{Total: chainTotal}}
		if p.Reverse {
			if n > 0 {
				resp.Registries = []anchor.Registry{regs[n-1]}
			}
			return resp, nil
		}
		start := p.Offset
		if len(p.Key) > 0 {
			start = binary.BigEndian.Uint64(p.Key)
		}
		start = min(start, n)
		end := min(start+p.Limit, n)
		resp.Registries = regs[start:end]
		if end < n {
			resp.Pagination.NextKey = chainCursor(end)
		}
		return resp, nil
	}
	return m
}

// chainCursor renders a row index the way the precompile renders next_key.
func chainCursor(index uint64) string {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, index)
	return anchor.EncodeCursor(b)
}

func (m *mockAnchor) GetRecords(_ context.Context, _ anchor.GetRecordsRequest) (*anchor.GetRecordsResponse, error) {
	return m.records, m.returnErr
}

func (m *mockAnchor) prepareTx(method string) (*anchor.UnsignedTransaction, error) {
	if m.unsignedTxByMethod != nil {
		if tx, ok := m.unsignedTxByMethod[method]; ok {
			return tx, m.returnErr
		}
	}
	return m.unsignedTx, m.returnErr
}

func (m *mockAnchor) PrepareAddRegistry(_ context.Context, _ anchor.PrepareAddRegistryRequest) (*anchor.UnsignedTransaction, error) {
	return m.prepareTx("addRegistry")
}
func (m *mockAnchor) PrepareAddRecord(_ context.Context, _ anchor.PrepareAddRecordRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
	return m.prepareTx("addRecord")
}
func (m *mockAnchor) PrepareUpdateRecordStatus(
	_ context.Context, _ anchor.PrepareUpdateRecordStatusRequest,
) (*anchor.UnsignedTransaction, error) {
	return m.prepareTx("updateRecordStatus")
}
func (m *mockAnchor) PrepareGrantRole(_ context.Context, _ anchor.PrepareGrantRoleRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
	return m.prepareTx("grantRole")
}
func (m *mockAnchor) PrepareRevokeRole(_ context.Context, _ anchor.PrepareRevokeRoleRequest) (*anchor.UnsignedTransaction, error) { //nolint:gocritic // interface conformance requires value receiver
	return m.prepareTx("revokeRole")
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

	_, out, err := handler(ctx, nil, getBlockInput{BlockNumber: blockNumArg(42)})
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

// TestHandler_GetBlock_BothNumberAndHashRejected pins the fail-fast contract
// for the two lookup modes: supplying both used to silently prefer the hash
// and discard the number (tool-description audit, D3).
func TestHandler_GetBlock_BothNumberAndHashRejected(t *testing.T) {
	handler := makeGetBlockHandler(&mockEVM{})

	hash := testTxHash
	_, _, err := handler(ctx, nil, getBlockInput{
		BlockNumber: blockNumArg(42),
		BlockHash:   &hash,
	})
	if err == nil {
		t.Fatal("expected error when both block_number and block_hash are set")
	}
	if !errors.Is(err, apperrors.ErrInvalidBlockRef) {
		t.Errorf("error = %v, want ErrInvalidBlockRef", err)
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
	// A block hash is not a transaction hash; the message must not say it
	// is (directory review 2026-09-15, L-1).
	if !errors.Is(err, apperrors.ErrInvalidHash) {
		t.Errorf("error = %v, want ErrInvalidHash", err)
	}
	if errors.Is(err, apperrors.ErrInvalidTxHash) || strings.Contains(err.Error(), "transaction hash") {
		t.Errorf("block-hash error must not be reported as a transaction-hash error: %v", err)
	}
	if !apperrors.IsInputError(err) {
		t.Errorf("must be an input-class error: %v", err)
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

func TestHandler_GetReceipt_NotFound_MessageNotDoubled(t *testing.T) {
	handler := makeGetReceiptHandler(&mockEVM{returnErr: apperrors.ErrTxNotFound})

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

func TestHandler_GetReceipt_DeadlineIsNotFound(t *testing.T) {
	handler := makeGetReceiptHandler(&mockEVM{returnErr: context.DeadlineExceeded})

	_, _, err := handler(ctx, nil, txHashInput{TxHash: testTxHash})
	if !errors.Is(err, apperrors.ErrTxNotFound) {
		t.Fatalf("deadline should surface as ErrTxNotFound; got %v", err)
	}
	if got, want := err.Error(), apperrors.ErrTxNotFound.Error(); got != want {
		t.Errorf("client-visible message = %q, want %q", got, want)
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
	handler := makeGetBalanceHandler(m, testServerConfig(false))

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
	handler := makeGetBalanceHandler(m, testServerConfig(false))

	_, out, err := handler(ctx, nil, getBalanceInput{Address: testAddr, BlockNum: blockNumArg(50)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Wei != "500" {
		t.Errorf("Wei = %q, want %q", out.Wei, "500")
	}
}

func TestHandler_GetBalance_InvalidAddress(t *testing.T) {
	handler := makeGetBalanceHandler(&mockEVM{}, testServerConfig(false))

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
	handler := makeGetCodeHandler(m, testServerConfig(false))

	_, out, err := handler(ctx, nil, getCodeInput{Address: testAddr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.IsContract {
		t.Error("IsContract = false, want true")
	}
}

func TestHandler_GetCode_InvalidAddress(t *testing.T) {
	handler := makeGetCodeHandler(&mockEVM{}, testServerConfig(false))

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
	_, out, err := handler(ctx, nil, getLogsInput{
		Address:   &addr,
		FromBlock: blockNumArg(1),
		ToBlock:   blockNumArg(100),
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

	_, out, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xab", BlockNum: blockNumArg(42)})
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
		t.Fatal("expected error when id is zero")
	}
	// id=0 is present but invalid (IDs start at 1); an absent id is already
	// a schema rejection, so "missing" was the wrong word (M-2).
	if !errors.Is(err, apperrors.ErrInvalidRegistryID) {
		t.Errorf("error = %v, want ErrInvalidRegistryID", err)
	}
	if !strings.Contains(err.Error(), "1 or greater") {
		t.Errorf("error must state the valid range: %v", err)
	}
}

// TestHandler_GetRegistries_NoFilter proves the unfiltered listing is a
// single direct page fetch, not a table walk: one precompile call carrying
// the caller's offset and limit. The whole 2-row table fits the page, so
// total is exact from the page alone, no peek, and no next_key.
func TestHandler_GetRegistries_NoFilter(t *testing.T) {
	m := newChainMock([]anchor.Registry{{ID: 1}, {ID: 2}})
	handler := makeGetRegistriesHandler(m)

	_, out, err := handler(ctx, nil, getRegistriesInput{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 2 {
		t.Errorf("len(Registries) = %d, want 2", len(out.Registries))
	}
	if out.Pagination == nil || out.Pagination.Total != 2 || out.Pagination.NextKey != "" {
		t.Errorf("Pagination = %+v, want Total=2 and no next_key (table ended)", out.Pagination)
	}
	if out.TotalIsLowerBound {
		t.Error("TotalIsLowerBound = true, want false (page reached the end of the table)")
	}
	if m.registriesCallCount != 1 {
		t.Errorf("registriesCallCount = %d, want 1 (no peek, no walk)", m.registriesCallCount)
	}
	// RegistryID=&0 (not nil) is the unambiguous "no ID filter" sentinel.
	if m.lastRegistriesReq.RegistryID == nil || *m.lastRegistriesReq.RegistryID != 0 {
		t.Errorf("last RegistryID = %v, want ptr-to-0 (unfiltered listing)", m.lastRegistriesReq.RegistryID)
	}
	// The caller's window is what the chain is asked for: offset 0 and the
	// default page size, cursor-free.
	page := m.lastRegistriesReq.Pagination
	if page == nil {
		t.Fatal("Pagination = nil")
	}
	if page.Limit != defaultRegistriesPageSize || page.Offset != 0 || len(page.Key) != 0 {
		t.Errorf("precompile page = %+v, want Offset=0 Limit=%d no Key", page, defaultRegistriesPageSize)
	}
}

// TestHandler_GetRegistries_WithPagination proves the caller's offset and
// limit are forwarded to the chain as-is, and that a mid-table page reports
// the table size (from the reverse peek, since this chain counts nothing)
// plus the chain's cursor -- never offset + rows as "total".
func TestHandler_GetRegistries_WithPagination(t *testing.T) {
	regs := make([]anchor.Registry, 10)
	for i := range regs {
		regs[i] = anchor.Registry{ID: uint64(i + 1)}
	}
	m := newChainMock(regs)
	handler := makeGetRegistriesHandler(m)

	offset := uint64(4)
	limit := uint64(3)
	_, out, err := handler(ctx, nil, getRegistriesInput{Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 3 || out.Registries[0].ID != 5 || out.Registries[2].ID != 7 {
		t.Errorf("Registries = %+v, want IDs [5 6 7]", out.Registries)
	}
	if out.Pagination == nil || out.Pagination.Total != 10 {
		t.Errorf("Pagination.Total = %v, want 10 (table size, not offset+rows)", out.Pagination)
	}
	if out.Pagination.NextKey != chainCursor(7) {
		t.Errorf("next_key = %q, want the chain's cursor for row 7", out.Pagination.NextKey)
	}
	if out.TotalIsLowerBound {
		t.Error("TotalIsLowerBound = true, want false (peek supplied the total)")
	}
	if m.registriesCallCount != 2 {
		t.Errorf("registriesCallCount = %d, want 2 (page + reverse peek)", m.registriesCallCount)
	}
	// The peek is the last call: reverse=true limit=1.
	if p := m.lastRegistriesReq.Pagination; p == nil || !p.Reverse || p.Limit != 1 {
		t.Errorf("last call = %+v, want the reverse peek (Reverse=true Limit=1)", p)
	}
}

// TestHandler_GetRegistries_ByCursor proves key positions the page: the
// chain is asked with Key (offset untouched), rows start where the cursor
// says, and the response carries the next cursor.
func TestHandler_GetRegistries_ByCursor(t *testing.T) {
	regs := make([]anchor.Registry, 10)
	for i := range regs {
		regs[i] = anchor.Registry{ID: uint64(i + 1)}
	}
	m := newChainMock(regs)
	handler := makeGetRegistriesHandler(m)

	limit := uint64(3)
	_, out, err := handler(ctx, nil, getRegistriesInput{Key: chainCursor(4), Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 3 || out.Registries[0].ID != 5 || out.Registries[2].ID != 7 {
		t.Errorf("Registries = %+v, want IDs [5 6 7]", out.Registries)
	}
	if out.Pagination == nil || out.Pagination.Total != 10 || out.Pagination.NextKey != chainCursor(7) {
		t.Errorf("Pagination = %+v, want Total=10 next_key=%q", out.Pagination, chainCursor(7))
	}
	if out.TotalIsLowerBound {
		t.Error("TotalIsLowerBound = true, want false")
	}
	// First call carried the cursor and no offset.
	first := m.registriesReqs[0].Pagination
	if first == nil || !bytes.Equal(first.Key, mustCursorBytes(t, chainCursor(4))) || first.Offset != 0 {
		t.Errorf("first precompile page = %+v, want Key=cursor(4) Offset=0", first)
	}
}

// TestHandler_GetRegistries_CursorLastPage proves a cursor page that ends
// the table still resolves total through the peek (a cursor page does not
// know its own offset) and reports no next_key.
func TestHandler_GetRegistries_CursorLastPage(t *testing.T) {
	regs := []anchor.Registry{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}, {ID: 5}}
	m := newChainMock(regs)
	handler := makeGetRegistriesHandler(m)

	limit := uint64(10)
	_, out, err := handler(ctx, nil, getRegistriesInput{Key: chainCursor(3), Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 2 || out.Registries[0].ID != 4 {
		t.Errorf("Registries = %+v, want IDs [4 5]", out.Registries)
	}
	if out.Pagination == nil || out.Pagination.Total != 5 || out.Pagination.NextKey != "" || out.TotalIsLowerBound {
		t.Errorf("Pagination = %+v lower_bound=%v, want Total=5 exact, no next_key",
			out.Pagination, out.TotalIsLowerBound)
	}
	if m.registriesCallCount != 2 {
		t.Errorf("registriesCallCount = %d, want 2 (page + peek: a cursor page cannot use offset+rows)",
			m.registriesCallCount)
	}
}

// TestHandler_GetRegistries_CursorRejections covers every way a key can be
// unusable: combined with a non-zero offset (the SDK rejects both), with a
// name filter (different table), with registry_id, or not base64. All are
// rejected before any chain call.
func TestHandler_GetRegistries_CursorRejections(t *testing.T) {
	offset := uint64(3)
	zero := uint64(0)
	name := "anything"
	regID := uint64(1)
	garbage := "not!base64"
	tests := []struct {
		name  string
		input getRegistriesInput
		want  error
	}{
		{"key with non-zero offset", getRegistriesInput{Key: chainCursor(4), Offset: &offset}, apperrors.ErrInvalidCursor},
		{"key with name", getRegistriesInput{Key: chainCursor(4), Name: &name}, apperrors.ErrInvalidCursor},
		{"key with registry_id", getRegistriesInput{Key: chainCursor(4), RegistryID: &regID},
			apperrors.ErrInvalidFilterCombination},
		{"key not base64", getRegistriesInput{Key: garbage}, apperrors.ErrInvalidCursor},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newChainMock([]anchor.Registry{{ID: 1}})
			handler := makeGetRegistriesHandler(m)

			_, _, err := handler(ctx, nil, tc.input)
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
			if m.registriesCallCount != 0 {
				t.Errorf("registriesCallCount = %d, want 0 (rejected before any chain call)", m.registriesCallCount)
			}
		})
	}
	// key with an explicit zero offset is fine: zero is "not positioned".
	m := newChainMock([]anchor.Registry{{ID: 1}, {ID: 2}})
	handler := makeGetRegistriesHandler(m)
	if _, _, err := handler(ctx, nil, getRegistriesInput{Key: chainCursor(1), Offset: &zero}); err != nil {
		t.Errorf("key + offset=0: unexpected error %v", err)
	}
}

// TestHandler_GetRegistries_ListingDefaultsPagination covers the offset/limit
// defaults on the direct-fetch path against a 3-row chain, and that total
// is the table size in every case -- including a page that is smaller than
// the table and an offset past its end.
func TestHandler_GetRegistries_ListingDefaultsPagination(t *testing.T) {
	zero := uint64(0)
	two := uint64(2)
	five := uint64(5)
	regIDZero := uint64(0)

	chain := []anchor.Registry{{ID: 1}, {ID: 2}, {ID: 3}}

	tests := []struct {
		name         string
		input        getRegistriesInput
		wantOffset   uint64 // forwarded to the precompile on the first call
		wantLimit    uint64 // forwarded to the precompile on the first call
		wantRegCount int
		wantNextKey  string
		wantCalls    int
	}{
		{"nothing supplied", getRegistriesInput{}, 0, defaultRegistriesPageSize, 3, "", 1},
		{"offset within range", getRegistriesInput{Offset: &two}, 2, defaultRegistriesPageSize, 1, "", 1},
		{"limit below table", getRegistriesInput{Limit: &two}, 0, 2, 2, chainCursor(2), 2},
		{"explicit zero limit defaults to page size",
			getRegistriesInput{Offset: &two, Limit: &zero}, 2, defaultRegistriesPageSize, 1, "", 1},
		{"offset past end", getRegistriesInput{Offset: &five, Limit: &two}, 5, 2, 0, "", 2},
		{"registry_id zero treated as unfiltered listing",
			getRegistriesInput{RegistryID: &regIDZero}, 0, defaultRegistriesPageSize, 3, "", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newChainMock(chain)
			handler := makeGetRegistriesHandler(m)

			_, out, err := handler(ctx, nil, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out.Registries) != tc.wantRegCount {
				t.Errorf("len(Registries) = %d, want %d", len(out.Registries), tc.wantRegCount)
			}
			if out.Pagination == nil || out.Pagination.Total != 3 {
				t.Errorf("Pagination.Total = %v, want 3 (the table size)", out.Pagination)
			}
			if out.Pagination.NextKey != tc.wantNextKey {
				t.Errorf("next_key = %q, want %q", out.Pagination.NextKey, tc.wantNextKey)
			}
			if out.TotalIsLowerBound {
				t.Error("TotalIsLowerBound = true, want false")
			}
			if m.registriesCallCount != tc.wantCalls {
				t.Errorf("registriesCallCount = %d, want %d", m.registriesCallCount, tc.wantCalls)
			}
			page := m.registriesReqs[0].Pagination
			if page == nil {
				t.Fatal("Pagination = nil on first GetRegistries call")
			}
			if page.Offset != tc.wantOffset || page.Limit != tc.wantLimit {
				t.Errorf("precompile page = %+v, want Offset=%d Limit=%d", page, tc.wantOffset, tc.wantLimit)
			}
		})
	}
}

// TestHandler_GetRegistries_ChainTotalPreferred proves that when the chain
// does report a count, it is used as-is and no peek is spent.
func TestHandler_GetRegistries_ChainTotalPreferred(t *testing.T) {
	regs := make([]anchor.Registry, 10)
	for i := range regs {
		regs[i] = anchor.Registry{ID: uint64(i + 1)}
	}
	m := newChainMockWithTotal(regs, 10)
	handler := makeGetRegistriesHandler(m)

	limit := uint64(3)
	_, out, err := handler(ctx, nil, getRegistriesInput{Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Pagination == nil || out.Pagination.Total != 10 || out.TotalIsLowerBound {
		t.Errorf("Pagination = %+v lower_bound=%v, want Total=10 exact from the chain",
			out.Pagination, out.TotalIsLowerBound)
	}
	if m.registriesCallCount != 1 {
		t.Errorf("registriesCallCount = %d, want 1 (chain counted; no peek)", m.registriesCallCount)
	}
}

// TestHandler_GetRegistries_EmptyTable proves an empty chain yields an
// exact total of 0: the page is empty and the peek finds nothing.
func TestHandler_GetRegistries_EmptyTable(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}
	handler := makeGetRegistriesHandler(m)

	offset := uint64(5000)
	_, out, err := handler(ctx, nil, getRegistriesInput{Offset: &offset})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 0 {
		t.Errorf("len(Registries) = %d, want 0", len(out.Registries))
	}
	if out.Pagination == nil || out.Pagination.Total != 0 || out.TotalIsLowerBound {
		t.Errorf("Pagination = %+v, TotalIsLowerBound = %v; want Total=0 exact",
			out.Pagination, out.TotalIsLowerBound)
	}
}

// TestHandler_GetRegistries_PeekFailureIsLowerBound proves a failed reverse
// peek does not fail the listing: the page still returns, total falls back
// to the rows confirmed so far, and total_is_lower_bound says so.
func TestHandler_GetRegistries_PeekFailureIsLowerBound(t *testing.T) {
	regs := make([]anchor.Registry, 10)
	for i := range regs {
		regs[i] = anchor.Registry{ID: uint64(i + 1)}
	}
	inner := newChainMock(regs)
	m := &mockAnchor{}
	m.registriesFn = func(i int) (*anchor.GetRegistriesResponse, error) {
		if p := m.lastRegistriesReq.Pagination; p != nil && p.Reverse {
			return nil, errors.New("peek down")
		}
		inner.lastRegistriesReq = m.lastRegistriesReq
		return inner.registriesFn(i)
	}
	handler := makeGetRegistriesHandler(m)

	offset := uint64(4)
	limit := uint64(3)
	_, out, err := handler(ctx, nil, getRegistriesInput{Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 3 {
		t.Errorf("len(Registries) = %d, want 3", len(out.Registries))
	}
	if out.Pagination == nil || out.Pagination.Total != 7 || !out.TotalIsLowerBound {
		t.Errorf("Pagination = %+v, TotalIsLowerBound = %v; want Total=7 (offset+rows) as a floor",
			out.Pagination, out.TotalIsLowerBound)
	}
	if out.Pagination.NextKey != chainCursor(7) {
		t.Errorf("next_key = %q, want %q (cursor survives a failed peek)", out.Pagination.NextKey, chainCursor(7))
	}
}

// TestHandler_GetRegistries_LimitSpansChainPages proves a caller limit above
// the chain page cap is served in successive fetches: the first asks for the
// cap, the next continues from the chain's cursor and asks only for what is
// still missing, and the exact total is reported when the last page ends the
// table.
func TestHandler_GetRegistries_LimitSpansChainPages(t *testing.T) {
	chain := make([]anchor.Registry, registriesChainPageSize+2)
	for i := range chain {
		chain[i] = anchor.Registry{ID: uint64(i + 1)}
	}
	m := newChainMock(chain)
	handler := makeGetRegistriesHandler(m)

	limit := uint64(registriesChainPageSize + 50)
	_, out, err := handler(ctx, nil, getRegistriesInput{Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != registriesChainPageSize+2 {
		t.Errorf("len(Registries) = %d, want %d", len(out.Registries), registriesChainPageSize+2)
	}
	if out.Pagination == nil || out.Pagination.Total != uint64(registriesChainPageSize+2) ||
		out.Pagination.NextKey != "" || out.TotalIsLowerBound {
		t.Errorf("Pagination = %+v, TotalIsLowerBound = %v; want exact total %d, no next_key",
			out.Pagination, out.TotalIsLowerBound, registriesChainPageSize+2)
	}
	if m.registriesCallCount != 2 {
		t.Errorf("registriesCallCount = %d, want 2", m.registriesCallCount)
	}
	// The second fetch resumes from the chain's cursor (not a recomputed
	// offset) and asks only for the 50 rows still missing.
	p := m.lastRegistriesReq.Pagination
	if !bytes.Equal(p.Key, mustCursorBytes(t, chainCursor(registriesChainPageSize))) || p.Offset != 0 || p.Limit != 50 {
		t.Errorf("second fetch = %+v, want Key=cursor(%d) Offset=0 Limit=50", p, registriesChainPageSize)
	}
}

// TestHandler_GetRegistries_ListingPropagatesError proves an upstream failure
// on the direct fetch surfaces as-is rather than as an empty page.
func TestHandler_GetRegistries_ListingPropagatesError(t *testing.T) {
	m := &mockAnchor{returnErr: errors.New("rpc down")}
	handler := makeGetRegistriesHandler(m)

	_, _, err := handler(ctx, nil, getRegistriesInput{})
	if err == nil || err.Error() != "rpc down" {
		t.Fatalf("err = %v, want rpc down", err)
	}
}

// mustCursorBytes decodes a base64 next_key for request assertions.
func mustCursorBytes(t *testing.T, key string) []byte {
	t.Helper()
	b, err := (&anchor.PageResponse{NextKey: key}).CursorBytes()
	if err != nil {
		t.Fatalf("decode cursor %q: %v", key, err)
	}
	return b
}

// TestHandler_GetRegistries_ByName_DefaultsPagination proves the name-filtered
// branch shares the listing defaults -- the window it applies to the match set
// comes from the same resolution as a chain-side page.
func TestHandler_GetRegistries_ByName_DefaultsPagination(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{
			{ID: 1, Name: "fund-documents"},
			{ID: 2, Name: "audit-reports"},
			{ID: 3, Name: "fund-documents"},
		},
	}}
	handler := makeGetRegistriesHandler(m)

	name := "fund-documents"
	_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 2 {
		t.Errorf("len(Registries) = %d, want 2 (both matches, inside the default page)", len(out.Registries))
	}
}

func TestResolveRegistriesPage(t *testing.T) {
	zero := uint64(0)
	seven := uint64(7)
	twenty := uint64(20)
	tests := []struct {
		name       string
		offset     *uint64
		limit      *uint64
		wantOffset uint64
		wantLimit  uint64
	}{
		{"both omitted", nil, nil, 0, defaultRegistriesPageSize},
		{"both supplied", &seven, &twenty, 7, 20},
		{"zero offset", &zero, &twenty, 0, 20},
		{"zero limit falls back to default", &seven, &zero, 7, defaultRegistriesPageSize},
		{"offset only", &seven, nil, 7, defaultRegistriesPageSize},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotOffset, gotLimit := resolveRegistriesPage(tc.offset, tc.limit)
			if gotOffset != tc.wantOffset || gotLimit != tc.wantLimit {
				t.Errorf("resolveRegistriesPage() = (%d, %d), want (%d, %d)",
					gotOffset, gotLimit, tc.wantOffset, tc.wantLimit)
			}
		})
	}
}

// TestHandler_GetRegistries_DeprecatedRegistryID covers the deprecated
// single-registry mode: it still works, needs no pagination, and accepts
// explicit zeros for the listing bounds it ignores.
func TestHandler_GetRegistries_DeprecatedRegistryID(t *testing.T) {
	zero := uint64(0)
	regID := uint64(7)
	tests := []struct {
		name  string
		input getRegistriesInput
	}{
		{"id alone", getRegistriesInput{RegistryID: &regID}},
		{"id with zero pagination", getRegistriesInput{RegistryID: &regID, Offset: &zero, Limit: &zero}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
				Registries: []anchor.Registry{{ID: 7, Name: "fund-documents"}},
			}}
			handler := makeGetRegistriesHandler(m)

			_, out, err := handler(ctx, nil, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out.Registries) != 1 || out.Registries[0].ID != 7 {
				t.Fatalf("Registries = %+v, want the single registry 7", out.Registries)
			}
			got := m.lastRegistriesReq
			if got.RegistryID == nil || *got.RegistryID != 7 {
				t.Errorf("RegistryID = %v, want 7", got.RegistryID)
			}
			if got.Pagination != nil {
				t.Errorf("Pagination = %+v, want nil (nothing to page)", got.Pagination)
			}
		})
	}
}

// TestHandler_GetRegistries_ByName_ReturnsAllMatches guards W3's core
// requirement: registry names are caller-supplied and not unique, so a
// by-name lookup must return every match, never auto-select one.
func TestHandler_GetRegistries_ByName_ReturnsAllMatches(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{
			{ID: 1, Name: "fund-documents", Creator: "nvnm1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5uyxmzt"},
			{ID: 2, Name: "audit-reports", Creator: "nvnm12r28dewjcpzfnrkpshvx5rh4eve086858qcn7n"},
			{ID: 3, Name: "fund-documents", Creator: "nvnm1zg69v7ys40x77y352eufp27daufrg4ncs5286h"}, // pragma: allowlist secret -- public on-chain address, test fixture
		},
	}}
	handler := makeGetRegistriesHandler(m)

	name := "fund-documents"
	offset := uint64(0)
	limit := uint64(10)
	_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name, Offset: &offset, Limit: &limit})
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
	// total is the full match count, not the chain's (always 0) report, so
	// a caller can tell it has seen every match.
	if out.Pagination == nil || out.Pagination.Total != 2 {
		t.Errorf("Pagination = %+v, want Total = 2 (the match count)", out.Pagination)
	}
}

// TestHandler_GetRegistries_ByName_PagesOverMatchSet proves offset/limit
// window the match set on chain, and how pagination.total reads without a
// chain count: exact once a page comes back short, a lower bound while a
// page comes back full or an offset lands past the end.
func TestHandler_GetRegistries_ByName_PagesOverMatchSet(t *testing.T) {
	registries := []anchor.Registry{
		{ID: 1, Name: "fund-documents"},
		{ID: 2, Name: "audit-reports"},
		{ID: 3, Name: "fund-documents"},
		{ID: 4, Name: "fund-documents"},
	}
	tests := []struct {
		name           string
		offset         uint64
		limit          uint64
		wantIDs        []uint64
		wantTotal      uint64
		wantLowerBound bool
	}{
		{"full first page", 0, 2, []uint64{1, 3}, 2, true},
		{"short second page", 2, 2, []uint64{4}, 3, false},
		{"limit past the end", 0, 100, []uint64{1, 3, 4}, 3, false},
		{"exactly full last page", 1, 2, []uint64{3, 4}, 3, true},
		{"offset past the end", 3, 2, nil, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAnchor{registries: &anchor.GetRegistriesResponse{Registries: registries}}
			handler := makeGetRegistriesHandler(m)

			name := "fund-documents"
			_, out, err := handler(ctx, nil, getRegistriesInput{
				Name: &name, Offset: &tc.offset, Limit: &tc.limit,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Registries == nil {
				t.Error("Registries = nil, want [] on the wire")
			}
			if len(out.Registries) != len(tc.wantIDs) {
				t.Fatalf("Registries = %+v, want IDs %v", out.Registries, tc.wantIDs)
			}
			for i, id := range tc.wantIDs {
				if out.Registries[i].ID != id {
					t.Errorf("Registries[%d].ID = %d, want %d", i, out.Registries[i].ID, id)
				}
			}
			if out.Pagination == nil || out.Pagination.Total != tc.wantTotal || out.Pagination.NextKey != "" {
				t.Errorf("Pagination = %+v, want Total = %d and no next_key", out.Pagination, tc.wantTotal)
			}
			if out.TotalIsLowerBound != tc.wantLowerBound || out.NameMatchTruncated != tc.wantLowerBound {
				t.Errorf("TotalIsLowerBound/NameMatchTruncated = %v/%v, want %v",
					out.TotalIsLowerBound, out.NameMatchTruncated, tc.wantLowerBound)
			}
			// Matches exist, so even a window that lands past the end must
			// not advise the caller that nothing matched.
			for _, na := range out.NextActions {
				if na.Tool == "anchor_prepare_add_registry" {
					t.Error("next_actions suggests creating a registry even though matches exist")
				}
			}
		})
	}
}

// TestHandler_GetRegistries_ByName_NoMatchAdvisesCreate proves an empty
// first page -- the one shape that does prove nothing matches -- still
// points the caller at creating the registry.
func TestHandler_GetRegistries_ByName_NoMatchAdvisesCreate(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{
		Registries: []anchor.Registry{{ID: 1, Name: "audit-reports"}},
	}}
	handler := makeGetRegistriesHandler(m)

	name := "fund-documents"
	_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Registries) != 0 || out.Pagination == nil || out.Pagination.Total != 0 || out.TotalIsLowerBound {
		t.Errorf("out = %+v, want an exact empty page", out)
	}
	advised := false
	for _, na := range out.NextActions {
		advised = advised || na.Tool == "anchor_prepare_add_registry"
	}
	if !advised {
		t.Errorf("next_actions = %+v, want anchor_prepare_add_registry", out.NextActions)
	}
}

// TestHandler_GetRegistries_ByName_ForwardsToChain proves the tool's
// name/match/offset/limit reach the by-name query as given -- the chain does
// the matching, so the mode mapping is the behavior under test.
func TestHandler_GetRegistries_ByName_ForwardsToChain(t *testing.T) {
	tests := []struct {
		match string
		want  anchor.RegistryNameMatch
	}{
		{"", anchor.RegistryNameExact},
		{"exact", anchor.RegistryNameExact},
		{"prefix", anchor.RegistryNamePrefix},
		{"suffix", anchor.RegistryNameSuffix},
		{"contains", anchor.RegistryNameContains},
	}
	for _, tc := range tests {
		t.Run("match="+tc.match, func(t *testing.T) {
			m := &mockAnchor{}
			handler := makeGetRegistriesHandler(m)

			name := "Fund-Docs"
			offset, limit := uint64(7), uint64(20)
			_, _, err := handler(ctx, nil, getRegistriesInput{
				Name: &name, Match: tc.match, Offset: &offset, Limit: &limit,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(m.byNameReqs) != 1 {
				t.Fatalf("by-name calls = %d, want 1", len(m.byNameReqs))
			}
			got := m.byNameReqs[0]
			if got.Name != name || got.Match != tc.want ||
				got.Pagination == nil || got.Pagination.Offset != 7 || got.Pagination.Limit != 20 {
				t.Errorf("request = %+v (pagination %+v), want name=%q match=%d offset=7 limit=20",
					got, got.Pagination, name, tc.want)
			}
			if m.registriesCallCount != 0 {
				t.Errorf("registries calls = %d, want 0 (no table walk)", m.registriesCallCount)
			}
		})
	}
}

// TestHandler_GetRegistries_ByName_ChainsFetchesPastPageCap proves a limit
// above the chain's per-call cap is served by consecutive offset-positioned
// fetches, stopping at the first short one.
func TestHandler_GetRegistries_ByName_ChainsFetchesPastPageCap(t *testing.T) {
	regs := make([]anchor.Registry, registriesChainPageSize+30)
	for i := range regs {
		regs[i] = anchor.Registry{ID: uint64(i + 1), Name: "fund-documents"}
	}
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{Registries: regs}}
	handler := makeGetRegistriesHandler(m)

	name := "fund-documents"
	offset, limit := uint64(5), uint64(registriesChainPageSize+100)
	_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name, Offset: &offset, Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantRows := len(regs) - 5
	if len(out.Registries) != wantRows || out.Registries[0].ID != 6 {
		t.Fatalf("got %d rows starting at %d, want %d starting at 6", len(out.Registries),
			out.Registries[0].ID, wantRows)
	}
	if out.Pagination.Total != uint64(len(regs)) || out.TotalIsLowerBound {
		t.Errorf("total = %d (lower bound %v), want exact %d", out.Pagination.Total, out.TotalIsLowerBound, len(regs))
	}
	if len(m.byNameReqs) != 2 {
		t.Fatalf("by-name calls = %d, want 2", len(m.byNameReqs))
	}
	second := m.byNameReqs[1].Pagination
	if second.Offset != 5+registriesChainPageSize || second.Limit != 100 {
		t.Errorf("second fetch = %+v, want offset %d limit 100", second, 5+registriesChainPageSize)
	}
}

// TestHandler_GetRegistries_ByName_StopsAtFetchBackstop proves a chain that
// keeps answering full pages cannot hold the request open: the walk stops at
// maxRegistriesChainPages and reports the total as a lower bound.
func TestHandler_GetRegistries_ByName_StopsAtFetchBackstop(t *testing.T) {
	m := &mockAnchor{byNameFn: func(req anchor.GetRegistriesByNameRequest) (*anchor.GetRegistriesResponse, error) {
		return &anchor.GetRegistriesResponse{Registries: make([]anchor.Registry, req.Pagination.Limit)}, nil
	}}
	handler := makeGetRegistriesHandler(m)

	name := "anything"
	limit := uint64(math.MaxUint64)
	_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name, Limit: &limit})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.byNameReqs) != maxRegistriesChainPages {
		t.Errorf("by-name calls = %d, want %d", len(m.byNameReqs), maxRegistriesChainPages)
	}
	if !out.TotalIsLowerBound || !out.NameMatchTruncated {
		t.Error("want total flagged as a lower bound after the backstop")
	}
}

// TestHandler_GetRegistries_ByName_PropagatesError proves a chain failure --
// including a node that predates registriesByName -- surfaces as-is rather
// than as an empty match set.
func TestHandler_GetRegistries_ByName_PropagatesError(t *testing.T) {
	for _, want := range []error{errors.New("rpc down"), apperrors.ErrRegistriesByNameUnsupported} {
		t.Run(want.Error(), func(t *testing.T) {
			m := &mockAnchor{returnErr: want}
			handler := makeGetRegistriesHandler(m)

			name := "fund-documents"
			_, _, err := handler(ctx, nil, getRegistriesInput{Name: &name})
			if !errors.Is(err, want) {
				t.Fatalf("err = %v, want %v", err, want)
			}
		})
	}
}

// TestHandler_GetRegistries_ByName_RejectsKey proves the unfiltered
// listing's cursor is refused alongside a name filter, before any chain call.
func TestHandler_GetRegistries_ByName_RejectsKey(t *testing.T) {
	m := &mockAnchor{}
	handler := makeGetRegistriesHandler(m)

	name := "fund-documents"
	_, _, err := handler(ctx, nil, getRegistriesInput{Name: &name, Key: chainCursor(1)})
	if !errors.Is(err, apperrors.ErrInvalidCursor) {
		t.Fatalf("err = %v, want ErrInvalidCursor", err)
	}
	if len(m.byNameReqs) != 0 {
		t.Errorf("by-name calls = %d, want 0", len(m.byNameReqs))
	}
}

func TestHandler_GetRegistries_ByName_InvalidMatchMode(t *testing.T) {
	m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}
	handler := makeGetRegistriesHandler(m)

	name := "anything"
	_, _, err := handler(ctx, nil, getRegistriesInput{Name: &name, Match: "fuzzy"})
	if !errors.Is(err, apperrors.ErrInvalidMatchMode) {
		t.Errorf("error = %v, want ErrInvalidMatchMode", err)
	}
}

// TestHandler_GetRegistries_RegistryIDCombinedWithListingParamsErrors covers
// every listing parameter the deprecated registry_id lookup is mutually
// exclusive with -- one table instead of one function per field, since each
// case exercises the same guard, not distinct behavior.
func TestHandler_GetRegistries_RegistryIDCombinedWithListingParamsErrors(t *testing.T) {
	regID := uint64(1)
	name := "anything"
	offset := uint64(10)
	limit := uint64(10)
	tests := []struct {
		name  string
		input getRegistriesInput
	}{
		{"name", getRegistriesInput{RegistryID: &regID, Name: &name}},
		{"match", getRegistriesInput{RegistryID: &regID, Match: "prefix"}},
		{"offset", getRegistriesInput{RegistryID: &regID, Offset: &offset}},
		{"limit", getRegistriesInput{RegistryID: &regID, Limit: &limit}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := &mockAnchor{registries: &anchor.GetRegistriesResponse{}}
			handler := makeGetRegistriesHandler(m)

			_, _, err := handler(ctx, nil, tc.input)
			if !errors.Is(err, apperrors.ErrInvalidFilterCombination) {
				t.Errorf("error = %v, want ErrInvalidFilterCombination", err)
			}
			if m.registriesCallCount != 0 {
				t.Errorf("registriesCallCount = %d, want 0 (rejected before any chain call)", m.registriesCallCount)
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
			handler := makeGetRegistriesHandler(m)

			_, _, err := handler(ctx, nil, tc.input)
			if !errors.Is(err, apperrors.ErrMatchWithoutName) {
				t.Errorf("error = %v, want ErrMatchWithoutName", err)
			}
		})
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
	registryID := uint64(1)
	_, out, err := handler(ctx, nil, getRecordsInput{RegistryID: &registryID, Offset: &offset, Limit: &limit})
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

func TestAuditIndex(t *testing.T) {
	if got := auditIndex(nil); got != "latest" {
		t.Errorf("nil = %q, want latest", got)
	}
	zero := uint64(0)
	if got := auditIndex(&zero); got != "latest" {
		t.Errorf("0 = %q, want latest", got)
	}
	three := uint64(3)
	if got := auditIndex(&three); got != "3" {
		t.Errorf("3 = %q, want 3", got)
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
