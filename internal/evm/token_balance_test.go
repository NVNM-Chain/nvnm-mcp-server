// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

const (
	testFeeToken  = "0x20c0000000000000000000000000000000000000"
	testFeeHolder = "0x50d476e5d2c044998eC185D86A0EF5CB32f3e8F4"
)

// word left-pads n to one 32-byte ABI word.
func word(n int64) []byte {
	out := make([]byte, abiWordLen)
	big.NewInt(n).FillBytes(out)
	return out
}

// TestTokenBalanceAt pins the balanceOf call and the scaling. The values
// are the ones observed on chain 787222 (2026-11-09): balanceOf of the
// holder returned 0x05f5105c = 99,946,588 base units at 6 decimals.
func TestTokenBalanceAt(t *testing.T) {
	rpc := &fakeRPC{callOut: word(99_946_588)}
	c := newFakeClient(rpc)
	token := defitypes.MustAddressFromHex(testFeeToken)
	holder := defitypes.MustAddressFromHex(testFeeHolder)

	got, err := TokenBalanceAt(context.Background(), c, token, holder, 6, nil)
	if err != nil {
		t.Fatalf("TokenBalanceAt: %v", err)
	}
	if got.Wei != "99946588" {
		t.Errorf("Wei = %q, want 99946588", got.Wei)
	}
	if got.Ether != "99.946588" {
		t.Errorf("Ether = %q, want 99.946588", got.Ether)
	}
	if got.Address != AddressHex(holder) {
		t.Errorf("Address = %q, want %q", got.Address, AddressHex(holder))
	}

	if rpc.gotCall == nil || rpc.gotCall.To == nil || *rpc.gotCall.To != token {
		t.Fatalf("call target = %+v, want the token contract", rpc.gotCall)
	}
	// selector + 12 zero bytes + the holder's public address.
	wantInput := "70a08231" + "000000000000000000000000" +
		"50d476e5d2c044998ec185d86a0ef5cb32f3e8f4" // pragma: allowlist secret -- public address
	if gotInput := hex.EncodeToString(rpc.gotCall.Input); gotInput != wantInput {
		t.Errorf("calldata = %s, want balanceOf(holder) %s", gotInput, wantInput)
	}
}

func TestTokenBalanceAt_Zero(t *testing.T) {
	c := newFakeClient(&fakeRPC{callOut: word(0)})
	got, err := TokenBalanceAt(context.Background(), c,
		defitypes.MustAddressFromHex(testFeeToken), defitypes.MustAddressFromHex(testFeeHolder), 6, nil)
	if err != nil {
		t.Fatalf("TokenBalanceAt: %v", err)
	}
	if got.Wei != "0" || got.Ether != "0.000000" {
		t.Errorf("zero balance = (%q, %q), want (0, 0.000000)", got.Wei, got.Ether)
	}
}

func TestTokenBalanceAt_Errors(t *testing.T) {
	token := defitypes.MustAddressFromHex(testFeeToken)
	holder := defitypes.MustAddressFromHex(testFeeHolder)

	t.Run("call error is wrapped", func(t *testing.T) {
		c := newFakeClient(&fakeRPC{callErr: errRPCDown})
		_, err := TokenBalanceAt(context.Background(), c, token, holder, 6, nil)
		if !errors.Is(err, errRPCDown) {
			t.Errorf("error = %v, want %v", err, errRPCDown)
		}
	})
	// Verbatim from chain 787222 (2026-11-12) for a balanceOf at a block
	// past the head. The native path curates this; the token path must too.
	t.Run("block the node does not have is block not found", func(t *testing.T) {
		c := newFakeClient(&fakeRPC{callErr: errors.New("RPC error: -32001 block not found: 0x3b9ac9ff")})
		got, err := TokenBalanceAt(context.Background(), c, token, holder, 6, big.NewInt(999_999_999))
		if !errors.Is(err, apperrors.ErrBlockNotFound) {
			t.Fatalf("error = %v, want ErrBlockNotFound", err)
		}
		if got != nil {
			t.Errorf("balance = %+v, want nil", got)
		}
		if msg := apperrors.SafeForClient(err).Error(); msg != "block not found" {
			t.Errorf("client sees %q, want %q", msg, "block not found")
		}
	})
	t.Run("unrecognized call error still collapses for the client", func(t *testing.T) {
		c := newFakeClient(&fakeRPC{callErr: errRPCDown})
		_, err := TokenBalanceAt(context.Background(), c, token, holder, 6, nil)
		if msg := apperrors.SafeForClient(err).Error(); msg != "upstream operation failed" {
			t.Errorf("client sees %q, want the generic collapse", msg)
		}
	})
	// A token that is not deployed answers eth_call with empty bytes. That
	// must not read as a zero balance.
	for name, out := range map[string][]byte{"empty": {}, "short": {0x01}, "long": make([]byte, abiWordLen*2)} {
		t.Run(name+" output is a decode error", func(t *testing.T) {
			c := newFakeClient(&fakeRPC{callOut: out})
			got, err := TokenBalanceAt(context.Background(), c, token, holder, 6, nil)
			if !errors.Is(err, apperrors.ErrNodeResponseDecode) {
				t.Errorf("error = %v, want ErrNodeResponseDecode", err)
			}
			if got != nil {
				t.Errorf("balance = %+v, want nil", got)
			}
		})
	}
}

func TestFormatUnits(t *testing.T) {
	huge, _ := new(big.Int).SetString("4242424242424242424242424242", 10)
	tests := []struct {
		amount   *big.Int
		decimals int
		want     string
	}{
		{big.NewInt(0), 6, "0.000000"},
		{big.NewInt(1), 6, "0.000001"},
		{big.NewInt(999_999), 6, "0.999999"},
		{big.NewInt(1_000_000), 6, "1.000000"},
		{big.NewInt(100_000_000), 6, "100.000000"},
		{big.NewInt(2466), 6, "0.002466"},
		{big.NewInt(1_500_000_000_000_000_000), 18, "1.500000000000000000"},
		{huge, 18, "4242424242.424242424242424242"},
		{big.NewInt(42), 0, "42"},
		{big.NewInt(7), 1, "0.7"},
	}
	for _, tt := range tests {
		if got := FormatUnits(tt.amount, tt.decimals); got != tt.want {
			t.Errorf("FormatUnits(%s, %d) = %q, want %q", tt.amount, tt.decimals, got, tt.want)
		}
	}
}
