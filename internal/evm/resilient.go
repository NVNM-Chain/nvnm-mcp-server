package evm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"time"

	"github.com/cenkalti/backoff/v5"
	defitypes "github.com/defiweb/go-eth/types"
	gobreaker "github.com/sony/gobreaker/v2"
	"golang.org/x/time/rate"

	ierrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/telemetry"
)

// ResilientConfig configures the resilience wrapper for the EVM client.
type ResilientConfig struct {
	// MaxRetries is the maximum number of retry attempts after the initial call.
	MaxRetries int
	// InitialBackoff is the initial backoff interval between retries.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff interval between retries.
	MaxBackoff time.Duration
	// RateLimit is the sustained requests-per-second limit.
	RateLimit float64
	// RateBurst is the maximum burst size for the rate limiter.
	RateBurst int
	// BreakerThreshold is the number of consecutive failures before the circuit opens.
	BreakerThreshold uint32
	// BreakerTimeout is the duration the circuit stays open before moving to half-open.
	BreakerTimeout time.Duration
}

type resilientClient struct {
	inner   Client
	breaker *gobreaker.CircuitBreaker[any]
	limiter *rate.Limiter
	cfg     ResilientConfig
	metrics *telemetry.Metrics
	logger  *slog.Logger
}

// NewResilientClient wraps an existing Client with retry (exponential backoff),
// rate limiting, and circuit breaker functionality. Close and Ping are delegated
// directly without resilience wrapping.
func NewResilientClient(inner Client, cfg ResilientConfig, metrics *telemetry.Metrics, logger *slog.Logger) Client {
	cbSettings := gobreaker.Settings{
		Name: "evm-rpc",
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= cfg.BreakerThreshold
		},
		Timeout: cfg.BreakerTimeout,
		OnStateChange: func(name string, from, to gobreaker.State) {
			logger.Warn("circuit breaker state change",
				slog.String("name", name),
				slog.String("from", from.String()),
				slog.String("to", to.String()),
			)
			var stateVal int64
			switch to {
			case gobreaker.StateClosed:
				stateVal = 0
			case gobreaker.StateHalfOpen:
				stateVal = 1
			case gobreaker.StateOpen:
				stateVal = 2
			}
			metrics.CircuitBreakerState.Record(context.Background(), stateVal)
		},
	}

	return &resilientClient{
		inner:   inner,
		breaker: gobreaker.NewCircuitBreaker[any](cbSettings),
		limiter: rate.NewLimiter(rate.Limit(cfg.RateLimit), cfg.RateBurst),
		cfg:     cfg,
		metrics: metrics,
		logger:  logger,
	}
}

// resilientCall executes fn with rate limiting, circuit breaker, and retry.
func resilientCall[T any](
	ctx context.Context, r *resilientClient, method string, fn func(context.Context) (T, error),
) (T, error) {
	var zero T

	if err := r.limiter.Wait(ctx); err != nil {
		r.metrics.RPCRateLimited.Add(ctx, 1)
		return zero, fmt.Errorf("%w: %w", ierrors.ErrRateLimited, err)
	}

	// Clamp MaxRetries to >= 0 and convert to uint in the same basic
	// block so gosec G115 sees the bounds check immediately before the
	// cast. Config validation already rejects negative values; the cast
	// happens at use-site so the linter does not have to reason across
	// the closure boundary below.
	maxRetries := r.cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	maxTries := uint(maxRetries) + 1

	result, err := r.breaker.Execute(func() (any, error) {
		b := backoff.NewExponentialBackOff()
		b.InitialInterval = r.cfg.InitialBackoff
		b.MaxInterval = r.cfg.MaxBackoff

		val, retryErr := backoff.Retry(ctx, func() (T, error) {
			res, callErr := fn(ctx)
			if callErr != nil {
				if isTransientRPCError(callErr) {
					return zero, callErr
				}
				return zero, backoff.Permanent(callErr)
			}
			return res, nil
		},
			backoff.WithBackOff(b),
			backoff.WithMaxTries(maxTries),
			backoff.WithMaxElapsedTime(0),
			backoff.WithNotify(func(err error, d time.Duration) {
				r.metrics.RPCRetryCount.Add(ctx, 1)
				r.logger.Debug("retrying RPC call",
					slog.String("method", method),
					slog.String("error", err.Error()),
					slog.Duration("backoff", d),
				)
			}),
		)
		if retryErr != nil {
			return nil, retryErr
		}
		return val, nil
	})

	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return zero, fmt.Errorf("%w: %w", ierrors.ErrCircuitOpen, err)
		}
		return zero, err
	}

	if result == nil {
		return zero, nil
	}
	typed, ok := result.(T)
	if !ok {
		return zero, fmt.Errorf("%w: %T", ierrors.ErrUnexpectedType, result)
	}
	return typed, nil
}

// cometReceiptsRaceMarker is the upstream Cosmos-EVM error chain that
// appears when eth_gasPrice walks the latest block's receipts and finds
// a tx we just broadcast is in the block but not yet in the receipt
// index. The race settles in 1-3 seconds. The marker is intentionally
// specific to the nested "comet receipts" chain so we do NOT retry on
// legitimate tx-not-found-by-hash errors from get_transaction_receipt.
//
// If upstream rewords this chain, integration tests against testnet
// will surface the regression by going flaky again; the fix is to
// update this constant.
const cometReceiptsRaceMarker = "failed to get receipts from comet block"

func isTransientRPCError(err error) bool {
	if ierrors.IsTransientError(err) {
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if strings.Contains(err.Error(), cometReceiptsRaceMarker) {
		return true
	}
	return false
}

func (r *resilientClient) ChainID(ctx context.Context) (*big.Int, error) {
	return resilientCall(ctx, r, "eth_chainId", func(ctx context.Context) (*big.Int, error) {
		return r.inner.ChainID(ctx)
	})
}

func (r *resilientClient) LatestBlockNumber(ctx context.Context) (uint64, error) {
	return resilientCall(ctx, r, "eth_blockNumber", func(ctx context.Context) (uint64, error) {
		return r.inner.LatestBlockNumber(ctx)
	})
}

func (r *resilientClient) GetChainInfo(ctx context.Context) (*ChainInfo, error) {
	return resilientCall(ctx, r, "eth_chainInfo", func(ctx context.Context) (*ChainInfo, error) {
		return r.inner.GetChainInfo(ctx)
	})
}

func (r *resilientClient) BlockByNumber(ctx context.Context, number *big.Int, fullTx bool) (*NormalizedBlock, error) {
	return resilientCall(ctx, r, "eth_getBlockByNumber", func(ctx context.Context) (*NormalizedBlock, error) {
		return r.inner.BlockByNumber(ctx, number, fullTx)
	})
}

func (r *resilientClient) BlockByHash(ctx context.Context, hash defitypes.Hash, fullTx bool) (*NormalizedBlock, error) {
	return resilientCall(ctx, r, "eth_getBlockByHash", func(ctx context.Context) (*NormalizedBlock, error) {
		return r.inner.BlockByHash(ctx, hash, fullTx)
	})
}

func (r *resilientClient) TransactionByHash(ctx context.Context, hash defitypes.Hash) (*NormalizedTransaction, error) {
	return resilientCall(ctx, r, "eth_getTransactionByHash", func(ctx context.Context) (*NormalizedTransaction, error) {
		return r.inner.TransactionByHash(ctx, hash)
	})
}

func (r *resilientClient) TransactionReceipt(ctx context.Context, hash defitypes.Hash) (*NormalizedReceipt, error) {
	return resilientCall(ctx, r, "eth_getTransactionReceipt", func(ctx context.Context) (*NormalizedReceipt, error) {
		return r.inner.TransactionReceipt(ctx, hash)
	})
}

func (r *resilientClient) BalanceAt(
	ctx context.Context, address defitypes.Address, block *big.Int,
) (*NormalizedBalance, error) {
	return resilientCall(ctx, r, "eth_getBalance", func(ctx context.Context) (*NormalizedBalance, error) {
		return r.inner.BalanceAt(ctx, address, block)
	})
}

func (r *resilientClient) CodeAt(ctx context.Context, address defitypes.Address, block *big.Int) (*CodeResult, error) {
	return resilientCall(ctx, r, "eth_getCode", func(ctx context.Context) (*CodeResult, error) {
		return r.inner.CodeAt(ctx, address, block)
	})
}

//nolint:gocritic // hugeParam: msg matches go-ethereum's CallContract signature
func (r *resilientClient) CallContract(ctx context.Context, msg defitypes.Call, block *big.Int) ([]byte, error) {
	return resilientCall(ctx, r, "eth_call", func(ctx context.Context) ([]byte, error) {
		return r.inner.CallContract(ctx, msg, block)
	})
}

func (r *resilientClient) FilterLogs(ctx context.Context, q defitypes.FilterLogsQuery) ([]NormalizedLog, error) {
	return resilientCall(ctx, r, "eth_getLogs", func(ctx context.Context) ([]NormalizedLog, error) {
		return r.inner.FilterLogs(ctx, q)
	})
}

func (r *resilientClient) PendingNonceAt(ctx context.Context, address defitypes.Address) (uint64, error) {
	return resilientCall(ctx, r, "eth_getTransactionCount", func(ctx context.Context) (uint64, error) {
		return r.inner.PendingNonceAt(ctx, address)
	})
}

func (r *resilientClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return resilientCall(ctx, r, "eth_gasPrice", func(ctx context.Context) (*big.Int, error) {
		return r.inner.SuggestGasPrice(ctx)
	})
}

func (r *resilientClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return resilientCall(ctx, r, "eth_maxPriorityFeePerGas", func(ctx context.Context) (*big.Int, error) {
		return r.inner.SuggestGasTipCap(ctx)
	})
}

//nolint:gocritic // hugeParam: msg matches go-ethereum's EstimateGas signature
func (r *resilientClient) EstimateGas(ctx context.Context, msg defitypes.Call) (uint64, error) {
	return resilientCall(ctx, r, "eth_estimateGas", func(ctx context.Context) (uint64, error) {
		return r.inner.EstimateGas(ctx, msg)
	})
}

// SendRawTransaction bypasses retry logic (transaction submission is not idempotent)
// but still applies rate limiting and circuit breaker protection.
func (r *resilientClient) SendRawTransaction(ctx context.Context, signedTxHex string) (string, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		r.metrics.RPCRateLimited.Add(ctx, 1)
		return "", fmt.Errorf("%w: %w", ierrors.ErrRateLimited, err)
	}

	result, err := r.breaker.Execute(func() (any, error) {
		return r.inner.SendRawTransaction(ctx, signedTxHex)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			return "", fmt.Errorf("%w: %w", ierrors.ErrCircuitOpen, err)
		}
		return "", err
	}
	typed, ok := result.(string)
	if !ok {
		return "", fmt.Errorf("%w: %T", ierrors.ErrUnexpectedType, result)
	}
	return typed, nil
}

func (r *resilientClient) Ping(ctx context.Context) error {
	return r.inner.Ping(ctx)
}

func (r *resilientClient) Close() {
	r.inner.Close()
}
