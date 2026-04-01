package errors

import "errors"

// Input validation errors.
var (
	ErrInvalidAddress    = errors.New("invalid Ethereum address")
	ErrInvalidBlockRef   = errors.New("invalid block reference")
	ErrInvalidTxHash     = errors.New("invalid transaction hash")
	ErrInvalidTopics     = errors.New("invalid log topics")
	ErrInvalidABI        = errors.New("invalid ABI fragment")
	ErrMissingRequired   = errors.New("missing required parameter")
	ErrInvalidRegistryID = errors.New("invalid registry ID")
	ErrInvalidRecordID   = errors.New("invalid record ID")
	ErrInvalidChecksum   = errors.New("invalid checksum")
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
	ErrAnchorABIMissing = errors.New("anchor precompile ABI not loaded")
	ErrWriteDisabled    = errors.New("write tools are not enabled")
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

// IsInputError returns true if the error is an input validation error.
func IsInputError(err error) bool {
	return errors.Is(err, ErrInvalidAddress) ||
		errors.Is(err, ErrInvalidBlockRef) ||
		errors.Is(err, ErrInvalidTxHash) ||
		errors.Is(err, ErrInvalidTopics) ||
		errors.Is(err, ErrInvalidABI) ||
		errors.Is(err, ErrMissingRequired) ||
		errors.Is(err, ErrInvalidRegistryID) ||
		errors.Is(err, ErrInvalidRecordID) ||
		errors.Is(err, ErrInvalidChecksum)
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
