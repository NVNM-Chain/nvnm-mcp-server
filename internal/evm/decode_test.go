// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"bytes"
	"context"
	"encoding/hex"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
)

const testChainID = uint64(787111)

// signedTxHex builds a signed transaction of the given type and returns its
// canonical 0x-prefixed hex. A nil to produces a contract-creation tx.
func signedTxHex(
	t *testing.T,
	key *wallet.PrivateKey,
	txType defitypes.TransactionType,
	to *defitypes.Address,
	value *big.Int,
	input []byte,
) string {
	t.Helper()
	tx := defitypes.NewTransaction().
		SetType(txType).
		SetChainID(testChainID).
		SetNonce(0).
		SetGasLimit(21000).
		SetValue(value).
		SetInput(input)
	if to != nil {
		tx.SetTo(*to)
	}
	switch txType {
	case defitypes.DynamicFeeTxType:
		tx.SetMaxFeePerGas(big.NewInt(2_000_000_000))
		tx.SetMaxPriorityFeePerGas(big.NewInt(1_000_000_000))
	default: // legacy / access-list
		tx.SetGasPrice(big.NewInt(1_000_000_000))
	}
	if err := key.SignTransaction(context.Background(), tx); err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	raw, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}
	return "0x" + hex.EncodeToString(raw)
}

func TestDecodeSignedTx_DynamicFeeHappyPath(t *testing.T) {
	key := wallet.NewRandomKey()
	to := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000A00")
	input := []byte{0xde, 0xad, 0xbe, 0xef}

	hexStr := signedTxHex(t, key, defitypes.DynamicFeeTxType, &to, big.NewInt(0), input)

	dtx, err := DecodeSignedTx(hexStr)
	if err != nil {
		t.Fatalf("DecodeSignedTx: %v", err)
	}
	if dtx.Signer != key.Address() {
		t.Errorf("signer = %s, want %s", dtx.Signer, key.Address())
	}
	if dtx.To == nil || *dtx.To != to {
		t.Errorf("to = %v, want %s", dtx.To, to)
	}
	if !bytes.Equal(dtx.Input, input) {
		t.Errorf("input = %x, want %x", dtx.Input, input)
	}
	if dtx.Value == nil || dtx.Value.Sign() != 0 {
		t.Errorf("value = %v, want 0", dtx.Value)
	}
	if len(dtx.CanonicalRaw) == 0 {
		t.Error("CanonicalRaw is empty")
	}
}

func TestDecodeSignedTx_TypesAndDestinations(t *testing.T) {
	key := wallet.NewRandomKey()
	anchor := defitypes.MustAddressFromHex("0x0000000000000000000000000000000000000A00")
	eoa := defitypes.MustAddressFromHex("0x00000000000000000000000000000000000000Ee")

	cases := []struct {
		name      string
		txType    defitypes.TransactionType
		to        *defitypes.Address
		value     *big.Int
		input     []byte
		wantToNil bool
	}{
		{"legacy anchor call", defitypes.LegacyTxType, &anchor, big.NewInt(0), []byte{0x01, 0x02}, false},
		{"dynamic anchor call", defitypes.DynamicFeeTxType, &anchor, big.NewInt(0), []byte{0x01, 0x02}, false},
		{"legacy value transfer", defitypes.LegacyTxType, &eoa, big.NewInt(1_000), nil, false},
		{"contract creation (to nil)", defitypes.DynamicFeeTxType, nil, big.NewInt(0), []byte{0x60, 0x80}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hexStr := signedTxHex(t, key, tc.txType, tc.to, tc.value, tc.input)
			dtx, err := DecodeSignedTx(hexStr)
			if err != nil {
				t.Fatalf("DecodeSignedTx: %v", err)
			}
			if dtx.Signer != key.Address() {
				t.Errorf("signer = %s, want %s", dtx.Signer, key.Address())
			}
			if tc.wantToNil {
				if dtx.To != nil {
					t.Errorf("to = %s, want nil (contract creation)", dtx.To)
				}
			} else {
				if dtx.To == nil || *dtx.To != *tc.to {
					t.Errorf("to = %v, want %s", dtx.To, tc.to)
				}
			}
			if dtx.Value == nil {
				t.Error("value is nil, want non-nil")
			}
		})
	}
}
