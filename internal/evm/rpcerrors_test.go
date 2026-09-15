// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package evm

import (
	"context"
	"errors"
	"math/big"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func bigIntPtr(n int64) *big.Int { return big.NewInt(n) }

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
	t.Run("null block", func(t *testing.T) {
		c := newFakeClient(&fakeRPC{block: &defitypes.Block{}})
		_, err := c.BlockByHash(context.Background(), defitypes.Hash{}, false)
		if !errors.Is(err, apperrors.ErrBlockNotFound) {
			t.Fatalf("err = %v, want ErrBlockNotFound", err)
		}
	})
}
