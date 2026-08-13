// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// The MCP SDK infers each tool's outputSchema from its envelope type by
// reflection, then validates every response against that schema before it
// reaches the client. A field whose Go type marshals differently than the
// schema inferred for it therefore does not fail at compile time or in a
// handler test -- it fails at call time, for real callers only, and the
// middleware sanitizes the cause to "upstream operation failed".
//
// That is exactly how anchor_get_registries broke: PageResponse.NextKey was
// a []byte, which encoding/json marshals to a base64 *string* while the
// inferred schema said "array of integers". Registry listings carrying a
// pagination cursor -- the common case -- were rejected, and the unfiltered
// listing was unusable with no diagnosable error.
//
// These tests assert the invariant that would have caught it: for every
// tool output envelope, the schema the SDK derives must accept the JSON we
// actually emit. Populate optional fields in the fixtures below -- a zero
// value proves nothing, since omitempty simply drops the field.

func assertSchemaAcceptsMarshaled[T any](t *testing.T, v T) {
	t.Helper()

	schema, err := jsonschema.For[T](nil)
	if err != nil {
		t.Fatalf("infer schema: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve schema: %v", err)
	}

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	var instance any
	if err := json.Unmarshal(raw, &instance); err != nil {
		t.Fatalf("unmarshal value: %v", err)
	}

	if err := resolved.Validate(instance); err != nil {
		t.Errorf("emitted JSON does not satisfy its own inferred output schema: %v\njson: %s", err, raw)
	}
}

// TestOutputSchemasAcceptEmittedJSON is the general guard: every envelope,
// with its optional fields populated.
func TestOutputSchemasAcceptEmittedJSON(t *testing.T) {
	nextActions := []NextAction{{Tool: "evm_get_block", Hint: "inspect the block"}}
	cursor := anchor.EncodeCursor([]byte{0, 0, 0, 0, 0, 0, 0, 101})

	t.Run("registriesOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, registriesOutput{
			GetRegistriesResponse: anchor.GetRegistriesResponse{
				Registries: []anchor.Registry{{ID: 1, Name: "reg", Metadata: "{}"}},
				Pagination: &anchor.PageResponse{Total: 3, NextKey: cursor},
			},
			ContentTrust:       contentTrustNotice,
			NameMatchTruncated: true,
			NextActions:        nextActions,
		})
	})

	t.Run("recordsOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, recordsOutput{
			GetRecordsResponse: anchor.GetRecordsResponse{
				Pagination: &anchor.PageResponse{Total: 3, NextKey: cursor},
			},
			ContentTrust: contentTrustNotice,
			NextActions:  nextActions,
		})
	})

	t.Run("registryOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, registryOutput{
			Registry:     anchor.Registry{ID: 1, Name: "reg", Metadata: "{}"},
			ContentTrust: contentTrustNotice,
			NextActions:  nextActions,
		})
	})

	t.Run("chainIDOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, chainIDOutput{
			ChainInfo:   evm.ChainInfo{},
			NextActions: nextActions,
		})
	})

	t.Run("blockOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, blockOutput{NextActions: nextActions})
	})

	t.Run("transactionOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, transactionOutput{NextActions: nextActions})
	})

	t.Run("receiptOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, receiptOutput{NextActions: nextActions})
	})

	t.Run("balanceOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, balanceOutput{NextActions: nextActions})
	})

	t.Run("codeOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, codeOutput{NextActions: nextActions})
	})

	t.Run("anchorInfoOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, anchorInfoOutput{NextActions: nextActions})
	})

	t.Run("unsignedTxOutput", func(t *testing.T) {
		assertSchemaAcceptsMarshaled(t, unsignedTxOutput{NextActions: nextActions})
	})
}

// TestPaginationCursorIsSchemaTypedString pins the specific regression: a
// non-empty cursor must be declared as (and emitted as) a string. Asserting
// the declared type directly means a future change back to []byte fails
// here with a clear message, not just as a validation error elsewhere.
func TestPaginationCursorIsSchemaTypedString(t *testing.T) {
	schema, err := jsonschema.For[registriesOutput](nil)
	if err != nil {
		t.Fatalf("infer schema: %v", err)
	}
	pagination, ok := schema.Properties["pagination"]
	if !ok {
		t.Fatal("registriesOutput schema has no pagination property")
	}
	nextKey, ok := pagination.Properties["next_key"]
	if !ok {
		t.Fatal("pagination schema has no next_key property")
	}

	types := nextKey.Types
	if nextKey.Type != "" {
		types = append(types, nextKey.Type)
	}
	for _, typ := range types {
		if typ == "array" {
			t.Fatalf("next_key is schema-typed %v; encoding/json emits a base64 string for it, "+
				"so any response with a cursor fails output validation", types)
		}
	}
	var sawString bool
	for _, typ := range types {
		if typ == "string" {
			sawString = true
		}
	}
	if !sawString {
		t.Errorf("next_key types = %v, want to include \"string\"", types)
	}
}

// TestCursorRoundTrip covers the encode/decode pair the paginated scans rely
// on: whatever EncodeCursor produces must decode back to the exact bytes,
// since those bytes become the next page's PageRequest.Key.
func TestCursorRoundTrip(t *testing.T) {
	for _, raw := range [][]byte{
		{0, 0, 0, 0, 0, 0, 0, 101},
		{0xff, 0x00, 0xff},
		[]byte("cursor-1"),
	} {
		p := &anchor.PageResponse{NextKey: anchor.EncodeCursor(raw)}
		got, err := p.CursorBytes()
		if err != nil {
			t.Fatalf("CursorBytes(%x): %v", raw, err)
		}
		if !bytes.Equal(got, raw) {
			t.Errorf("round trip of %x = %x, want %x", raw, got, raw)
		}
	}

	// Empty stays empty: "no more data" must survive the round trip as the
	// zero value, since the scans treat an empty cursor as the stop signal.
	if s := anchor.EncodeCursor(nil); s != "" {
		t.Errorf("EncodeCursor(nil) = %q, want empty", s)
	}
	empty := &anchor.PageResponse{}
	got, err := empty.CursorBytes()
	if err != nil || len(got) != 0 {
		t.Errorf("CursorBytes() on empty = %x, %v; want empty, nil", got, err)
	}
	// A nil Pagination is the common shape when a page has no cursor at all.
	var nilPage *anchor.PageResponse
	if got, err := nilPage.CursorBytes(); err != nil || got != nil {
		t.Errorf("CursorBytes() on nil = %x, %v; want nil, nil", got, err)
	}
}

// TestCursorBytesRejectsGarbage keeps the decode failure loud rather than
// silently truncating a scan at a corrupt cursor.
func TestCursorBytesRejectsGarbage(t *testing.T) {
	p := &anchor.PageResponse{NextKey: "not!valid!base64"}
	if _, err := p.CursorBytes(); err == nil {
		t.Error("CursorBytes() on malformed base64 = nil error, want a decode error")
	}
}
