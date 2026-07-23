// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// fakeRPC is an in-memory defiRPCClient. Each method returns the
// configured value/error pair and records the arguments it was called
// with so tests can assert on the wiring (e.g. nil block -> "latest").
type fakeRPC struct {
	chainID    uint64
	chainIDErr error

	blockNumber    *big.Int
	blockNumberErr error

	block        *defitypes.Block
	blockErr     error
	gotBlockNum  defitypes.BlockNumber
	gotBlockHash defitypes.Hash
	gotBlockFull bool

	tx    *defitypes.OnChainTransaction
	txErr error

	receipt    *defitypes.TransactionReceipt
	receiptErr error

	balance     *big.Int
	balanceErr  error
	gotBalBlock defitypes.BlockNumber

	code    []byte
	codeErr error

	nonce         uint64
	nonceErr      error
	gotNonceBlock defitypes.BlockNumber

	logs    []defitypes.Log
	logsErr error

	gasPrice    *big.Int
	gasPriceErr error

	tipCap    *big.Int
	tipCapErr error

	callOut []byte
	callErr error

	gasEstimate uint64
	estimateErr error

	sendHash *defitypes.Hash
	sendErr  error
	sentRaw  []byte
}

func (f *fakeRPC) ChainID(_ context.Context) (uint64, error) {
	return f.chainID, f.chainIDErr
}

func (f *fakeRPC) BlockNumber(_ context.Context) (*big.Int, error) {
	return f.blockNumber, f.blockNumberErr
}

func (f *fakeRPC) BlockByNumber(
	_ context.Context, number defitypes.BlockNumber, full bool,
) (*defitypes.Block, error) {
	f.gotBlockNum = number
	f.gotBlockFull = full
	return f.block, f.blockErr
}

func (f *fakeRPC) BlockByHash(_ context.Context, hash defitypes.Hash, full bool) (*defitypes.Block, error) {
	f.gotBlockHash = hash
	f.gotBlockFull = full
	return f.block, f.blockErr
}

func (f *fakeRPC) GetTransactionByHash(
	_ context.Context, _ defitypes.Hash,
) (*defitypes.OnChainTransaction, error) {
	return f.tx, f.txErr
}

func (f *fakeRPC) GetTransactionReceipt(
	_ context.Context, _ defitypes.Hash,
) (*defitypes.TransactionReceipt, error) {
	return f.receipt, f.receiptErr
}

func (f *fakeRPC) GetBalance(
	_ context.Context, _ defitypes.Address, block defitypes.BlockNumber,
) (*big.Int, error) {
	f.gotBalBlock = block
	return f.balance, f.balanceErr
}

func (f *fakeRPC) GetCode(_ context.Context, _ defitypes.Address, _ defitypes.BlockNumber) ([]byte, error) {
	return f.code, f.codeErr
}

func (f *fakeRPC) GetTransactionCount(
	_ context.Context, _ defitypes.Address, block defitypes.BlockNumber,
) (uint64, error) {
	f.gotNonceBlock = block
	return f.nonce, f.nonceErr
}

func (f *fakeRPC) GetLogs(_ context.Context, _ *defitypes.FilterLogsQuery) ([]defitypes.Log, error) {
	return f.logs, f.logsErr
}

func (f *fakeRPC) GasPrice(_ context.Context) (*big.Int, error) {
	return f.gasPrice, f.gasPriceErr
}

func (f *fakeRPC) MaxPriorityFeePerGas(_ context.Context) (*big.Int, error) {
	return f.tipCap, f.tipCapErr
}

func (f *fakeRPC) Call(
	_ context.Context, call *defitypes.Call, _ defitypes.BlockNumber,
) ([]byte, *defitypes.Call, error) {
	return f.callOut, call, f.callErr
}

func (f *fakeRPC) EstimateGas(
	_ context.Context, call *defitypes.Call, _ defitypes.BlockNumber,
) (uint64, *defitypes.Call, error) {
	return f.gasEstimate, call, f.estimateErr
}

func (f *fakeRPC) SendRawTransaction(_ context.Context, raw []byte) (*defitypes.Hash, error) {
	f.sentRaw = raw
	return f.sendHash, f.sendErr
}

// newFakeClient constructs the concrete client around a fakeRPC with a
// generous timeout so tests never race the context deadline.
func newFakeClient(f *fakeRPC) *client {
	return &client{rpc: f, timeout: 5 * time.Second}
}

var errRPCDown = errors.New("rpc down")

func TestClient_ChainID(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainID: 58887})
	got, err := c.ChainID(context.Background())
	if err != nil {
		t.Fatalf("ChainID: %v", err)
	}
	if got.Cmp(big.NewInt(58887)) != 0 {
		t.Errorf("ChainID = %v, want 58887", got)
	}
}

func TestClient_ChainID_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainIDErr: errRPCDown})
	if _, err := c.ChainID(context.Background()); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_LatestBlockNumber(t *testing.T) {
	c := newFakeClient(&fakeRPC{blockNumber: big.NewInt(1234567)})
	got, err := c.LatestBlockNumber(context.Background())
	if err != nil {
		t.Fatalf("LatestBlockNumber: %v", err)
	}
	if got != 1234567 {
		t.Errorf("LatestBlockNumber = %d, want 1234567", got)
	}
}

func TestClient_LatestBlockNumber_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{blockNumberErr: errRPCDown})
	if _, err := c.LatestBlockNumber(context.Background()); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_GetChainInfo(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainID: 58887, blockNumber: big.NewInt(42)})
	info, err := c.GetChainInfo(context.Background())
	if err != nil {
		t.Fatalf("GetChainInfo: %v", err)
	}
	if info.ChainID != 58887 || info.LatestBlockNumber != 42 {
		t.Errorf("GetChainInfo = %+v, want {58887 42}", info)
	}
}

func TestClient_GetChainInfo_ChainIDError(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainIDErr: errRPCDown})
	if _, err := c.GetChainInfo(context.Background()); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_GetChainInfo_BlockNumberError(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainID: 58887, blockNumberErr: errRPCDown})
	if _, err := c.GetChainInfo(context.Background()); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func testBlock() *defitypes.Block {
	to := defitypes.MustAddressFromHex("0x742d35cc6634c0532925a3b844bc9e7595f2bd00")
	value := big.NewInt(1_000_000_000_000_000_000)
	tx := defitypes.OnChainTransaction{
		Hash: defitypes.MustHashFromHexPtr(
			"0x1111111111111111111111111111111111111111111111111111111111111111",
			defitypes.PadNone,
		),
	}
	tx.To = &to
	tx.Value = value
	return &defitypes.Block{
		Number: big.NewInt(1000),
		Hash: defitypes.MustHashFromHex(
			"0xabc123def456abc123def456abc123def456abc123def456abc123def456abc1",
			defitypes.PadNone,
		),
		ParentHash: defitypes.MustHashFromHex(
			"0xdef456abc123def456abc123def456abc123def456abc123def456abc123def4",
			defitypes.PadNone,
		),
		Timestamp:    time.Unix(1709300000, 0),
		GasLimit:     30000000,
		GasUsed:      15000000,
		Miner:        defitypes.MustAddressFromHex("0x742d35cc6634c0532925a3b844bc9e7595f2bd00"),
		Transactions: []defitypes.OnChainTransaction{tx},
	}
}

func TestClient_BlockByNumber_LatestWhenNil(t *testing.T) {
	f := &fakeRPC{block: testBlock()}
	c := newFakeClient(f)
	nb, err := c.BlockByNumber(context.Background(), nil, false)
	if err != nil {
		t.Fatalf("BlockByNumber: %v", err)
	}
	if !f.gotBlockNum.IsLatest() {
		t.Errorf("block number passed = %v, want latest", &f.gotBlockNum)
	}
	if nb.Number != 1000 || nb.TransactionCount != 1 {
		t.Errorf("normalized block = %+v, want number=1000 txcount=1", nb)
	}
	if len(nb.Transactions) != 0 {
		t.Errorf("fullTx=false should not populate transactions, got %d", len(nb.Transactions))
	}
}

func TestClient_BlockByNumber_ExplicitNumber(t *testing.T) {
	f := &fakeRPC{block: testBlock()}
	c := newFakeClient(f)
	if _, err := c.BlockByNumber(context.Background(), big.NewInt(1000), true); err != nil {
		t.Fatalf("BlockByNumber: %v", err)
	}
	if f.gotBlockNum.Big().Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("block number passed = %v, want 1000", f.gotBlockNum)
	}
	if !f.gotBlockFull {
		t.Error("fullTx flag was not forwarded")
	}
}

func TestClient_BlockByNumber_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{blockErr: errRPCDown})
	if _, err := c.BlockByNumber(context.Background(), nil, false); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_BlockByHash(t *testing.T) {
	f := &fakeRPC{block: testBlock()}
	c := newFakeClient(f)
	hash := defitypes.MustHashFromHex(
		"0xabc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		defitypes.PadNone,
	)
	nb, err := c.BlockByHash(context.Background(), hash, true)
	if err != nil {
		t.Fatalf("BlockByHash: %v", err)
	}
	if f.gotBlockHash != hash {
		t.Errorf("hash passed = %v, want %v", f.gotBlockHash, hash)
	}
	if len(nb.Transactions) != 1 {
		t.Fatalf("fullTx=true should populate transactions, got %d", len(nb.Transactions))
	}
	if nb.Transactions[0].Value != "1000000000000000000" {
		t.Errorf("tx value = %q, want 1000000000000000000", nb.Transactions[0].Value)
	}
}

func TestClient_BlockByHash_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{blockErr: errRPCDown})
	_, err := c.BlockByHash(context.Background(), defitypes.Hash{}, false)
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_TransactionByHash_Mined(t *testing.T) {
	tx := &defitypes.OnChainTransaction{
		Hash: defitypes.MustHashFromHexPtr(
			"0x1111111111111111111111111111111111111111111111111111111111111111",
			defitypes.PadNone,
		),
		BlockNumber: big.NewInt(1000),
	}
	c := newFakeClient(&fakeRPC{tx: tx})
	nt, err := c.TransactionByHash(context.Background(), *tx.Hash)
	if err != nil {
		t.Fatalf("TransactionByHash: %v", err)
	}
	if nt.IsPending {
		t.Error("mined transaction reported as pending")
	}
	if nt.Hash != tx.Hash.String() {
		t.Errorf("hash = %q, want %q", nt.Hash, tx.Hash.String())
	}
}

func TestClient_TransactionByHash_Pending(t *testing.T) {
	tx := &defitypes.OnChainTransaction{
		Hash: defitypes.MustHashFromHexPtr(
			"0x2222222222222222222222222222222222222222222222222222222222222222",
			defitypes.PadNone,
		),
	}
	c := newFakeClient(&fakeRPC{tx: tx})
	nt, err := c.TransactionByHash(context.Background(), *tx.Hash)
	if err != nil {
		t.Fatalf("TransactionByHash: %v", err)
	}
	if !nt.IsPending {
		t.Error("transaction without block number should be pending")
	}
}

func TestClient_TransactionByHash_NilResultIsNotFound(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	_, err := c.TransactionByHash(context.Background(), defitypes.Hash{})
	if !errors.Is(err, apperrors.ErrTxNotFound) {
		t.Errorf("error = %v, want ErrTxNotFound", err)
	}
}

func TestClient_TransactionByHash_ZeroStructIsNotFound(t *testing.T) {
	// Some RPC/decoder combinations return a non-nil zero-value struct
	// for a missing hash; the nil Hash field must map to not-found.
	c := newFakeClient(&fakeRPC{tx: &defitypes.OnChainTransaction{}})
	_, err := c.TransactionByHash(context.Background(), defitypes.Hash{})
	if !errors.Is(err, apperrors.ErrTxNotFound) {
		t.Errorf("error = %v, want ErrTxNotFound", err)
	}
}

func TestClient_TransactionByHash_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{txErr: errRPCDown})
	_, err := c.TransactionByHash(context.Background(), defitypes.Hash{})
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_TransactionReceipt(t *testing.T) {
	status := uint64(1)
	receipt := &defitypes.TransactionReceipt{
		TransactionHash: defitypes.MustHashFromHex(
			"0x1111111111111111111111111111111111111111111111111111111111111111",
			defitypes.PadNone,
		),
		BlockNumber: big.NewInt(1000),
		GasUsed:     21000,
		Status:      &status,
	}
	c := newFakeClient(&fakeRPC{receipt: receipt})
	nr, err := c.TransactionReceipt(context.Background(), receipt.TransactionHash)
	if err != nil {
		t.Fatalf("TransactionReceipt: %v", err)
	}
	if nr.Status != "success" || nr.BlockNumber != 1000 || nr.GasUsed != 21000 {
		t.Errorf("receipt = %+v, want status=success block=1000 gas=21000", nr)
	}
}

func TestClient_TransactionReceipt_NilResultIsNotFound(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	_, err := c.TransactionReceipt(context.Background(), defitypes.Hash{})
	if !errors.Is(err, apperrors.ErrTxNotFound) {
		t.Errorf("error = %v, want ErrTxNotFound", err)
	}
}

func TestClient_TransactionReceipt_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{receiptErr: errRPCDown})
	_, err := c.TransactionReceipt(context.Background(), defitypes.Hash{})
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_BalanceAt(t *testing.T) {
	f := &fakeRPC{balance: big.NewInt(1_000_000_000_000_000_000)}
	c := newFakeClient(f)
	addr := defitypes.MustAddressFromHex("0x5aaeb6053f3e94c9b9a09f33669435e7ef1beaed")
	nb, err := c.BalanceAt(context.Background(), addr, nil)
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if !f.gotBalBlock.IsLatest() {
		t.Errorf("block passed = %v, want latest", &f.gotBalBlock)
	}
	if nb.Wei != "1000000000000000000" || nb.Ether != "1.000000000000000000" {
		t.Errorf("balance = %+v, want wei=1000000000000000000 ether=1.000000000000000000", nb)
	}
	if nb.Address != "0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed" {
		t.Errorf("address = %q, want EIP-55 checksummed form", nb.Address)
	}
}

func TestClient_BalanceAt_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{balanceErr: errRPCDown})
	_, err := c.BalanceAt(context.Background(), defitypes.Address{}, big.NewInt(1))
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_CodeAt_Contract(t *testing.T) {
	c := newFakeClient(&fakeRPC{code: []byte{0x60, 0x80}})
	res, err := c.CodeAt(context.Background(), defitypes.Address{}, nil)
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if !res.IsContract || res.Bytecode != "0x6080" {
		t.Errorf("code result = %+v, want contract with bytecode 0x6080", res)
	}
}

func TestClient_CodeAt_EOA(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	res, err := c.CodeAt(context.Background(), defitypes.Address{}, big.NewInt(5))
	if err != nil {
		t.Fatalf("CodeAt: %v", err)
	}
	if res.IsContract || res.Bytecode != "0x" {
		t.Errorf("code result = %+v, want non-contract with bytecode 0x", res)
	}
}

func TestClient_CodeAt_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{codeErr: errRPCDown})
	_, err := c.CodeAt(context.Background(), defitypes.Address{}, nil)
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_CallContract(t *testing.T) {
	want := []byte{0x01, 0x02}
	c := newFakeClient(&fakeRPC{callOut: want})
	got, err := c.CallContract(context.Background(), defitypes.Call{}, nil)
	if err != nil {
		t.Fatalf("CallContract: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("CallContract = %x, want %x", got, want)
	}
}

func TestClient_CallContract_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{callErr: errRPCDown})
	_, err := c.CallContract(context.Background(), defitypes.Call{}, big.NewInt(3))
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_FilterLogs(t *testing.T) {
	blockNum := big.NewInt(1000)
	txIdx := uint64(0)
	logIdx := uint64(2)
	logs := []defitypes.Log{
		{
			Address: defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000a00"),
			Topics: []defitypes.Hash{
				defitypes.MustHashFromHex(
					"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
					defitypes.PadNone,
				),
			},
			Data:        []byte{0x01},
			BlockNumber: blockNum,
			TransactionHash: defitypes.MustHashFromHexPtr(
				"0x1111111111111111111111111111111111111111111111111111111111111111",
				defitypes.PadNone,
			),
			TransactionIndex: &txIdx,
			LogIndex:         &logIdx,
		},
	}
	c := newFakeClient(&fakeRPC{logs: logs})
	got, err := c.FilterLogs(context.Background(), defitypes.FilterLogsQuery{})
	if err != nil {
		t.Fatalf("FilterLogs: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("FilterLogs returned %d logs, want 1", len(got))
	}
	if got[0].BlockNumber != 1000 || got[0].LogIndex != 2 || got[0].Data != "0x01" {
		t.Errorf("log = %+v, want block=1000 logIndex=2 data=0x01", got[0])
	}
}

func TestClient_FilterLogs_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{logsErr: errRPCDown})
	_, err := c.FilterLogs(context.Background(), defitypes.FilterLogsQuery{})
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_PendingNonceAt(t *testing.T) {
	f := &fakeRPC{nonce: 42}
	c := newFakeClient(f)
	got, err := c.PendingNonceAt(context.Background(), defitypes.Address{})
	if err != nil {
		t.Fatalf("PendingNonceAt: %v", err)
	}
	if got != 42 {
		t.Errorf("PendingNonceAt = %d, want 42", got)
	}
	if !f.gotNonceBlock.IsPending() {
		t.Errorf("block passed = %v, want pending", &f.gotNonceBlock)
	}
}

func TestClient_SuggestGasPrice(t *testing.T) {
	c := newFakeClient(&fakeRPC{gasPrice: big.NewInt(8_000_000_000)})
	got, err := c.SuggestGasPrice(context.Background())
	if err != nil {
		t.Fatalf("SuggestGasPrice: %v", err)
	}
	if got.Cmp(big.NewInt(8_000_000_000)) != 0 {
		t.Errorf("SuggestGasPrice = %v, want 8000000000", got)
	}
}

func TestClient_SuggestGasTipCap(t *testing.T) {
	c := newFakeClient(&fakeRPC{tipCap: big.NewInt(1_000_000_000)})
	got, err := c.SuggestGasTipCap(context.Background())
	if err != nil {
		t.Fatalf("SuggestGasTipCap: %v", err)
	}
	if got.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Errorf("SuggestGasTipCap = %v, want 1000000000", got)
	}
}

func TestClient_EstimateGas(t *testing.T) {
	c := newFakeClient(&fakeRPC{gasEstimate: 21000})
	got, err := c.EstimateGas(context.Background(), defitypes.Call{})
	if err != nil {
		t.Fatalf("EstimateGas: %v", err)
	}
	if got != 21000 {
		t.Errorf("EstimateGas = %d, want 21000", got)
	}
}

func TestClient_EstimateGas_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{estimateErr: errRPCDown})
	if _, err := c.EstimateGas(context.Background(), defitypes.Call{}); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_SendRawTransaction(t *testing.T) {
	hash := defitypes.MustHashFromHex(
		"0x1111111111111111111111111111111111111111111111111111111111111111",
		defitypes.PadNone,
	)
	f := &fakeRPC{sendHash: &hash}
	c := newFakeClient(f)
	got, err := c.SendRawTransaction(context.Background(), "0xdeadbeef")
	if err != nil {
		t.Fatalf("SendRawTransaction: %v", err)
	}
	if got != hash.String() {
		t.Errorf("hash = %q, want %q", got, hash.String())
	}
	if string(f.sentRaw) != "\xde\xad\xbe\xef" {
		t.Errorf("raw bytes = %x, want deadbeef", f.sentRaw)
	}
}

func TestClient_SendRawTransaction_NoPrefix(t *testing.T) {
	hash := defitypes.MustHashFromHex(
		"0x2222222222222222222222222222222222222222222222222222222222222222",
		defitypes.PadNone,
	)
	f := &fakeRPC{sendHash: &hash}
	c := newFakeClient(f)
	if _, err := c.SendRawTransaction(context.Background(), "deadbeef"); err != nil {
		t.Fatalf("SendRawTransaction: %v", err)
	}
	if string(f.sentRaw) != "\xde\xad\xbe\xef" {
		t.Errorf("raw bytes = %x, want deadbeef", f.sentRaw)
	}
}

func TestClient_SendRawTransaction_InvalidHex(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	_, err := c.SendRawTransaction(context.Background(), "0xzznothex")
	if err == nil {
		t.Fatal("expected error for invalid hex input")
	}
	if !strings.Contains(err.Error(), "decode signed tx hex") {
		t.Errorf("error = %v, want decode error", err)
	}
}

func TestClient_SendRawTransaction_TooLarge(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	huge := "0x" + strings.Repeat("ab", maxSignedTxHexLen/2+1)
	_, err := c.SendRawTransaction(context.Background(), huge)
	if !errors.Is(err, apperrors.ErrInputTooLarge) {
		t.Errorf("error = %v, want ErrInputTooLarge", err)
	}
}

func TestClient_SendRawTransaction_RPCError(t *testing.T) {
	c := newFakeClient(&fakeRPC{sendErr: errRPCDown})
	_, err := c.SendRawTransaction(context.Background(), "0xdeadbeef")
	if !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_SendRawTransaction_NilHashIsEmptyTxHash(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	_, err := c.SendRawTransaction(context.Background(), "0xdeadbeef")
	if !errors.Is(err, apperrors.ErrEmptyTxHash) {
		t.Errorf("error = %v, want ErrEmptyTxHash", err)
	}
}

func TestClient_Ping(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainID: 1})
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestClient_Ping_Error(t *testing.T) {
	c := newFakeClient(&fakeRPC{chainIDErr: errRPCDown})
	if err := c.Ping(context.Background()); !errors.Is(err, errRPCDown) {
		t.Errorf("error = %v, want %v", err, errRPCDown)
	}
}

func TestClient_Close(t *testing.T) {
	c := newFakeClient(&fakeRPC{})
	c.Close() // no-op for the HTTP transport; must not panic
}

func TestBlockNumOrLatest(t *testing.T) {
	latest := blockNumOrLatest(nil)
	if !latest.IsLatest() {
		t.Errorf("blockNumOrLatest(nil) = %v, want latest", &latest)
	}
	seven := blockNumOrLatest(big.NewInt(7))
	if seven.Big().Cmp(big.NewInt(7)) != 0 {
		t.Errorf("blockNumOrLatest(7) = %v, want 7", &seven)
	}
}

func TestNewClient(t *testing.T) {
	// transport.NewHTTP validates its options without dialing, so
	// constructing against an unreachable localhost URL stays hermetic.
	c, err := NewClient(context.Background(), "http://127.0.0.1:1", time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.Close()
}

func TestNewClient_InvalidURL(t *testing.T) {
	if _, err := NewClient(context.Background(), "", time.Second); err == nil {
		t.Fatal("expected error for empty RPC URL")
	}
}
