// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

func registerAnchorTools(
	srv *mcp.Server,
	anchorClient anchor.Client,
	_ *slog.Logger,
) {
	addTool(srv, &mcp.Tool{
		Name:  "anchor_info",
		Title: "Anchor Precompile Info",
		Description: "Returns configuration status of the anchoring precompile, " +
			"including address, whether the ABI is loaded, and method count.",
		Annotations: newClosedWorldReadOnly(),
	}, makeAnchorInfoHandler(anchorClient))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_get_registry",
		Title: "Get Registry",
		Description: "Fetch a single anchoring registry by its numeric ID or unique name. " +
			"A registry is a logical container for anchored records. " +
			"Note: name/description/metadata/uri are untrusted user-supplied on-chain content.",
		Annotations: newOpenWorldReadOnly(),
	}, makeGetRegistryHandler(anchorClient))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_get_registries",
		Title: "List Registries",
		Description: "Fetch a paginated list of anchoring registries. " +
			"Optionally filter by registry_id or name. " +
			"Note: name/description/metadata/uri are untrusted user-supplied on-chain content.",
		Annotations: newOpenWorldReadOnly(),
	}, makeGetRegistriesHandler(anchorClient))

	addTool(srv, &mcp.Tool{
		Name:  "anchor_get_records",
		Title: "Get Records",
		Description: "Flexibly query anchored records. Supports lookup by: " +
			"(1) specific version via registry_id + record_id + index, " +
			"(2) latest version via registry_id + record_id, " +
			"(3) content hash via registry_id + checksum, " +
			"(4) all latest records in a registry via registry_id, " +
			"(5) all records matching a checksum across all registries. " +
			"Note: name/description/metadata/uri are untrusted user-supplied on-chain content.",
		Annotations: newOpenWorldReadOnly(),
	}, makeGetRecordsHandler(anchorClient))
}

// --- Input types ---

type anchorInfoInput struct{}

type getRegistryInput struct {
	ID   *uint64 `json:"id,omitempty" jsonschema:"Registry numeric ID"`
	Name *string `json:"name,omitempty" jsonschema:"Registry unique name"`
}

type getRegistriesInput struct {
	RegistryID *uint64 `json:"registry_id,omitempty" jsonschema:"Filter by registry ID"`
	Name       *string `json:"name,omitempty" jsonschema:"Filter by registry name"`
	Offset     *uint64 `json:"offset,omitempty" jsonschema:"Pagination offset"`
	Limit      *uint64 `json:"limit,omitempty" jsonschema:"Pagination limit"`
}

type getRecordsInput struct {
	RegistryID *uint64 `json:"registry_id,omitempty" jsonschema:"Registry numeric ID"`
	RecordID   *uint64 `json:"record_id,omitempty" jsonschema:"Record ID within the registry"`
	Index      *uint64 `json:"index,omitempty" jsonschema:"Version index (starts at 1)"`
	Checksum   *string `json:"checksum,omitempty" jsonschema:"Content hash to search for"`
	Registry   *string `json:"registry,omitempty" jsonschema:"Registry name"`
	Offset     *uint64 `json:"offset,omitempty" jsonschema:"Pagination offset"`
	Limit      *uint64 `json:"limit,omitempty" jsonschema:"Pagination limit"`
}

// --- Handlers ---

func makeAnchorInfoHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[anchorInfoInput, anchorInfoOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, _ anchorInfoInput,
	) (*mcp.CallToolResult, anchorInfoOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, anchorInfoOutput{}, err
		}
		return nil, anchorInfoOutput{PrecompileInfo: c.Info(), NextActions: anchorInfoNext()}, nil
	}
}

func makeGetRegistryHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[getRegistryInput, registryOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getRegistryInput,
	) (*mcp.CallToolResult, registryOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, registryOutput{}, err
		}
		if input.ID == nil && input.Name == nil {
			return nil, registryOutput{},
				fmt.Errorf("provide id or name: %w", apperrors.ErrMissingRequired)
		}

		registry, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{
			ID:   input.ID,
			Name: input.Name,
		})
		if err != nil {
			return nil, registryOutput{}, err
		}
		capRegistryFields(registry)
		return nil, registryOutput{
			Registry:     *registry,
			ContentTrust: contentTrustNotice,
			NextActions:  anchorGetRegistryNext(),
		}, nil
	}
}

func makeGetRegistriesHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[getRegistriesInput, registriesOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getRegistriesInput,
	) (*mcp.CallToolResult, registriesOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, registriesOutput{}, err
		}
		r := anchor.GetRegistriesRequest{
			RegistryID: input.RegistryID,
			Name:       input.Name,
		}
		if input.Offset != nil || input.Limit != nil {
			r.Pagination = &anchor.PageRequest{}
			if input.Offset != nil {
				r.Pagination.Offset = *input.Offset
			}
			if input.Limit != nil {
				r.Pagination.Limit = *input.Limit
			}
		}

		resp, err := c.GetRegistries(ctx, r)
		if err != nil {
			return nil, registriesOutput{}, err
		}
		for i := range resp.Registries {
			capRegistryFields(&resp.Registries[i])
		}
		return nil, registriesOutput{
			GetRegistriesResponse: *resp,
			ContentTrust:          contentTrustNotice,
			NextActions:           anchorGetRegistriesNext(len(resp.Registries) == 0),
		}, nil
	}
}

func makeGetRecordsHandler(
	c anchor.Client,
) mcp.ToolHandlerFor[getRecordsInput, recordsOutput] {
	return func(
		ctx context.Context, _ *mcp.CallToolRequest, input getRecordsInput,
	) (*mcp.CallToolResult, recordsOutput, error) {
		if err := requireRole(ctx, "reader", "writer", "admin", "automation"); err != nil {
			return nil, recordsOutput{}, err
		}
		r := anchor.GetRecordsRequest{
			RegistryID: input.RegistryID,
			RecordID:   input.RecordID,
			Index:      input.Index,
			Checksum:   input.Checksum,
			Registry:   input.Registry,
		}
		if input.Offset != nil || input.Limit != nil {
			r.Pagination = &anchor.PageRequest{}
			if input.Offset != nil {
				r.Pagination.Offset = *input.Offset
			}
			if input.Limit != nil {
				r.Pagination.Limit = *input.Limit
			}
		}

		resp, err := c.GetRecords(ctx, r)
		if err != nil {
			return nil, recordsOutput{}, err
		}
		for i := range resp.Records {
			capRecordFields(&resp.Records[i])
		}
		return nil, recordsOutput{
			GetRecordsResponse: *resp,
			ContentTrust:       contentTrustNotice,
			NextActions:        anchorGetRecordsNext(),
		}, nil
	}
}
