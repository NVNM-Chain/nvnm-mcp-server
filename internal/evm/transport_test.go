// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"testing"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestLimitedTransport_ErrorsPastLimit verifies that a node streaming a
// response larger than the cap is cut off with ErrNodeResponseTooLarge instead
// of being read into memory unbounded. Without this a hostile or MITM'd RPC
// node could OOM the process (EV-1).
func TestLimitedTransport_ErrorsPastLimit(t *testing.T) {
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), 1000))),
		}, nil
	})
	rt := &limitedTransport{base: base, limit: 100}

	req, _ := http.NewRequest(http.MethodPost, "http://node.example/rpc", http.NoBody)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip itself should not error: %v", err)
	}
	defer resp.Body.Close()

	_, readErr := io.ReadAll(resp.Body)
	if !errors.Is(readErr, apperrors.ErrNodeResponseTooLarge) {
		t.Fatalf("expected ErrNodeResponseTooLarge reading an oversized body, got %v", readErr)
	}
}

// TestLimitedTransport_AllowsUnderLimit confirms a normal-sized response passes
// through unchanged.
func TestLimitedTransport_AllowsUnderLimit(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	base := roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(payload)),
		}, nil
	})
	rt := &limitedTransport{base: base, limit: 1024}

	req, _ := http.NewRequest(http.MethodPost, "http://node.example/rpc", http.NoBody)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip errored: %v", err)
	}
	defer resp.Body.Close()

	got, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		t.Fatalf("reading an under-limit body errored: %v", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("body mismatch: got %q want %q", got, payload)
	}
}
