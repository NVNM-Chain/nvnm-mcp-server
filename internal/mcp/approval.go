package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	apperrors "github.com/inveniam/nvnm-mcp-server/internal/errors"
)

const (
	// ApprovalRequired means the user must confirm every write.
	ApprovalRequired = "required"
	// ApprovalAuto means writes are sent without human confirmation.
	ApprovalAuto = "auto"
)

// ResolveWriteApproval returns the effective write-approval policy.
// Priority: per-client value > global default > "required".
func ResolveWriteApproval(perClient, globalDefault string) string {
	if perClient == ApprovalRequired || perClient == ApprovalAuto {
		return perClient
	}
	if globalDefault == ApprovalRequired || globalDefault == ApprovalAuto {
		return globalDefault
	}
	return ApprovalRequired
}

// TxSummary holds decoded transaction fields for the approval prompt.
type TxSummary struct {
	To       string
	Value    *big.Int
	Gas      uint64
	Nonce    uint64
	ChainID  *big.Int
	DataLen  int
	ClientID string
}

// DecodeTxSummary decodes a 0x-prefixed signed transaction hex string into
// a TxSummary for display in the approval prompt.
func DecodeTxSummary(signedTxHex, clientID string) (*TxSummary, error) {
	raw := strings.TrimPrefix(signedTxHex, "0x")
	txBytes, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode tx hex for approval prompt: %w", err)
	}

	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(txBytes); err != nil {
		return nil, fmt.Errorf("unmarshal tx for approval prompt: %w", err)
	}

	to := "(contract creation)"
	if tx.To() != nil {
		to = tx.To().Hex()
	}

	return &TxSummary{
		To:       to,
		Value:    tx.Value(),
		Gas:      tx.Gas(),
		Nonce:    tx.Nonce(),
		ChainID:  tx.ChainId(),
		DataLen:  len(tx.Data()),
		ClientID: clientID,
	}, nil
}

func formatApprovalMessage(s *TxSummary) string {
	return fmt.Sprintf(
		"Approve transaction broadcast?\n\n"+
			"  To:       %s\n"+
			"  Value:    %s wei\n"+
			"  Gas:      %d\n"+
			"  Nonce:    %d\n"+
			"  Chain ID: %s\n"+
			"  Data:     %d bytes\n"+
			"  Client:   %s\n\n"+
			"This is irreversible. The signed transaction will be broadcast to the chain.",
		s.To, s.Value.String(), s.Gas, s.Nonce, s.ChainID.String(), s.DataLen, s.ClientID,
	)
}

// RequestWriteApproval uses MCP elicitation to ask the human user to approve a
// transaction broadcast. Returns nil if approved, ErrWriteDeclined if declined
// or canceled, and ErrElicitationUnsupported if the client has no elicitation
// capability.
func RequestWriteApproval(
	ctx context.Context,
	session *mcp.ServerSession,
	summary *TxSummary,
) error {
	result, err := session.Elicit(ctx, &mcp.ElicitParams{
		Message: formatApprovalMessage(summary),
	})
	if err != nil {
		if strings.Contains(err.Error(), "does not support elicitation") {
			return fmt.Errorf("%w: %w", apperrors.ErrElicitationUnsupported, err)
		}
		return fmt.Errorf("elicitation request failed: %w", err)
	}

	if result.Action != "accept" {
		return fmt.Errorf(
			"user action %q: %w",
			result.Action, apperrors.ErrWriteDeclined,
		)
	}
	return nil
}

// CheckWriteApproval resolves the approval policy from context and config,
// and if approval is required, sends an elicitation to the human user.
// Returns nil if the write should proceed.
func CheckWriteApproval(
	ctx context.Context,
	session *mcp.ServerSession,
	signedTxHex string,
	globalDefault string,
) error {
	perClient := auth.WriteApprovalFromContext(ctx)
	policy := ResolveWriteApproval(perClient, globalDefault)

	if policy == ApprovalAuto {
		return nil
	}

	clientID := auth.ClientIDFromContext(ctx)
	summary, err := DecodeTxSummary(signedTxHex, clientID)
	if err != nil {
		return err
	}

	return RequestWriteApproval(ctx, session, summary)
}
