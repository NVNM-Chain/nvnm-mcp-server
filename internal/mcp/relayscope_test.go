// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func TestCheckRelayScope(t *testing.T) {
	anchor := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000A00")
	other := defitypes.MustAddressFromHex("0x00000000000000000000000000000000000000Ee")

	t.Run("anchor destination allowed", func(t *testing.T) {
		if err := checkRelayScope(&anchor, anchor); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
	t.Run("other contract rejected", func(t *testing.T) {
		err := checkRelayScope(&other, anchor)
		if !errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Errorf("err = %v, want ErrRelayScopeRejected", err)
		}
	})
	t.Run("contract creation (nil to) rejected", func(t *testing.T) {
		err := checkRelayScope(nil, anchor)
		if !errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Errorf("err = %v, want ErrRelayScopeRejected", err)
		}
	})
}
