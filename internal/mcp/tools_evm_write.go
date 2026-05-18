// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

func registerEVMWriteTools(
	srv *mcp.Server,
	evmClient evm.Client,
	approvalDefault string,
	chainEnvironment string,
	logger *slog.Logger,
) {
	mcp.AddTool(srv, &mcp.Tool{
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
	}, makeSendRawTxHandler(evmClient, approvalDefault, chainEnvironment, logger))
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
	c evm.Client, approvalDefault, chainEnvironment string, logger *slog.Logger,
) mcp.ToolHandlerFor[sendRawTxInput, sendRawTxOutput] {
	return func(
		ctx context.Context,
		req *mcp.CallToolRequest,
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

		clientID := auth.ClientIDFromContext(ctx)

		if err := CheckWriteApproval(
			ctx, req.Session, input.SignedTxHex, approvalDefault, chainEnvironment,
		); err != nil {
			logger.LogAttrs(ctx, slog.LevelWarn, "audit",
				slog.Group("audit",
					slog.String("tool", "evm_send_raw_transaction"),
					slog.String("phase", "approval_denied"),
					slog.String("client_id", clientID),
					slog.String("error", err.Error()),
				),
			)
			return nil, sendRawTxOutput{}, err
		}

		txHash, err := c.SendRawTransaction(ctx, input.SignedTxHex)
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
