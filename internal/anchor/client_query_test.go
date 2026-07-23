// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	defiabi "github.com/defiweb/go-eth/abi"
	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// parsedTestABI loads the checked-in anchoring ABI for constructing valid
// ABI-encoded responses in tests. The encoding is the exact inverse of the
// DecodeValues call in the production code, so the decode paths
// (toRegistries / toRecords) are genuinely exercised.
func parsedTestABI(t *testing.T) *defiabi.Contract {
	t.Helper()
	parsed, err := loadABI(testABIPath(t))
	if err != nil {
		t.Fatalf("load test ABI: %v", err)
	}
	return parsed
}

// encodeRegistriesOutput ABI-encodes a `registries` view return payload.
func encodeRegistriesOutput(t *testing.T, rows []abiRegistryRow, page abiPaginationOutput) []byte {
	t.Helper()
	m := parsedTestABI(t).Methods["registries"]
	out, err := defiabi.EncodeValues(m.Outputs(), rows, page)
	if err != nil {
		t.Fatalf("encode registries output: %v", err)
	}
	return out
}

// encodeRecordsOutput ABI-encodes a `records` view return payload.
func encodeRecordsOutput(t *testing.T, rows []abiRecordRow, page abiPaginationOutput) []byte {
	t.Helper()
	m := parsedTestABI(t).Methods["records"]
	out, err := defiabi.EncodeValues(m.Outputs(), rows, page)
	if err != nil {
		t.Fatalf("encode records output: %v", err)
	}
	return out
}

// writeTempABI writes an ABI JSON document to a temp file and returns its path.
func writeTempABI(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "abi.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sampleRegistryRows() []abiRegistryRow {
	return []abiRegistryRow{
		{
			ID:          1,
			Name:        "registry-one",
			Description: "first registry",
			Creator:     "inveniam1abc",
			CreatedAt:   "2026-03-01 12:00:00.000000000 +0000 UTC",
			Metadata:    "{\"env\":\"test\"}",
		},
		{
			ID:          2,
			Name:        "registry-two",
			Description: "second registry",
			Creator:     "inveniam1def",
			CreatedAt:   "2026-03-02 12:00:00.000000000 +0000 UTC",
			Metadata:    "",
		},
	}
}

func sampleRecordRows() []abiRecordRow {
	return []abiRecordRow{
		{
			Registry:     "registry-one",
			URI:          "ipfs://Qm123",
			Checksum:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			ChecksumAlgo: "sha256",
			Metadata:     "{\"file\":\"a.pdf\"}",
			Timestamp:    "2026-03-01 14:30:00.000000000 +0000 UTC",
			Status:       "Active",
			RecordID:     7,
			Index:        1,
			IsLatest:     true,
		},
		{
			Registry:     "registry-one",
			URI:          "ipfs://Qm456",
			Checksum:     "aa0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b8bb",
			ChecksumAlgo: "sha256",
			Metadata:     "{\"file\":\"b.pdf\"}",
			Timestamp:    "2026-03-02 14:30:00.000000000 +0000 UTC",
			Status:       "Superseded",
			RecordID:     7,
			Index:        0,
			IsLatest:     false,
		},
	}
}

func TestMethodSelector_NoABI(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, "", logging.New("error"))
	if sel, ok := c.MethodSelector("grantRole"); ok || sel != "" {
		t.Errorf("MethodSelector without ABI = (%q, %v), want (\"\", false)", sel, ok)
	}
}

func TestMethodSelector_UnknownMethod(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
	if sel, ok := c.MethodSelector("noSuchMethod"); ok || sel != "" {
		t.Errorf("MethodSelector for unknown method = (%q, %v), want (\"\", false)", sel, ok)
	}
}

func TestMethodSelector_KnownMethods(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
	parsed := parsedTestABI(t)

	seen := map[string]string{}
	for _, name := range []string{"addRecord", "addRegistry", "grantRole", "records", "registries"} {
		sel, ok := c.MethodSelector(name)
		if !ok {
			t.Fatalf("MethodSelector(%q) not found", name)
		}
		if len(sel) != 10 || !strings.HasPrefix(sel, "0x") {
			t.Errorf("MethodSelector(%q) = %q, want 0x-prefixed 4-byte selector", name, sel)
		}
		if prev, dup := seen[sel]; dup {
			t.Errorf("selector %q collides between %q and %q", sel, prev, name)
		}
		seen[sel] = name

		fb := parsed.Methods[name].FourBytes()
		want := "0x" + hex.EncodeToString(fb[:])
		if sel != want {
			t.Errorf("MethodSelector(%q) = %q, want %q", name, sel, want)
		}
	}
}

func TestGetRegistry_Success(t *testing.T) {
	rows := sampleRegistryRows()[:1]
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			if msg.To == nil || !strings.EqualFold(msg.To.String(), PrecompileAddress) {
				t.Errorf("call target = %v, want %s", msg.To, PrecompileAddress)
			}
			return encodeRegistriesOutput(t, rows, abiPaginationOutput{Total: 1}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	id := uint64(1)
	reg, err := c.GetRegistry(context.Background(), GetRegistryRequest{ID: &id})
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}

	want := Registry{
		ID:          1,
		Name:        "registry-one",
		Description: "first registry",
		Creator:     "inveniam1abc",
		CreatedAt:   "2026-03-01 12:00:00.000000000 +0000 UTC",
		Metadata:    "{\"env\":\"test\"}",
	}
	if *reg != want {
		t.Errorf("registry = %+v, want %+v", *reg, want)
	}

	// The single-registry lookup must request exactly one row.
	var reqID uint64
	var reqName string
	var reqPage abiPaginationInput
	m := parsedTestABI(t).Methods["registries"]
	if decErr := m.DecodeArgs(gotInput, &reqID, &reqName, &reqPage); decErr != nil {
		t.Fatalf("decode call input: %v", decErr)
	}
	if reqID != 1 || reqName != "" {
		t.Errorf("filter = (id=%d, name=%q), want (1, \"\")", reqID, reqName)
	}
	if reqPage.Limit != 1 || reqPage.CountTotal {
		t.Errorf("pagination = %+v, want limit=1 countTotal=false", reqPage)
	}
}

func TestGetRegistry_ByName(t *testing.T) {
	rows := sampleRegistryRows()[1:]
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRegistriesOutput(t, rows, abiPaginationOutput{Total: 1}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	name := "registry-two"
	reg, err := c.GetRegistry(context.Background(), GetRegistryRequest{Name: &name})
	if err != nil {
		t.Fatalf("GetRegistry: %v", err)
	}
	if reg.Name != "registry-two" || reg.ID != 2 {
		t.Errorf("registry = %+v", *reg)
	}

	var reqID uint64
	var reqName string
	var reqPage abiPaginationInput
	m := parsedTestABI(t).Methods["registries"]
	if decErr := m.DecodeArgs(gotInput, &reqID, &reqName, &reqPage); decErr != nil {
		t.Fatalf("decode call input: %v", decErr)
	}
	if reqID != 0 || reqName != "registry-two" {
		t.Errorf("filter = (id=%d, name=%q), want (0, registry-two)", reqID, reqName)
	}
}

func TestGetRegistry_EmptyResultIsNotFound(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return encodeRegistriesOutput(t, []abiRegistryRow{}, abiPaginationOutput{Total: 0}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	id := uint64(999)
	_, err := c.GetRegistry(context.Background(), GetRegistryRequest{ID: &id})
	if !errors.Is(err, apperrors.ErrRegistryNotFound) {
		t.Fatalf("want ErrRegistryNotFound for empty result, got %v", err)
	}
}

// TestGetRegistry_CallError: a CallContract failure whose text does not
// contain "not found" is passed through wrapped, not mapped to the
// registry-not-found sentinel.
func TestGetRegistry_CallError(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	id := uint64(1)
	_, err := c.GetRegistry(context.Background(), GetRegistryRequest{ID: &id})
	if err == nil || !strings.Contains(err.Error(), "registries call failed") {
		t.Fatalf("want wrapped call failure, got %v", err)
	}
	if errors.Is(err, apperrors.ErrRegistryNotFound) {
		t.Errorf("generic RPC failure must not map to ErrRegistryNotFound: %v", err)
	}
}

func TestGetRegistry_MalformedResponse(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return []byte{0x01, 0x02, 0x03}, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	id := uint64(1)
	_, err := c.GetRegistry(context.Background(), GetRegistryRequest{ID: &id})
	if err == nil || !strings.Contains(err.Error(), "unpack registries response") {
		t.Fatalf("want unpack error, got %v", err)
	}
}

func TestGetRegistries_Success(t *testing.T) {
	rows := sampleRegistryRows()
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRegistriesOutput(t, rows, abiPaginationOutput{Total: 138}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	resp, err := c.GetRegistries(context.Background(), GetRegistriesRequest{})
	if err != nil {
		t.Fatalf("GetRegistries: %v", err)
	}
	if len(resp.Registries) != 2 {
		t.Fatalf("got %d registries, want 2", len(resp.Registries))
	}
	if resp.Registries[0].Name != "registry-one" || resp.Registries[1].Name != "registry-two" {
		t.Errorf("registries = %+v", resp.Registries)
	}
	if resp.Registries[1].Metadata != "" {
		t.Errorf("Metadata = %q, want empty", resp.Registries[1].Metadata)
	}
	if resp.Pagination == nil || resp.Pagination.Total != 138 {
		t.Errorf("Pagination = %+v, want Total=138", resp.Pagination)
	}

	// Defaults: limit 100, offset 0, countTotal true.
	var reqID uint64
	var reqName string
	var reqPage abiPaginationInput
	m := parsedTestABI(t).Methods["registries"]
	if decErr := m.DecodeArgs(gotInput, &reqID, &reqName, &reqPage); decErr != nil {
		t.Fatalf("decode call input: %v", decErr)
	}
	if reqPage.Limit != 100 || reqPage.Offset != 0 || !reqPage.CountTotal {
		t.Errorf("pagination = %+v, want limit=100 offset=0 countTotal=true", reqPage)
	}
}

func TestGetRegistries_PaginationAndFiltersForwarded(t *testing.T) {
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRegistriesOutput(t, []abiRegistryRow{}, abiPaginationOutput{Total: 0}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	regID := uint64(42)
	name := "filter-name"
	resp, err := c.GetRegistries(context.Background(), GetRegistriesRequest{
		RegistryID: &regID,
		Name:       &name,
		Pagination: &PageRequest{Offset: 5, Limit: 7},
	})
	if err != nil {
		t.Fatalf("GetRegistries: %v", err)
	}
	if len(resp.Registries) != 0 {
		t.Errorf("got %d registries, want 0", len(resp.Registries))
	}
	if resp.Pagination == nil || resp.Pagination.Total != 0 {
		t.Errorf("Pagination = %+v, want Total=0", resp.Pagination)
	}

	var reqID uint64
	var reqName string
	var reqPage abiPaginationInput
	m := parsedTestABI(t).Methods["registries"]
	if decErr := m.DecodeArgs(gotInput, &reqID, &reqName, &reqPage); decErr != nil {
		t.Fatalf("decode call input: %v", decErr)
	}
	if reqID != 42 || reqName != "filter-name" {
		t.Errorf("filter = (id=%d, name=%q), want (42, filter-name)", reqID, reqName)
	}
	if reqPage.Offset != 5 || reqPage.Limit != 7 {
		t.Errorf("pagination = %+v, want offset=5 limit=7", reqPage)
	}
}

func TestGetRegistries_NotFoundMapsToSentinel(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return nil, errors.New(
				"RPC error: -32000 rpc error: code = Internal desc = collections: " +
					"not found: key '999' of type github.com/cosmos/gogoproto/" +
					"mantrachain.anchoring.v1.Registry")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	regID := uint64(999)
	_, err := c.GetRegistries(context.Background(), GetRegistriesRequest{RegistryID: &regID})
	if !errors.Is(err, apperrors.ErrRegistryNotFound) {
		t.Fatalf("want ErrRegistryNotFound, got %v", err)
	}
	if strings.Contains(err.Error(), "gogoproto") {
		t.Errorf("error leaks internal proto type path: %v", err)
	}
}

func TestGetRegistries_CallError(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.GetRegistries(context.Background(), GetRegistriesRequest{})
	if err == nil || !strings.Contains(err.Error(), "registries call failed") {
		t.Fatalf("want wrapped call failure, got %v", err)
	}
}

func TestGetRegistries_MalformedResponse(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return []byte{0xde, 0xad}, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.GetRegistries(context.Background(), GetRegistriesRequest{})
	if err == nil || !strings.Contains(err.Error(), "unpack registries response") {
		t.Fatalf("want unpack error, got %v", err)
	}
}

func TestGetRecords_Success(t *testing.T) {
	rows := sampleRecordRows()
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRecordsOutput(t, rows, abiPaginationOutput{Total: 2}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	registry := "registry-one"
	resp, err := c.GetRecords(context.Background(), GetRecordsRequest{Registry: &registry})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(resp.Records) != 2 {
		t.Fatalf("got %d records, want 2", len(resp.Records))
	}

	want := Record{
		Registry:     "registry-one",
		RecordID:     7,
		Index:        1,
		Checksum:     "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		ChecksumAlgo: "sha256",
		URI:          "ipfs://Qm123",
		Status:       "Active",
		IsLatest:     true,
		Timestamp:    "2026-03-01 14:30:00.000000000 +0000 UTC",
		Metadata:     "{\"file\":\"a.pdf\"}",
	}
	if resp.Records[0] != want {
		t.Errorf("record[0] = %+v, want %+v", resp.Records[0], want)
	}
	if resp.Records[1].IsLatest || resp.Records[1].Status != "Superseded" {
		t.Errorf("record[1] = %+v", resp.Records[1])
	}
	if resp.Pagination == nil || resp.Pagination.Total != 2 {
		t.Errorf("Pagination = %+v, want Total=2", resp.Pagination)
	}

	var reqRegistry, reqChecksum string
	var reqRecordID, reqIndex uint64
	var reqPage abiPaginationInput
	m := parsedTestABI(t).Methods["records"]
	if decErr := m.DecodeArgs(gotInput, &reqRegistry, &reqChecksum, &reqRecordID, &reqIndex, &reqPage); decErr != nil {
		t.Fatalf("decode call input: %v", decErr)
	}
	if reqRegistry != "registry-one" {
		t.Errorf("registry filter = %q, want registry-one", reqRegistry)
	}
	if reqPage.Limit != 100 || !reqPage.CountTotal {
		t.Errorf("pagination = %+v, want limit=100 countTotal=true", reqPage)
	}
}

func TestGetRecords_FiltersAndPaginationForwarded(t *testing.T) {
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRecordsOutput(t, []abiRecordRow{}, abiPaginationOutput{Total: 0}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	registry := "reg"
	checksum := "abc123"
	recordID := uint64(9)
	index := uint64(3)
	resp, err := c.GetRecords(context.Background(), GetRecordsRequest{
		Registry:   &registry,
		Checksum:   &checksum,
		RecordID:   &recordID,
		Index:      &index,
		Pagination: &PageRequest{Offset: 11, Limit: 13},
	})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(resp.Records) != 0 {
		t.Errorf("got %d records, want 0", len(resp.Records))
	}

	var reqRegistry, reqChecksum string
	var reqRecordID, reqIndex uint64
	var reqPage abiPaginationInput
	m := parsedTestABI(t).Methods["records"]
	if decErr := m.DecodeArgs(gotInput, &reqRegistry, &reqChecksum, &reqRecordID, &reqIndex, &reqPage); decErr != nil {
		t.Fatalf("decode call input: %v", decErr)
	}
	if reqRegistry != "reg" || reqChecksum != "abc123" || reqRecordID != 9 || reqIndex != 3 {
		t.Errorf("filters = (%q, %q, %d, %d)", reqRegistry, reqChecksum, reqRecordID, reqIndex)
	}
	if reqPage.Offset != 11 || reqPage.Limit != 13 {
		t.Errorf("pagination = %+v, want offset=11 limit=13", reqPage)
	}
}

// TestGetRecords_ResolvesRegistryIDToName verifies the registry_id
// convenience filter: the id is first resolved to a registry name via the
// registries view, and the records query is keyed by that name.
func TestGetRecords_ResolvesRegistryIDToName(t *testing.T) {
	parsed := parsedTestABI(t)
	registriesFB := parsed.Methods["registries"].FourBytes()
	recordsFB := parsed.Methods["records"].FourBytes()

	var recordsInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			switch {
			case len(msg.Input) >= 4 && registriesFB.Match(msg.Input[:4]):
				rows := []abiRegistryRow{{ID: 5, Name: "resolved-name"}}
				return encodeRegistriesOutput(t, rows, abiPaginationOutput{Total: 1}), nil
			case len(msg.Input) >= 4 && recordsFB.Match(msg.Input[:4]):
				recordsInput = msg.Input
				return encodeRecordsOutput(t, sampleRecordRows()[:1], abiPaginationOutput{Total: 1}), nil
			default:
				return nil, errors.New("unexpected call")
			}
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	regID := uint64(5)
	resp, err := c.GetRecords(context.Background(), GetRecordsRequest{RegistryID: &regID})
	if err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Fatalf("got %d records, want 1", len(resp.Records))
	}

	var reqRegistry, reqChecksum string
	var reqRecordID, reqIndex uint64
	var reqPage abiPaginationInput
	m := parsed.Methods["records"]
	if decErr := m.DecodeArgs(recordsInput, &reqRegistry, &reqChecksum, &reqRecordID, &reqIndex, &reqPage); decErr != nil {
		t.Fatalf("decode records call input: %v", decErr)
	}
	if reqRegistry != "resolved-name" {
		t.Errorf("records query keyed by %q, want resolved-name", reqRegistry)
	}
}

// TestGetRecords_ExplicitNameWinsOverRegistryID: when both a name and an id
// are given, the name is used directly and no registries lookup happens.
func TestGetRecords_ExplicitNameWinsOverRegistryID(t *testing.T) {
	parsed := parsedTestABI(t)
	registriesFB := parsed.Methods["registries"].FourBytes()

	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			if len(msg.Input) >= 4 && registriesFB.Match(msg.Input[:4]) {
				t.Error("registries lookup must not happen when a registry name is given")
			}
			return encodeRecordsOutput(t, []abiRecordRow{}, abiPaginationOutput{Total: 0}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	regID := uint64(5)
	name := "explicit-name"
	if _, err := c.GetRecords(context.Background(), GetRecordsRequest{
		RegistryID: &regID,
		Registry:   &name,
	}); err != nil {
		t.Fatalf("GetRecords: %v", err)
	}
}

func TestGetRecords_RegistryIDResolutionFails(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return nil, errors.New("collections: not found: key '404'")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	regID := uint64(404)
	_, err := c.GetRecords(context.Background(), GetRecordsRequest{RegistryID: &regID})
	if err == nil {
		t.Fatal("expected error when registry_id cannot be resolved")
	}
	if !strings.Contains(err.Error(), "resolve registry_id 404") {
		t.Errorf("error = %v, want resolve registry_id context", err)
	}
	if !errors.Is(err, apperrors.ErrRegistryNotFound) {
		t.Errorf("error must wrap ErrRegistryNotFound, got %v", err)
	}
}

func TestGetRecords_CallError(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	registry := "reg"
	_, err := c.GetRecords(context.Background(), GetRecordsRequest{Registry: &registry})
	if err == nil || !strings.Contains(err.Error(), "records call failed") {
		t.Fatalf("want wrapped call failure, got %v", err)
	}
}

func TestGetRecords_MalformedResponse(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, _ defitypes.Call, _ *big.Int) ([]byte, error) {
			return []byte{0xba, 0xad}, nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	registry := "reg"
	_, err := c.GetRecords(context.Background(), GetRecordsRequest{Registry: &registry})
	if err == nil || !strings.Contains(err.Error(), "unpack records response") {
		t.Fatalf("want unpack error, got %v", err)
	}
}

// TestCallPrecompile_MethodMissing exercises the guard for an ABI that
// parses but lacks the required view method.
func TestCallPrecompile_MethodMissing(t *testing.T) {
	abiPath := writeTempABI(t, `[
	  {"type":"function","name":"addRegistry","stateMutability":"nonpayable",
	   "inputs":[{"name":"name","type":"string"}],"outputs":[]}
	]`)
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, abiPath, logging.New("error"))
	if !c.Available() {
		t.Fatal("client should be available with a parseable ABI")
	}

	// GetRecords surfaces the sentinel unmapped.
	registry := "reg"
	_, err := c.GetRecords(context.Background(), GetRecordsRequest{Registry: &registry})
	if !errors.Is(err, apperrors.ErrAnchorABIMethodMissing) {
		t.Fatalf("want ErrAnchorABIMethodMissing, got %v", err)
	}

	// GetRegistries funnels the same failure through its "not found" text
	// mapping: the ABI-method-missing message contains "not found", so it is
	// reported as ErrRegistryNotFound. Lock in the current behavior.
	_, err = c.GetRegistries(context.Background(), GetRegistriesRequest{})
	if !errors.Is(err, apperrors.ErrRegistryNotFound) {
		t.Fatalf("want ErrRegistryNotFound from GetRegistries text mapping, got %v", err)
	}
}

// TestCallPrecompile_PackError exercises the EncodeArgs failure branch via
// an ABI whose registries/records methods take a different argument list
// than the client passes.
func TestCallPrecompile_PackError(t *testing.T) {
	abiPath := writeTempABI(t, `[
	  {"type":"function","name":"registries","stateMutability":"view",
	   "inputs":[{"name":"registryId","type":"uint64"}],"outputs":[]},
	  {"type":"function","name":"records","stateMutability":"view",
	   "inputs":[{"name":"registry","type":"string"}],"outputs":[]}
	]`)
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, abiPath, logging.New("error"))

	_, err := c.GetRegistries(context.Background(), GetRegistriesRequest{})
	if err == nil || !strings.Contains(err.Error(), "failed to pack registries call") {
		t.Fatalf("want pack error for registries, got %v", err)
	}

	registry := "reg"
	_, err = c.GetRecords(context.Background(), GetRecordsRequest{Registry: &registry})
	if err == nil || !strings.Contains(err.Error(), "failed to pack records call") {
		t.Fatalf("want pack error for records, got %v", err)
	}
}
