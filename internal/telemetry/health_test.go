package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/inveniam/nvnm-mcp-server/internal/version"
)

type mockChecker struct {
	err error
}

func (m *mockChecker) Ping(_ context.Context) error { return m.err }

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
