// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	deficrypto "github.com/defiweb/go-eth/crypto"
	defitypes "github.com/defiweb/go-eth/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
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
// The Signer field is the address recovered from the signature, NOT
// the auth-context client ID -- the user is approving the on-chain
// actor, which may differ from the API-key identity that submitted
// the broadcast call.
type TxSummary struct {
	To               string
	Value            *big.Int
	Gas              uint64
	Nonce            uint64
	ChainID          *big.Int
	DataLen          int
	MethodSelector   string // first 4 bytes of Data() as "0x..."; empty if Data is short
	Signer           string // recovered from signature; "(recovery failed)" on error
	ClientID         string
	ChainEnvironment string // "testnet" / "mainnet" / "" if unknown
}

// DecodeTxSummary decodes a 0x-prefixed signed transaction hex string
// into a TxSummary for display in the approval prompt. chainEnv is
// a human label ("testnet", "mainnet") shown alongside the chain ID
// so the user does not have to memorize numeric chain IDs.
func DecodeTxSummary(signedTxHex, clientID, chainEnv string) (*TxSummary, error) {
	raw := strings.TrimPrefix(signedTxHex, "0x")
	txBytes, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode tx hex for approval prompt: %w", err)
	}

	tx := defitypes.NewTransaction()
	if _, err := tx.DecodeRLP(txBytes); err != nil {
		return nil, fmt.Errorf("unmarshal tx for approval prompt: %w", err)
	}

	to := "(contract creation)"
	if tx.To != nil {
		to = tx.To.Checksum(deficrypto.Keccak256)
	}

	selector := ""
	data := tx.Input
	if len(data) >= 4 {
		selector = "0x" + hex.EncodeToString(data[:4])
	}

	// Signature recovery. ECRecoverer.RecoverTransaction inspects the
	// signature embedded in the tx and returns the signer address. It
	// handles type-0 (LegacyTx), type-1 (AccessListTx), and type-2
	// (DynamicFeeTx) transactions uniformly.
	signer := "(recovery failed)"
	if from, recErr := deficrypto.ECRecoverer.RecoverTransaction(tx); recErr == nil && from != nil {
		signer = from.Checksum(deficrypto.Keccak256)
	}

	// defiweb stores ChainID as *uint64; surface it as *big.Int for the
	// existing TxSummary contract.
	var chainID *big.Int
	if tx.ChainID != nil {
		chainID = new(big.Int).SetUint64(*tx.ChainID)
	}

	value := tx.Value
	if value == nil {
		value = big.NewInt(0)
	}
	var gas uint64
	if tx.GasLimit != nil {
		gas = *tx.GasLimit
	}
	var nonce uint64
	if tx.Nonce != nil {
		nonce = *tx.Nonce
	}

	return &TxSummary{
		To:               to,
		Value:            value,
		Gas:              gas,
		Nonce:            nonce,
		ChainID:          chainID,
		DataLen:          len(data),
		MethodSelector:   selector,
		Signer:           signer,
		ClientID:         clientID,
		ChainEnvironment: chainEnv,
	}, nil
}

// formatWei converts a wei integer to a human-readable string showing
// both the wei integer (with thousand separators) and the equivalent
// ETH-unit value to 6 decimal places. The ETH-unit conversion is for
// readability only -- the wei integer is the source of truth.
func formatWei(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	weiStr := withThousands(wei.String())
	// 1 ETH = 1e18 wei. We compute the truncated ETH integer and
	// 6-digit micro-ETH fraction to avoid float drift on large values.
	ether := new(big.Int).Quo(wei, big.NewInt(1_000_000_000_000_000_000))
	microRem := new(big.Int).Mod(wei, big.NewInt(1_000_000_000_000_000_000))
	micro := new(big.Int).Quo(microRem, big.NewInt(1_000_000_000_000))
	return fmt.Sprintf("%s wei (≈ %s.%06d ETH)", weiStr, ether.String(), micro.Int64())
}

// withThousands inserts comma separators every 3 digits, right-to-left.
// "1234567890" -> "1,234,567,890". Negative-sign safe.
func withThousands(s string) string {
	if s == "" {
		return s
	}
	sign := ""
	if s[0] == '-' {
		sign = "-"
		s = s[1:]
	}
	n := len(s)
	if n <= 3 {
		return sign + s
	}
	// Number of commas to insert.
	commas := (n - 1) / 3
	out := make([]byte, 0, n+commas)
	first := n - 3*((n-1)/3+1) + 3 // first chunk length, 1..3
	if first <= 0 {
		first += 3
	}
	out = append(out, s[:first]...)
	for i := first; i < n; i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return sign + string(out)
}

func formatApprovalMessage(s *TxSummary) string {
	chainLabel := s.ChainID.String()
	if s.ChainEnvironment != "" {
		chainLabel = fmt.Sprintf("%s (%s)", s.ChainID.String(), s.ChainEnvironment)
	}
	selectorLine := s.MethodSelector
	if selectorLine == "" {
		selectorLine = "(no calldata)"
	}
	return fmt.Sprintf(
		"Approve transaction broadcast?\n\n"+
			"  Signer (recovered): %s\n"+
			"  To:                 %s\n"+
			"  Method selector:    %s\n"+
			"  Value:              %s\n"+
			"  Gas:                %d\n"+
			"  Nonce:              %d\n"+
			"  Chain ID:           %s\n"+
			"  Data:               %d bytes\n"+
			"  Submitted by:       %s\n\n"+
			"This is irreversible. The signed transaction will be broadcast to the chain. "+
			"Verify the signer matches your wallet and the method selector matches the operation you intended.",
		s.Signer, s.To, selectorLine, formatWei(s.Value),
		s.Gas, s.Nonce, chainLabel, s.DataLen, s.ClientID,
	)
}

// approvalFieldName is the single boolean property in the elicitation form
// schema below.
const approvalFieldName = "approve"

// writeApprovalSchema is the form `requestedSchema` sent with every
// write-approval elicitation. The MCP spec requires a form elicitation to
// carry a requestedSchema; a bare message with no schema is rejected by
// spec-strict clients (e.g. Claude / Fable 5), so without this the approval
// round-trip never completes and a `required` write can never broadcast from
// those clients. It is an object schema with one flat primitive property, per
// the elicitation restriction that only top-level primitives are allowed. The
// property is intentionally NOT required: a client that accepts without echoing
// form content still succeeds, since the accept/decline/cancel action is the
// primary decision (see RequestWriteApproval).
var writeApprovalSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		approvalFieldName: map[string]any{
			"type": "boolean",
			"description": "Approve broadcasting this signed transaction to the " +
				"chain. This action is irreversible. Decline or cancel to abort.",
		},
	},
}

// RequestWriteApproval uses MCP elicitation to ask the human user to approve a
// transaction broadcast. Returns nil if approved, ErrWriteDeclined if declined
// or canceled, and ErrElicitationUnsupported if the client has no elicitation
// capability. The elicitation is sent as a spec-compliant `form` request
// carrying writeApprovalSchema.
func RequestWriteApproval(
	ctx context.Context,
	session *mcp.ServerSession,
	summary *TxSummary,
) error {
	result, err := session.Elicit(ctx, &mcp.ElicitParams{
		Mode:            "form",
		Message:         formatApprovalMessage(summary),
		RequestedSchema: writeApprovalSchema,
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
	// Defensive: honor an explicit negative in the returned form content even
	// when the action was "accept" -- e.g. a client that renders the checkbox
	// and lets the user uncheck it before submitting.
	if v, ok := result.Content[approvalFieldName]; ok {
		if approved, isBool := v.(bool); isBool && !approved {
			return fmt.Errorf(
				"user set %s=false: %w",
				approvalFieldName, apperrors.ErrWriteDeclined,
			)
		}
	}
	return nil
}

// CheckWriteApproval resolves the approval policy from context and config,
// and if approval is required, sends an elicitation to the human user.
// chainEnv is the operator-facing chain label ("testnet"/"mainnet")
// rendered in the approval prompt alongside the numeric chain ID.
// Returns nil if the write should proceed.
func CheckWriteApproval(
	ctx context.Context,
	session *mcp.ServerSession,
	signedTxHex string,
	globalDefault string,
	chainEnv string,
) error {
	perClient := auth.WriteApprovalFromContext(ctx)
	policy := ResolveWriteApproval(perClient, globalDefault)

	if policy == ApprovalAuto {
		return nil
	}

	clientID := auth.ClientIDFromContext(ctx)
	summary, err := DecodeTxSummary(signedTxHex, clientID, chainEnv)
	if err != nil {
		return err
	}

	return RequestWriteApproval(ctx, session, summary)
}
