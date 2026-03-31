package evm

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

func newTestTracingMetrics(t *testing.T) *TracingMetrics {
	t.Helper()
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})
	meter := mp.Meter("test")

	dur, err := meter.Float64Histogram("test.rpc.duration")
	if err != nil {
		t.Fatal(err)
	}
	errs, err := meter.Int64Counter("test.rpc.errors")
	if err != nil {
		t.Fatal(err)
	}
	return &TracingMetrics{RPCDuration: dur, RPCErrors: errs}
}

func TestTracingClient_DelegatesChainID(t *testing.T) {
	want := big.NewInt(58887)
	inner := &stubClient{chainID: want}
	tc := NewTracingClient(inner, "test-host", newTestTracingMetrics(t))

	got, err := tc.ChainID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Cmp(want) != 0 {
		t.Errorf("ChainID = %v, want %v", got, want)
	}
}

func TestTracingClient_PropagatesError(t *testing.T) {
	wantErr := errors.New("rpc down")
	inner := &stubClient{chainIDErr: wantErr}
	tc := NewTracingClient(inner, "test-host", newTestTracingMetrics(t))

	_, err := tc.ChainID(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

func TestTracingClient_Ping(t *testing.T) {
	inner := &stubClient{}
	tc := NewTracingClient(inner, "test-host", newTestTracingMetrics(t))

	if err := tc.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestTracingClient_Close(t *testing.T) {
	inner := &stubClient{}
	tc := NewTracingClient(inner, "test-host", newTestTracingMetrics(t))
	tc.Close()
	if !inner.closed {
		t.Error("Close was not delegated to inner client")
	}
}

type stubClient struct {
	chainID    *big.Int
	chainIDErr error
	closed     bool
}

func (s *stubClient) ChainID(_ context.Context) (*big.Int, error) {
	return s.chainID, s.chainIDErr
}
func (s *stubClient) LatestBlockNumber(_ context.Context) (uint64, error) { return 0, nil }
func (s *stubClient) GetChainInfo(_ context.Context) (*ChainInfo, error)  { return nil, nil }
func (s *stubClient) BlockByNumber(_ context.Context, _ *big.Int, _ bool) (*NormalizedBlock, error) {
	return nil, nil
}
func (s *stubClient) BlockByHash(_ context.Context, _ common.Hash, _ bool) (*NormalizedBlock, error) {
	return nil, nil
}
func (s *stubClient) TransactionByHash(_ context.Context, _ common.Hash) (*NormalizedTransaction, error) {
	return nil, nil
}
func (s *stubClient) TransactionReceipt(_ context.Context, _ common.Hash) (*NormalizedReceipt, error) {
	return nil, nil
}
func (s *stubClient) BalanceAt(_ context.Context, _ common.Address, _ *big.Int) (*NormalizedBalance, error) {
	return nil, nil
}
func (s *stubClient) CodeAt(_ context.Context, _ common.Address, _ *big.Int) (*CodeResult, error) {
	return nil, nil
}

//nolint:gocritic // hugeParam: matches go-ethereum's CallContract signature
func (s *stubClient) CallContract(_ context.Context, _ ethereum.CallMsg, _ *big.Int) ([]byte, error) {
	return nil, nil
}
func (s *stubClient) FilterLogs(_ context.Context, _ ethereum.FilterQuery) ([]NormalizedLog, error) {
	return nil, nil
}
func (s *stubClient) PendingNonceAt(_ context.Context, _ common.Address) (uint64, error) {
	return 0, nil
}
func (s *stubClient) SuggestGasPrice(_ context.Context) (*big.Int, error) { return big.NewInt(0), nil }

//nolint:gocritic // hugeParam: matches go-ethereum's EstimateGas signature
func (s *stubClient) EstimateGas(_ context.Context, _ ethereum.CallMsg) (uint64, error) {
	return 0, nil
}
func (s *stubClient) SendRawTransaction(_ context.Context, _ string) (string, error) {
	return "", nil
}
func (s *stubClient) Ping(_ context.Context) error { return nil }
func (s *stubClient) Close()                       { s.closed = true }
