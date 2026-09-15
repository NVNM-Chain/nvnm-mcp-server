// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func bigIntPtr(n int64) *big.Int { return big.NewInt(n) }

// The error strings below are verbatim node output captured from the
// testnet on 2026-09-15 (directory review, H-1/H-2). Keep them verbatim: the
// classifier is substring-based and the tests are the record of what was
// actually observed.
const (
	rawNonceLow    = "RPC error: -32000 failed to broadcast transaction: invalid nonce; got 286, expected 288: tx nonce is lower than account nonce: internal [cosmossdk.io/errors@v1.0.2/errors.go:77]"
	rawInMempool   = "RPC error: -32000 failed to broadcast transaction: : tx already in mempool [cosmossdk.io/errors@v1.0.2/errors.go:77]"
	rawWrongChain  = "RPC error: -32000 failed to convert ethereum transaction: invalid chain id for signer: have 1 want 787111"
	rawNoFunds     = "RPC error: -32000 insufficient funds for gas * price + value: address 0x1111 have 0 want 5864590000000000"
	rawBeyondHead  = "RPC error: -32000 height 999999999 must be less than or equal to the current blockchain height 4005936"
	rawFutureCode  = "RPC error: -32000 rpc error: code = Unknown desc = codespace sdk code 26: invalid height: cannot query with height in the future; please provide a valid height"
	rawHashMissing = "RPC error: -32000 block not found for hash 0x0000000000000000000000000000000000000000000000000000000000000001"
	rawUnknownSel  = "RPC error: -32000 rpc error: code = Internal desc = unknown method id: 3735928559"
	rawShortInput  = "RPC error: -32000 rpc error: code = Internal desc = invalid input length"
	rawReverted    = "RPC error: -32000 execution reverted: Ownable: caller is not the owner"
)

// leakMarkers are fragments of raw node output that must never appear in a
// curated, client-facing message.
var leakMarkers = []string{"cosmossdk", "RPC error", "-32000", "got 286", "0x1111", "4005936", "3735928559", "Ownable"}

func assertNoLeak(t *testing.T, err error) {
	t.Helper()
	msg := apperrors.SafeForClient(err).Error()
	for _, m := range leakMarkers {
		if strings.Contains(msg, m) {
			t.Errorf("client-facing message leaks raw node detail %q: %q", m, msg)
		}
	}
}

func TestClassifyNodeRPCError_BroadcastAndBlockRejections(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"nonce lower than account nonce", rawNonceLow, apperrors.ErrTxNonceConflict},
		{"already in mempool", rawInMempool, apperrors.ErrTxAlreadyKnown},
		{"wrong chain id", rawWrongChain, apperrors.ErrTxChainIDMismatch},
		{"insufficient funds", rawNoFunds, apperrors.ErrInsufficientFunds},
		{"block beyond head", rawBeyondHead, apperrors.ErrBlockBeyondHead},
		{"block beyond head (eth_getCode phrasing)", rawFutureCode, apperrors.ErrBlockBeyondHead},
		{"block hash unknown", rawHashMissing, apperrors.ErrBlockNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyNodeRPCError(errors.New(tt.raw))
			if !errors.Is(got, tt.want) {
				t.Fatalf("classify(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			if safe := apperrors.SafeForClient(got); safe.Error() == "upstream operation failed" {
				t.Errorf("curated sentinel must survive SafeForClient")
			}
			assertNoLeak(t, got)
		})
	}
	if got := classifyNodeRPCError(errors.New("dial tcp: connection refused")); got != nil {
		t.Errorf("unrelated error classified as %v", got)
	}
	if got := classifyNodeRPCError(nil); got != nil {
		t.Errorf("nil classified as %v", got)
	}
}

// A re-broadcast of a still-pending tx can mention both the mempool and the
// nonce; the mempool entry must win so the caller is told not to retry.
func TestClassifyNodeRPCError_MempoolWinsOverNonce(t *testing.T) {
	err := errors.New("tx already in mempool; invalid nonce")
	if got := classifyNodeRPCError(err); !errors.Is(got, apperrors.ErrTxAlreadyKnown) {
		t.Errorf("got %v, want ErrTxAlreadyKnown", got)
	}
}

func TestClassifyCallRevert(t *testing.T) {
	for _, raw := range []string{rawUnknownSel, rawShortInput, rawReverted} {
		got := ClassifyCallRevert(errors.New(raw))
		if !errors.Is(got, apperrors.ErrCallReverted) {
			t.Errorf("ClassifyCallRevert(%q) = %v, want ErrCallReverted", raw, got)
		}
		assertNoLeak(t, got)
	}
	// Precompile validation reasons are NOT eth_call reverts: the anchor
	// client relies on seeing them raw.
	for _, raw := range []string{"desc = unauthorized", "collections: not found: key '1'", "dial tcp: refused"} {
		if got := ClassifyCallRevert(errors.New(raw)); got != nil {
			t.Errorf("ClassifyCallRevert(%q) = %v, want nil", raw, got)
		}
	}
	if ClassifyCallRevert(nil) != nil {
		t.Error("nil must classify as nil")
	}
}

// TestClient_BlockByNumber_NullIsNotFound is the H-1 regression: defiweb
// decodes a JSON-RPC null block into a zero-value struct, which used to be
// normalized into a fabricated all-zero block with isError=false.
func TestClient_BlockByNumber_NullIsNotFound(t *testing.T) {
	for name, block := range map[string]*defitypes.Block{
		"zero-value struct (defiweb null decode)": {},
		"nil pointer": nil,
	} {
		t.Run(name, func(t *testing.T) {
			c := newFakeClient(&fakeRPC{block: block})
			out, err := c.BlockByNumber(context.Background(), bigIntPtr(999999999), false)
			if !errors.Is(err, apperrors.ErrBlockNotFound) {
				t.Fatalf("err = %v, want ErrBlockNotFound", err)
			}
			if out != nil {
				t.Errorf("out = %+v, want nil (no phantom block)", out)
			}
			if got := apperrors.SafeForClient(err).Error(); got != "block not found" {
				t.Errorf("client sees %q, want %q", got, "block not found")
			}
		})
	}
}

// A real block is never mistaken for a missing one -- including the genesis
// block, whose number is zero but whose hash is not.
func TestClient_BlockByNumber_GenesisIsNotMissing(t *testing.T) {
	b := testBlock()
	b.Number = bigIntPtr(0)
	c := newFakeClient(&fakeRPC{block: b})
	out, err := c.BlockByNumber(context.Background(), bigIntPtr(0), false)
	if err != nil {
		t.Fatalf("genesis lookup: %v", err)
	}
	if out == nil || out.Hash != b.Hash.String() {
		t.Errorf("genesis block not returned: %+v", out)
	}
}

func TestClient_BlockByHash_NotFoundIsCurated(t *testing.T) {
	t.Run("node error text", func(t *testing.T) {
		c := newFakeClient(&fakeRPC{blockErr: errors.New(rawHashMissing)})
		_, err := c.BlockByHash(context.Background(), defitypes.Hash{}, false)
		if !errors.Is(err, apperrors.ErrBlockNotFound) {
			t.Fatalf("err = %v, want ErrBlockNotFound", err)
		}
		assertNoLeak(t, err)
	})
	t.Run("null block", func(t *testing.T) {
		c := newFakeClient(&fakeRPC{block: &defitypes.Block{}})
		_, err := c.BlockByHash(context.Background(), defitypes.Hash{}, false)
		if !errors.Is(err, apperrors.ErrBlockNotFound) {
			t.Fatalf("err = %v, want ErrBlockNotFound", err)
		}
	})
}

func TestClient_BalanceAt_BeyondHeadIsCurated(t *testing.T) {
	c := newFakeClient(&fakeRPC{balanceErr: errors.New(rawBeyondHead)})
	_, err := c.BalanceAt(context.Background(), defitypes.Address{}, bigIntPtr(999999999))
	if !errors.Is(err, apperrors.ErrBlockBeyondHead) {
		t.Fatalf("err = %v, want ErrBlockBeyondHead", err)
	}
	if !apperrors.IsInputError(err) {
		t.Errorf("beyond-head must be an input-class error: %v", err)
	}
	assertNoLeak(t, err)
}

func TestClient_CodeAt_BeyondHeadIsCurated(t *testing.T) {
	c := newFakeClient(&fakeRPC{codeErr: errors.New(rawFutureCode)})
	_, err := c.CodeAt(context.Background(), defitypes.Address{}, bigIntPtr(999999999))
	if !errors.Is(err, apperrors.ErrBlockBeyondHead) {
		t.Fatalf("err = %v, want ErrBlockBeyondHead", err)
	}
}

// TestClient_SendRawTransaction_CuratedRejections verifies the broadcast
// path surfaces the actionable sentinel to the client while keeping the raw
// node text reachable for the write-audit trail (apperrors.RawCause).
func TestClient_SendRawTransaction_CuratedRejections(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{"stale nonce", rawNonceLow, apperrors.ErrTxNonceConflict},
		{"replay of pending tx", rawInMempool, apperrors.ErrTxAlreadyKnown},
		{"signed for another chain", rawWrongChain, apperrors.ErrTxChainIDMismatch},
		{"unfunded signer", rawNoFunds, apperrors.ErrInsufficientFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawErr := errors.New(tt.raw)
			c := newFakeClient(&fakeRPC{sendErr: rawErr})
			_, err := c.SendRawTransaction(context.Background(), "0xdeadbeef")
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			// Client-facing: curated only.
			assertNoLeak(t, err)
			if strings.Contains(err.Error(), "cosmossdk") || strings.Contains(err.Error(), "got 286") {
				t.Errorf("Error() must not carry raw node text: %q", err.Error())
			}
			// Operator-facing: raw cause preserved for the audit line.
			if raw := apperrors.RawCause(err); !errors.Is(raw, rawErr) || !strings.Contains(raw.Error(), tt.raw) {
				t.Errorf("RawCause = %v, want the original node error", raw)
			}
		})
	}
}

func TestClient_SendRawTransaction_UnrecognizedStaysGeneric(t *testing.T) {
	c := newFakeClient(&fakeRPC{sendErr: errors.New("RPC error: -32000 something new")})
	_, err := c.SendRawTransaction(context.Background(), "0xdeadbeef")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := apperrors.SafeForClient(err).Error(); got != "upstream operation failed" {
		t.Errorf("client sees %q, want the generic collapse", got)
	}
	if raw := apperrors.RawCause(err); !errors.Is(raw, err) || raw.Error() != err.Error() {
		t.Errorf("RawCause of an uncurated error must be the error itself, got %v", raw)
	}
}
