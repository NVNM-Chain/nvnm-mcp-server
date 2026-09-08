// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build integration

package anchor_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
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

const (
	// findRegistryPageSize matches the precompile's hard page cap.
	findRegistryPageSize = 200
	// findRegistryReversePages is enough for a registry this process just
	// created (it is at the tip). One page of 200 is plenty; a second is
	// margin for concurrent suites.
	findRegistryReversePages = 2
	// findRegistryForwardPages bounds a full-table walk for a seeded name
	// that is no longer in the reverse window (e.g. mcp-test-data).
	findRegistryForwardPages = 100
	findRegistryPageTimeout  = 30 * time.Second
)

// findRegistryIDByName resolves a registry by exact name. The precompile
// has no by-name index, so this paginates client-side. Newly created
// unique names are at the tip: reverse-first returns in one RPC instead
// of walking ~thousands of older rows (which looked like a hang after
// cmd/seed-test-data when make test-integration entered internal/anchor).
func findRegistryIDByName(t *testing.T, c anchor.Client, name string) uint64 {
	t.Helper()
	if id, ok := scanRegistriesForName(t, c, name, true, findRegistryReversePages); ok {
		return id
	}
	if id, ok := scanRegistriesForName(t, c, name, false, findRegistryForwardPages); ok {
		return id
	}
	t.Fatalf("registry %q not found (reverse then forward listing)", name)
	return 0
}

var cachedSeedRegistryID uint64

func mcpTestDataRegistryID(t *testing.T, c anchor.Client) uint64 {
	t.Helper()
	if cachedSeedRegistryID != 0 {
		return cachedSeedRegistryID
	}
	cachedSeedRegistryID = findRegistryIDByName(t, c, "mcp-test-data")
	return cachedSeedRegistryID
}

func scanRegistriesForName(
	t *testing.T, c anchor.Client, name string, reverse bool, maxPages int,
) (uint64, bool) {
	t.Helper()
	noID := uint64(0)
	var cursorKey []byte
	var lastCursor string
	scanned := 0
	for page := 0; page < maxPages; page++ {
		ctx, cancel := context.WithTimeout(context.Background(), findRegistryPageTimeout)
		resp, err := c.GetRegistries(ctx, anchor.GetRegistriesRequest{
			RegistryID: &noID,
			Pagination: &anchor.PageRequest{
				Key:     cursorKey,
				Limit:   findRegistryPageSize,
				Reverse: reverse,
			},
		})
		cancel()
		if err != nil {
			t.Fatalf("GetRegistries(reverse=%v page=%d): %v", reverse, page, err)
		}
		scanned += len(resp.Registries)
		t.Logf("  name lookup %q reverse=%v page=%d rows=%d scanned=%d",
			name, reverse, page, len(resp.Registries), scanned)
		for i := range resp.Registries {
			if resp.Registries[i].Name == name {
				return resp.Registries[i].ID, true
			}
		}
		nextKey, err := resp.Pagination.CursorBytes()
		if err != nil {
			t.Fatalf("decode pagination cursor: %v", err)
		}
		if len(nextKey) == 0 {
			return 0, false
		}
		cursor := string(nextKey)
		if cursor == lastCursor {
			t.Fatalf("GetRegistries cursor did not advance (reverse=%v page=%d)", reverse, page)
		}
		lastCursor = cursor
		cursorKey = nextKey
	}
	t.Logf("  name lookup %q reverse=%v hit %d-page cap after %d rows",
		name, reverse, maxPages, scanned)
	return 0, false
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
	if info.MethodCount != 7 {
		t.Errorf("MethodCount = %d, want 7", info.MethodCount)
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
	assertIntegrationCreatorFormat(t, first.Creator, first.CreatorEVM)
	if first.CreatedAt == "" {
		t.Error("first registry created_at should not be empty")
	}
}

func TestIntegration_GetRegistry_ByID(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	reg, err := c.GetRegistry(ctx, anchor.GetRegistryRequest{ID: 1})
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

func TestIntegration_GetRecords_ByRegistryID(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// mcp-test-data is the stable registry seeded by cmd/seed-test-data;
	// it carries 3 records. Registries are no longer name-queryable
	// on-chain, so the id is resolved client-side first.
	regID := mcpTestDataRegistryID(t, c)
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		RegistryID: &regID,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(registry_id=%d): %v", regID, err)
	}
	if len(resp.Records) == 0 {
		t.Fatalf("expected at least one record in registry %d", regID)
	}

	rec := resp.Records[0]
	if rec.RegistryID != regID {
		t.Errorf("RegistryID = %d, want %d", rec.RegistryID, regID)
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

// TestIntegration_GetRecords_UnknownRegistryIDReturnsEmpty confirms an
// unknown registry_id returns an empty, non-error result: the precompile's
// records query prefix-walks within the given registry_id and a registry
// with no records (or no such registry) simply yields nothing, matching the
// keeper's Records handler (registry_id != 0, record_id == 0 => prefix walk,
// never a not-found error).
func TestIntegration_GetRecords_UnknownRegistryIDReturnsEmpty(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	badID := uint64(99999999)
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{RegistryID: &badID})
	if err != nil {
		t.Fatalf("GetRecords(registry_id=%d): unexpected error: %v", badID, err)
	}
	if len(resp.Records) != 0 {
		t.Errorf("expected 0 records for unknown registry_id, got %d", len(resp.Records))
	}
}

func TestIntegration_GetRecords_Pagination(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// mcp-test-data is the stable registry seeded by cmd/seed-test-data;
	// it carries 3 records -- enough to exercise offset/limit paging.
	regID := mcpTestDataRegistryID(t, c)

	// Confirm the registry has enough records to page through. The
	// nvnm-testnet-1 precompile returns pagination.total=0 even with
	// countTotal=true, so count the returned slice rather than trust Total.
	all, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		RegistryID: &regID,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(limit=10): %v", err)
	}
	if len(all.Records) < 2 {
		t.Fatalf("need >= 2 records in registry %d for pagination test, got %d", regID, len(all.Records))
	}

	// Page 1: first record only.
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		RegistryID: &regID,
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
		RegistryID: &regID,
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

// assertIntegrationCreatorFormat pins the two-representation creator
// contract (P1 / ADR 0001) against the live chain: `creator` is the
// chain-native bech32 identity and `creator_evm` its derived 0x form.
func assertIntegrationCreatorFormat(t *testing.T, creator, creatorEVM string) {
	t.Helper()
	if !strings.HasPrefix(creator, "nvnm1") {
		t.Errorf("creator = %q, want chain-native nvnm1 bech32", creator)
	}
	if creatorEVM == "" {
		t.Errorf("creator_evm missing for creator %q, want derived 0x address", creator)
		return
	}
	if !strings.HasPrefix(creatorEVM, "0x") || len(creatorEVM) != 42 {
		t.Errorf("creator_evm = %q, want 0x + 40 hex chars", creatorEVM)
	}
}
