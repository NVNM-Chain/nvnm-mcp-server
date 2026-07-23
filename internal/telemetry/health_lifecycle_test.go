// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// freeLoopbackAddr reserves an ephemeral loopback port and releases it so the
// HealthServer under test can bind it. The tiny reuse race is acceptable in a
// hermetic unit test.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func waitForServer(t *testing.T, url string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec,noctx // loopback URL built by the test
		if err == nil {
			return resp
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable at %s: %v", url, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestHealthServer_StartServeClose exercises the full lifecycle: Start (which
// launches probeLoop), live /healthz, /readyz, and /metrics endpoints, then
// graceful Close returning nil from Start.
func TestHealthServer_StartServeClose(t *testing.T) {
	addr := freeLoopbackAddr(t)
	prom := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "# metrics")
	})
	srv := NewHealthServer(addr, prom, &mockChecker{}, true, testLogger())

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	base := "http://" + addr

	resp := waitForServer(t, base+"/healthz")
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/healthz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close body: %v", err)
	}

	resp, err := http.Get(base + "/readyz") //nolint:noctx // loopback URL built by the test
	if err != nil {
		t.Fatalf("/readyz: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/readyz status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close body: %v", err)
	}

	resp, err = http.Get(base + "/metrics") //nolint:noctx // loopback URL built by the test
	if err != nil {
		t.Fatalf("/metrics: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read /metrics body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("/metrics status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if !strings.Contains(string(body), "# metrics") {
		t.Errorf("/metrics body = %q, want prom handler output", string(body))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Shut the embedded server down directly: h.srv was written before the
	// Start goroutine launched, so this is race-free, while calling Close
	// here would read stopProbe concurrently with Start's write.
	if err := srv.srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned %v after graceful shutdown, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}

	// Start has returned (synchronized via errCh), so Close may now safely
	// read stopProbe and stop the probe loop.
	if err := srv.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestHealthServer_ProbeLoopStopsOnCancel covers probeLoop's ctx.Done branch
// directly: with an already-canceled context it runs one probe and returns.
func TestHealthServer_ProbeLoopStopsOnCancel(t *testing.T) {
	srv := NewHealthServer(freeLoopbackAddr(t), nil, &mockChecker{}, true, testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.probeLoop(ctx)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("probeLoop did not return after context cancellation")
	}
}

// TestHealthServer_StartListenError covers the error branch where the listen
// address is unusable and Start must cancel the probe loop and report.
func TestHealthServer_StartListenError(t *testing.T) {
	srv := NewHealthServer("127.0.0.1:-1", nil, nil, false, testLogger())
	if err := srv.Start(); err == nil {
		t.Fatal("Start with invalid address returned nil, want error")
	}
}

// TestHealthServer_CloseWithoutStart covers Close's nil-stopProbe branch.
func TestHealthServer_CloseWithoutStart(t *testing.T) {
	srv := NewHealthServer(freeLoopbackAddr(t), nil, nil, false, testLogger())
	if err := srv.Close(context.Background()); err != nil {
		t.Fatalf("Close without Start: %v", err)
	}
}

// TestWriteJSON_EncodeError covers the encode-failure branch: a channel is
// not JSON-serializable, so the handler must respond 500.
func TestWriteJSON_EncodeError(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, make(chan int))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "encode error") {
		t.Errorf("body = %q, want encode error message", w.Body.String())
	}
}
