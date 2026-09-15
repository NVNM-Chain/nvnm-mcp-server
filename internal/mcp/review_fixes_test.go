// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

// Regression tests for the High findings of the 2026-09-15 connector-
// directory review (docs/ANTHROPIC_DIRECTORY_REVIEW_2026-09-15.md):
//
//	H-1  evm_get_block fabricated a block for a non-existent number and
//	     accepted negative block numbers.
//	H-2  reviewer-reachable inputs collapsed to "upstream operation failed".
//	H-3  the relay scope did not enforce value == 0.
//	M-1  the prepare-tool descriptions claimed an unconditional RBAC
//	     requirement that keyless-read deployments do not enforce.

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
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

// --- H-3 ---------------------------------------------------------------

// signedTxToWithValue is signedTxTo with a caller-chosen native value.
func signedTxToWithValue(t *testing.T, key *wallet.PrivateKey, to defitypes.Address, value *big.Int) string {
	t.Helper()
	tx := defitypes.NewTransaction().
		SetType(defitypes.DynamicFeeTxType).
		SetChainID(787111).SetNonce(0).SetGasLimit(21000).
		SetMaxFeePerGas(big.NewInt(2_000_000_000)).
		SetMaxPriorityFeePerGas(big.NewInt(1_000_000_000)).
		SetTo(to).SetValue(value).SetInput([]byte{0x01})
	if err := key.SignTransaction(context.Background(), tx); err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return "0x" + hex.EncodeToString(raw)
}

// The live review broadcast a 1-wei call to the precompile; it reverted and
// burned gas. Both relay modes that enforce scope must now refuse it before
// the node ever sees it, and the audit must record the rejection.
func TestSendRawTx_ValueToPrecompileRejected(t *testing.T) {
	key := wallet.NewRandomKey()
	anchorAddr := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxToWithValue(t, key, anchorAddr, big.NewInt(1))

	for _, keyless := range []bool{true, false} {
		var logBuf strings.Builder
		logger := slog.New(slog.NewTextHandler(&logBuf, nil))
		cc := &captureClient{}
		fm := &fakeWriteMetrics{}
		h := makeSendRawTxHandler(cc, anchorHex, keyless, false, nil, fm, signerGates{}, logger)

		_, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw})
		if !errors.Is(err, apperrors.ErrRelayValueRejected) {
			t.Fatalf("keyless=%v: err = %v, want ErrRelayValueRejected", keyless, err)
		}
		if cc.called {
			t.Errorf("keyless=%v: transaction must not reach the node", keyless)
		}
		if len(fm.rejects) != 1 || fm.rejects[0] != "relay_scope" {
			t.Errorf("keyless=%v: rejects = %v, want [relay_scope]", keyless, fm.rejects)
		}
		if !strings.Contains(logBuf.String(), "relay_scope_rejected") || !strings.Contains(logBuf.String(), "value_wei=1") {
			t.Errorf("keyless=%v: audit line missing or lacks value_wei: %s", keyless, logBuf.String())
		}
	}
}

// The escape hatch (MCP_RELAY_ALLOW_ANY) is unchanged: no scope, no value
// gate -- the operator opted out of the relay restrictions entirely.
func TestSendRawTx_ValueAllowedUnderRelayAllowAny(t *testing.T) {
	key := wallet.NewRandomKey()
	anchorAddr := defitypes.MustAddressFromHex(anchorHex)
	raw := signedTxToWithValue(t, key, anchorAddr, big.NewInt(1))
	cc := &captureClient{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := makeSendRawTxHandler(cc, anchorHex, false, true, nil, nil, signerGates{}, logger)
	if _, _, err := h(context.Background(), &sdkmcp.CallToolRequest{}, sendRawTxInput{SignedTxHex: raw}); err != nil {
		t.Fatalf("relayAllowAny: unexpected error %v", err)
	}
	if !cc.called {
		t.Error("relayAllowAny: transaction should have been broadcast")
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

// --- M-1 ---------------------------------------------------------------

// The prepare tools run anonymously under keyless reads (authpolicy.go
// authExemptTools), so their descriptions may not state the API-key role as
// an unconditional requirement. They must say the role applies only when the
// deployment authenticates the caller.
func TestPrepareToolDescriptions_RoleClaimIsConditional(t *testing.T) {
	session := startTestServer(t)
	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	prepareTools := map[string]bool{
		"anchor_prepare_add_registry":         true,
		"anchor_prepare_add_record":           true,
		"anchor_prepare_update_record_status": true,
		"anchor_prepare_grant_role":           true,
		"anchor_prepare_revoke_role":          true,
	}
	seen := 0
	for _, tool := range result.Tools {
		if !prepareTools[tool.Name] {
			continue
		}
		seen++
		d := tool.Description
		if !authExemptTools[tool.Name] {
			t.Errorf("%s: expected to be auth-exempt under keyless reads", tool.Name)
		}
		if !strings.Contains(d, "when this deployment") {
			t.Errorf("%s: description must scope the role requirement to authenticating deployments: %q", tool.Name, d)
		}
		for _, stale := range []string{"but requires the writer", "but requires the admin"} {
			if strings.Contains(d, stale) {
				t.Errorf("%s: description still states the role as unconditional (%q)", tool.Name, stale)
			}
		}
	}
	if seen != len(prepareTools) {
		t.Errorf("saw %d prepare tools, want %d", seen, len(prepareTools))
	}
}
