// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"context"
	"errors"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// TestClassifyPrecompileRevert verifies the curated allowlist: known, safe
// precompile input-validation reasons are recognized and mapped to canonical
// text, while everything else (especially internal type paths) is left for the
// generic collapse. The classifier must only ever emit allowlisted text, never
// the raw revert string.
func TestClassifyPrecompileRevert(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantOK     bool
		wantReason string
	}{
		{
			name:       "empty metadata revert",
			err:        errors.New("estimate gas: RPC error: execution reverted: metadata cannot be empty: invalid request"),
			wantOK:     true,
			wantReason: "metadata cannot be empty",
		},
		{
			name:       "oversized checksum revert",
			err:        errors.New("estimate gas: execution reverted: checksum exceeds max length: got=66 max=64: invalid request"),
			wantOK:     true,
			wantReason: "checksum exceeds the maximum length allowed by the registry",
		},
		{
			name:   "internal cosmos type path is NOT surfaced",
			err:    errors.New("estimate gas: RPC error: -32000 desc = collections: not found: key '1' of type github.com/cosmos/gogoproto/mantrachain.anchoring.v1.Registry"),
			wantOK: false,
		},
		{
			name:   "generic upstream RPC error is NOT surfaced",
			err:    errors.New("estimate gas: dial tcp 10.0.0.1:8545: connect: connection refused"),
			wantOK: false,
		},
		{"nil error", nil, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok := classifyPrecompileRevert(tt.err)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (reason=%q)", ok, tt.wantOK, reason)
			}
			if !ok {
				return
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
			// The classifier must never echo raw chain detail.
			for _, leak := range []string{"invalid request", "execution reverted", "RPC error", "got=", "max="} {
				if strings.Contains(reason, leak) {
					t.Errorf("reason leaks raw revert detail %q: %q", leak, reason)
				}
			}
		})
	}
}

// TestBuildUnsignedTx_SurfacesPrecompileValidationReason confirms that when gas
// estimation fails with an allowlisted validation reason, PrepareAddRecord
// returns an ErrPrecompileValidation error carrying the canonical reason and
// none of the raw chain text.
func TestBuildUnsignedTx_SurfacesPrecompileValidationReason(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	mock := &mockEVMClient{
		estimateGasFn: func(_ context.Context, _ defitypes.Call) (uint64, error) {
			return 0, errors.New("RPC error: execution reverted: checksum exceeds max length: got=66 max=64: invalid request")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, abiPath, logger)

	_, err := c.PrepareAddRecord(context.Background(), PrepareAddRecordRequest{
		From:         "0x1234567890abcdef1234567890abcdef12345678",
		Registry:     "test-reg",
		URI:          "ipfs://x",
		Checksum:     strings.Repeat("a", 70),
		ChecksumAlgo: "sha256",
		Metadata:     "smoke",
	})
	if err == nil {
		t.Fatal("expected an error from gas estimation")
	}
	if !errors.Is(err, apperrors.ErrPrecompileValidation) {
		t.Errorf("error must wrap ErrPrecompileValidation so SafeForClient surfaces it; got %v", err)
	}
	if !strings.Contains(err.Error(), "checksum exceeds the maximum length") {
		t.Errorf("error must carry the canonical reason; got %v", err)
	}
	for _, leak := range []string{"RPC error", "execution reverted", "got=", "invalid request"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaks raw revert detail %q: %v", leak, err)
		}
	}
}
