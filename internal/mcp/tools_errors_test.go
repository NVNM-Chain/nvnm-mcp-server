// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

// deniedCtx carries an authenticated identity with zero roles, which
// default-deny RBAC must reject on every role-gated handler.
func deniedCtx() context.Context {
	return auth.ContextWithClaims(context.Background(), &auth.Claims{ClientID: "no-roles"})
}

// nonceFailEVM lets BalanceAt succeed while PendingNonceAt fails, to
// reach the nonce-error branches behind a successful balance read.
type nonceFailEVM struct {
	*mockEVM
	nonceErr error
}

func (n *nonceFailEVM) PendingNonceAt(_ context.Context, _ defitypes.Address) (uint64, error) {
	return 0, n.nonceErr
}

func TestHandlers_DefaultDenyWithoutRoles(t *testing.T) {
	m := &mockEVM{}
	a := &mockAnchor{}
	cfg := testServerConfig(true)
	ctx := deniedCtx()

	num := int64(1)
	addr := testAddr
	cases := []struct {
		name string
		call func() error
	}{
		{"get_block", func() error {
			_, _, err := makeGetBlockHandler(m)(ctx, nil, getBlockInput{BlockNumber: &num})
			return err
		}},
		{"get_transaction", func() error {
			_, _, err := makeGetTransactionHandler(m)(ctx, nil, txHashInput{TxHash: testTxHash})
			return err
		}},
		{"get_receipt", func() error {
			_, _, err := makeGetReceiptHandler(m)(ctx, nil, txHashInput{TxHash: testTxHash})
			return err
		}},
		{"get_balance", func() error {
			_, _, err := makeGetBalanceHandler(m)(ctx, nil, getBalanceInput{Address: testAddr})
			return err
		}},
		{"get_code", func() error {
			_, _, err := makeGetCodeHandler(m)(ctx, nil, getCodeInput{Address: testAddr})
			return err
		}},
		{"get_logs", func() error {
			_, _, err := makeGetLogsHandler(m)(ctx, nil, getLogsInput{Address: &addr})
			return err
		}},
		{"call_contract", func() error {
			_, _, err := makeCallContractHandler(m)(ctx, nil, callContractInput{To: testAddr, Data: "0x"})
			return err
		}},
		{"anchor_info", func() error {
			_, _, err := makeAnchorInfoHandler(a)(ctx, nil, anchorInfoInput{})
			return err
		}},
		{"get_registry", func() error {
			_, _, err := makeGetRegistryHandler(a)(ctx, nil, getRegistryInput{})
			return err
		}},
		{"get_registries", func() error {
			_, _, err := makeGetRegistriesHandler(a)(ctx, nil, getRegistriesInput{})
			return err
		}},
		{"get_records", func() error {
			_, _, err := makeGetRecordsHandler(a)(ctx, nil, getRecordsInput{})
			return err
		}},
		{"verify_hash", func() error {
			_, _, err := makeVerifyHashHandler()(ctx, nil, verifyHashInput{Address: testAddr, Hash: "0xab"})
			return err
		}},
		{"verify_signature", func() error {
			_, _, err := makeVerifySignatureHandler()(ctx, nil,
				verifySignatureInput{Address: testAddr, Signature: "0xab"})
			return err
		}},
		{"setup_wizard", func() error {
			_, _, err := makeSetupWizardHandler(m, cfg)(ctx, nil, setupWizardInput{Address: testAddr})
			return err
		}},
		{"wallet_status", func() error {
			_, _, err := makeWalletStatusHandler(m, cfg)(ctx, nil, walletStatusInput{Address: testAddr})
			return err
		}},
		{"prepare_add_registry", func() error {
			_, _, err := makePrepareAddRegistryHandler(a, testLogger())(ctx, nil,
				prepareAddRegistryInput{From: testAddr, Name: "r"})
			return err
		}},
		{"prepare_add_record", func() error {
			_, _, err := makePrepareAddRecordHandler(a, testLogger())(ctx, nil,
				prepareAddRecordInput{From: testAddr, Registry: "r", URI: "u", Checksum: "ab", ChecksumAlgo: "sha256"})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, apperrors.ErrPermissionDenied) {
				t.Errorf("error = %v, want ErrPermissionDenied", err)
			}
		})
	}
}

func TestHandlers_ClientErrorsPropagate(t *testing.T) {
	rpcDown := errors.New("rpc down")
	m := &mockEVM{returnErr: rpcDown}
	a := &mockAnchor{returnErr: rpcDown}
	cfg := testServerConfig(true)

	hash := testTxHash
	num := int64(1)
	blockNum := int64(2)
	regID := uint64(1)
	cases := []struct {
		name string
		call func() error
	}{
		{"get_block by hash", func() error {
			_, _, err := makeGetBlockHandler(m)(ctx, nil, getBlockInput{BlockHash: &hash})
			return err
		}},
		{"get_block by number", func() error {
			_, _, err := makeGetBlockHandler(m)(ctx, nil, getBlockInput{BlockNumber: &num})
			return err
		}},
		{"get_transaction", func() error {
			_, _, err := makeGetTransactionHandler(m)(ctx, nil, txHashInput{TxHash: testTxHash})
			return err
		}},
		{"get_receipt", func() error {
			_, _, err := makeGetReceiptHandler(m)(ctx, nil, txHashInput{TxHash: testTxHash})
			return err
		}},
		{"get_balance", func() error {
			_, _, err := makeGetBalanceHandler(m)(ctx, nil, getBalanceInput{Address: testAddr})
			return err
		}},
		{"get_code with block", func() error {
			_, _, err := makeGetCodeHandler(m)(ctx, nil, getCodeInput{Address: testAddr, BlockNum: &blockNum})
			return err
		}},
		{"get_logs", func() error {
			_, _, err := makeGetLogsHandler(m)(ctx, nil, getLogsInput{})
			return err
		}},
		{"call_contract", func() error {
			_, _, err := makeCallContractHandler(m)(ctx, nil, callContractInput{To: testAddr, Data: "0xcafe"})
			return err
		}},
		{"get_registry", func() error {
			_, _, err := makeGetRegistryHandler(a)(ctx, nil, getRegistryInput{ID: &regID})
			return err
		}},
		{"get_registries", func() error {
			_, _, err := makeGetRegistriesHandler(a)(ctx, nil, getRegistriesInput{})
			return err
		}},
		{"get_records", func() error {
			_, _, err := makeGetRecordsHandler(a)(ctx, nil, getRecordsInput{})
			return err
		}},
		{"setup_wizard balance", func() error {
			_, _, err := makeSetupWizardHandler(m, cfg)(ctx, nil, setupWizardInput{Address: testAddr})
			return err
		}},
		{"wallet_status balance", func() error {
			_, _, err := makeWalletStatusHandler(m, cfg)(ctx, nil, walletStatusInput{Address: testAddr})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, rpcDown) {
				t.Errorf("error = %v, want wrapped rpc-down", err)
			}
		})
	}
}

func TestHandlers_NonceErrors(t *testing.T) {
	nonceErr := errors.New("nonce unavailable")
	m := &nonceFailEVM{
		mockEVM:  &mockEVM{balance: &evm.NormalizedBalance{Address: testAddr, Wei: "5", Ether: "0.000000000000000005"}},
		nonceErr: nonceErr,
	}
	cfg := testServerConfig(true)

	_, _, err := makeSetupWizardHandler(m, cfg)(ctx, nil, setupWizardInput{Address: testAddr})
	if !errors.Is(err, nonceErr) {
		t.Errorf("setup_wizard error = %v, want nonce error", err)
	}
	_, _, err = makeWalletStatusHandler(m, cfg)(ctx, nil, walletStatusInput{Address: testAddr})
	if !errors.Is(err, nonceErr) {
		t.Errorf("wallet_status error = %v, want nonce error", err)
	}
}

// TestSetupWizard_NilBalanceIsUnfunded covers the balance==nil snapshot
// branch: the wizard reports 0 balance rather than dereferencing nil.
func TestSetupWizard_NilBalanceIsUnfunded(t *testing.T) {
	m := &mockEVM{} // BalanceAt returns (nil, nil)
	cfg := testServerConfig(true)
	_, out, err := makeSetupWizardHandler(m, cfg)(ctx, nil, setupWizardInput{Address: testAddr})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.State != "unfunded" {
		t.Errorf("state = %q, want unfunded", out.State)
	}
}

func TestVerifyHash_AcceptsUnprefixedDigest(t *testing.T) {
	addr, err := parseAddress(testAddr)
	if err != nil {
		t.Fatal(err)
	}
	expected := expectedHashForChallenge(challengeForAddress(addr))
	unprefixed := strings.TrimPrefix(expected, "0x")

	_, out, err := makeVerifyHashHandler()(ctx, nil, verifyHashInput{Address: testAddr, Hash: unprefixed})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.OK {
		t.Errorf("ok = false, want true (got=%q expected=%q)", out.Got, out.Expected)
	}
}

func TestVerifySignature_InputErrors(t *testing.T) {
	if _, _, err := makeVerifySignatureHandler()(ctx, nil,
		verifySignatureInput{Address: testBadAddr, Signature: "0xab"}); !errors.Is(err, apperrors.ErrInvalidAddress) {
		t.Errorf("bad address error = %v, want ErrInvalidAddress", err)
	}
	if _, _, err := makeVerifySignatureHandler()(ctx, nil,
		verifySignatureInput{Address: testAddr}); !errors.Is(err, apperrors.ErrMissingRequired) {
		t.Errorf("missing signature error = %v, want ErrMissingRequired", err)
	}
}

// TestVerifySignature_UnrecoverableSignature submits a well-formed but
// cryptographically invalid 65-byte signature: recovery fails and the
// handler must answer ok=false with remediation, not an error.
func TestVerifySignature_UnrecoverableSignature(t *testing.T) {
	sig := "0x" + strings.Repeat("00", 65)
	_, out, err := makeVerifySignatureHandler()(ctx, nil,
		verifySignatureInput{Address: testAddr, Signature: sig})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.OK {
		t.Error("ok = true, want false for an unrecoverable signature")
	}
	if out.Challenge == "" || len(out.NextActions) == 0 {
		t.Errorf("remediation payload missing: challenge=%q next_actions=%v", out.Challenge, out.NextActions)
	}
}

func TestParseHash_NonHex64(t *testing.T) {
	if _, err := parseHash("0x" + strings.Repeat("zz", 32)); err == nil {
		t.Error("parseHash should reject 64 chars of non-hex")
	}
}

func TestWalletStatusNextActions_UnknownStatus(t *testing.T) {
	if got := walletStatusNextActions("bogus"); got != nil {
		t.Errorf("walletStatusNextActions(bogus) = %v, want nil", got)
	}
}

func TestAddrString_NilIsEmpty(t *testing.T) {
	if got := addrString(nil); got != "" {
		t.Errorf("addrString(nil) = %q, want empty", got)
	}
}

func TestToolNameFromRequest_UnknownForNonToolParams(t *testing.T) {
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{Params: &mcp.ListToolsParams{}}
	if got := ToolNameFromRequest(req); got != "unknown" {
		t.Errorf("ToolNameFromRequest = %q, want unknown", got)
	}
}

// TestSendRawTx_AuditPersistFailureIsLoggedNotFatal covers the
// write-audit persist-failure warn branch: a failing store must not
// fail the broadcast.
func TestSendRawTx_AuditPersistFailureIsLoggedNotFatal(t *testing.T) {
	signedTx := buildSignedTxHex(t)
	m := &mockEVM{sendTxHash: "0xhash"}
	h := makeSendRawTxHandler(m, testAddr, false, false, &failingWriteAudit{}, nil, signerGates{}, testLogger())

	writerCtx := auth.ContextWithClaims(context.Background(),
		&auth.Claims{ClientID: "writer-1", Roles: []string{"writer"}})
	_, out, err := h(writerCtx, nil, sendRawTxInput{SignedTxHex: signedTx})
	if err != nil {
		t.Fatalf("broadcast should succeed despite audit failure: %v", err)
	}
	if out.TxHash != "0xhash" {
		t.Errorf("tx_hash = %q, want 0xhash", out.TxHash)
	}
}
