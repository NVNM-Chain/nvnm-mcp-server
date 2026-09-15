// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package errors

import (
	"errors"
	"fmt"
	"testing"
)

// TestSafeForClient_PassThrough verifies every error class that must cross
// the trust boundary unchanged: input validation, not-found, and the curated
// feature/permission sentinels.
func TestSafeForClient_PassThrough(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"nil error", nil},
		{"input error", ErrInvalidAddress},
		{"wrapped input error", fmt.Errorf("validate: %w", ErrInvalidTxHash)},
		{"relay scope rejected", ErrRelayScopeRejected},
		{"precompile validation", ErrPrecompileValidation},
		{"not-found error", ErrBlockNotFound},
		{"wrapped not-found", fmt.Errorf("lookup: %w", ErrRecordNotFound)},
		{"anchor ABI missing", ErrAnchorABIMissing},
		{"write disabled", ErrWriteDisabled},
		{"permission denied", ErrPermissionDenied},
		{"wrapped permission denied", fmt.Errorf("check: %w", ErrPermissionDenied)},
		{"auth required", ErrAuthRequired},
		{"wrapped auth required", fmt.Errorf("gate: %w", ErrAuthRequired)},
		// Sentinels added for the 2026-09-15 directory review (H-2/H-3):
		// each must cross the boundary so the caller gets the reason.
		{"invalid hex data", ErrInvalidHexData},
		{"invalid role", ErrInvalidRole},
		{"block beyond head", ErrBlockBeyondHead},
		{"call reverted", ErrCallReverted},
		{"relay value rejected", ErrRelayValueRejected},
		{"tx nonce conflict", ErrTxNonceConflict},
		{"tx already known", ErrTxAlreadyKnown},
		{"tx chain id mismatch", ErrTxChainIDMismatch},
		{"insufficient funds", ErrInsufficientFunds},
		{"curated nonce conflict with raw cause", Curate(ErrTxNonceConflict, errors.New("raw node text"))},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeForClient(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("SafeForClient(nil) = %v, want nil", got)
				}
				return
			}
			if !errors.Is(got, tc.err) && got.Error() != tc.err.Error() {
				t.Errorf("SafeForClient(%v) = %v, want pass-through", tc.err, got)
			}
		})
	}
}

// TestSafeForClient_Sanitized verifies that upstream/internal errors are
// replaced with generic messages so URLs, hostnames, and stack details never
// leak to external MCP clients.
func TestSafeForClient_Sanitized(t *testing.T) {
	const leaky = "dial tcp 10.0.0.5:8545: connection refused"

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"circuit open", ErrCircuitOpen, "service temporarily unavailable (circuit open)"},
		{
			"wrapped circuit open",
			fmt.Errorf("%s: %w", leaky, ErrCircuitOpen),
			"service temporarily unavailable (circuit open)",
		},
		{"rate limited", ErrRateLimited, "service temporarily unavailable (rate limited)"},
		{
			"wrapped rate limited",
			fmt.Errorf("%s: %w", leaky, ErrRateLimited),
			"service temporarily unavailable (rate limited)",
		},
		{"upstream RPC", ErrUpstreamRPC, "upstream operation failed"},
		{"contract call failed", ErrContractCallFailed, "upstream operation failed"},
		{"precompile call failed", ErrPrecompileCall, "upstream operation failed"},
		{"unexpected type", ErrUnexpectedType, "upstream operation failed"},
		{"unclassified error", errors.New(leaky), "upstream operation failed"},
		{
			"wrapped unclassified error",
			fmt.Errorf("outer: %w", errors.New(leaky)),
			"upstream operation failed",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SafeForClient(tc.err)
			if got == nil {
				t.Fatalf("SafeForClient(%v) = nil, want sanitized error", tc.err)
			}
			if got.Error() != tc.want {
				t.Errorf("SafeForClient(%v) = %q, want %q", tc.err, got.Error(), tc.want)
			}
		})
	}
}

// TestCurate_RawCauseNeverReachesClient pins the contract of Curate: the
// error reads (and sanitizes) as the safe sentinel, matches both the sentinel
// and the raw cause under errors.Is, and RawCause recovers the raw cause for
// operator logs. A curated error that leaked its raw text through Error()
// would defeat SafeForClient's verbatim pass-through of input-class errors.
func TestCurate_RawCauseNeverReachesClient(t *testing.T) {
	raw := errors.New("invalid nonce; got 286, expected 288 [cosmossdk.io/errors]")
	curated := Curate(ErrTxNonceConflict, raw)
	wrapped := fmt.Errorf("send transaction: %w", curated)

	if curated.Error() != ErrTxNonceConflict.Error() {
		t.Errorf("Error() = %q, want the safe sentinel text only", curated.Error())
	}
	if !errors.Is(wrapped, ErrTxNonceConflict) {
		t.Error("curated error must match its safe sentinel")
	}
	if !errors.Is(wrapped, raw) {
		t.Error("curated error must still match its raw cause")
	}
	if !IsInputError(wrapped) {
		t.Error("curated input sentinel must classify as an input error")
	}
	if got := SafeForClient(wrapped).Error(); got != "send transaction: "+ErrTxNonceConflict.Error() {
		t.Errorf("SafeForClient = %q, must not contain raw text", got)
	}
	if got := RawCause(wrapped); !errors.Is(got, raw) || got.Error() != raw.Error() {
		t.Errorf("RawCause = %v, want the raw node error", got)
	}
}

func TestCurate_NilRawReturnsSafeUnchanged(t *testing.T) {
	if got := Curate(ErrBlockNotFound, nil); !errors.Is(got, ErrBlockNotFound) || got.Error() != ErrBlockNotFound.Error() {
		t.Errorf("Curate(safe, nil) = %v, want safe itself", got)
	}
	plain := errors.New("plain")
	if got := RawCause(plain); !errors.Is(got, plain) {
		t.Errorf("RawCause(plain) = %v, want plain", got)
	}
	if got := RawCause(nil); got != nil {
		t.Errorf("RawCause(nil) = %v, want nil", got)
	}
}
