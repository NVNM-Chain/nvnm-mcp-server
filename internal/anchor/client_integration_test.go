// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package anchor_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/evm"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

const (
	testRPCURL         = "https://evm.testnet.nvnmchain.io"
	testChainID        = int64(787111)
	testABIRelPath     = "../../abi/anchoring.json"
	testConnectTimeout = 15 * time.Second
)

func integrationClient(t *testing.T) anchor.Client {
	t.Helper()
	evmClient := integrationResilientEVMClient(t)
	logger := logging.New("error")
	c := anchor.NewClient(evmClient, anchor.PrecompileAddress, testChainID, testABIRelPath, logger)
	if !c.Available() {
		t.Fatal("anchor client not available (ABI not loaded)")
	}
	return c
}

// integrationResilientEVMClient mirrors the production wiring:
// bare evm client -> resilient wrapper (retry / rate-limit / breaker).
// Required because the testnet RPC has a documented transient race on
// eth_gasPrice immediately after a broadcast (cometReceiptsRaceMarker
// in evm/resilient.go). Without the wrapper, back-to-back
// PrepareXxx calls hit the race uncovered and fail spuriously.
func integrationResilientEVMClient(t *testing.T) evm.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testConnectTimeout)
	defer cancel()

	raw, err := evm.NewClient(ctx, testRPCURL, testConnectTimeout)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { raw.Close() })

	mp := sdkmetric.NewMeterProvider()
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	mets, err := telemetry.NewMetrics(mp)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	return evm.NewResilientClient(raw, evm.ResilientConfig{
		MaxRetries:       5,
		InitialBackoff:   500 * time.Millisecond,
		MaxBackoff:       5 * time.Second,
		RateLimit:        100,
		RateBurst:        20,
		BreakerThreshold: 10,
		BreakerTimeout:   30 * time.Second,
	}, mets, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestIntegration_Info(t *testing.T) {
	c := integrationClient(t)
	info := c.Info()

	if info.Address != "0x0000000000000000000000000000000000000A00" {
		t.Errorf("Address = %q", info.Address)
	}
	if info.ChainID != testChainID {
		t.Errorf("ChainID = %d, want %d", info.ChainID, testChainID)
	}
	if !info.ABILoaded {
		t.Error("ABILoaded should be true")
	}
	if info.MethodCount != 5 {
		t.Errorf("MethodCount = %d, want 5", info.MethodCount)
	}
}

func TestIntegration_GetRegistries(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	resp, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
		Pagination: &anchor.PageRequest{Limit: 5},
	})
	if err != nil {
		t.Fatalf("GetRegistries: %v", err)
	}
	if resp.Pagination == nil {
		t.Fatal("pagination should not be nil")
	}
	// The nvnm-testnet-1 anchor precompile returns pagination.total=0 even
	// with countTotal=true, so assert on the returned slice -- that is what
	// the server actually guarantees. See docs/TESTING.md on the count_total
	// behavioral difference.
	if len(resp.Registries) == 0 {
		t.Fatal("expected at least one registry in results")
	}

	first := resp.Registries[0]
	if first.ID == 0 {
		t.Error("first registry ID should be > 0")
	}
	if first.Name == "" {
		t.Error("first registry name should not be empty")
	}
	if first.Creator == "" {
		t.Error("first registry creator should not be empty")
	}
	if first.CreatedAt == "" {
		t.Error("first registry created_at should not be empty")
	}
}

func TestIntegration_GetRegistry_ByID(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	id := uint64(1)
	reg, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{ID: &id})
	if err != nil {
		t.Fatalf("GetRegistry(ID=1): %v", err)
	}
	if reg.ID != 1 {
		t.Errorf("ID = %d, want 1", reg.ID)
	}
	if reg.Name == "" {
		t.Error("name should not be empty")
	}
}

func TestIntegration_GetRegistry_ByName(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// First get a registry to know a valid name
	regs, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
		Pagination: &anchor.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatalf("GetRegistries: %v", err)
	}
	if len(regs.Registries) == 0 {
		t.Skip("no registries on chain")
	}

	name := regs.Registries[0].Name
	reg, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{Name: &name})
	if err != nil {
		t.Fatalf("GetRegistry(Name=%q): %v", name, err)
	}
	if reg.Name != name {
		t.Errorf("Name = %q, want %q", reg.Name, name)
	}
}

func TestIntegration_GetRecords_ByRegistry(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// mcp-test-data is the stable registry seeded by cmd/seed-test-data;
	// it carries 3 records.
	name := "mcp-test-data"
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &name,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(registry=%q): %v", name, err)
	}
	if len(resp.Records) == 0 {
		t.Fatalf("expected at least one record in %q", name)
	}

	rec := resp.Records[0]
	if rec.Registry != name {
		t.Errorf("Registry = %q, want %q", rec.Registry, name)
	}
	if rec.RecordID == 0 {
		t.Error("RecordID should be > 0")
	}
	if rec.Checksum == "" {
		t.Error("Checksum should not be empty")
	}
	if rec.URI == "" {
		t.Error("URI should not be empty")
	}
	if rec.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
	if rec.Status == "" {
		t.Error("Status should not be empty")
	}
}

// TestIntegration_GetRecords_ByID verifies that querying records by numeric
// registry_id returns the same records as querying by registry name. The
// precompile's records query is name-keyed, so the client resolves
// registry_id -> name internally; before that fix a registry_id filter was
// silently ignored and returned an empty set.
func TestIntegration_GetRecords_ByID(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	name := "mcp-test-data"

	// Resolve the registry's numeric id by name.
	reg, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{Name: &name})
	if err != nil {
		t.Fatalf("GetRegistry(name=%q): %v", name, err)
	}
	if reg.ID == 0 {
		t.Fatalf("registry %q has id 0", name)
	}

	byName, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &name,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(registry=%q): %v", name, err)
	}
	if len(byName.Records) == 0 {
		t.Fatalf("expected records in %q", name)
	}

	byID, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		RegistryID: &reg.ID,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(registry_id=%d): %v", reg.ID, err)
	}

	// The by-id query must return the same non-empty set as the by-name query.
	if len(byID.Records) != len(byName.Records) {
		t.Fatalf("by-id returned %d records, by-name returned %d (registry_id ignored?)",
			len(byID.Records), len(byName.Records))
	}
	if byID.Records[0].Registry != name {
		t.Errorf("by-id record Registry = %q, want %q", byID.Records[0].Registry, name)
	}
	if byID.Records[0].Checksum != byName.Records[0].Checksum {
		t.Errorf("by-id first checksum %q != by-name %q",
			byID.Records[0].Checksum, byName.Records[0].Checksum)
	}
}

// TestIntegration_GetRecords_BadID confirms an unknown registry_id fails loud
// (resolve error) rather than silently returning an empty record set.
func TestIntegration_GetRecords_BadID(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	badID := uint64(99999999)
	_, err := c.GetRecords(ctx, anchor.GetRecordsRequest{RegistryID: &badID})
	if err == nil {
		t.Fatal("expected error resolving a non-existent registry_id, got nil")
	}
}

func TestIntegration_GetRecords_Pagination(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// mcp-test-data is the stable registry seeded by cmd/seed-test-data;
	// it carries 3 records -- enough to exercise offset/limit paging.
	name := "mcp-test-data"

	// Confirm the registry has enough records to page through. The
	// nvnm-testnet-1 precompile returns pagination.total=0 even with
	// countTotal=true, so count the returned slice rather than trust Total.
	all, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &name,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(limit=10): %v", err)
	}
	if len(all.Records) < 2 {
		t.Fatalf("need >= 2 records in %q for pagination test, got %d", name, len(all.Records))
	}

	// Page 1: first record only.
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &name,
		Pagination: &anchor.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatalf("GetRecords(limit=1): %v", err)
	}
	if len(resp.Records) != 1 {
		t.Errorf("expected 1 record, got %d", len(resp.Records))
	}
	if resp.Pagination == nil {
		t.Fatal("pagination should not be nil")
	}

	// Page 2: offset by 1.
	resp2, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &name,
		Pagination: &anchor.PageRequest{Offset: 1, Limit: 1},
	})
	if err != nil {
		t.Fatalf("GetRecords(offset=1, limit=1): %v", err)
	}
	if len(resp2.Records) != 1 {
		t.Errorf("page 2: expected 1 record, got %d", len(resp2.Records))
	}
	if resp2.Records[0].RecordID == resp.Records[0].RecordID &&
		resp2.Records[0].Index == resp.Records[0].Index {
		t.Error("page 2 returned the same record as page 1")
	}
}
