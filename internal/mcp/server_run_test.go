// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/config"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

func newRunTestServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()
	evmClient := &mockEVM{chainInfo: &evm.ChainInfo{ChainID: 58887}}
	anchorClient := &mockAnchor{info: anchor.PrecompileInfo{Address: testAddr, ChainID: 58887}}
	return NewServer(evmClient, anchorClient, cfg, nil, nil, nil, nil, nil, testLogger())
}

// TestNewServer_KeylessReadsIgnoredOnStdio covers the transport gate: a
// stdio server with MCP_KEYLESS_READS set must not enable keyless mode.
func TestNewServer_KeylessReadsIgnoredOnStdio(t *testing.T) {
	cfg := testServerConfig(false)
	cfg.KeylessReads = true
	cfg.Transport = "stdio"
	s := newRunTestServer(t, cfg)
	if s.keylessReads {
		t.Error("keylessReads should be false when transport is not http")
	}
}

// TestRunHTTP_ServesAndShutsDownOnCancel starts a full RunHTTP stack
// (rate limiters, fail limiter, key-request handler, nil origin
// allowlist so the localhost default kicks in) on an OS-assigned port,
// then cancels the context and expects a clean shutdown.
func TestRunHTTP_ServesAndShutsDownOnCancel(t *testing.T) {
	cfg := testServerConfig(false)
	cfg.Transport = "http"
	cfg.KeylessReads = true
	s := newRunTestServer(t, cfg)

	// Reserve a loopback port, then free it for the server. The tiny
	// window between Close and ListenAndServe is acceptable in a test.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	limiter := NewClientRateLimiter(100, 10)
	anonLimiter := NewAnonReadRateLimiter(100, 10, false, 1)
	failLimiter := NewIPFailRateLimiter(1, 5, false, 1)

	store, err := NewPendingKeyStore(t.TempDir() + "/pending.json")
	if err != nil {
		t.Fatalf("NewPendingKeyStore: %v", err)
	}
	krh := NewKeyRequestHandler(KeyRequestHandlerConfig{Store: store, Logger: testLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.RunHTTP(ctx, addr, nil, limiter, anonLimiter, failLimiter,
			nil, nil, krh, "https://renew.example.com", true)
	}()

	// Give the server a moment to bind before initiating shutdown.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case runErr := <-errCh:
		if runErr != nil {
			t.Fatalf("RunHTTP returned error on graceful shutdown: %v", runErr)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("RunHTTP did not shut down after context cancellation")
	}
}

// TestRunStdio_ReturnsOnCanceledContext runs the stdio transport with a
// pre-canceled context: the SDK observes ctx.Done and returns without
// consuming the test process's stdin.
func TestRunStdio_ReturnsOnCanceledContext(t *testing.T) {
	s := newRunTestServer(t, testServerConfig(false))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- s.RunStdio(ctx) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Logf("RunStdio returned %v (any prompt return is acceptable)", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunStdio did not return after context cancellation")
	}
}

// TestRunHTTP_ListenError covers the ListenAndServe failure path: the
// address is already bound, so RunHTTP must return the bind error.
func TestRunHTTP_ListenError(t *testing.T) {
	cfg := testServerConfig(false)
	cfg.Transport = "http"
	s := newRunTestServer(t, cfg)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.RunHTTP(ctx, ln.Addr().String(), nil, nil, nil, nil, nil, nil, nil, "", false)
	}()
	select {
	case runErr := <-errCh:
		if runErr == nil {
			t.Fatal("RunHTTP on an occupied port should return an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunHTTP did not return on bind failure")
	}
}
