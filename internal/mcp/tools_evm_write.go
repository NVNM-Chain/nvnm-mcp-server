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
	}, makeSendRawTxHandler(evmClient, anchorAddr, keylessWrites, logger))
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
	c evm.Client, anchorAddr string, keylessWrites bool, logger *slog.Logger,
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
				logger.LogAttrs(ctx, slog.LevelWarn, "audit",
					slog.Group("audit",
						slog.String("tool", "evm_send_raw_transaction"),
						slog.String("phase", "relay_scope_rejected"),
						slog.String("signer", dtx.Signer.String()),
					),
				)
				return nil, sendRawTxOutput{}, serr // ErrRelayScopeRejected (input class)
			}
			broadcastHex = "0x" + hex.EncodeToString(dtx.CanonicalRaw)
		}

		clientID := auth.ClientIDFromContext(ctx)

		txHash, err := c.SendRawTransaction(ctx, broadcastHex)
		if err != nil {
			logger.LogAttrs(ctx, slog.LevelWarn, "audit",
				slog.Group("audit",
					slog.String("tool", "evm_send_raw_transaction"),
					slog.String("phase", "broadcast_failed"),
					slog.String("client_id", clientID),
					slog.Int("signed_tx_len", len(input.SignedTxHex)),
					slog.String("error", err.Error()),
				),
			)
			return nil, sendRawTxOutput{}, err
		}

		logger.LogAttrs(ctx, slog.LevelInfo, "audit",
			slog.Group("audit",
				slog.String("tool", "evm_send_raw_transaction"),
				slog.String("phase", "broadcast_ok"),
				slog.String("client_id", clientID),
				slog.String("tx_hash", txHash),
			),
		)
		return nil, sendRawTxOutput{TxHash: txHash, NextActions: evmSendRawTxNext(txHash)}, nil
	}
}
