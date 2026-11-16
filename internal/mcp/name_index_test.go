// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"strings"
	"testing"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/anchor"
	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// --- anchor_get_registry: registry_id, with id as the deprecated alias ---

func TestGetRegistry_RegistryIDAndAlias(t *testing.T) {
	handler := makeGetRegistryHandler(&mockAnchor{registry: &anchor.Registry{ID: 42, Name: "by-id"}})

	ok := []struct {
		name  string
		input getRegistryInput
	}{
		{"registry_id", getRegistryInput{RegistryID: u64p(42)}},
		{"deprecated id", getRegistryInput{ID: u64p(42)}},
		{"both, agreeing", getRegistryInput{RegistryID: u64p(42), ID: u64p(42)}},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := handler(ctx, nil, tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.ID != 42 {
				t.Errorf("id = %d, want 42", out.ID)
			}
		})
	}

	bad := []struct {
		name    string
		input   getRegistryInput
		want    error
		mention string
	}{
		{"neither", getRegistryInput{}, apperrors.ErrMissingRequired, "registry_id"},
		{"registry_id zero", getRegistryInput{RegistryID: u64p(0)}, apperrors.ErrInvalidRegistryID, "1 or greater"},
		{"id zero", getRegistryInput{ID: u64p(0)}, apperrors.ErrInvalidRegistryID, "1 or greater"},
		{
			"both, disagreeing", getRegistryInput{RegistryID: u64p(1), ID: u64p(2)},
			apperrors.ErrInvalidRegistryID, "different registries",
		},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := handler(ctx, nil, tc.input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.mention) {
				t.Errorf("error %q must mention %q", err, tc.mention)
			}
			if safe := apperrors.SafeForClient(err); safe.Error() != err.Error() {
				t.Errorf("client would see %q, not the reason", safe)
			}
		})
	}
}

// --- name-filter cursor on a chain whose name index ignores it ---

// TestGetRegistries_ByName_IgnoredCursorIsRejected reproduces chain 787222:
// registriesByName drops pagination.key, so a keyed request returns the
// first match again. Two registries named tempo-e2e (IDs 2184, 2185): the
// key for 2185 came back with 2184.
func TestGetRegistries_ByName_IgnoredCursorIsRejected(t *testing.T) {
	regs := []anchor.Registry{{ID: 2184, Name: "tempo-e2e"}, {ID: 2185, Name: "tempo-e2e"}}
	m := &mockAnchor{}
	m.byNameFn = func(req anchor.GetRegistriesByNameRequest) (*anchor.GetRegistriesResponse, error) {
		noKey := req
		noKey.Pagination = &anchor.PageRequest{Offset: req.Pagination.Offset, Limit: req.Pagination.Limit}
		resp := chainByName(regs, noKey)
		resp.Pagination.NextKey = "" // this name index never returns a cursor
		return resp, nil
	}
	handler := makeGetRegistriesHandler(m)
	name := "tempo-e2e"
	limit := uint64(1)

	_, _, err := handler(ctx, nil, getRegistriesInput{Name: &name, Limit: &limit, Key: chainCursor(2185)})
	if !errors.Is(err, apperrors.ErrNameCursorUnsupported) {
		t.Fatalf("err = %v, want ErrNameCursorUnsupported", err)
	}
	if safe := apperrors.SafeForClient(err); !strings.Contains(safe.Error(), "offset") {
		t.Errorf("client message must point at offset paging: %q", safe)
	}
	if len(m.byNameReqs) != 1 {
		t.Errorf("by-name calls = %d, want 1 (no retry, no offset restart)", len(m.byNameReqs))
	}

	// Offset paging is what works there, and its stop rules are unchanged.
	want := [][]uint64{{2184}, {2185}, {}}
	for offset, ids := range want {
		off := uint64(offset)
		_, out, err := handler(ctx, nil, getRegistriesInput{Name: &name, Limit: &limit, Offset: &off})
		if err != nil {
			t.Fatalf("offset %d: %v", offset, err)
		}
		if len(out.Registries) != len(ids) {
			t.Fatalf("offset %d: %d rows, want %d", offset, len(out.Registries), len(ids))
		}
		for i, id := range ids {
			if out.Registries[i].ID != id {
				t.Errorf("offset %d row %d: id %d, want %d", offset, i, out.Registries[i].ID, id)
			}
		}
		if out.Pagination == nil || out.Pagination.NextKey != "" {
			t.Errorf("offset %d: pagination = %+v, want no next_key", offset, out.Pagination)
		}
	}
}

func TestCursorIgnored(t *testing.T) {
	key := func(id uint64) []byte {
		raw, err := (&anchor.PageResponse{NextKey: chainCursor(id)}).CursorBytes()
		if err != nil {
			t.Fatalf("cursor bytes: %v", err)
		}
		return raw
	}
	rows := func(ids ...uint64) []anchor.Registry {
		out := make([]anchor.Registry, len(ids))
		for i, id := range ids {
			out[i] = anchor.Registry{ID: id}
		}
		return out
	}
	tests := []struct {
		name   string
		cursor []byte
		rows   []anchor.Registry
		want   bool
	}{
		{"page starts at the cursor", key(46), rows(46, 84), false},
		{"page starts after the cursor (non-matching ids skipped)", key(46), rows(50), false},
		{"page starts before the cursor", key(2185), rows(2184), true},
		{"empty page says nothing", key(2185), nil, false},
		{"no cursor", nil, rows(1), false},
		{"wrong-length cursor is not this check's job", []byte{1, 2}, rows(1), false},
	}
	for _, tt := range tests {
		if got := cursorIgnored(tt.cursor, tt.rows); got != tt.want {
			t.Errorf("%s: cursorIgnored = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestGetRegistries_ByName_UnsupportedMatchModeSurfaces proves the exact-only
// rejection reaches the caller as a reason, not "upstream operation failed".
func TestGetRegistries_ByName_UnsupportedMatchModeSurfaces(t *testing.T) {
	m := &mockAnchor{returnErr: apperrors.ErrMatchModeUnsupported}
	name := "tempo"
	_, _, err := makeGetRegistriesHandler(m)(ctx, nil, getRegistriesInput{Name: &name, Match: "prefix"})
	if !errors.Is(err, apperrors.ErrMatchModeUnsupported) {
		t.Fatalf("err = %v, want ErrMatchModeUnsupported", err)
	}
	if safe := apperrors.SafeForClient(err).Error(); safe == "upstream operation failed" {
		t.Error("match-mode rejection collapsed to the generic upstream failure")
	}
}
