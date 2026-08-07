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
	// ErrRelayScopeRejected marks a write whose destination is not the NVNM
	// anchor precompile, rejected by precompile-only relay scope. The message
	// is the client-facing text (input-validation class, surfaced verbatim).
	ErrRelayScopeRejected = errors.New(
		"transaction not relayed: this connector only broadcasts writes to the " +
			"NVNM anchoring registry (reads of any data are unrestricted); other " +
			"transaction destinations are rejected")
	// ErrPrecompileValidation marks a caller-input rejection that the
	// anchoring precompile reported (e.g. a value the server does not
	// pre-validate). It is treated as an input error so SafeForClient
	// surfaces the curated reason instead of collapsing it. The reason
	// text is drawn from a fixed allowlist, never from raw chain output.
	ErrPrecompileValidation = errors.New("precompile rejected input")
	// ErrLogRangeTooWide marks an eth_getLogs query whose block window exceeds
	// the upstream node's configured maximum from-to distance. Input class so
	// SafeForClient surfaces this message instead of collapsing it; the node's
	// raw error (including its configured limit) is never echoed. The message
	// is the client-facing text, surfaced verbatim.
	ErrLogRangeTooWide = errors.New(
		"block range too wide: the upstream node caps the from_block-to_block " +
			"distance for log queries; narrow the range and retry")
	// ErrEmptyMetadataObject marks the literal empty JSON object "{}" passed
	// as record metadata, which the anchoring precompile rejects on-chain.
	// The message is the client-facing text, surfaced verbatim (input class);
	// a classifier wrap would leak its own text into the message.
	ErrEmptyMetadataObject = errors.New(
		"metadata must not be the empty JSON object \"{}\" (the anchoring " +
			"precompile rejects it); pass a non-empty value such as a short " +
			"label or a JSON object with at least one field")
	// ErrInvalidMatchMode marks an anchor_get_registries name-match mode
	// outside the supported set. The message is the client-facing text,
	// surfaced verbatim (input class).
	ErrInvalidMatchMode = errors.New(
		"invalid match mode: must be exact, prefix, suffix, or contains")
	// ErrInvalidFilterCombination marks an anchor_get_registries call that
	// combined the client-side name filter with registry_id, offset, or
	// limit. The name filter scans the entire registry table and returns
	// every match; those other filters describe a different (single-page,
	// ID-scoped) query shape that cannot be honored at the same time. The
	// message is the client-facing text, surfaced verbatim (input class).
	ErrInvalidFilterCombination = errors.New(
		"name cannot be combined with registry_id, offset, or limit: " +
			"the name filter scans and returns all matches, which is a " +
			"different query shape from a registry_id/offset/limit page")
	// ErrMatchWithoutName marks an anchor_get_registries call that supplied
	// a match mode with no name to match against. Silently ignoring the
	// parameter would leave the caller believing a filter was applied when
	// none was. The message is the client-facing text, surfaced verbatim
	// (input class).
	ErrMatchWithoutName = errors.New(
		"match requires name: the match mode only applies to a name lookup; " +
			"supply name, or omit match for a paged listing")
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
	// ErrNodeResponseDecode marks a node/RPC response that could not be
	// decoded -- including a decode that panicked in the underlying
	// defiweb/go-eth library on malformed input. Node responses are untrusted
	// (plaintext http:// is permitted); an unrecovered panic on the stdio
	// transport would crash the process, so decode panics on node responses
	// are converted to this error (EV-2).
	ErrNodeResponseDecode = errors.New("decode of node response failed")
	// ErrNodeResponseTooLarge marks a node/RPC response body that exceeded the
	// maximum size the client will read. A hostile or MITM'd node could
	// otherwise stream an unbounded reply and exhaust process memory (EV-1).
	ErrNodeResponseTooLarge = errors.New("node response exceeds maximum allowed size")
)

// Client-safe sentinel errors returned by SafeForClient to avoid dynamic error construction.
var (
	errSafeCircuitOpen  = errors.New("service temporarily unavailable (circuit open)")
	errSafeRateLimited  = errors.New("service temporarily unavailable (rate limited)")
	errSafeUpstreamFail = errors.New("upstream operation failed")
)

// inputErrors enumerates the input-validation sentinels. Membership means
// SafeForClient surfaces the full error text to the client verbatim, so add
// a sentinel here only if its message (and any wrapping context) is safe to
// disclose.
var inputErrors = []error{
	ErrInvalidAddress,
	ErrInvalidBlockRef,
	ErrInvalidTxHash,
	ErrInvalidTopics,
	ErrInvalidABI,
	ErrInvalidSignature,
	ErrInvalidHash,
	ErrMissingRequired,
	ErrInvalidRegistryID,
	ErrInvalidRecordID,
	ErrInvalidChecksum,
	ErrInputTooLarge,
	ErrTxDecode,
	ErrRelayScopeRejected,
	ErrPrecompileValidation,
	ErrLogRangeTooWide,
	ErrEmptyMetadataObject,
	ErrInvalidMatchMode,
	ErrInvalidFilterCombination,
	ErrMatchWithoutName,
}

// IsInputError returns true if the error is an input validation error.
func IsInputError(err error) bool {
	for _, target := range inputErrors {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
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
