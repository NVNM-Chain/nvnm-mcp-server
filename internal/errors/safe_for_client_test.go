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
