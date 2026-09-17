// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"context"
	"errors"
	"fmt"
	"math/big"
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
			// Classified (as registry-not-found) but the internal proto type
			// path must never appear in the reason; the leak loop below
			// checks "gogoproto".
			name:       "collections miss on a Registry key classifies as registry not found",
			err:        errors.New("estimate gas: RPC error: -32000 desc = collections: not found: key '1' of type github.com/cosmos/gogoproto/mantrachain.anchoring.v1.Registry"),
			wantOK:     true,
			wantReason: "registry not found",
		},
		{
			// Observed 2026-09-15 for anchor_get_records {registry_id, record_id}
			// with an unknown record: the key is the (registry, record) tuple
			// and the type is uint64 -- no "registry" word, so record-not-found.
			name:       "collections miss on a record key classifies as record not found",
			err:        errors.New("records call failed: RPC error: -32000 rpc error: code = Internal desc = collections: not found: key '(\"2512\", \"77\")' of type uint64"),
			wantOK:     true,
			wantReason: "record not found",
		},
		{
			name:       "collections miss on a Record version classifies as record not found",
			err:        errors.New("collections: not found: key '(\"2512\", \"1\", \"5\")' of type github.com/cosmos/gogoproto/nvnmchain.anchoring.v1.Record"),
			wantOK:     true,
			wantReason: "record not found",
		},
		{
			name:       "record_id without registry_id",
			err:        errors.New("RPC error: -32000 rpc error: code = Internal desc = rpc error: code = InvalidArgument desc = record_id requires registry_id"),
			wantOK:     true,
			wantReason: "record_id requires registry_id: a record ID is only unique within its registry, so pass registry_id together with record_id",
		},
		{
			name:       "registry name over the precompile cap",
			err:        errors.New("RPC error: -32000 rpc error: code = Unknown desc = name exceeds max length: got=2000 max=128: invalid request"),
			wantOK:     true,
			wantReason: "name exceeds the maximum length the anchoring precompile allows (128 characters); shorten the registry name",
		},
		{
			name:       "revoke of a role the account never held",
			err:        errors.New("RPC error: -32000 rpc error: code = Unknown desc = address does not have the specified role: invalid request"),
			wantOK:     true,
			wantReason: "the account does not hold the specified role on this registry (or record), so there is nothing to revoke; check the role name and whether the grant was registry-wide or scoped to a record checksum",
		},
		{
			// grantRole's phrasing of the same on-chain role denial that
			// addRecord reports as "unauthorized". Before this entry the
			// most important denial on the surface collapsed to the generic
			// upstream failure (directory review 2026-09-15, H-2 #7).
			name:       "grantRole by a non-admin uses the shared role-denial text",
			err:        errors.New("RPC error: -32000 rpc error: code = Unknown desc = account nvnm12l4ja8hfx3ww8h2qv0snp4vwc7dt5us84mlpzy is missing role 0x6b3d724913a5b50b16768e04131c5913e11d0212e449e3b6620eb2e4000b3db8: missing required role"),
			wantOK:     true,
			wantReason: onChainRoleDenial,
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
			reason, ok, _ := classifyPrecompileRevert(tt.err)
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
			for _, leak := range []string{
				"invalid request", "execution reverted", "RPC error", "got=", "max=",
				"gogoproto", "collections:", "nvnm1", "0x6b3d", "desc =",
			} {
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
		RegistryID:   1,
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

// TestClassifyPrecompileRevert_Sentinels pins the sentinel each curated reason
// is wrapped with. The sentinel decides whether SafeForClient passes the
// reason through or collapses it to "upstream operation failed", so a reason
// mapped to a non-passthrough sentinel is silently discarded -- which is how
// an on-chain role denial used to reach callers with no explanation at all.
func TestClassifyPrecompileRevert_Sentinels(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind error
	}{
		{
			"on-chain role denial classifies as permission denied",
			errors.New("RPC error: -32000 rpc error: code = Unknown desc = unauthorized"),
			apperrors.ErrPermissionDenied,
		},
		{
			"zero version index classifies as input validation",
			errors.New("RPC error: -32000 rpc error: code = Unknown desc = index cannot be zero: invalid request"),
			apperrors.ErrPrecompileValidation,
		},
		{
			"oversized checksum classifies as input validation",
			errors.New("checksum exceeds max length: got=100 max=64: invalid request"),
			apperrors.ErrPrecompileValidation,
		},
		{
			"grantRole role denial classifies as permission denied",
			errors.New("desc = account nvnm1x is missing role 0xabc: missing required role"),
			apperrors.ErrPermissionDenied,
		},
		{
			"revoke of unheld role classifies as input validation",
			errors.New("desc = address does not have the specified role: invalid request"),
			apperrors.ErrPrecompileValidation,
		},
		{
			"record miss classifies as record not found",
			errors.New("desc = collections: not found: key '(\"1\", \"9\")' of type uint64"),
			apperrors.ErrRecordNotFound,
		},
		{
			"registry miss classifies as registry not found",
			errors.New("desc = collections: not found: key '9' of type x.anchoring.v1.Registry"),
			apperrors.ErrRegistryNotFound,
		},
		{
			"record_id without registry_id classifies as input validation",
			errors.New("desc = record_id requires registry_id"),
			apperrors.ErrPrecompileValidation,
		},
		{
			"over-long name classifies as input validation",
			errors.New("desc = name exceeds max length: got=200 max=128"),
			apperrors.ErrPrecompileValidation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, ok, kind := classifyPrecompileRevert(tt.err)
			if !ok {
				t.Fatalf("not classified: %v", tt.err)
			}
			if !errors.Is(kind, tt.kind) {
				t.Errorf("kind = %v, want %v", kind, tt.kind)
			}
			// The whole point of the sentinel: the curated reason must
			// survive the client-facing sanitizer.
			wrapped := fmt.Errorf("%s: %w", reason, kind)
			if got := apperrors.SafeForClient(wrapped); got.Error() == "upstream operation failed" {
				t.Errorf("reason collapsed by SafeForClient: %v", got)
			}
		})
	}
}

// TestCuratePrecompileErr pins the shape of the client-facing error: a
// reason wrapped in its sentinel, except when the reason IS the sentinel text
// (not-found), where doubling it would read "record not found: record not
// found".
func TestCuratePrecompileErr(t *testing.T) {
	got := curatePrecompileErr("record not found", apperrors.ErrRecordNotFound)
	if got.Error() != "record not found" {
		t.Errorf("not-found error = %q, want bare sentinel text", got.Error())
	}
	if !errors.Is(got, apperrors.ErrRecordNotFound) {
		t.Error("not-found error must still match the sentinel")
	}

	got = curatePrecompileErr("metadata cannot be empty", apperrors.ErrPrecompileValidation)
	if want := "metadata cannot be empty: precompile rejected input"; got.Error() != want {
		t.Errorf("validation error = %q, want %q", got.Error(), want)
	}
	if !errors.Is(got, apperrors.ErrPrecompileValidation) {
		t.Error("validation error must wrap the sentinel")
	}
}

// TestCallPrecompile_SurfacesCuratedReadReverts verifies the READ path now
// classifies precompile rejections the same way gas estimation does: a
// missing record and a record_id-without-registry_id call reach the caller
// as record-not-found / input-validation errors instead of the generic
// upstream collapse (directory review 2026-09-15, H-2 #4-6). An
// unrecognized RPC failure must still collapse.
func TestCallPrecompile_SurfacesCuratedReadReverts(t *testing.T) {
	abiPath := testABIPath(t)
	logger := logging.New("error")
	newClient := func(callErr error) Client {
		mock := &mockEVMClient{
			callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
				return nil, callErr
			},
		}
		return NewClient(mock, PrecompileAddress, 58887, abiPath, logger)
	}
	registryID, recordID := uint64(2512), uint64(77)

	t.Run("missing record is record not found", func(t *testing.T) {
		c := newClient(errors.New("RPC error: -32000 rpc error: code = Internal desc = " +
			"collections: not found: key '(\"2512\", \"77\")' of type uint64"))
		_, err := c.GetRecords(context.Background(), GetRecordsRequest{RegistryID: &registryID, RecordID: &recordID})
		if !errors.Is(err, apperrors.ErrRecordNotFound) {
			t.Fatalf("err = %v, want ErrRecordNotFound", err)
		}
		if got := apperrors.SafeForClient(err).Error(); got != "record not found" {
			t.Errorf("client sees %q, want %q", got, "record not found")
		}
	})
	t.Run("record_id without registry_id is an input error", func(t *testing.T) {
		c := newClient(errors.New("RPC error: -32000 rpc error: code = Internal desc = " +
			"rpc error: code = InvalidArgument desc = record_id requires registry_id"))
		_, err := c.GetRecords(context.Background(), GetRecordsRequest{RecordID: &recordID})
		if !errors.Is(err, apperrors.ErrPrecompileValidation) {
			t.Fatalf("err = %v, want ErrPrecompileValidation", err)
		}
		if !strings.Contains(apperrors.SafeForClient(err).Error(), "pass registry_id together with record_id") {
			t.Errorf("client message lacks the remediation: %v", apperrors.SafeForClient(err))
		}
	})
	t.Run("unrecognized RPC failure still collapses", func(t *testing.T) {
		c := newClient(errors.New("dial tcp 10.0.0.1:8545: connect: connection refused"))
		_, err := c.GetRecords(context.Background(), GetRecordsRequest{RegistryID: &registryID})
		if err == nil {
			t.Fatal("expected error")
		}
		if got := apperrors.SafeForClient(err).Error(); got != "upstream operation failed" {
			t.Errorf("client sees %q, want the generic collapse", got)
		}
	})
}
