//go:build integration

package evm

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	defitypes "github.com/defiweb/go-eth/types"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/inveniam/nvnm-mcp-server/internal/telemetry"
)

func newIntegrationMetrics(t *testing.T) *telemetry.Metrics {
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

func newResilientTestClient(t *testing.T) Client {
	t.Helper()
	rpcURL := os.Getenv("NVNM_EVM_RPC_URL")
	if rpcURL == "" {
		rpcURL = "https://evm.testnet.nvnmchain.io"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	raw, err := NewClient(ctx, rpcURL, 15*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	t.Cleanup(raw.Close)

	metrics := newIntegrationMetrics(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewResilientClient(raw, ResilientConfig{
		MaxRetries:       3,
		InitialBackoff:   500 * time.Millisecond,
		MaxBackoff:       5 * time.Second,
		RateLimit:        50,
		RateBurst:        10,
		BreakerThreshold: 5,
		BreakerTimeout:   30 * time.Second,
	}, metrics, logger)
}

func TestResilientIntegration_ChainID(t *testing.T) {
	c := newResilientTestClient(t)
	ctx := context.Background()

	chainID, err := c.ChainID(ctx)
	if err != nil {
		t.Fatalf("ChainID: %v", err)
	}
	if chainID.Int64() != 787111 {
		t.Errorf("ChainID = %d, want 787111", chainID.Int64())
	}
}

func TestResilientIntegration_GetChainInfo(t *testing.T) {
	c := newResilientTestClient(t)
	ctx := context.Background()

	info, err := c.GetChainInfo(ctx)
	if err != nil {
		t.Fatalf("GetChainInfo: %v", err)
	}
	if info.ChainID != 787111 {
		t.Errorf("ChainID = %d, want 787111", info.ChainID)
	}
	if info.LatestBlockNumber == 0 {
		t.Error("LatestBlockNumber should be > 0")
	}
}

func TestResilientIntegration_BalanceAt(t *testing.T) {
	c := newResilientTestClient(t)
	ctx := context.Background()

	addr := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000000")
	bal, err := c.BalanceAt(ctx, addr, nil)
	if err != nil {
		t.Fatalf("BalanceAt: %v", err)
	}
	if bal.Address == "" {
		t.Error("address should not be empty")
	}
	if bal.Wei == "" {
		t.Error("wei should not be empty")
	}
}

func TestResilientIntegration_Ping(t *testing.T) {
	c := newResilientTestClient(t)
	ctx := context.Background()

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
