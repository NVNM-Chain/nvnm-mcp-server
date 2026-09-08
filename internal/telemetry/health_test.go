// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/version"
)

type mockChecker struct {
	err error
}

func (m *mockChecker) Ping(_ context.Context) error { return m.err }

type mockHead struct {
	ts  time.Time
	err error
}

func (m *mockHead) LatestBlockTimestamp(_ context.Context) (time.Time, error) {
	return m.ts, m.err
}

func TestHandleHealth(t *testing.T) {
	srv := NewHealthServer(":0", nil, nil, true, testLogger())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("status = %q, want %q", resp["status"], "ok")
	}
	if resp["version"] != version.Version {
		t.Errorf("version = %q, want %q", resp["version"], version.Version)
	}
}

func TestHandleReady_AllHealthy(t *testing.T) {
	checker := &mockChecker{}
	srv := NewHealthServer(":0", nil, checker, true, testLogger())
	srv.runProbe(context.Background())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)

	srv.handleReady(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp readinessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "ready" {
		t.Errorf("status = %q, want %q", resp.Status, "ready")
	}
	if resp.Checks["evm_rpc"] != "ok" {
		t.Errorf("evm_rpc = %q, want %q", resp.Checks["evm_rpc"], "ok")
	}
	if resp.Checks["abi"] != "loaded" {
		t.Errorf("abi = %q, want %q", resp.Checks["abi"], "loaded")
	}
}

func TestHandleReady_RPCDown(t *testing.T) {
	checker := &mockChecker{err: errors.New("connection refused")}
	srv := NewHealthServer(":0", nil, checker, true, testLogger())
	srv.runProbe(context.Background())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)

	srv.handleReady(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}

	var resp readinessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "not_ready" {
		t.Errorf("status = %q, want %q", resp.Status, "not_ready")
	}
	if resp.Checks["evm_rpc"] != "unreachable" {
		t.Errorf("evm_rpc = %q, want %q", resp.Checks["evm_rpc"], "unreachable")
	}
}

// TestHandleReady_ChainStaleness covers the chain-freshness probe: a fresh
// head keeps readiness green, a stale head or an unavailable head flips it
// red (an RPC that answers Ping while the chain is halted must not report
// ready — the 2026-08-21 testnet halt stayed "ready" for three days), and
// a zero threshold disables the check entirely.
func TestHandleReady_ChainStaleness(t *testing.T) {
	const maxAge = 5 * time.Minute

	tests := []struct {
		name       string
		head       *mockHead
		maxAge     time.Duration
		wantStatus string
		wantCheck  string // prefix of checks["chain_head"]; "" = key absent
	}{
		{
			name:       "fresh head is ready",
			head:       &mockHead{ts: time.Now()},
			maxAge:     maxAge,
			wantStatus: "ready",
			wantCheck:  "ok",
		},
		{
			name:       "stale head is not ready",
			head:       &mockHead{ts: time.Now().Add(-2 * maxAge)},
			maxAge:     maxAge,
			wantStatus: "not_ready",
			wantCheck:  "stale",
		},
		{
			name:       "unavailable head is not ready",
			head:       &mockHead{err: errors.New("boom")},
			maxAge:     maxAge,
			wantStatus: "not_ready",
			wantCheck:  "unavailable",
		},
		{
			name:       "zero threshold disables the check",
			head:       &mockHead{ts: time.Now().Add(-24 * time.Hour)},
			maxAge:     0,
			wantStatus: "ready",
			wantCheck:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewHealthServer(":0", nil, &mockChecker{}, true, testLogger()).
				WithChainStaleness(tt.head, tt.maxAge)
			srv.runProbe(context.Background())

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)
			srv.handleReady(w, req)

			var resp readinessResponse
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", resp.Status, tt.wantStatus)
			}
			got, present := resp.Checks["chain_head"]
			if tt.wantCheck == "" {
				if present {
					t.Errorf("chain_head = %q, want absent", got)
				}
				return
			}
			if !strings.HasPrefix(got, tt.wantCheck) {
				t.Errorf("chain_head = %q, want prefix %q", got, tt.wantCheck)
			}
		})
	}
}

func TestHandleReady_ABINotLoaded(t *testing.T) {
	srv := NewHealthServer(":0", nil, nil, false, testLogger())
	srv.runProbe(context.Background())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", http.NoBody)

	srv.handleReady(w, req)

	var resp readinessResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Checks["abi"] != "not_configured" {
		t.Errorf("abi = %q, want %q", resp.Checks["abi"], "not_configured")
	}
}
