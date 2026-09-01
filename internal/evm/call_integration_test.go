// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package evm_test

import (
	"context"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
)

// TestIntegration_CallContract_EOAEmptyCalldata is the one eth_call whose
// result is deterministic on any chain: an account with no code returns
// empty data and does not revert.
func TestIntegration_CallContract_EOAEmptyCalldata(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	to := defitypes.MustAddressFromHex("0x9f8a6425F7AD925701fE1CdF85fd883340b2A9CD")
	result, err := c.CallContract(ctx, defitypes.Call{
		To:    &to,
		Input: []byte{},
	}, nil)
	if err != nil {
		t.Fatalf("CallContract: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("result = %x, want empty return data from an EOA", result)
	}
}
