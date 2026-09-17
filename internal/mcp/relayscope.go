// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"math/big"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// checkRelayScope enforces precompile-only relay scope: a write is permitted
// only when its destination equals the anchor precompile address AND it
// carries no native value. Every other destination -- other contracts,
// externally-owned accounts, and contract creation (to == nil) -- is rejected
// with apperrors.ErrRelayScopeRejected; a precompile call with a non-zero
// value is rejected with apperrors.ErrRelayValueRejected (the precompile is
// not payable, so the transaction could only revert and burn the signer's
// gas -- observed live 2026-09-15). Returns nil when permitted.
func checkRelayScope(to *defitypes.Address, value *big.Int, anchor defitypes.Address) error {
	if to == nil || *to != anchor {
		return apperrors.ErrRelayScopeRejected
	}
	if value != nil && value.Sign() != 0 {
		return apperrors.ErrRelayValueRejected
	}
	return nil
}

// addrString renders an optional destination address for audit logs,
// returning "" for a nil pointer (contract creation). Used so the
// signer-keyed audit record carries the destination without panicking on
// contract-creation transactions.
func addrString(to *defitypes.Address) string {
	if to == nil {
		return ""
	}
	return to.String()
}
