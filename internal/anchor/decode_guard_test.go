// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// TestGuardABIDecode_PanicBecomesError verifies the boundary guard converts a
// panic in an ABI decode of untrusted node output into an ErrNodeResponseDecode
// error, rather than letting it unwind and crash the process. defiweb's ABI
// decoder is bounds-checked for the inputs exercised elsewhere, but it is an
// unaudited third-party library decoding node-controlled bytes; this guard is
// the recover() INV-6 requires at that boundary (EV-2).
func TestGuardABIDecode_PanicBecomesError(t *testing.T) {
	err := guardABIDecode(func() error {
		panic("simulated defiweb ABI decoder panic")
	})
	if !errors.Is(err, apperrors.ErrNodeResponseDecode) {
		t.Fatalf("expected ErrNodeResponseDecode, got %v", err)
	}
}

// TestGuardABIDecode_PassesThroughNormalError confirms the guard does not
// rewrite an ordinary decode error into the panic sentinel -- a graceful
// defiweb error must remain distinguishable from a recovered panic.
func TestGuardABIDecode_PassesThroughNormalError(t *testing.T) {
	sentinel := errors.New("ordinary decode error")
	err := guardABIDecode(func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected the original error to pass through, got %v", err)
	}
	if errors.Is(err, apperrors.ErrNodeResponseDecode) {
		t.Fatal("a normal error must not be reported as a decode panic")
	}
}

// TestGetRegistry_HostilePrecompileOutputHandledGracefully is an end-to-end
// regression: malformed ABI return data from an untrusted precompile/node must
// produce a non-nil error and must not crash the process. defiweb currently
// rejects this input with a clean decode error; the guard ensures that even if
// a future/exotic input panics the decoder, the boundary still returns an error.
func TestGetRegistry_HostilePrecompileOutputHandledGracefully(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			// A head word of all-0xff decodes as an astronomically large
			// dynamic offset -- past the end of the buffer.
			return bytes.Repeat([]byte{0xff}, 128), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	id := uint64(1)
	if _, err := c.GetRegistry(context.Background(), GetRegistryRequest{ID: &id}); err == nil {
		t.Fatal("expected an error for hostile precompile output, got nil")
	}
}
