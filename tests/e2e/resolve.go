// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	defiabi "github.com/defiweb/go-eth/abi"
)

// createLookupWindow is how far back from the latest registry ID we
// search after a create. Concurrent suites may insert a few IDs after
// ours; names are unique per run so a match in this window is ours.
const createLookupWindow = 32

var (
	errNoRepoRoot          = errors.New("cannot locate repo root (go.mod) to load abi/anchoring.json")
	errNoRegistriesMethod  = errors.New("ABI has no registries method")
	errEmptyRegistriesPeek = errors.New("registries reverse peek returned no rows")
)

type peekPageReq struct {
	Key        []byte `abi:"key"`
	Offset     uint64 `abi:"offset"`
	Limit      uint64 `abi:"limit"`
	CountTotal bool   `abi:"countTotal"`
	Reverse    bool   `abi:"reverse"`
}

type peekRegistryRow struct {
	ID          uint64 `abi:"id"`
	Name        string `abi:"name"`
	Description string `abi:"description"`
	Creator     string `abi:"creator"`
	CreatedAt   string `abi:"createdAt"`
	Metadata    string `abi:"metadata"`
}

type peekPageOut struct {
	NextKey []byte `abi:"nextKey"`
	Total   uint64 `abi:"total"`
}

var (
	anchorABIOnce sync.Once
	anchorABI     *defiabi.Contract
	errAnchorABI  error
)

func loadAnchorABI(t *testing.T) *defiabi.Contract {
	t.Helper()
	anchorABIOnce.Do(func() {
		root := RepoRoot()
		if root == "" {
			errAnchorABI = errNoRepoRoot
			return
		}
		path := filepath.Join(root, "abi", "anchoring.json")
		data, err := os.ReadFile(path) //nolint:gosec // repo-relative ABI path, not caller input
		if err != nil {
			errAnchorABI = fmt.Errorf("read %s: %w", path, err)
			return
		}
		parsed, err := defiabi.ParseJSON(data)
		if err != nil {
			errAnchorABI = fmt.Errorf("parse %s: %w", path, err)
			return
		}
		if _, ok := parsed.Methods["registries"]; !ok {
			errAnchorABI = fmt.Errorf("%s: %w", path, errNoRegistriesMethod)
			return
		}
		anchorABI = parsed
	})
	if errAnchorABI != nil {
		t.Fatalf("anchor ABI: %v", errAnchorABI)
	}
	return anchorABI
}

func encodeReverseRegistriesPeek(contract *defiabi.Contract) ([]byte, error) {
	m, ok := contract.Methods["registries"]
	if !ok || m == nil {
		return nil, errNoRegistriesMethod
	}
	return m.EncodeArgs(uint64(0), peekPageReq{
		Key:        []byte{},
		Limit:      1,
		CountTotal: true,
		Reverse:    true,
	})
}

func decodeRegistriesPeek(contract *defiabi.Contract, output []byte) (id uint64, name string, err error) {
	m, ok := contract.Methods["registries"]
	if !ok || m == nil {
		return 0, "", errNoRegistriesMethod
	}
	var rows []peekRegistryRow
	var page peekPageOut
	if decErr := m.DecodeValues(output, &rows, &page); decErr != nil {
		return 0, "", fmt.Errorf("decode registries peek: %w", decErr)
	}
	if len(rows) == 0 {
		return 0, "", errEmptyRegistriesPeek
	}
	return rows[0].ID, rows[0].Name, nil
}

// ResolveCreatedRegistry finds the registry just created by this run when
// a by-name listing missed it (scan hit its page cap). It peeks the
// highest ID with a reverse registries eth_call, then confirms via
// anchor_get_registry.
func (f *Flow) ResolveCreatedRegistry(t *testing.T, wantName string, minedBlock uint64) RegistryResponse {
	t.Helper()

	latestID, peekedName := f.PeekLatestRegistryID(t)
	t.Logf("  reverse peek: latest registry id=%d name=%q (create mined in block %d)",
		latestID, peekedName, minedBlock)

	start := latestID
	for i := uint64(0); i < createLookupWindow && start > i; i++ {
		id := start - i
		result := f.Call(t, "anchor_get_registry", map[string]any{"id": id})
		if result.IsError {
			continue
		}
		var byID RegistryResponse
		DecodeStructured(t, "anchor_get_registry", result, &byID)
		if byID.Name != wantName {
			continue
		}
		if byID.ContentTrust == "" {
			t.Fatal("anchor_get_registry content_trust empty")
		}
		AssertRegistryContract(t, byID.Registry, f.Address)
		return byID
	}

	t.Fatalf("after creating %q, latest registry id is %d but none of the last %d ids has that name",
		wantName, latestID, createLookupWindow)
	return RegistryResponse{}
}

func (f *Flow) PeekLatestRegistryID(t *testing.T) (id uint64, name string) {
	t.Helper()

	contract := loadAnchorABI(t)
	calldata, err := encodeReverseRegistriesPeek(contract)
	if err != nil {
		t.Fatalf("encode registries reverse peek: %v", err)
	}

	var out struct {
		Result string `json:"result"`
	}
	f.CallOK(t, "evm_call_contract", map[string]any{
		"to":   f.AnchorAddress,
		"data": "0x" + hex.EncodeToString(calldata),
	}, &out)
	raw, err := hex.DecodeString(strings.TrimPrefix(out.Result, "0x"))
	if err != nil {
		t.Fatalf("evm_call_contract result is not hex: %q: %v", out.Result, err)
	}
	id, name, err = decodeRegistriesPeek(contract, raw)
	if err != nil {
		t.Fatalf("decode registries reverse peek: %v", err)
	}
	if id == 0 {
		t.Fatal("registries reverse peek returned id 0")
	}
	return id, name
}
