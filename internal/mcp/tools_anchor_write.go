package mcp

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
)

func registerAnchorWriteTools(
	srv *mcp.Server,
	anchorClient anchor.Client,
	_ *slog.Logger,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_add_registry",
		Title: "Prepare Add Registry Transaction",
		Description: "Construct an unsigned addRegistry transaction. " +
			"Returns a complete unsigned transaction for the caller to sign " +
			"and submit via evm_send_raw_transaction.",
	}, makePrepareAddRegistryHandler(anchorClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_add_record",
		Title: "Prepare Add Record Transaction",
		Description: "Construct an unsigned addRecord transaction to anchor " +
			"a document checksum and URI in a registry. Returns a complete " +
			"unsigned transaction for the caller to sign and submit.",
	}, makePrepareAddRecordHandler(anchorClient))

	mcp.AddTool(srv, &mcp.Tool{
		Name:  "anchor_prepare_grant_role",
		Title: "Prepare Grant Role Transaction",
		Description: "Construct an unsigned grantRole transaction to assign " +
			"admin or editor permissions on a registry. Returns a complete " +
			"unsigned transaction for the caller to sign and submit.",
	}, makePrepareGrantRoleHandler(anchorClient))
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
	c anchor.Client,
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
		return nil, *tx, nil
	}
}

func makePrepareAddRecordHandler(
	c anchor.Client,
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
		return nil, *tx, nil
	}
}

func makePrepareGrantRoleHandler(
	c anchor.Client,
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
		return nil, *tx, nil
	}
}
