// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"errors"
	"testing"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// TestBlockNumberArg_UnmarshalJSON covers the accepted forms of the
// block-number union input (finding 19): integer, "latest", "earliest",
// null, and the rejected forms (unknown tag, non-integer JSON).
func TestBlockNumberArg_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantTag string
		wantNum *int64
		wantErr bool
	}{
		{name: "integer", in: `42`, wantNum: i64(42)},
		{name: "zero", in: `0`, wantNum: i64(0)},
		{name: "latest tag", in: `"latest"`, wantTag: "latest"},
		{name: "earliest tag", in: `"earliest"`, wantTag: "earliest"},
		{name: "null is unset", in: `null`},
		{name: "unknown tag rejected", in: `"pending"`, wantErr: true},
		{name: "float rejected", in: `1.5`, wantErr: true},
		{name: "object rejected", in: `{}`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b blockNumberArg
			err := json.Unmarshal([]byte(tt.in), &b)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("unmarshal %s: expected error, got nil", tt.in)
				}
				if !errors.Is(err, apperrors.ErrInvalidBlockRef) {
					t.Errorf("error = %v, want ErrInvalidBlockRef", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unmarshal %s: %v", tt.in, err)
			}
			if b.tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", b.tag, tt.wantTag)
			}
			switch {
			case tt.wantNum == nil && b.num != nil:
				t.Errorf("num = %d, want nil", *b.num)
			case tt.wantNum != nil && (b.num == nil || *b.num != *tt.wantNum):
				t.Errorf("num = %v, want %d", b.num, *tt.wantNum)
			}
		})
	}
}

// TestBlockNumberArg_BigInt covers the mapping used by the nil-means-latest
// client calls: unset and "latest" resolve to nil, "earliest" to block zero.
func TestBlockNumberArg_BigInt(t *testing.T) {
	if got := (blockNumberArg{}).bigInt(); got != nil {
		t.Errorf("unset bigInt = %v, want nil", got)
	}
	if got := (blockNumberArg{tag: "latest"}).bigInt(); got != nil {
		t.Errorf("latest bigInt = %v, want nil", got)
	}
	if got := (blockNumberArg{tag: "earliest"}).bigInt(); got == nil || got.Int64() != 0 {
		t.Errorf("earliest bigInt = %v, want 0", got)
	}
	if got := blockNumArg(7).bigInt(); got == nil || got.Int64() != 7 {
		t.Errorf("numeric bigInt = %v, want 7", got)
	}
}

// TestBlockNumberArg_BlockNumber covers the go-eth filter-query mapping,
// where tags stay tags on the wire instead of degrading to numbers.
func TestBlockNumberArg_BlockNumber(t *testing.T) {
	if _, ok := (blockNumberArg{}).blockNumber(); ok {
		t.Error("unset blockNumber: ok = true, want false")
	}
	if bn, ok := (blockNumberArg{tag: "latest"}).blockNumber(); !ok || !bn.IsLatest() {
		t.Errorf("latest blockNumber = (%v, %v), want (LatestBlockNumber, true)", bn, ok)
	}
	if bn, ok := (blockNumberArg{tag: "earliest"}).blockNumber(); !ok || !bn.IsEarliest() {
		t.Errorf("earliest blockNumber = (%v, %v), want (EarliestBlockNumber, true)", bn, ok)
	}
	bn, ok := blockNumArg(9).blockNumber()
	if !ok || bn.Big().Int64() != 9 {
		t.Errorf("numeric blockNumber = (%v, %v), want (9, true)", bn, ok)
	}
}

// TestBlockNumberArg_MarshalJSON pins the round trip so an input echoed
// back (logs, goldens) reproduces what the caller sent.
func TestBlockNumberArg_MarshalJSON(t *testing.T) {
	for _, in := range []string{`42`, `"latest"`, `"earliest"`, `null`} {
		var b blockNumberArg
		if err := json.Unmarshal([]byte(in), &b); err != nil {
			t.Fatalf("unmarshal %s: %v", in, err)
		}
		out, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal %s: %v", in, err)
		}
		if string(out) != in {
			t.Errorf("round trip = %s, want %s", out, in)
		}
	}
}

func i64(n int64) *int64 { return &n }
