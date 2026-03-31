package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
)

func registerEVMWriteTools(srv *mcp.Server, evmClient evm.Client, _ *slog.Logger) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "evm_send_raw_transaction",
		Title: "Send Raw Transaction",
		Description: "Broadcast a signed transaction to the network. " +
			"Input is the signed transaction as a hex string (0x-prefixed). " +
			"Returns the transaction hash.",
	}, makeSendRawTxHandler(evmClient))
}

// --- Input/output types ---

type sendRawTxInput struct {
	SignedTxHex string `json:"signed_tx" jsonschema:"Signed transaction hex (0x-prefixed)"`
}

type sendRawTxOutput struct {
	TxHash string `json:"tx_hash"`
}

// --- Handler ---

func makeSendRawTxHandler(
	c evm.Client,
) mcp.ToolHandlerFor[sendRawTxInput, sendRawTxOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input sendRawTxInput,
	) (*mcp.CallToolResult, sendRawTxOutput, error) {
		if input.SignedTxHex == "" {
			return nil, sendRawTxOutput{},
				fmt.Errorf(
					"signed_tx is required: %w",
					apperrors.ErrMissingRequired,
				)
		}

		txHash, err := c.SendRawTransaction(ctx, input.SignedTxHex)
		if err != nil {
			return nil, sendRawTxOutput{}, err
		}

		return nil, sendRawTxOutput{TxHash: txHash}, nil
	}
}
