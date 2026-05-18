// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"math/big"
	"time"

	defitypes "github.com/defiweb/go-eth/types"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const evmTracerName = "nvnm-mcp-server/evm"

// TracingMetrics holds the metric instruments used by the tracing wrapper.
type TracingMetrics struct {
	// RPCDuration records the latency of each upstream EVM RPC call.
	RPCDuration metric.Float64Histogram
	// RPCErrors counts upstream EVM RPC calls that returned an error.
	RPCErrors metric.Int64Counter
}

// NewTracingClient wraps an existing Client with OpenTelemetry spans and
// duration/error metrics for every RPC call. The rpcHost is logged as
// an attribute (hostname only, no credentials).
func NewTracingClient(inner Client, rpcHost string, m *TracingMetrics) Client {
	return &tracingClient{
		inner:   inner,
		tracer:  otel.Tracer(evmTracerName),
		metrics: m,
		rpcHost: rpcHost,
	}
}

type tracingClient struct {
	inner   Client
	tracer  trace.Tracer
	metrics *TracingMetrics
	rpcHost string
}

func (t *tracingClient) rpcCall(
	ctx context.Context,
	method string,
	fn func(context.Context) error,
) error {
	ctx, span := t.tracer.Start(ctx, "evm.rpc",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("rpc.method", method),
			attribute.String("rpc.target", t.rpcHost),
		),
	)
	defer span.End()

	attrs := metric.WithAttributes(attribute.String("rpc.method", method))
	start := time.Now()

	err := fn(ctx)

	t.metrics.RPCDuration.Record(ctx, time.Since(start).Seconds(), attrs)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, method+" failed")
		t.metrics.RPCErrors.Add(ctx, 1, attrs)
	}
	return err
}

func (t *tracingClient) ChainID(ctx context.Context) (result *big.Int, err error) {
	err = t.rpcCall(ctx, "eth_chainId", func(c context.Context) error {
		result, err = t.inner.ChainID(c)
		return err
	})
	return
}

func (t *tracingClient) LatestBlockNumber(ctx context.Context) (result uint64, err error) {
	err = t.rpcCall(ctx, "eth_blockNumber", func(c context.Context) error {
		result, err = t.inner.LatestBlockNumber(c)
		return err
	})
	return
}

func (t *tracingClient) GetChainInfo(ctx context.Context) (result *ChainInfo, err error) {
	err = t.rpcCall(ctx, "eth_chainInfo", func(c context.Context) error {
		result, err = t.inner.GetChainInfo(c)
		return err
	})
	return
}

func (t *tracingClient) BlockByNumber(
	ctx context.Context, number *big.Int, fullTx bool,
) (result *NormalizedBlock, err error) {
	err = t.rpcCall(ctx, "eth_getBlockByNumber", func(c context.Context) error {
		result, err = t.inner.BlockByNumber(c, number, fullTx)
		return err
	})
	return
}

func (t *tracingClient) BlockByHash(
	ctx context.Context, hash defitypes.Hash, fullTx bool,
) (result *NormalizedBlock, err error) {
	err = t.rpcCall(ctx, "eth_getBlockByHash", func(c context.Context) error {
		result, err = t.inner.BlockByHash(c, hash, fullTx)
		return err
	})
	return
}

func (t *tracingClient) TransactionByHash(
	ctx context.Context, hash defitypes.Hash,
) (result *NormalizedTransaction, err error) {
	err = t.rpcCall(ctx, "eth_getTransactionByHash", func(c context.Context) error {
		result, err = t.inner.TransactionByHash(c, hash)
		return err
	})
	return
}

func (t *tracingClient) TransactionReceipt(
	ctx context.Context, hash defitypes.Hash,
) (result *NormalizedReceipt, err error) {
	err = t.rpcCall(ctx, "eth_getTransactionReceipt", func(c context.Context) error {
		result, err = t.inner.TransactionReceipt(c, hash)
		return err
	})
	return
}

func (t *tracingClient) BalanceAt(
	ctx context.Context, address defitypes.Address, block *big.Int,
) (result *NormalizedBalance, err error) {
	err = t.rpcCall(ctx, "eth_getBalance", func(c context.Context) error {
		result, err = t.inner.BalanceAt(c, address, block)
		return err
	})
	return
}

func (t *tracingClient) CodeAt(
	ctx context.Context, address defitypes.Address, block *big.Int,
) (result *CodeResult, err error) {
	err = t.rpcCall(ctx, "eth_getCode", func(c context.Context) error {
		result, err = t.inner.CodeAt(c, address, block)
		return err
	})
	return
}

//nolint:gocritic // hugeParam: msg matches go-ethereum's CallContract signature
func (t *tracingClient) CallContract(
	ctx context.Context, msg defitypes.Call, block *big.Int,
) (result []byte, err error) {
	err = t.rpcCall(ctx, "eth_call", func(c context.Context) error {
		result, err = t.inner.CallContract(c, msg, block)
		return err
	})
	return
}

func (t *tracingClient) FilterLogs(
	ctx context.Context, q defitypes.FilterLogsQuery,
) (result []NormalizedLog, err error) {
	err = t.rpcCall(ctx, "eth_getLogs", func(c context.Context) error {
		result, err = t.inner.FilterLogs(c, q)
		return err
	})
	return
}

func (t *tracingClient) PendingNonceAt(
	ctx context.Context, address defitypes.Address,
) (result uint64, err error) {
	err = t.rpcCall(ctx, "eth_getTransactionCount", func(c context.Context) error {
		result, err = t.inner.PendingNonceAt(c, address)
		return err
	})
	return
}

func (t *tracingClient) SuggestGasPrice(ctx context.Context) (result *big.Int, err error) {
	err = t.rpcCall(ctx, "eth_gasPrice", func(c context.Context) error {
		result, err = t.inner.SuggestGasPrice(c)
		return err
	})
	return
}

func (t *tracingClient) SuggestGasTipCap(ctx context.Context) (result *big.Int, err error) {
	err = t.rpcCall(ctx, "eth_maxPriorityFeePerGas", func(c context.Context) error {
		result, err = t.inner.SuggestGasTipCap(c)
		return err
	})
	return
}

//nolint:gocritic // hugeParam: msg matches go-ethereum's EstimateGas signature
func (t *tracingClient) EstimateGas(
	ctx context.Context, msg defitypes.Call,
) (result uint64, err error) {
	err = t.rpcCall(ctx, "eth_estimateGas", func(c context.Context) error {
		result, err = t.inner.EstimateGas(c, msg)
		return err
	})
	return
}

func (t *tracingClient) SendRawTransaction(
	ctx context.Context, signedTxHex string,
) (result string, err error) {
	err = t.rpcCall(ctx, "eth_sendRawTransaction", func(c context.Context) error {
		result, err = t.inner.SendRawTransaction(c, signedTxHex)
		return err
	})
	return
}

func (t *tracingClient) Ping(ctx context.Context) error {
	return t.rpcCall(ctx, "ping", func(c context.Context) error {
		return t.inner.Ping(c)
	})
}

func (t *tracingClient) Close() {
	t.inner.Close()
}
