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
	// ErrLogRangeInvalid marks an eth_getLogs query the node rejects outright
	// (observed live for to_block beyond the chain head): not a width problem,
	// so it gets its own instruction rather than "narrow the range".
	ErrLogRangeInvalid = errors.New(
		"invalid block range: to_block must not exceed the current chain head " +
			"and from_block must not exceed to_block; use to_block=\"latest\" " +
			"or a block number at or below the head")
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
	// combined the deprecated registry_id lookup with any listing
	// parameter. registry_id fetches one registry by ID; name, match,
	// offset, and limit describe a listing, which is a different query
	// shape that cannot be honored at the same time. The message is the
	// client-facing text, surfaced verbatim (input class).
	ErrInvalidFilterCombination = errors.New(
		"registry_id cannot be combined with name, match, offset, limit, or key: " +
			"registry_id fetches a single registry by ID, which is a " +
			"different query shape from a registry listing; drop the other " +
			"parameters, or omit registry_id to list registries")
	// ErrMatchWithoutName marks an anchor_get_registries call that supplied
	// a match mode with no name to match against. Silently ignoring the
	// parameter would leave the caller believing a filter was applied when
	// none was. The message is the client-facing text, surfaced verbatim
	// (input class).
	ErrMatchWithoutName = errors.New(
		"match requires name: the match mode only applies to a name lookup; " +
			"supply name, or omit match for a paged listing")
	// ErrInvalidCursor marks an anchor_get_registries call whose key
	// (pagination cursor) cannot be used: it is not valid base64, or it was
	// combined with a non-zero offset, a name filter, or registry_id. The
	// cursor names a position in the unfiltered registry table, so it
	// cannot be mixed with the other ways of naming one. The message is the
	// client-facing text, surfaced verbatim (input class).
	ErrInvalidCursor = errors.New(
		"key must be the next_key from a previous unfiltered listing and " +
			"cannot be combined with a non-zero offset, name, match, or " +
			"registry_id: page with either key or offset, not both")
	// ErrInvalidHexData marks calldata (or similar free-form hex input) that
	// is not valid hex. Input class so the caller learns it is their bytes,
	// not the node, that failed.
	ErrInvalidHexData = errors.New("invalid hex data: must be 0x-prefixed hex with an even number of digits")
	// ErrInvalidRole marks a registry role outside the precompile's set. The
	// message is the client-facing text, surfaced verbatim (input class).
	ErrInvalidRole = errors.New("invalid role: must be \"admin\" or \"editor\"")
	// ErrBlockBeyondHead marks a block reference past the chain head (observed
	// live as "height N must be less than or equal to the current blockchain
	// height M"). Input class; the message is the client-facing text.
	ErrBlockBeyondHead = errors.New(
		"block number is beyond the current chain head: use \"latest\" or a " +
			"block number at or below the head (evm_get_chain_id reports it)")
	// ErrCallReverted marks an eth_call the target contract rejected. The
	// node's raw reason is never echoed (it may carry internal type paths);
	// the message is the client-facing text (input class).
	ErrCallReverted = errors.New(
		"contract call reverted: the target rejected the call (unknown function " +
			"selector, malformed calldata, or a failed require/permission check); " +
			"check `to` and `data`, and pass `from` if the function checks msg.sender")
	// ErrRelayValueRejected marks a write to the anchor precompile that carries
	// a non-zero native value. The precompile is not payable, so such a
	// transaction can only revert and burn the signer's gas; the relay refuses
	// it before broadcast. Input class; the message is the client-facing text.
	ErrRelayValueRejected = errors.New(
		"transaction not relayed: this connector only broadcasts zero-value calls " +
			"to the NVNM anchoring registry and the signed transaction carries a " +
			"non-zero value; re-prepare with the anchor_prepare_* tools (value is " +
			"always 0) and re-sign")
	// ErrTxNonceConflict marks a broadcast the node rejected because the signed
	// nonce is already used or already occupied by a pending transaction
	// (observed live as "invalid nonce; got N, expected M: tx nonce is lower
	// than account nonce"). Input class; the message is the client-facing text.
	ErrTxNonceConflict = errors.New(
		"transaction rejected: nonce conflict -- the signed nonce is already used " +
			"by a mined or pending transaction from this address; call the " +
			"anchor_prepare_* tool again for a fresh nonce, re-sign, and re-broadcast")
	// ErrTxAlreadyKnown marks a re-broadcast of a transaction the node already
	// holds (observed live as "tx already in mempool"). Input class.
	ErrTxAlreadyKnown = errors.New(
		"transaction already known to the network: this exact signed transaction " +
			"is already pending or mined; do not re-broadcast -- poll " +
			"evm_get_transaction_receipt with its hash instead")
	// ErrTxChainIDMismatch marks a signature made for a different chain
	// (observed live as "invalid chain id for signer: have 1 want 787111").
	// Input class; the message is the client-facing text.
	ErrTxChainIDMismatch = errors.New(
		"transaction signed for a different chain: sign with the chain_id " +
			"returned by the anchor_prepare_* tool for this deployment")
	// ErrInsufficientFunds marks a broadcast the node rejected because the
	// signer cannot cover gas * price + value. Input class.
	ErrInsufficientFunds = errors.New(
		"insufficient funds: the signer's balance does not cover gas for this " +
			"transaction; fund the address (nvnm_setup_wizard explains how) and " +
			"re-broadcast")
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
	ErrLogRangeInvalid,
	ErrEmptyMetadataObject,
	ErrInvalidMatchMode,
	ErrInvalidFilterCombination,
	ErrMatchWithoutName,
	ErrInvalidCursor,
	ErrInvalidHexData,
	ErrInvalidRole,
	ErrBlockBeyondHead,
	ErrCallReverted,
	ErrRelayValueRejected,
	ErrTxNonceConflict,
	ErrTxAlreadyKnown,
	ErrTxChainIDMismatch,
	ErrInsufficientFunds,
}

// curatedError pairs a client-safe sentinel with the raw upstream cause. Its
// text is the sentinel's text only, so SafeForClient (which returns input-
// class errors verbatim) never leaks the raw node output; the raw cause stays
// reachable through errors.Is and RawCause for operator-facing audit logs.
type curatedError struct {
	safe error
	raw  error
}

func (e *curatedError) Error() string   { return e.safe.Error() }
func (e *curatedError) Unwrap() []error { return []error{e.safe, e.raw} }

// Curate returns an error that reads as safe (and classifies as safe under
// errors.Is / SafeForClient) while retaining raw for diagnostics. A nil raw
// returns safe unchanged.
func Curate(safe, raw error) error {
	if raw == nil {
		return safe
	}
	return &curatedError{safe: safe, raw: raw}
}

// RawCause returns the raw upstream cause behind a curated error, or err
// itself when no curated wrapper is present. Intended for operator logs and
// audit rows, never for client-facing messages.
func RawCause(err error) error {
	var ce *curatedError
	if errors.As(err, &ce) {
		return ce.raw
	}
	return err
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
