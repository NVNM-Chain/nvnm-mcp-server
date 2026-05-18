// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsInputError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ErrInvalidAddress", ErrInvalidAddress, true},
		{"ErrInvalidBlockRef", ErrInvalidBlockRef, true},
		{"ErrInvalidTxHash", ErrInvalidTxHash, true},
		{"ErrInvalidTopics", ErrInvalidTopics, true},
		{"ErrInvalidABI", ErrInvalidABI, true},
		{"ErrMissingRequired", ErrMissingRequired, true},
		{"ErrInvalidRegistryID", ErrInvalidRegistryID, true},
		{"ErrInvalidRecordID", ErrInvalidRecordID, true},
		{"ErrInvalidChecksum", ErrInvalidChecksum, true},
		{"wrapped input error", fmt.Errorf("context: %w", ErrInvalidAddress), true},
		{"ErrBlockNotFound is not input error", ErrBlockNotFound, false},
		{"ErrUpstreamRPC is not input error", ErrUpstreamRPC, false},
		{"ErrCircuitOpen is not input error", ErrCircuitOpen, false},
		{"ErrRateLimited is not input error", ErrRateLimited, false},
		{"unrelated error", errors.New("something else"), false},
		{"nil error", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsInputError(tc.err)
			if got != tc.want {
				t.Errorf("IsInputError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ErrUpstreamRPC", ErrUpstreamRPC, true},
		{"ErrContractCallFailed", ErrContractCallFailed, true},
		{"wrapped upstream", fmt.Errorf("context: %w", ErrUpstreamRPC), true},
		{"ErrCircuitOpen is not transient", ErrCircuitOpen, false},
		{"ErrRateLimited is not transient", ErrRateLimited, false},
		{"ErrInvalidAddress is not transient", ErrInvalidAddress, false},
		{"nil error", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTransientError(tc.err)
			if got != tc.want {
				t.Errorf("IsTransientError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"ErrBlockNotFound", ErrBlockNotFound, true},
		{"ErrTxNotFound", ErrTxNotFound, true},
		{"ErrRegistryNotFound", ErrRegistryNotFound, true},
		{"ErrRecordNotFound", ErrRecordNotFound, true},
		{"wrapped not-found", fmt.Errorf("lookup: %w", ErrRegistryNotFound), true},
		{"ErrInvalidAddress is not not-found", ErrInvalidAddress, false},
		{"ErrUpstreamRPC is not not-found", ErrUpstreamRPC, false},
		{"unrelated error", errors.New("something else"), false},
		{"nil error", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsNotFound(tc.err)
			if got != tc.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSentinelErrors_AreDistinct(t *testing.T) {
	allErrors := []error{
		ErrInvalidAddress, ErrInvalidBlockRef, ErrInvalidTxHash, ErrInvalidTopics,
		ErrInvalidABI, ErrMissingRequired, ErrInvalidRegistryID, ErrInvalidRecordID,
		ErrInvalidChecksum, ErrBlockNotFound, ErrTxNotFound, ErrRegistryNotFound,
		ErrRecordNotFound, ErrAnchorABIMissing, ErrWriteDisabled,
		ErrUpstreamRPC, ErrContractCallFailed, ErrPrecompileCall,
		ErrCircuitOpen, ErrRateLimited,
	}

	for i, a := range allErrors {
		for j, b := range allErrors {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors should be distinct: errors.Is(%q, %q) = true", a, b)
			}
		}
	}
}

func TestSentinelErrors_HaveMessages(t *testing.T) {
	allErrors := []error{
		ErrInvalidAddress, ErrInvalidBlockRef, ErrInvalidTxHash, ErrInvalidTopics,
		ErrInvalidABI, ErrMissingRequired, ErrInvalidRegistryID, ErrInvalidRecordID,
		ErrInvalidChecksum, ErrBlockNotFound, ErrTxNotFound, ErrRegistryNotFound,
		ErrRecordNotFound, ErrAnchorABIMissing, ErrWriteDisabled,
		ErrUpstreamRPC, ErrContractCallFailed, ErrPrecompileCall,
		ErrCircuitOpen, ErrRateLimited,
	}

	for _, err := range allErrors {
		if err.Error() == "" {
			t.Errorf("sentinel error has empty message: %v", err)
		}
	}
}
