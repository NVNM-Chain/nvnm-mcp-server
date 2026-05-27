// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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

type checkResult struct {
	mu     sync.RWMutex
	status map[string]string
	ready  bool
}

// HealthServer serves /healthz, /readyz, and optionally /metrics on a
// dedicated port, separate from the MCP transport.
type HealthServer struct {
	srv       *http.Server
	logger    *slog.Logger
	checker   ReadinessChecker
	abiLoaded bool
	check     checkResult
	stopProbe context.CancelFunc
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

// Start runs the health server and begins background readiness probes.
func (h *HealthServer) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	h.stopProbe = cancel
	go h.probeLoop(ctx)

	h.logger.Info("health/metrics server started",
		slog.String("addr", h.srv.Addr),
	)

	if err := h.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
