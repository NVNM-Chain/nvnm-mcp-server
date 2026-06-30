// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"

	defitypes "github.com/defiweb/go-eth/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
)

func registerEVMWriteTools(
	srv *mcp.Server,
	evmClient evm.Client,
	anchorAddr string,
	keylessWrites bool,
	audit WriteAuditStore,
	logger *slog.Logger,
) {
	addTool(srv, &mcp.Tool{
		Name:  "evm_send_raw_transaction",
		Title: "Send Raw Transaction",
		Description: "Broadcast a signed transaction to the network. " +
			"Use this for the local/headless signer path only: " +
			"sign the raw_tx bytes from anchor_prepare_* externally, " +
			"then pass the signed hex here. " +
			"Do NOT use this after signing with MetaMask or a browser wallet -- " +
			"those wallets broadcast directly and return a tx_hash themselves. " +
			"Input is a 0x-prefixed signed transaction hex string. " +
			"Returns the transaction hash. " +
			"Confirm the result with evm_get_transaction_receipt.",
		Annotations: newDestructiveWriteTool(),
	}, makeSendRawTxHandler(evmClient, anchorAddr, keylessWrites, audit, logger))
}

// --- Input/output types ---

type sendRawTxInput struct {
	SignedTxHex string `json:"signed_tx" jsonschema:"Signed transaction hex (0x-prefixed)"`
}

type sendRawTxOutput struct {
	TxHash      string       `json:"tx_hash"`
	NextActions []NextAction `json:"next_actions,omitempty"`
}

// --- Handler ---

func makeSendRawTxHandler(
	c evm.Client, anchorAddr string, keylessWrites bool, audit WriteAuditStore, logger *slog.Logger,
) mcp.ToolHandlerFor[sendRawTxInput, sendRawTxOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input sendRawTxInput,
	) (*mcp.CallToolResult, sendRawTxOutput, error) {
		if err := requireRole(ctx, "writer", "admin", "automation"); err != nil {
			return nil, sendRawTxOutput{}, err
		}
		if input.SignedTxHex == "" {
			return nil, sendRawTxOutput{},
				fmt.Errorf(
					"signed_tx is required: %w",
					apperrors.ErrMissingRequired,
				)
		}

		// Broadcast bytes: today's raw passthrough (authed/self-host), or the
		// scoped, canonical re-serialization under keyless writes (D9 / §5).
		broadcastHex := input.SignedTxHex
		var decoded *evm.DecodedTx // populated under keyless writes; drives signer-keyed audit
		if keylessWrites {
			dtx, derr := evm.DecodeSignedTx(input.SignedTxHex)
			if derr != nil {
				return nil, sendRawTxOutput{}, derr // ErrTxDecode (input class)
			}
			anchor, aerr := defitypes.AddressFromHex(anchorAddr)
			if aerr != nil {
				return nil, sendRawTxOutput{},
					fmt.Errorf("anchor address misconfigured: %w", apperrors.ErrInvalidAddress)
			}
			if serr := checkRelayScope(dtx.To, anchor); serr != nil {
				logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
					slog.String("tool", "evm_send_raw_transaction"),
					slog.String("phase", "relay_scope_rejected"),
					slog.String("signer", dtx.Signer.String()),
					slog.String("to", addrString(dtx.To)),
				}))
				return nil, sendRawTxOutput{}, serr // ErrRelayScopeRejected (input class)
			}
			decoded = dtx
			broadcastHex = "0x" + hex.EncodeToString(dtx.CanonicalRaw)
		}

		clientID := auth.ClientIDFromContext(ctx)

		// identityAttrs keys the audit record on the recovered signer under
		// keyless writes (client_id is empty under authless), or on client_id
		// in authed/self-host mode -- §4.D. Addresses/tx hashes only, no keys.
		identityAttrs := func() []slog.Attr {
			if decoded != nil {
				return []slog.Attr{
					slog.String("signer", decoded.Signer.String()),
					slog.String("to", addrString(decoded.To)),
					slog.String("value_wei", decoded.Value.String()),
					slog.Int("calldata_len", len(decoded.Input)),
				}
			}
			return []slog.Attr{slog.String("client_id", clientID)}
		}

		// recordAudit persists one broadcast attempt best-effort. Only the
		// keyless path has a recovered signer (decoded != nil); authed mode
		// keeps client_id in the slog line and is not persisted here.
		recordAudit := func(outcome, txHash, errMsg string) {
			if audit == nil || decoded == nil {
				return
			}
			rerr := audit.Record(ctx, WriteAuditEntry{
				Signer:      decoded.Signer.String(),
				To:          addrString(decoded.To),
				ValueWei:    decoded.Value.String(),
				CalldataLen: len(decoded.Input),
				TxHash:      txHash,
				Outcome:     outcome,
				Error:       errMsg,
			})
			if rerr != nil {
				logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup([]slog.Attr{
					slog.String("tool", "evm_send_raw_transaction"),
					slog.String("phase", "write_audit_persist_failed"),
					slog.String("signer", decoded.Signer.String()),
					slog.String("error", rerr.Error()),
				}))
			}
		}

		txHash, err := c.SendRawTransaction(ctx, broadcastHex)
		if err != nil {
			failAttrs := append([]slog.Attr{
				slog.String("tool", "evm_send_raw_transaction"),
				slog.String("phase", "broadcast_failed"),
				slog.Int("signed_tx_len", len(input.SignedTxHex)),
				slog.String("error", err.Error()),
			}, identityAttrs()...)
			logger.LogAttrs(ctx, slog.LevelWarn, "audit", auditGroup(failAttrs))
			recordAudit("broadcast_failed", "", err.Error())
			return nil, sendRawTxOutput{}, err
		}

		okAttrs := append([]slog.Attr{
			slog.String("tool", "evm_send_raw_transaction"),
			slog.String("phase", "broadcast_ok"),
			slog.String("tx_hash", txHash),
		}, identityAttrs()...)
		logger.LogAttrs(ctx, slog.LevelInfo, "audit", auditGroup(okAttrs))
		recordAudit("broadcast_ok", txHash, "")
		return nil, sendRawTxOutput{TxHash: txHash, NextActions: evmSendRawTxNext(txHash)}, nil
	}
}

// auditGroup builds the "audit" slog group from a dynamic attribute set.
// It uses slog.Group rather than a slog.Attr{Key: ...} composite literal so
// the package's raw-key-literal guard (keys_constraint_test.go, which forbids
// `Key:` struct literals) is not tripped by audit logging. slog.Group accepts
// slog.Attr values among its variadic args.
func auditGroup(attrs []slog.Attr) slog.Attr {
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return slog.Group("audit", args...)
}
