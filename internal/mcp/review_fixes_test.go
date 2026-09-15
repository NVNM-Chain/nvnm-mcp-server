// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

// Regression tests for the High findings of the 2026-09-15 connector-
// directory review (docs/ANTHROPIC_DIRECTORY_REVIEW_2026-09-15.md):
//
//	H-1  evm_get_block fabricated a block for a non-existent number and
//	     accepted negative block numbers.
//	H-2  reviewer-reachable inputs collapsed to "upstream operation failed".

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/defiweb/go-eth/wallet"
	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

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

// --- H-2 ---------------------------------------------------------------

func TestHandler_CallContract_BadHexIsInputError(t *testing.T) {
	handler := makeCallContractHandler(&mockEVM{})
	for _, data := range []string{"0xzz", "0xGGGG", "0xabc" /* odd length */} {
		_, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: data})
		if !errors.Is(err, apperrors.ErrInvalidHexData) {
			t.Errorf("data %q: err = %v, want ErrInvalidHexData", data, err)
		}
		if got := apperrors.SafeForClient(err).Error(); got == "upstream operation failed" {
			t.Errorf("data %q: caller typo collapsed to the generic upstream failure", data)
		}
	}
}

func TestHandler_CallContract_RevertIsCurated(t *testing.T) {
	m := &mockEVM{returnErr: errors.New(
		"RPC error: -32000 rpc error: code = Internal desc = unknown method id: 3735928559",
	)}
	handler := makeCallContractHandler(m)
	_, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xdeadbeef"})
	if !errors.Is(err, apperrors.ErrCallReverted) {
		t.Fatalf("err = %v, want ErrCallReverted", err)
	}
	msg := apperrors.SafeForClient(err).Error()
	if !strings.Contains(msg, "contract call reverted") {
		t.Errorf("client message = %q, want the curated revert text", msg)
	}
	if strings.Contains(msg, "3735928559") || strings.Contains(msg, "RPC error") {
		t.Errorf("client message leaks raw node detail: %q", msg)
	}
}

func TestHandler_CallContract_UnrelatedErrorStillCollapses(t *testing.T) {
	m := &mockEVM{returnErr: errors.New("dial tcp 10.0.0.1:8545: connection refused")}
	handler := makeCallContractHandler(m)
	_, _, err := handler(ctx, nil, callContractInput{To: testAddr, Data: "0xdeadbeef"})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := apperrors.SafeForClient(err).Error(); got != "upstream operation failed" {
		t.Errorf("client sees %q, want the generic collapse (no host leak)", got)
	}
}

// The write-audit row must keep the node's RAW rejection (operator
// diagnostics) even though the client only receives the curated sentinel.
func TestSendRawTx_AuditKeepsRawCauseOfCuratedBroadcastError(t *testing.T) {
	fa := &fakeWriteAudit{}
	var logBuf strings.Builder
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	key := wallet.NewRandomKey()
	anchorAddr := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxTo(t, key, anchorAddr)

	nodeErr := errors.New("invalid nonce; got 286, expected 288: tx nonce is lower than account nonce")
	curated := apperrors.Curate(apperrors.ErrTxNonceConflict, nodeErr)
	h := makeSendRawTxHandler(&captureClient{err: curated}, anchorHex, true, false, fa, nil, signerGates{}, logger)
	_, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})

	if !errors.Is(err, apperrors.ErrTxNonceConflict) {
		t.Fatalf("err = %v, want ErrTxNonceConflict", err)
	}
	if client := apperrors.SafeForClient(err).Error(); strings.Contains(client, "got 286") {
		t.Errorf("client message leaks raw node text: %q", client)
	}
	if len(fa.recorded) != 1 || !strings.Contains(fa.recorded[0].Error, "got 286, expected 288") {
		t.Errorf("audit row must keep the raw node reason, got %+v", fa.recorded)
	}
	if !strings.Contains(logBuf.String(), "got 286, expected 288") {
		t.Errorf("audit log line must keep the raw node reason: %s", logBuf.String())
	}
}
