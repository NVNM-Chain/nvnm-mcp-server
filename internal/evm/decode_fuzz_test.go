// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
)

// fixedFuzzKey returns a deterministic secp256k1 key so fuzz seeds are
// reproducible (NewRandomKey would not be).
func fixedFuzzKey() *wallet.PrivateKey {
	b := make([]byte, 32)
	for i := range b {
		b[i] = 0x01
	}
	return wallet.NewKeyFromBytes(b)
}

// buildFuzzSignedTx builds a signed tx hex without a *testing.T, for use as a
// fuzz seed. Mirrors signedTxHex in decode_test.go.
func buildFuzzSignedTx(key *wallet.PrivateKey, txType defitypes.TransactionType, to *defitypes.Address) (string, error) {
	tx := defitypes.NewTransaction().
		SetType(txType).
		SetChainID(testChainID).
		SetNonce(0).
		SetGasLimit(21000).
		SetValue(big.NewInt(0)).
		SetInput([]byte{0xde, 0xad, 0xbe, 0xef})
	if to != nil {
		tx.SetTo(*to)
	}
	switch txType {
	case defitypes.DynamicFeeTxType:
		tx.SetMaxFeePerGas(big.NewInt(2_000_000_000)).SetMaxPriorityFeePerGas(big.NewInt(1_000_000_000))
	default:
		tx.SetGasPrice(big.NewInt(1_000_000_000))
	}
	if err := key.SignTransaction(context.Background(), tx); err != nil {
		return "", err
	}
	raw, err := tx.EncodeRLP()
	if err != nil {
		return "", err
	}
	return "0x" + hex.EncodeToString(raw), nil
}

// FuzzDecodeSignedTx drives arbitrary bytes through the untrusted caller-tx
// decode boundary. Two invariants must hold for EVERY input:
//
//  1. DecodeSignedTx never panics. Its recover() must convert any panic in
//     defiweb's RLP decoder on malformed input into a normal error. On the
//     stdio relay a panic here would be a denial of service (EV-2, caller leg).
//     The fuzz runner turns any escaped panic into a test failure with the
//     crashing input, so a clean run is positive evidence the guard holds.
//
//  2. When a decode succeeds, CanonicalRaw is a fixed point on the signer:
//     re-decoding it recovers the same signer. This is the parser-differential
//     defense -- the relay broadcasts CanonicalRaw, not the caller's bytes, so
//     a caller must not be able to craft input that decodes to signer A while
//     its canonical re-encode decodes to signer B (or fails to decode).
func FuzzDecodeSignedTx(f *testing.F) {
	key := fixedFuzzKey()
	to := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000A00")
	for _, txType := range []defitypes.TransactionType{defitypes.LegacyTxType, defitypes.DynamicFeeTxType} {
		if s, err := buildFuzzSignedTx(key, txType, &to); err == nil {
			f.Add(s)
		}
	}
	// Contract-creation (nil to) and malformed seeds.
	if s, err := buildFuzzSignedTx(key, defitypes.DynamicFeeTxType, nil); err == nil {
		f.Add(s)
	}
	for _, s := range []string{"", "0x", "0xdeadbeef", "02f0", "0x02", "f8"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, hexStr string) {
		dtx, err := DecodeSignedTx(hexStr) // invariant 1: must not panic
		if err != nil {
			if dtx != nil {
				t.Fatalf("error returned with non-nil result for %q: %v", hexStr, err)
			}
			return
		}
		if dtx == nil {
			t.Fatalf("nil result with nil error for %q", hexStr)
		}
		if len(dtx.CanonicalRaw) == 0 {
			t.Fatalf("successful decode with empty CanonicalRaw for %q", hexStr)
		}
		// invariant 2: canonical re-encode is a fixed point on the signer.
		reHex := "0x" + hex.EncodeToString(dtx.CanonicalRaw)
		again, err2 := DecodeSignedTx(reHex)
		if err2 != nil {
			t.Fatalf("canonical re-decode failed (orig %q): %v", hexStr, err2)
		}
		if again.Signer != dtx.Signer {
			t.Fatalf("signer not stable under canonical re-encode: %s -> %s (orig %q)",
				dtx.Signer, again.Signer, hexStr)
		}
		// The canonical form must itself be canonical (re-encode is idempotent).
		if !strings.EqualFold(reHex, "0x"+hex.EncodeToString(again.CanonicalRaw)) {
			t.Fatalf("CanonicalRaw not idempotent for %q", hexStr)
		}
	})
}
