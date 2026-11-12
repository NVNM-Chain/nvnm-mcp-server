// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	defitypes "github.com/defiweb/go-eth/types"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
	"github.com/NVNM-Chain/nvnm-mcp-server/internal/logging"
)

// decodeByNameInput decodes the calldata GetRegistriesByName sent, so tests
// assert on the arguments the precompile actually receives.
func decodeByNameInput(t *testing.T, input []byte) (name string, mode uint8, page abiPaginationInput) {
	t.Helper()
	m := parsedTestABI(t).Methods["registriesByName"]
	if err := m.DecodeArgs(input, &name, &mode, &page); err != nil {
		t.Fatalf("decode registriesByName input: %v", err)
	}
	return name, mode, page
}

func TestGetRegistriesByName_Success(t *testing.T) {
	rows := sampleRegistryRows()
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRegistriesOutput(t, rows, abiPaginationOutput{}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	resp, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
		Name:       "Registry",
		Match:      RegistryNamePrefix,
		Pagination: &PageRequest{Offset: 3, Limit: 9},
	})
	if err != nil {
		t.Fatalf("GetRegistriesByName: %v", err)
	}
	if len(resp.Registries) != 2 || resp.Registries[0].Name != "registry-one" {
		t.Fatalf("registries = %+v", resp.Registries)
	}
	if resp.Registries[0].CreatorEVM == "" {
		t.Error("CreatorEVM not derived; rows must go through toRegistries")
	}

	if sel, _ := c.MethodSelector("registriesByName"); sel != "0x5522e6c6" {
		t.Errorf("selector = %s, want 0x5522e6c6 (what the chain dispatches)", sel)
	}
	name, mode, page := decodeByNameInput(t, gotInput)
	if name != "Registry" || mode != uint8(RegistryNamePrefix) {
		t.Errorf("args = (%q, %d), want (Registry, %d)", name, mode, RegistryNamePrefix)
	}
	if page.Offset != 3 || page.Limit != 9 || page.Reverse || len(page.Key) != 0 {
		t.Errorf("pagination = %+v, want offset=3 limit=9, no key, forward", page)
	}
}

func TestGetRegistriesByName_ForwardsCursor(t *testing.T) {
	cursor := []byte{0, 0, 0, 0, 0, 0, 0, 46}
	next := []byte{0, 0, 0, 0, 0, 0, 0, 93}
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRegistriesOutput(t, sampleRegistryRows()[:1], abiPaginationOutput{NextKey: next}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	resp, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
		Name: "a", Match: RegistryNamePrefix,
		Pagination: &PageRequest{Key: cursor, Limit: 2},
	})
	if err != nil {
		t.Fatalf("GetRegistriesByName: %v", err)
	}
	if resp.Pagination == nil || resp.Pagination.NextKey != EncodeCursor(next) || resp.Pagination.Total != 0 {
		t.Fatalf("pagination = %+v, want next_key of the chain cursor and total 0", resp.Pagination)
	}
	_, _, page := decodeByNameInput(t, gotInput)
	if !bytes.Equal(page.Key, cursor) || page.Limit != 2 || page.Offset != 0 {
		t.Errorf("pagination = %+v, want key forwarded, limit 2, offset 0", page)
	}
}

func TestGetRegistriesByName_DefaultLimit(t *testing.T) {
	var gotInput []byte
	mock := &mockEVMClient{
		callContractFn: func(_ context.Context, msg defitypes.Call, _ *big.Int) ([]byte, error) {
			gotInput = msg.Input
			return encodeRegistriesOutput(t, []abiRegistryRow{}, abiPaginationOutput{}), nil
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	resp, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
		Name: "x", Match: RegistryNameExact,
	})
	if err != nil {
		t.Fatalf("GetRegistriesByName: %v", err)
	}
	if len(resp.Registries) != 0 {
		t.Errorf("registries = %+v, want none", resp.Registries)
	}
	_, mode, page := decodeByNameInput(t, gotInput)
	if mode != uint8(RegistryNameExact) || page.Limit != 100 || page.Offset != 0 {
		t.Errorf("mode=%d pagination=%+v, want exact, limit=100 offset=0", mode, page)
	}
}

// TestGetRegistriesByName_RejectsBeforeCall proves inputs the chain would
// reject are refused locally, with no RPC.
func TestGetRegistriesByName_RejectsBeforeCall(t *testing.T) {
	tests := []struct {
		name string
		req  GetRegistriesByNameRequest
		want error
	}{
		{"empty name", GetRegistriesByNameRequest{Match: RegistryNameExact}, apperrors.ErrMissingRequired},
		{"zero match", GetRegistriesByNameRequest{Name: "x"}, apperrors.ErrInvalidMatchMode},
		{"unknown match", GetRegistriesByNameRequest{Name: "x", Match: 9}, apperrors.ErrInvalidMatchMode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEVMClient{
				callContractFn: func(context.Context, defitypes.Call, *big.Int) ([]byte, error) {
					t.Fatal("chain called for a locally invalid request")
					return nil, nil
				},
			}
			c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
			if _, err := c.GetRegistriesByName(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestGetRegistriesByName_UnsupportedNode proves a precompile that predates
// registriesByName -- observed live on testnet 787111 as "unknown method id:
// 1428350662" -- reports the dedicated sentinel without leaking node text.
func TestGetRegistriesByName_UnsupportedNode(t *testing.T) {
	mock := &mockEVMClient{
		callContractFn: func(context.Context, defitypes.Call, *big.Int) ([]byte, error) {
			return nil, errors.New("RPC error: -32000 rpc error: code = Internal desc = unknown method id: 1428350662")
		},
	}
	c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))

	_, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
		Name: "x", Match: RegistryNameExact,
	})
	if !errors.Is(err, apperrors.ErrRegistriesByNameUnsupported) {
		t.Fatalf("err = %v, want ErrRegistriesByNameUnsupported", err)
	}
	if strings.Contains(err.Error(), "1428350662") {
		t.Errorf("error leaks raw node text: %v", err)
	}
	if !errors.Is(apperrors.SafeForClient(err), apperrors.ErrRegistriesByNameUnsupported) {
		t.Error("SafeForClient collapsed the unsupported-node error")
	}
}

// TestGetRegistriesByName_ExactOnlyChain proves a name index that serves
// exact match only -- observed live on chain 787222 for match modes 2, 3
// and 4 -- reports the dedicated sentinel. It is not the "method missing"
// case: the lookup exists, so the caller must not be told to give up on
// name lookups, and it must not collapse to "upstream operation failed".
func TestGetRegistriesByName_ExactOnlyChain(t *testing.T) {
	const raw = "RPC error: 3 execution reverted: only exact match is on chain; " +
		"search prefix/suffix/contains off chain"
	mock := &mockEVMClient{
		callContractFn: func(context.Context, defitypes.Call, *big.Int) ([]byte, error) {
			return nil, errors.New(raw)
		},
	}
	c := NewClient(mock, PrecompileAddress, 787222, testABIPath(t), logging.New("error"))

	for _, mode := range []RegistryNameMatch{RegistryNamePrefix, RegistryNameSuffix, RegistryNameContains} {
		_, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
			Name: "tempo", Match: mode,
		})
		if !errors.Is(err, apperrors.ErrMatchModeUnsupported) {
			t.Fatalf("mode %d: err = %v, want ErrMatchModeUnsupported", mode, err)
		}
		if errors.Is(err, apperrors.ErrRegistriesByNameUnsupported) {
			t.Errorf("mode %d: reported as a missing method; exact match still works", mode)
		}
		safe := apperrors.SafeForClient(err)
		if !errors.Is(safe, apperrors.ErrMatchModeUnsupported) {
			t.Errorf("mode %d: SafeForClient collapsed the error to %q", mode, safe)
		}
		if strings.Contains(safe.Error(), "execution reverted") || strings.Contains(safe.Error(), "RPC error") {
			t.Errorf("mode %d: client message leaks raw node text: %q", mode, safe)
		}
		if !strings.Contains(safe.Error(), "match=exact") {
			t.Errorf("mode %d: client message must name the mode that works: %q", mode, safe)
		}
	}
}

func TestGetRegistriesByName_CallAndDecodeErrors(t *testing.T) {
	tests := []struct {
		name   string
		output []byte
		err    error
		want   string
	}{
		{"call error", nil, errors.New("dial tcp: connection refused"), "registriesByName call failed"},
		{"malformed response", []byte{0xde, 0xad}, nil, "unpack registriesByName response"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mock := &mockEVMClient{
				callContractFn: func(context.Context, defitypes.Call, *big.Int) ([]byte, error) {
					return tc.output, tc.err
				},
			}
			c := NewClient(mock, PrecompileAddress, 58887, testABIPath(t), logging.New("error"))
			_, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
				Name: "x", Match: RegistryNameExact,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestGetRegistriesByName_NoABI(t *testing.T) {
	c := NewClient(&mockEVMClient{}, PrecompileAddress, 58887, "", logging.New("error"))
	_, err := c.GetRegistriesByName(context.Background(), GetRegistriesByNameRequest{
		Name: "x", Match: RegistryNameExact,
	})
	if !errors.Is(err, apperrors.ErrAnchorABIMissing) {
		t.Fatalf("err = %v, want ErrAnchorABIMissing", err)
	}
}
