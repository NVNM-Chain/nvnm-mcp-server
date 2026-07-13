// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
)

// fakeAnchor implements anchor.Client for read-handler tests. Read methods
// return the configured fixtures; the rest are unused stubs (non-nil returns
// to satisfy the nilnil linter -- they are never called here).
type fakeAnchor struct {
	registry   *anchor.Registry
	registries []anchor.Registry
	records    []anchor.Record
}

func (f *fakeAnchor) Info() anchor.PrecompileInfo { return anchor.PrecompileInfo{} }
func (f *fakeAnchor) Available() bool             { return true }

// MethodSelector returns a stable stub selector; no test in this file
// exercises selector-keyed behavior (see purge_test.go for that).
func (f *fakeAnchor) MethodSelector(string) (string, bool) { return "", false }

func (f *fakeAnchor) GetRegistry(context.Context, anchor.GetRegistryRequest) (*anchor.Registry, error) {
	return f.registry, nil
}

func (f *fakeAnchor) GetRegistries(context.Context, anchor.GetRegistriesRequest) (*anchor.GetRegistriesResponse, error) {
	return &anchor.GetRegistriesResponse{Registries: f.registries}, nil
}

func (f *fakeAnchor) GetRecords(context.Context, anchor.GetRecordsRequest) (*anchor.GetRecordsResponse, error) {
	return &anchor.GetRecordsResponse{Records: f.records}, nil
}

func (f *fakeAnchor) PrepareAddRegistry(context.Context, anchor.PrepareAddRegistryRequest) (*anchor.UnsignedTransaction, error) {
	return &anchor.UnsignedTransaction{}, nil
}

func (f *fakeAnchor) PrepareAddRecord(context.Context, anchor.PrepareAddRecordRequest) (*anchor.UnsignedTransaction, error) {
	return &anchor.UnsignedTransaction{}, nil
}

func (f *fakeAnchor) PrepareGrantRole(context.Context, anchor.PrepareGrantRoleRequest) (*anchor.UnsignedTransaction, error) {
	return &anchor.UnsignedTransaction{}, nil
}

func TestGetRecords_CapsAndLabelsUntrusted(t *testing.T) {
	big := strings.Repeat("x", maxUntrustedMetadata+500)
	c := &fakeAnchor{records: []anchor.Record{{RecordID: 1, Metadata: big, URI: "ipfs://ok"}}}
	h := makeGetRecordsHandler(c)

	_, out, err := h(context.Background(), &sdkmcp.CallToolRequest{}, getRecordsInput{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.ContentTrust == "" {
		t.Fatal("content_trust advisory not set")
	}
	got := out.Records[0].Metadata
	if !strings.Contains(got, "[truncated,") {
		t.Fatalf("oversized metadata not capped/marked: %q", got[:60])
	}
	if out.Records[0].URI != "ipfs://ok" {
		t.Fatalf("under-cap uri should be unchanged, got %q", out.Records[0].URI)
	}
}

func TestGetRegistries_CapsAndLabelsUntrusted(t *testing.T) {
	big := strings.Repeat("d", maxUntrustedDescription+500)
	c := &fakeAnchor{registries: []anchor.Registry{{ID: 1, Name: "ok", Description: big}}}
	h := makeGetRegistriesHandler(c)

	_, out, err := h(context.Background(), &sdkmcp.CallToolRequest{}, getRegistriesInput{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.ContentTrust == "" {
		t.Fatal("content_trust advisory not set")
	}
	if !strings.Contains(out.Registries[0].Description, "[truncated,") {
		t.Fatalf("oversized description not capped/marked")
	}
	if out.Registries[0].Name != "ok" {
		t.Fatalf("under-cap name should be unchanged, got %q", out.Registries[0].Name)
	}
}

func TestGetRegistry_CapsAndLabelsUntrusted(t *testing.T) {
	big := strings.Repeat("m", maxUntrustedMetadata+500)
	c := &fakeAnchor{registry: &anchor.Registry{ID: 1, Name: "ok", Metadata: big}}
	id := uint64(1)
	h := makeGetRegistryHandler(c)

	_, out, err := h(context.Background(), &sdkmcp.CallToolRequest{}, getRegistryInput{ID: &id})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.ContentTrust == "" {
		t.Fatal("content_trust advisory not set")
	}
	if !strings.Contains(out.Metadata, "[truncated,") {
		t.Fatalf("oversized metadata not capped/marked")
	}
}
