package mcp

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/auth"
	"github.com/inveniam/nvnm-mcp-server/internal/logging"
)

func registerAnchorWriteTools(
	srv *mcp.Server,
	anchorClient anchor.Client,
	logger *slog.Logger,
) {
	const walletSigningPaths = "Returns two signing paths: " +
		"(1) wallet_tx_request -- pass this object directly to a MetaMask / EIP-1193 " +
		"wallet via eth_sendTransaction; the wallet signs and broadcasts, " +
		"so do NOT call evm_send_raw_transaction in that case. " +
		"(2) raw_tx -- RLP-encoded unsigned bytes for local or headless signers; " +
		"sign externally, then broadcast via evm_send_raw_transaction. " +
		"Confirm either path with evm_get_transaction_receipt(tx_hash). " +
		"The server never holds or receives private keys."

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "anchor_prepare_add_registry",
		Title:       "Prepare Add Registry Transaction",
		Description: "Construct an unsigned addRegistry transaction. " + walletSigningPaths,
	}, makePrepareAddRegistryHandler(anchorClient, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_add_record",
		Title: "Prepare Add Record Transaction",
		Description: "Construct an unsigned addRecord transaction to anchor " +
			"a document checksum and URI in a registry. " + walletSigningPaths +
			" After confirming, verify with anchor_get_records.",
	}, makePrepareAddRecordHandler(anchorClient, logger))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_grant_role",
		Title: "Prepare Grant Role Transaction",
		Description: "Construct an unsigned grantRole transaction to assign " +
			"admin or editor permissions on a registry or specific record. " +
			walletSigningPaths,
	}, makePrepareGrantRoleHandler(anchorClient, logger))
}

// --- Input types ---

type prepareAddRegistryInput struct {
	From        string `json:"from" jsonschema:"Sender EVM address (0x...)"`
	Name        string `json:"name" jsonschema:"Registry name (unique)"`
	Description string `json:"description" jsonschema:"Registry description"`
	Metadata    string `json:"metadata,omitempty" jsonschema:"Optional JSON metadata"`
}

type prepareAddRecordInput struct {
	From         string `json:"from" jsonschema:"Sender EVM address (0x...)"`
	Registry     string `json:"registry" jsonschema:"Registry name"`
	URI          string `json:"uri" jsonschema:"Document URI"`
	Checksum     string `json:"checksum" jsonschema:"Document checksum hash"`
	ChecksumAlgo string `json:"checksum_algo,omitempty" jsonschema:"Hash algorithm (e.g. sha256)"`
	Status       string `json:"status,omitempty" jsonschema:"Record status (default: Active)"`
	Metadata     string `json:"metadata,omitempty" jsonschema:"Optional JSON metadata"`
}

type prepareGrantRoleInput struct {
	From       string `json:"from" jsonschema:"Admin EVM address (0x...)"`
	RegistryID uint64 `json:"registry_id" jsonschema:"Registry numeric ID"`
	Checksum   string `json:"checksum,omitempty" jsonschema:"Optional: scope role to a specific record checksum"`
	Account    string `json:"account" jsonschema:"Address to grant the role to (0x...)"`
	Role       string `json:"role" jsonschema:"Role to grant: admin or editor"`
}

// --- Handlers ---

func makePrepareAddRegistryHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareAddRegistryInput, anchor.UnsignedTransaction] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareAddRegistryInput,
	) (*mcp.CallToolResult, anchor.UnsignedTransaction, error) {
		tx, err := c.PrepareAddRegistry(ctx, anchor.PrepareAddRegistryRequest{
			From:        input.From,
			Name:        input.Name,
			Description: input.Description,
			Metadata:    input.Metadata,
		})
		if err != nil {
			return nil, anchor.UnsignedTransaction{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit: prepare_add_registry",
			slog.String("client_id", auth.ClientIDFromContext(ctx)),
			logging.SafeAddr("from", input.From),
			slog.String("registry_name", input.Name),
		)
		return nil, *tx, nil
	}
}

func makePrepareAddRecordHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareAddRecordInput, anchor.UnsignedTransaction] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareAddRecordInput,
	) (*mcp.CallToolResult, anchor.UnsignedTransaction, error) {
		tx, err := c.PrepareAddRecord(ctx, anchor.PrepareAddRecordRequest{
			From:         input.From,
			Registry:     input.Registry,
			URI:          input.URI,
			Checksum:     input.Checksum,
			ChecksumAlgo: input.ChecksumAlgo,
			Status:       input.Status,
			Metadata:     input.Metadata,
		})
		if err != nil {
			return nil, anchor.UnsignedTransaction{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit: prepare_add_record",
			slog.String("client_id", auth.ClientIDFromContext(ctx)),
			logging.SafeAddr("from", input.From),
			slog.String("registry", input.Registry),
			slog.String("uri", input.URI),
		)
		return nil, *tx, nil
	}
}

func makePrepareGrantRoleHandler(
	c anchor.Client, logger *slog.Logger,
) mcp.ToolHandlerFor[prepareGrantRoleInput, anchor.UnsignedTransaction] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input prepareGrantRoleInput,
	) (*mcp.CallToolResult, anchor.UnsignedTransaction, error) {
		tx, err := c.PrepareGrantRole(ctx, anchor.PrepareGrantRoleRequest{
			From:       input.From,
			RegistryID: input.RegistryID,
			Checksum:   input.Checksum,
			Account:    input.Account,
			Role:       input.Role,
		})
		if err != nil {
			return nil, anchor.UnsignedTransaction{}, err
		}
		logger.LogAttrs(ctx, slog.LevelInfo, "audit: prepare_grant_role",
			slog.String("client_id", auth.ClientIDFromContext(ctx)),
			logging.SafeAddr("from", input.From),
			slog.Uint64("registry_id", input.RegistryID),
			logging.SafeAddr("account", input.Account),
			slog.String("role", input.Role),
		)
		return nil, *tx, nil
	}
}
