// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// checkRelayScope enforces precompile-only relay scope: a write is permitted
// only when its destination equals the anchor precompile address. Every other
// destination -- other contracts, externally-owned accounts, native value
// transfers, and contract creation (to == nil) -- is rejected with
// apperrors.ErrRelayScopeRejected. Returns nil when permitted.
func checkRelayScope(to *defitypes.Address, anchor defitypes.Address) error {
	if to != nil && *to == anchor {
		return nil
	}
	return apperrors.ErrRelayScopeRejected
}
