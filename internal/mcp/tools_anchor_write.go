// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/auth"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

func registerAnchorWriteTools(
	srv *mcp.Server,
	anchorClient anchor.Client,
	logger *slog.Logger,
) {
	// walletSigningPaths describes the two signing outputs every prepare
	// tool returns. It intentionally does NOT name the required role -- that
	// differs per tool (see accessControl* below) and a shared role sentence
	// previously leaked add_record's writer/admin/automation set onto
	// grant_role, which is admin-only (rc8 E2E F4).
	const walletSigningPaths = "Returns two signing paths: " +
		"(1) wallet_tx_request -- pass this object directly to a MetaMask / EIP-1193 " +
		"wallet via eth_sendTransaction; the wallet signs and broadcasts, " +
		"so do NOT call evm_send_raw_transaction in that case. " +
		"(2) raw_tx -- RLP-encoded unsigned bytes for local or headless signers; " +
		"sign externally, then broadcast via evm_send_raw_transaction. " +
		"Confirm either path with evm_get_transaction_receipt(tx_hash). " +
		"The server never holds or receives private keys."

	// accessControlReadWrite matches requireRole(ctx, "writer", "admin",
	// "automation") on add_registry / add_record.
	const accessControlReadWrite = " Access control: this tool is annotated " +
		"read-only (it does not modify server or chain state by itself) but " +
		"requires the writer, admin, or automation role because the output is " +
		"a signing-ready payload."

	// accessControlAdminOnlyGrant matches requireRole(ctx, "admin") on grant_role.
	const accessControlAdminOnlyGrant = " Access control: this tool is annotated " +
		"read-only (it does not modify server or chain state by itself) but " +
		"requires the admin role -- granting roles is an administrative " +
		"operation -- and the output is a signing-ready payload."

	// accessControlAdminOnlyRevoke matches requireRole(ctx, "admin") on revoke_role.
	const accessControlAdminOnlyRevoke = " Access control: this tool is annotated " +
		"read-only (it does not modify server or chain state by itself) but " +
		"requires the admin role -- revoking roles is an administrative " +
		"operation -- and the output is a signing-ready payload."

	addTool(srv, &mcp.Tool{
		Name:        "anchor_prepare_add_registry",
		Title:       "Prepare Add Registry Transaction",
		Description: "Construct an unsigned addRegistry transaction. " + walletSigningPaths + accessControlReadWrite,
		Annotations: newOpenWorldReadOnly(),
	}, makePrepareAddRegistryHandler(anchorClient, logger))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_add_record",
		Title: "Prepare Add Record Transaction",
		Description: "Construct an unsigned addRecord transaction to anchor " +
			"a document checksum and URI in a registry. " + walletSigningPaths +
			accessControlReadWrite + " After confirming, verify with anchor_get_records.",
		Annotations: newOpenWorldReadOnly(),
	}, makePrepareAddRecordHandler(anchorClient, logger))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_update_record_status",
		Title: "Prepare Update Record Status Transaction",
		Description: "Construct an unsigned updateRecordStatus transaction to " +
			"change the status of an existing anchored record (e.g. Active, " +
			"Superseded, Revoked). " + walletSigningPaths +
			accessControlReadWrite + " After confirming, verify with anchor_get_records.",
		Annotations: newOpenWorldReadOnly(),
	}, makePrepareUpdateRecordStatusHandler(anchorClient, logger))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_grant_role",
		Title: "Prepare Grant Role Transaction",
		Description: "Construct an unsigned grantRole transaction to assign " +
			"admin or editor permissions on a registry or specific record. " +
			walletSigningPaths + accessControlAdminOnlyGrant,
		Annotations: newOpenWorldReadOnly(),
	}, makePrepareGrantRoleHandler(anchorClient, logger))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_revoke_role",
		Title: "Prepare Revoke Role Transaction",
		Description: "Construct an unsigned revokeRole transaction to remove " +
			"admin or editor permissions from a registry or specific record. " +
			walletSigningPaths + accessControlAdminOnlyRevoke,
		Annotations: newOpenWorldReadOnly(),
	}, makePrepareRevokeRoleHandler(anchorClient, logger))
}

// --- Input types ---

type prepareAddRegistryInput struct {
	From        string `json:"from" jsonschema:"Sender EVM address (0x...)"`
	Name        string `json:"name" jsonschema:"Registry name (unique)"`
	Description string `json:"description" jsonschema:"Registry description"`
	Metadata    string `json:"metadata,omitempty" jsonschema:"Optional JSON metadata"`
	//nolint:lll // descriptive prose for agents
	PreferLegacyTx bool `json:"prefer_legacy_tx,omitempty" jsonschema:"Opt back into a type-0 LegacyTx instead of the EIP-1559 (type-2) default. Use only when the signer cannot produce type-2 signatures."`
}

type prepareAddRecordInput struct {
	From       string `json:"from" jsonschema:"Sender EVM address (0x...)"`
	RegistryID uint64 `json:"registry_id" jsonschema:"Registry numeric ID"`
	URI        string `json:"uri" jsonschema:"Document URI"`
	//nolint:lll // descriptive prose for agents
	Checksum string `json:"checksum" jsonschema:"Document checksum as a hex digest, max 64 chars (e.g. a SHA-256 digest is 64 hex chars). A leading 0x is accepted and stripped."`
	//nolint:lll // descriptive prose for agents
	ChecksumAlgo string `json:"checksum_algo" jsonschema:"Hash algorithm, e.g. sha256. Required by the anchoring precompile -- must be non-empty."`
	Status       string `json:"status,omitempty" jsonschema:"Record status (default: Active)"`
	//nolint:lll // descriptive prose for agents
	Metadata string `json:"metadata" jsonschema:"Metadata string. Required by the anchoring precompile and must be non-empty -- the empty JSON object {} is rejected. If you have no structured metadata, pass a short label (e.g. the document name) or a JSON object with at least one field."`
	//nolint:lll // descriptive prose for agents
	PreferLegacyTx bool `json:"prefer_legacy_tx,omitempty" jsonschema:"Opt back into a type-0 LegacyTx instead of the EIP-1559 (type-2) default."`
}

type prepareUpdateRecordStatusInput struct {
	From       string `json:"from" jsonschema:"Editor EVM address (0x...)"`
	RegistryID uint64 `json:"registry_id" jsonschema:"Registry numeric ID"`
	RecordID   uint64 `json:"record_id" jsonschema:"Record numeric ID"`
	// Index is required and 1-based. It carried `omitempty` and a "default:
	// latest" description, both of which were wrong: there is no server-side
	// default, and the precompile rejects index 0 outright. A caller that
	// followed the old schema and omitted it always failed, with the rejection
	// arriving as an opaque upstream error. Dropping `omitempty` makes the
	// generated schema mark it required, so the SDK rejects the omission
	// before a chain round-trip.
	//nolint:lll // descriptive prose for agents
	Index  uint64 `json:"index" jsonschema:"Version index of the record to update, 1-based (the first version is 1, not 0). Required -- there is no 'latest' default. anchor_get_records reports the index of each version."`
	Status string `json:"status" jsonschema:"New record status, e.g. Active, Superseded, Revoked"`
	//nolint:lll // descriptive prose for agents
	PreferLegacyTx bool `json:"prefer_legacy_tx,omitempty" jsonschema:"Opt back into a type-0 LegacyTx instead of the EIP-1559 (type-2) default."`
}

type prepareGrantRoleInput struct {
	From       string `json:"from" jsonschema:"Admin EVM address (0x...)"`
	RegistryID uint64 `json:"registry_id" jsonschema:"Registry numeric ID"`
	Checksum   string `json:"checksum,omitempty" jsonschema:"Optional: scope role to a specific record checksum"`
	Account    string `json:"account" jsonschema:"Address to grant the role to (0x...)"`
	Role       string `json:"role" jsonschema:"Role to grant: admin or editor"`
	//nolint:lll // descriptive prose for agents
	PreferLegacyTx bool `json:"prefer_legacy_tx,omitempty" jsonschema:"Opt back into a type-0 LegacyTx instead of the EIP-1559 (type-2) default."`
}

type prepareRevokeRoleInput struct {
	From       string `json:"from" jsonschema:"Admin EVM address (0x...)"`
	RegistryID uint64 `json:"registry_id" jsonschema:"Registry numeric ID"`
	Checksum   string `json:"checksum,omitempty" jsonschema:"Optional: scope role to a specific record checksum"`
	Account    string `json:"account" jsonschema:"Address to revoke the role from (0x...)"`
	Role       string `json:"role" jsonschema:"Role to revoke: admin or editor"`
	//nolint:lll // descriptive prose for agents
	PreferLegacyTx bool `json:"prefer_legacy_tx,omitempty" jsonschema:"Opt back into a type-0 LegacyTx instead of the EIP-1559 (type-2) default."`
}

// --- Handlers ---

func makePrepareAddRegistryHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareAddRegistryInput, unsignedTxOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareAddRegistryInput,
	) (*mcp.CallToolResult, unsignedTxOutput, error) {
		if err := requireRole(ctx, "writer", "admin", "automation"); err != nil {
			return nil, unsignedTxOutput{}, err
		}
		tx, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
			From:         input.From,
			Name:         input.Name,
			Description:  input.Description,
			Metadata:     input.Metadata,
			PreferLegacy: input.PreferLegacyTx,
		})
		if err != nil {
			return nil, unsignedTxOutput{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit",
			slog.Group("audit",
				slog.String("tool", "anchor_prepare_add_registry"),
				slog.String("phase", "prepared"),
				slog.String("client_id", auth.ClientIDFromContext(ctx)),
				logging.SafeAddr("from", input.From),
				slog.String("registry_name", input.Name),
			),
		)
		return nil, unsignedTxOutput{UnsignedTransaction: *tx, NextActions: anchorPrepareWriteNext()}, nil
	}
}

func makePrepareAddRecordHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareAddRecordInput, unsignedTxOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareAddRecordInput,
	) (*mcp.CallToolResult, unsignedTxOutput, error) {
		if err := requireRole(ctx, "writer", "admin", "automation"); err != nil {
			return nil, unsignedTxOutput{}, err
		}
		tx, err := c.PrepareAddRecord(ctx, anchor.PrepareAddRecordRequest{
			From:         input.From,
			RegistryID:   input.RegistryID,
			URI:          input.URI,
			Checksum:     input.Checksum,
			ChecksumAlgo: input.ChecksumAlgo,
			Status:       input.Status,
			Metadata:     input.Metadata,
			PreferLegacy: input.PreferLegacyTx,
		})
		if err != nil {
			return nil, unsignedTxOutput{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit",
			slog.Group("audit",
				slog.String("tool", "anchor_prepare_add_record"),
				slog.String("phase", "prepared"),
				slog.String("client_id", auth.ClientIDFromContext(ctx)),
				logging.SafeAddr("from", input.From),
				slog.Uint64("registry_id", input.RegistryID),
				slog.String("uri", input.URI),
			),
		)
		return nil, unsignedTxOutput{UnsignedTransaction: *tx, NextActions: anchorPrepareWriteNext()}, nil
	}
}

func makePrepareUpdateRecordStatusHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareUpdateRecordStatusInput, unsignedTxOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareUpdateRecordStatusInput,
	) (*mcp.CallToolResult, unsignedTxOutput, error) {
		if err := requireRole(ctx, "writer", "admin", "automation"); err != nil {
			return nil, unsignedTxOutput{}, err
		}
		// Defense in depth behind the required-field schema: a client that
		// bypasses schema validation would otherwise spend a gas-estimation
		// round-trip to be told "index cannot be zero" by the chain.
		if input.Index == 0 {
			return nil, unsignedTxOutput{}, fmt.Errorf(
				"index must be 1 or greater -- record version indexes start at 1, "+
					"and anchor_get_records reports the index of each version: %w",
				apperrors.ErrMissingRequired,
			)
		}
		tx, err := c.PrepareUpdateRecordStatus(ctx, anchor.PrepareUpdateRecordStatusRequest{
			From:         input.From,
			RegistryID:   input.RegistryID,
			RecordID:     input.RecordID,
			Index:        input.Index,
			Status:       input.Status,
			PreferLegacy: input.PreferLegacyTx,
		})
		if err != nil {
			return nil, unsignedTxOutput{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit",
			slog.Group("audit",
				slog.String("tool", "anchor_prepare_update_record_status"),
				slog.String("phase", "prepared"),
				slog.String("client_id", auth.ClientIDFromContext(ctx)),
				logging.SafeAddr("from", input.From),
				slog.Uint64("registry_id", input.RegistryID),
				slog.Uint64("record_id", input.RecordID),
				slog.String("status", input.Status),
			),
		)
		return nil, unsignedTxOutput{UnsignedTransaction: *tx, NextActions: anchorPrepareWriteNext()}, nil
	}
}

func makePrepareGrantRoleHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareGrantRoleInput, unsignedTxOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareGrantRoleInput,
	) (*mcp.CallToolResult, unsignedTxOutput, error) {
		if err := requireRole(ctx, "admin"); err != nil {
			return nil, unsignedTxOutput{}, err
		}
		tx, err := c.PrepareGrantRole(ctx, anchor.PrepareGrantRoleRequest{
			From:         input.From,
			RegistryID:   input.RegistryID,
			Checksum:     input.Checksum,
			Account:      input.Account,
			Role:         input.Role,
			PreferLegacy: input.PreferLegacyTx,
		})
		if err != nil {
			return nil, unsignedTxOutput{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit",
			slog.Group("audit",
				slog.String("tool", "anchor_prepare_grant_role"),
				slog.String("phase", "prepared"),
				slog.String("client_id", auth.ClientIDFromContext(ctx)),
				logging.SafeAddr("from", input.From),
				slog.Uint64("registry_id", input.RegistryID),
				logging.SafeAddr("account", input.Account),
				slog.String("role", input.Role),
			),
		)
		return nil, unsignedTxOutput{UnsignedTransaction: *tx, NextActions: anchorPrepareWriteNext()}, nil
	}
}

func makePrepareRevokeRoleHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareRevokeRoleInput, unsignedTxOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareRevokeRoleInput,
	) (*mcp.CallToolResult, unsignedTxOutput, error) {
		if err := requireRole(ctx, "admin"); err != nil {
			return nil, unsignedTxOutput{}, err
		}
		tx, err := c.PrepareRevokeRole(ctx, anchor.PrepareRevokeRoleRequest{
			From:         input.From,
			RegistryID:   input.RegistryID,
			Checksum:     input.Checksum,
			Account:      input.Account,
			Role:         input.Role,
			PreferLegacy: input.PreferLegacyTx,
		})
		if err != nil {
			return nil, unsignedTxOutput{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit",
			slog.Group("audit",
				slog.String("tool", "anchor_prepare_revoke_role"),
				slog.String("phase", "prepared"),
				slog.String("client_id", auth.ClientIDFromContext(ctx)),
				logging.SafeAddr("from", input.From),
				slog.Uint64("registry_id", input.RegistryID),
				logging.SafeAddr("account", input.Account),
				slog.String("role", input.Role),
			),
		)
		return nil, unsignedTxOutput{UnsignedTransaction: *tx, NextActions: anchorPrepareWriteNext()}, nil
	}
}
