//go:build integration

package anchor_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/inveniam/nvnm-mcp-server/internal/anchor"
	"github.com/inveniam/nvnm-mcp-server/internal/evm"
	"github.com/inveniam/nvnm-mcp-server/internal/logging"
	"github.com/inveniam/nvnm-mcp-server/internal/telemetry"
)

const (
	testRPCURL         = "https://evm.inveniam.mantrachain.io"
	testChainID        = int64(58887)
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
	if resp.Pagination.Total == 0 {
		t.Error("expected at least one registry on testnet")
	}
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

	// Registry 1 is known to have at least one record from our earlier survey
	name := "29466bfd-8ec8-446c-9e7d-a1fe2f91e81f"
	resp, err := c.GetRecords(ctx, anchor.GetRecordsRequest{
		Registry:   &name,
		Pagination: &anchor.PageRequest{Limit: 10},
	})
	if err != nil {
		t.Fatalf("GetRecords(registry=%q): %v", name, err)
	}
	if len(resp.Records) == 0 {
		t.Fatal("expected at least one record in registry 1")
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

func TestIntegration_GetRecords_Pagination(t *testing.T) {
	c := integrationClient(t)
	ctx := context.Background()

	// Registry 3 had 2 records, registry 13 had 3 -- use 13
	name := "b9d7f537-c84c-4dcd-a4be-c6f1253eca01"
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
	if resp.Pagination.Total < 2 {
		t.Skipf("expected total >= 2 for pagination test, got %d", resp.Pagination.Total)
	}

	// Fetch page 2
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
