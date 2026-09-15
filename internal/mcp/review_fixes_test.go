// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

// Regression tests for the High findings of the 2026-09-15 connector-
// directory review (docs/ANTHROPIC_DIRECTORY_REVIEW_2026-09-15.md):
//
//	H-1  evm_get_block fabricated a block for a non-existent number and
//	     accepted negative block numbers.

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// --- H-1 ---------------------------------------------------------------

func TestBlockNumberArg_RejectsNegative(t *testing.T) {
	for _, in := range []string{`-1`, `-5`, `-9223372036854775808`} {
		var b blockNumberArg
		err := json.Unmarshal([]byte(in), &b)
		if !errors.Is(err, apperrors.ErrInvalidBlockRef) {
			t.Errorf("unmarshal %s: err = %v, want ErrInvalidBlockRef", in, err)
		}
		if b.isSet() {
			t.Errorf("unmarshal %s: value must not be retained on error", in)
		}
	}
}

// The schema is what the SDK validates before the handler runs; the
// UnmarshalJSON guard above is only the belt for direct construction.
func TestBlockNumberArg_SchemaHasMinimumZero(t *testing.T) {
	schema, err := jsonschema.ForType(
		reflect.TypeFor[getBlockInput](), &jsonschema.ForOptions{TypeSchemas: customTypeSchemas},
	)
	if err != nil {
		t.Fatalf("derive schema: %v", err)
	}
	prop, ok := schema.Properties["block_number"]
	if !ok {
		t.Fatal("block_number missing from schema")
	}
	var intBranch *jsonschema.Schema
	for _, s := range prop.AnyOf {
		if s.Type == "integer" {
			intBranch = s
		}
	}
	if intBranch == nil {
		t.Fatal("block_number has no integer branch")
	}
	if intBranch.Minimum == nil || *intBranch.Minimum != 0 {
		t.Errorf("integer branch minimum = %v, want 0", intBranch.Minimum)
	}

	// And the resolved schema really rejects a negative number.
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := resolved.Validate(map[string]any{"block_number": -5}); err == nil {
		t.Error("schema accepted block_number=-5")
	}
	if err := resolved.Validate(map[string]any{"block_number": 0}); err != nil {
		t.Errorf("schema rejected block_number=0: %v", err)
	}
}

func TestHandler_GetBlock_NotFoundIsSurfaced(t *testing.T) {
	m := &mockEVM{returnErr: apperrors.ErrBlockNotFound}
	handler := makeGetBlockHandler(m)
	_, _, err := handler(ctx, nil, getBlockInput{BlockNumber: blockNumArg(999999999)})
	if !errors.Is(err, apperrors.ErrBlockNotFound) {
		t.Fatalf("err = %v, want ErrBlockNotFound", err)
	}
	if got := apperrors.SafeForClient(err).Error(); got != "block not found" {
		t.Errorf("client sees %q, want %q", got, "block not found")
	}
}
