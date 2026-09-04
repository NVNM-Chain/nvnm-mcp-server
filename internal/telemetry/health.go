// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/version"
)

const readinessCheckInterval = 30 * time.Second

// ReadinessChecker tests whether a downstream dependency is reachable.
type ReadinessChecker interface {
	Ping(ctx context.Context) error
}

// BlockTimestamper reports the timestamp of the chain's latest block.
// It is deliberately narrower than the EVM client interface (which this
// package cannot import without a cycle) so callers adapt their client
// down to exactly what the staleness probe needs.
type BlockTimestamper interface {
	LatestBlockTimestamp(ctx context.Context) (time.Time, error)
}

type checkResult struct {
	mu     sync.RWMutex
	status map[string]string
	ready  bool
}

// HealthServer serves /healthz, /readyz, and optionally /metrics on a
// dedicated port, separate from the MCP transport.
type HealthServer struct {
	srv         *http.Server
	ln          net.Listener
	logger      *slog.Logger
	checker     ReadinessChecker
	head        BlockTimestamper
	maxBlockAge time.Duration
	abiLoaded   bool
	check       checkResult
	stopProbe   context.CancelFunc
}

// NewHealthServer creates a health/metrics server.
// promHandler may be nil if Prometheus is disabled.
func NewHealthServer(
	addr string,
	promHandler http.Handler,
	checker ReadinessChecker,
	abiLoaded bool,
	logger *slog.Logger,
) *HealthServer {
	h := &HealthServer{
		logger:    logger,
		checker:   checker,
		abiLoaded: abiLoaded,
		check: checkResult{
			status: map[string]string{},
			ready:  true,
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.handleHealth)
	mux.HandleFunc("GET /readyz", h.handleReady)
	if promHandler != nil {
		mux.Handle("GET /metrics", promHandler)
	}

	h.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return h
}

// WithChainStaleness enables the chain-freshness readiness check: when the
// latest block's timestamp is older than maxAge, /readyz reports not_ready.
// A nil head or a maxAge of zero disables the check. Returns h for chaining.
func (h *HealthServer) WithChainStaleness(head BlockTimestamper, maxAge time.Duration) *HealthServer {
	h.head = head
	h.maxBlockAge = maxAge
	return h
}

// Listen binds the server's address without serving. Call it before Start to
// surface bind failures synchronously at boot (fail fast) instead of inside
// Start's serving goroutine, where they could only be logged.
func (h *HealthServer) Listen() error {
	ln, err := net.Listen("tcp", h.srv.Addr)
	if err != nil {
		return err
	}
	h.ln = ln
	return nil
}

// Start runs the health server and begins background readiness probes. If
// Listen was not called first, Start binds the address itself.
func (h *HealthServer) Start() error {
	if h.ln == nil {
		if err := h.Listen(); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	h.stopProbe = cancel
	go h.probeLoop(ctx)

	h.logger.Info("health/metrics server started",
		slog.String("addr", h.srv.Addr),
	)

	if err := h.srv.Serve(h.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		cancel()
		return err
	}
	return nil
}

// Close gracefully shuts down the health server.
func (h *HealthServer) Close(ctx context.Context) error {
	if h.stopProbe != nil {
		h.stopProbe()
	}
	return h.srv.Shutdown(ctx)
}

func (h *HealthServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]string{
		"status":  "ok",
		"version": version.Version,
	})
}

type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (h *HealthServer) handleReady(w http.ResponseWriter, _ *http.Request) {
	h.check.mu.RLock()
	ready := h.check.ready
	checks := make(map[string]string, len(h.check.status))
	for k, v := range h.check.status {
		checks[k] = v
	}
	h.check.mu.RUnlock()

	resp := readinessResponse{
		Status: "ready",
		Checks: checks,
	}
	if !ready {
		resp.Status = "not_ready"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, resp)
}

// writeJSON encodes v as JSON to w; accepts interface{} to serve multiple response types.
func writeJSON(w http.ResponseWriter, v interface{}) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
	}
}

func (h *HealthServer) probeLoop(ctx context.Context) {
	h.runProbe(ctx)

	ticker := time.NewTicker(readinessCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.runProbe(ctx)
		}
	}
}

func (h *HealthServer) runProbe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	checks := map[string]string{}
	ready := true

	if h.checker != nil {
		if err := h.checker.Ping(probeCtx); err != nil {
			checks["evm_rpc"] = "unreachable"
			ready = false
		} else {
			checks["evm_rpc"] = "ok"
		}
	}

	if h.head != nil && h.maxBlockAge > 0 {
		if ok := h.probeChainHead(probeCtx, checks); !ok {
			ready = false
		}
	}

	if h.abiLoaded {
		checks["abi"] = "loaded"
	} else {
		checks["abi"] = "not_configured"
	}

	h.check.mu.Lock()
	h.check.ready = ready
	h.check.status = checks
	h.check.mu.Unlock()
}

// probeChainHead records the chain-freshness check result in checks and
// reports whether the chain counts as advancing. An RPC that answers Ping
// but serves a latest block older than maxBlockAge is a halted chain: every
// write would silently fail to land, so readiness must go false.
func (h *HealthServer) probeChainHead(ctx context.Context, checks map[string]string) bool {
	ts, err := h.head.LatestBlockTimestamp(ctx)
	if err != nil {
		checks["chain_head"] = "unavailable"
		return false
	}
	if age := time.Since(ts); age > h.maxBlockAge {
		checks["chain_head"] = fmt.Sprintf("stale (last block %s old, max %s)",
			age.Round(time.Second), h.maxBlockAge)
		return false
	}
	checks["chain_head"] = "ok"
	return true
}
