package anchor

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

func goldenPath(name string) string {
	return filepath.Join("testdata", name+".golden.json")
}

func assertGolden(t *testing.T, name string, v interface{}) {
	t.Helper()

	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	got = append(got, '\n')

	path := goldenPath(name)

	if *update {
		if writeErr := os.WriteFile(path, got, 0o644); writeErr != nil {
			t.Fatalf("update golden %s: %v", path, writeErr)
		}
		t.Logf("updated %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("%s golden mismatch.\nGot:\n%s\nWant:\n%s\n"+
			"Run: go test ./internal/anchor/ -run %s -update",
			name, got, want, t.Name())
	}
}

func TestGolden_Registry(t *testing.T) {
	reg := Registry{
		ID:          42,
		Name:        "test-registry-uuid",
		Description: "A test registry for golden file validation",
		Creator:     "inveniam1abc123def456",
		CreatedAt:   "2026-03-01 12:00:00.000000000 +0000 UTC",
		Metadata:    "{\"env\":\"test\"}",
	}
	assertGolden(t, "registry", reg)
}

func TestGolden_Record(t *testing.T) {
	rec := Record{
		Registry:     "test-registry-uuid",
		RecordID:     7,
		Index:        1,
		Checksum:     "e4d5f79f1cfaf0cecd5f0e323a25fd08c1f64d0e1f8de349d75c77f29e51407d",
		ChecksumAlgo: "sha256",
		URI:          "https://qa8-api.inveniam.io/dataroom/2/103402411912619200",
		Status:       "Active",
		IsLatest:     true,
		Timestamp:    "2026-03-01 14:30:00.000000000 +0000 UTC",
		Metadata:     "{\"taxonomyId\":\"abc-123\",\"fileId\":\"def-456\"}",
	}
	assertGolden(t, "record", rec)
}

func TestGolden_GetRegistriesResponse(t *testing.T) {
	resp := GetRegistriesResponse{
		Registries: []Registry{
			{
				ID:          1,
				Name:        "29466bfd-8ec8-446c-9e7d-a1fe2f91e81f",
				Description: "29466bfd-8ec8-446c-9e7d-a1fe2f91e81f",
				Creator:     "inveniam12r28dewjcpzfnrkpshvx5rh4eve08685xyya3f",
				CreatedAt:   "2026-02-27 13:57:03.626641324 +0000 UTC",
			},
			{
				ID:          2,
				Name:        "1281d520-c91c-4d60-aa25-7f7b79fc4f80",
				Description: "1281d520-c91c-4d60-aa25-7f7b79fc4f80",
				Creator:     "inveniam12r28dewjcpzfnrkpshvx5rh4eve08685xyya3f",
				CreatedAt:   "2026-02-27 18:28:54.56053554 +0000 UTC",
			},
		},
		Pagination: &PageResponse{Total: 138},
	}
	assertGolden(t, "get_registries_response", resp)
}

func TestGolden_GetRecordsResponse(t *testing.T) {
	resp := GetRecordsResponse{
		Records: []Record{
			{
				Registry:     "29466bfd-8ec8-446c-9e7d-a1fe2f91e81f",
				RecordID:     1,
				Index:        1,
				Checksum:     "0xabc",
				ChecksumAlgo: "sha256",
				URI:          "ipfs://Qm123",
				Status:       "ACTIVE",
				IsLatest:     true,
				Timestamp:    "2026-02-27 14:00:25.684724306 +0000 UTC",
				Metadata:     "{\"key\": \"value\"}",
			},
		},
		Pagination: &PageResponse{Total: 1},
	}
	assertGolden(t, "get_records_response", resp)
}

func TestGolden_PrecompileInfo(t *testing.T) {
	info := PrecompileInfo{
		Address:     "0x0000000000000000000000000000000000000A00",
		ChainID:     58887,
		ABILoaded:   true,
		MethodCount: 5,
	}
	assertGolden(t, "precompile_info", info)
}

func TestGolden_EmptyResponses(t *testing.T) {
	resp := GetRecordsResponse{
		Records:    []Record{},
		Pagination: &PageResponse{Total: 0},
	}
	assertGolden(t, "empty_records_response", resp)
}

func TestGolden_UnsignedTransaction(t *testing.T) {
	tx := UnsignedTransaction{
		RawTx:    "0xf86c2a8501dcd650008301d4c0940000000000000000000000000000000000000a008080",
		To:       "0x0000000000000000000000000000000000000A00",
		Data:     "0xcafebabe01020304",
		Nonce:    42,
		Gas:      120000,
		GasPrice: "8000000000",
		Value:    "0",
		ChainID:  58887,
	}
	assertGolden(t, "unsigned_transaction", tx)
}
