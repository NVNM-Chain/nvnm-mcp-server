// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package errors

import "errors"

// Input validation errors.
var (
	ErrInvalidAddress    = errors.New("invalid Ethereum address")
	ErrInvalidBlockRef   = errors.New("invalid block reference")
	ErrInvalidTxHash     = errors.New("invalid transaction hash")
	ErrInvalidTopics     = errors.New("invalid log topics")
	ErrInvalidABI        = errors.New("invalid ABI fragment")
	ErrInvalidSignature  = errors.New("invalid signature")
	ErrInvalidHash       = errors.New("invalid hash")
	ErrMissingRequired   = errors.New("missing required parameter")
	ErrInvalidRegistryID = errors.New("invalid registry ID")
	ErrInvalidRecordID   = errors.New("invalid record ID")
	ErrInvalidChecksum   = errors.New("invalid checksum")
	ErrInputTooLarge     = errors.New("input exceeds maximum allowed size")
	// ErrTxDecode marks a signed transaction that could not be decoded,
	// had trailing bytes, or whose signer could not be recovered. It is a
	// caller-input rejection.
	ErrTxDecode = errors.New("decode signed transaction")
	// ErrPrecompileValidation marks a caller-input rejection that the
	// anchoring precompile reported (e.g. a value the server does not
	// pre-validate). It is treated as an input error so SafeForClient
	// surfaces the curated reason instead of collapsing it. The reason
	// text is drawn from a fixed allowlist, never from raw chain output.
	ErrPrecompileValidation = errors.New("precompile rejected input")
)

// Not-found errors.
var (
	ErrBlockNotFound    = errors.New("block not found")
	ErrTxNotFound       = errors.New("transaction not found")
	ErrRegistryNotFound = errors.New("registry not found")
	ErrRecordNotFound   = errors.New("record not found")
)

// Feature/capability errors.
var (
	ErrAnchorABIMissing       = errors.New("anchor precompile ABI not loaded")
	ErrAnchorABIMethodMissing = errors.New("anchor ABI method not found")
	ErrAnchorABIEmpty         = errors.New("anchor ABI has no methods")
	ErrInvalidChainID         = errors.New("invalid chain ID")
	ErrEmptyTxHash            = errors.New("empty transaction hash returned from broadcast")
	ErrWriteDisabled          = errors.New("write tools are not enabled")
	ErrPermissionDenied       = errors.New("permission denied")
	ErrAuthRequired           = errors.New("authentication required")
)

// Upstream errors.
var (
	ErrUpstreamRPC        = errors.New("upstream RPC error")
	ErrContractCallFailed = errors.New("contract call failed")
	ErrPrecompileCall     = errors.New("precompile call failed")
	ErrCircuitOpen        = errors.New("circuit breaker is open")
	ErrRateLimited        = errors.New("upstream rate limit exceeded")
	ErrUnexpectedType     = errors.New("unexpected result type")
)

// Client-safe sentinel errors returned by SafeForClient to avoid dynamic error construction.
var (
	errSafeCircuitOpen  = errors.New("service temporarily unavailable (circuit open)")
	errSafeRateLimited  = errors.New("service temporarily unavailable (rate limited)")
	errSafeUpstreamFail = errors.New("upstream operation failed")
)

// IsInputError returns true if the error is an input validation error.
func IsInputError(err error) bool {
	return errors.Is(err, ErrInvalidAddress) ||
		errors.Is(err, ErrInvalidBlockRef) ||
		errors.Is(err, ErrInvalidTxHash) ||
		errors.Is(err, ErrInvalidTopics) ||
		errors.Is(err, ErrInvalidABI) ||
		errors.Is(err, ErrInvalidSignature) ||
		errors.Is(err, ErrInvalidHash) ||
		errors.Is(err, ErrMissingRequired) ||
		errors.Is(err, ErrInvalidRegistryID) ||
		errors.Is(err, ErrInvalidRecordID) ||
		errors.Is(err, ErrInvalidChecksum) ||
		errors.Is(err, ErrInputTooLarge) ||
		errors.Is(err, ErrTxDecode) ||
		errors.Is(err, ErrPrecompileValidation)
}

// IsTransientError returns true if the error is a transient upstream error that may be retried.
func IsTransientError(err error) bool {
	return errors.Is(err, ErrUpstreamRPC) ||
		errors.Is(err, ErrContractCallFailed)
}

// IsNotFound returns true if the error is a not-found error.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrBlockNotFound) ||
		errors.Is(err, ErrTxNotFound) ||
		errors.Is(err, ErrRegistryNotFound) ||
		errors.Is(err, ErrRecordNotFound)
}

// SafeForClient returns a sanitized error message suitable for returning to
// external MCP clients. Input validation errors pass through unchanged.
// Upstream and internal errors are replaced with a generic message to
// prevent information leakage (URLs, hostnames, stack details).
func SafeForClient(err error) error {
	if err == nil {
		return nil
	}
	if IsInputError(err) || IsNotFound(err) {
		return err
	}
	if errors.Is(err, ErrAnchorABIMissing) || errors.Is(err, ErrWriteDisabled) {
		return err
	}
	if errors.Is(err, ErrPermissionDenied) {
		return err
	}
	if errors.Is(err, ErrAuthRequired) {
		return err
	}
	if errors.Is(err, ErrCircuitOpen) {
		return errSafeCircuitOpen
	}
	if errors.Is(err, ErrRateLimited) {
		return errSafeRateLimited
	}
	return errSafeUpstreamFail
}
