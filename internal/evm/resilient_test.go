// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	ierrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/telemetry"
)

// failingClient wraps stubClient and fails the first failCount calls to ChainID
// and SendRawTransaction, then succeeds.
type failingClient struct {
	stubClient
	callCount    atomic.Int32
	failCount    int
	failErr      error
	chainIDValue *big.Int

	sendCallCount atomic.Int32
	sendFailCount int
	sendFailErr   error
}

func (f *failingClient) ChainID(_ context.Context) (*big.Int, error) {
	n := int(f.callCount.Add(1))
	if n <= f.failCount {
		return nil, f.failErr
	}
	return f.chainIDValue, nil
}

func (f *failingClient) SendRawTransaction(_ context.Context, _ string) (string, error) {
	n := int(f.sendCallCount.Add(1))
	if n <= f.sendFailCount {
		return "", f.sendFailErr
	}
	return "0xabc123", nil
}

func newTestMetrics(t *testing.T) *telemetry.Metrics {
	t.Helper()
	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			t.Logf("meter shutdown: %v", err)
		}
	})
	m, err := telemetry.NewMetrics(mp)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func testResilientConfig() ResilientConfig {
	return ResilientConfig{
		MaxRetries:       3,
		InitialBackoff:   1 * time.Millisecond,
		MaxBackoff:       5 * time.Millisecond,
		RateLimit:        1000,
		RateBurst:        100,
		BreakerThreshold: 5,
		BreakerTimeout:   1 * time.Second,
	}
}

func TestResilientClient_RetryThenSucceed(t *testing.T) {
	inner := &failingClient{
		failCount:    2,
		failErr:      fmt.Errorf("%w: connection refused", ierrors.ErrUpstreamRPC),
		chainIDValue: big.NewInt(58887),
	}
	cfg := testResilientConfig()
	cfg.MaxRetries = 3
	rc := NewResilientClient(inner, cfg, newTestMetrics(t), slog.Default())

	got, err := rc.ChainID(context.Background())
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if got.Cmp(big.NewInt(58887)) != 0 {
		t.Errorf("ChainID = %v, want 58887", got)
	}
	if calls := int(inner.callCount.Load()); calls != 3 {
		t.Errorf("call count = %d, want 3 (1 initial + 2 retries)", calls)
	}
}

func TestResilientClient_ExhaustRetries(t *testing.T) {
	inner := &failingClient{
		failCount: 100, // always fails
		failErr:   fmt.Errorf("%w: connection refused", ierrors.ErrUpstreamRPC),
	}
	cfg := testResilientConfig()
	cfg.MaxRetries = 2
	rc := NewResilientClient(inner, cfg, newTestMetrics(t), slog.Default())

	_, err := rc.ChainID(context.Background())
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !errors.Is(err, ierrors.ErrUpstreamRPC) {
		t.Errorf("error = %v, want ErrUpstreamRPC", err)
	}
	// MaxRetries=2 means MaxTries=3: 1 initial + 2 retries = 3 total calls
	if calls := int(inner.callCount.Load()); calls != 3 {
		t.Errorf("call count = %d, want 3", calls)
	}
}

func TestResilientClient_PermanentErrorNoRetry(t *testing.T) {
	inner := &failingClient{
		failCount: 100,
		failErr:   ierrors.ErrInvalidAddress,
	}
	cfg := testResilientConfig()
	cfg.MaxRetries = 3
	rc := NewResilientClient(inner, cfg, newTestMetrics(t), slog.Default())

	_, err := rc.ChainID(context.Background())
	if err == nil {
		t.Fatal("expected error for permanent failure")
	}
	if !errors.Is(err, ierrors.ErrInvalidAddress) {
		t.Errorf("error = %v, want ErrInvalidAddress", err)
	}
	if calls := int(inner.callCount.Load()); calls != 1 {
		t.Errorf("call count = %d, want 1 (no retries for permanent error)", calls)
	}
}

func TestResilientClient_CircuitBreakerTrips(t *testing.T) {
	inner := &failingClient{
		failCount: 100,
		failErr:   fmt.Errorf("%w: connection refused", ierrors.ErrUpstreamRPC),
	}
	cfg := testResilientConfig()
	cfg.MaxRetries = 0 // no retries, so each call is a single failure
	cfg.BreakerThreshold = 3
	cfg.BreakerTimeout = 5 * time.Second
	rc := NewResilientClient(inner, cfg, newTestMetrics(t), slog.Default())

	// Trigger failures to trip the breaker (threshold=3 consecutive failures)
	for i := 0; i < 3; i++ {
		_, _ = rc.ChainID(context.Background())
	}

	// Next call should hit the open circuit
	_, err := rc.ChainID(context.Background())
	if err == nil {
		t.Fatal("expected error from open circuit breaker")
	}
	if !errors.Is(err, ierrors.ErrCircuitOpen) {
		t.Errorf("error = %v, want ErrCircuitOpen", err)
	}
}

func TestResilientClient_SendRawTransactionNotRetried(t *testing.T) {
	inner := &failingClient{
		sendFailCount: 100,
		sendFailErr:   fmt.Errorf("%w: connection refused", ierrors.ErrUpstreamRPC),
	}
	cfg := testResilientConfig()
	cfg.MaxRetries = 5
	rc := NewResilientClient(inner, cfg, newTestMetrics(t), slog.Default())

	_, err := rc.SendRawTransaction(context.Background(), "0xdeadbeef")
	if err == nil {
		t.Fatal("expected error from SendRawTransaction")
	}
	if calls := int(inner.sendCallCount.Load()); calls != 1 {
		t.Errorf("call count = %d, want 1 (SendRawTransaction must not retry)", calls)
	}
}

func TestResilientClient_RateLimitRejectsOnCancelledContext(t *testing.T) {
	inner := &stubClient{chainID: big.NewInt(1)}
	cfg := testResilientConfig()
	cfg.RateLimit = 0.001 // very low: ~1 request per 1000 seconds
	cfg.RateBurst = 1
	rc := NewResilientClient(inner, cfg, newTestMetrics(t), slog.Default())

	// First call consumes the burst token
	_, err := rc.ChainID(context.Background())
	if err != nil {
		t.Fatalf("first call should succeed using burst token: %v", err)
	}

	// Second call with already-canceled context should fail immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = rc.ChainID(ctx)
	if err == nil {
		t.Fatal("expected error from rate limiter with canceled context")
	}
	if !errors.Is(err, ierrors.ErrRateLimited) {
		t.Errorf("error = %v, want ErrRateLimited", err)
	}
}

func TestResilientClient_DelegatesClose(t *testing.T) {
	inner := &stubClient{}
	rc := NewResilientClient(inner, testResilientConfig(), newTestMetrics(t), slog.Default())
	rc.Close()
	if !inner.closed {
		t.Error("Close was not delegated to inner client")
	}
}

func TestResilientClient_DelegatesPing(t *testing.T) {
	inner := &stubClient{}
	rc := NewResilientClient(inner, testResilientConfig(), newTestMetrics(t), slog.Default())
	if err := rc.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestIsTransientRPCError_CometReceiptsRace pins the upstream-error
// marker that recognizes the Cosmos-EVM "tx not found" race on
// eth_gasPrice (see the cometReceiptsRaceMarker comment). If upstream
// rewords its error chain, this test will catch the regression -- the
// integration suite would otherwise just turn flaky again with no
// signal at the unit level.
func TestIsTransientRPCError_CometReceiptsRace(t *testing.T) {
	// Real error text observed from
	// https://evm.inveniam.mantrachain.io on 2026-05-13.
	upstream := errors.New(
		"RPC error: -32000 failed to get rpc block from comet block: " +
			"failed to get receipts from comet block: tx not found: " +
			"hash=0x397fdc78dc50de7c2e7162366f144c5a13f8a6228b886d23194d901b56ea88e9, " +
			"error=tx not found, hash: 0x397fdc78...",
	)
	if !isTransientRPCError(upstream) {
		t.Error("comet-receipts race error should be classified transient")
	}
}

// TestIsTransientRPCError_GenericTxNotFoundNotTransient confirms the
// marker is specific to the gas-price race: a plain "tx not found"
// from get_transaction_receipt (legitimate -- the tx simply does not
// exist or hasn't been broadcast) must NOT be retried, or we'd hang
// callers waiting on receipts for txs that will never appear.
func TestIsTransientRPCError_GenericTxNotFoundNotTransient(t *testing.T) {
	upstream := errors.New("transaction not found")
	if isTransientRPCError(upstream) {
		t.Error("generic tx-not-found should not be classified transient")
	}
}
