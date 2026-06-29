// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// DecodedTx holds the fields a relay needs from a signed transaction: the
// recovered signer, the destination, the calldata and value, and the
// canonical re-serialization to broadcast in place of the caller's bytes.
type DecodedTx struct {
	Signer       defitypes.Address  // recovered signer address
	To           *defitypes.Address // destination; nil for contract creation
	Input        []byte             // calldata
	Value        *big.Int           // value in wei; never nil (zero when absent)
	CanonicalRaw []byte             // EncodeRLP of the decoded tx, for broadcast
}

// DecodeSignedTx decodes a signed transaction hex string, recovers the
// signer, and returns the destination, calldata, value, and the canonical
// re-serialization. Decode and signer-recovery failures wrap
// apperrors.ErrTxDecode. The function performs no network or config access.
func DecodeSignedTx(signedTxHex string) (*DecodedTx, error) {
	raw := signedTxHex
	if len(raw) >= 2 && raw[:2] == "0x" {
		raw = raw[2:]
	}

	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode signed tx hex: %w", apperrors.ErrTxDecode)
	}

	tx := defitypes.NewTransaction()
	if _, err := tx.DecodeRLP(b); err != nil {
		return nil, fmt.Errorf("rlp-decode signed tx: %w", apperrors.ErrTxDecode)
	}

	// Workaround for defiweb/go-eth v0.7.0: DecodeRLP unconditionally sets
	// ChainID to zero for legacy transactions, because a legacy tx encodes
	// the chain ID inside V (EIP-155), not as a separate RLP field. That
	// bogus zero then makes crypto.ECRecoverer.RecoverTransaction fail its
	// own chain-ID cross-check. Re-derive the chain ID from V for EIP-155
	// legacy txs (V >= 35) so recovery and the signing-hash recomputation
	// agree. This does NOT affect CanonicalRaw: legacy EncodeRLP ignores
	// ChainID (V already carries it). Pre-EIP-155 legacy (V in {27,28}) and
	// typed txs decode their chain ID correctly and need no fixup.
	if tx.Type == defitypes.LegacyTxType && tx.Signature != nil &&
		tx.Signature.V.Cmp(big.NewInt(35)) >= 0 {
		derived := new(big.Int).Div(
			new(big.Int).Sub(tx.Signature.V, big.NewInt(35)),
			big.NewInt(2),
		)
		cid := derived.Uint64()
		tx.ChainID = &cid
	}

	signer, err := crypto.ECRecoverer.RecoverTransaction(tx)
	if err != nil {
		return nil, fmt.Errorf("recover signer: %w", apperrors.ErrTxDecode)
	}
	if signer == nil {
		return nil, fmt.Errorf("recover signer: nil address: %w", apperrors.ErrTxDecode)
	}

	canonical, err := tx.EncodeRLP()
	if err != nil {
		return nil, fmt.Errorf("re-encode signed tx: %w", apperrors.ErrTxDecode)
	}

	value := tx.Value
	if value == nil {
		value = big.NewInt(0)
	}

	return &DecodedTx{
		Signer:       *signer,
		To:           tx.To,
		Input:        tx.Input,
		Value:        value,
		CanonicalRaw: canonical,
	}, nil
}
