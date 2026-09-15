// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func TestCheckRelayScope(t *testing.T) {
	anchor := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000A00")
	other := defitypes.MustAddressFromHex("0x00000000000000000000000000000000000000Ee")
	zero := big.NewInt(0)

	t.Run("anchor destination, zero value allowed", func(t *testing.T) {
		if err := checkRelayScope(&anchor, zero, anchor); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
	t.Run("anchor destination, nil value allowed", func(t *testing.T) {
		if err := checkRelayScope(&anchor, nil, anchor); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})
	t.Run("other contract rejected", func(t *testing.T) {
		err := checkRelayScope(&other, zero, anchor)
		if !errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Errorf("err = %v, want ErrRelayScopeRejected", err)
		}
	})
	t.Run("contract creation (nil to) rejected", func(t *testing.T) {
		err := checkRelayScope(nil, zero, anchor)
		if !errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Errorf("err = %v, want ErrRelayScopeRejected", err)
		}
	})
	// Directory review 2026-09-15 (H-3): a 1-wei transfer to the precompile
	// was broadcast and reverted on chain, burning the signer's gas, while
	// the docs promised value transfers are refused. The value gate closes
	// that gap; it must be distinguishable from the destination rejection so
	// the caller knows the destination was fine.
	t.Run("anchor destination with non-zero value rejected", func(t *testing.T) {
		err := checkRelayScope(&anchor, big.NewInt(1), anchor)
		if !errors.Is(err, apperrors.ErrRelayValueRejected) {
			t.Errorf("err = %v, want ErrRelayValueRejected", err)
		}
		if errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Error("value rejection must not read as a destination rejection")
		}
		if !apperrors.IsInputError(err) {
			t.Errorf("value rejection must be an input-class error so SafeForClient surfaces it: %v", err)
		}
	})
	t.Run("wrong destination with value reports the destination first", func(t *testing.T) {
		err := checkRelayScope(&other, big.NewInt(1), anchor)
		if !errors.Is(err, apperrors.ErrRelayScopeRejected) {
			t.Errorf("err = %v, want ErrRelayScopeRejected", err)
		}
	})
}
