// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/google/jsonschema-go/jsonschema"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// blockNumberArg is a block-number tool input that accepts either a JSON
// integer or one of the standard EVM block tags "latest" / "earliest"
// (finding 19: "latest" is the value every EVM caller reaches for first, so
// schema-rejecting it cost a failed round trip). The zero value means "not
// provided" and resolves to the latest block, preserving the documented
// omit-for-latest contract; JSON null is likewise "not provided".
//
// Schema inference cannot see this union through reflection, so the type has
// a hand-written schema in customTypeSchemas, which addTool injects for
// every input struct that carries this type.
type blockNumberArg struct {
	tag string // "latest" or "earliest"; empty when numeric or unset
	num *int64
}

// customTypeSchemas maps input types whose JSON contract cannot be inferred
// by reflection (union types with a custom UnmarshalJSON) to hand-written
// schemas. addTool threads it into jsonschema.ForType for every tool.
var customTypeSchemas = map[reflect.Type]*jsonschema.Schema{
	reflect.TypeFor[blockNumberArg](): {
		AnyOf: []*jsonschema.Schema{
			{Type: "integer"},
			{Type: "string", Enum: []any{"latest", "earliest"}},
			{Type: "null"},
		},
	},
}

func (b *blockNumberArg) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*b = blockNumberArg{}
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var tag string
		if err := json.Unmarshal(data, &tag); err != nil {
			return err
		}
		if tag != "latest" && tag != "earliest" {
			// Schema validation already enforces the enum; this guards
			// direct construction paths and keeps the message actionable.
			return fmt.Errorf("unsupported block tag %q (use \"latest\", \"earliest\", "+
				"or an integer block number): %w", tag, apperrors.ErrInvalidBlockRef)
		}
		*b = blockNumberArg{tag: tag}
		return nil
	}
	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("block number must be an integer or \"latest\"/\"earliest\": %w",
			apperrors.ErrInvalidBlockRef)
	}
	*b = blockNumberArg{num: &n}
	return nil
}

func (b blockNumberArg) MarshalJSON() ([]byte, error) {
	switch {
	case b.tag != "":
		return json.Marshal(b.tag)
	case b.num != nil:
		return json.Marshal(*b.num)
	default:
		return []byte("null"), nil
	}
}

// isSet reports whether the caller provided any value (a tag or a number).
// The zero value -- field omitted or JSON null -- is not set.
func (b blockNumberArg) isSet() bool {
	return b.tag != "" || b.num != nil
}

// bigInt resolves the argument for clients whose API is *big.Int with
// nil-means-latest (BlockByNumber, BalanceAt, CodeAt, CallContract).
// "earliest" is block zero by EVM convention.
func (b blockNumberArg) bigInt() *big.Int {
	switch {
	case b.tag == "earliest":
		return big.NewInt(0)
	case b.num != nil:
		return big.NewInt(*b.num)
	default: // unset or "latest"
		return nil
	}
}

// blockNumber resolves the argument as a go-eth BlockNumber for filter
// queries. ok is false when the argument was not provided, so callers can
// leave the query field unset (the node's default).
func (b blockNumberArg) blockNumber() (bn defitypes.BlockNumber, ok bool) {
	switch {
	case b.tag == "latest":
		return defitypes.LatestBlockNumber, true
	case b.tag == "earliest":
		return defitypes.EarliestBlockNumber, true
	case b.num != nil:
		return defitypes.BlockNumberFromBigInt(big.NewInt(*b.num)), true
	default:
		return defitypes.BlockNumber{}, false
	}
}
