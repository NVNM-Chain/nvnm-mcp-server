// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// anchor_get_registries -- the registry listing, in both of its modes:
// unfiltered paging, and by-name lookup.
//
// Neither mode is a plain precompile passthrough. The chain has no
// by-name index and always reports pagination.total=0, so the server
// scans the table itself and substitutes its own count. That scan is
// worth exercising against a real table rather than a mock, which is why
// this phase runs against the fixture registry created by this run.

func phaseGetRegistries(t *testing.T, f *flow) {
	// Mode 1: unfiltered listing, paged.
	var listing registriesResponse
	f.callOK(t, "anchor_get_registries", map[string]any{"limit": 5}, &listing)

	if len(listing.Registries) > 5 {
		t.Errorf("limit=5 returned %d registries", len(listing.Registries))
	}
	if listing.ContentTrust == "" {
		t.Error("content_trust is empty; on-chain names and metadata are untrusted input and must be labelled as such")
	}
	var total uint64
	if listing.Pagination != nil {
		total = listing.Pagination.Total
	}
	t.Logf("listing returned %d registries (scanned total=%d lower_bound=%v)",
		len(listing.Registries), total, listing.TotalIsLowerBound)

	// Mode 2: exact name match. The fixture registry must be findable by
	// the name it was created with -- setupRegistry already depends on
	// this, but here it is the assertion rather than the mechanism.
	var exact registriesResponse
	f.callOK(t, "anchor_get_registries", map[string]any{
		"name":  f.registryName,
		"match": "exact",
		"limit": 10,
	}, &exact)

	var found *registry
	for i := range exact.Registries {
		if exact.Registries[i].Name == f.registryName {
			found = &exact.Registries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("exact-match lookup did not return registry %q (%d returned, truncated=%v)",
			f.registryName, len(exact.Registries), exact.TotalIsLowerBound)
	}
	if found.ID != f.registryID {
		t.Errorf("exact match returned id=%d, want %d", found.ID, f.registryID)
	}
	for _, reg := range exact.Registries {
		if reg.Name != f.registryName {
			t.Errorf("exact match on %q also returned %q", f.registryName, reg.Name)
		}
	}

	// Mode 3: prefix match. The fixture name starts with a fixed prefix
	// shared by every run of this suite, so a prefix search must find at
	// least the one this run created.
	const prefix = "mcp-e2e-"
	var byPrefix registriesResponse
	f.callOK(t, "anchor_get_registries", map[string]any{
		"name":  prefix,
		"match": "prefix",
		"limit": 50,
	}, &byPrefix)

	for _, reg := range byPrefix.Registries {
		// The documented match is case-insensitive, so compare that way
		// rather than assuming every registry was named in lower case.
		if !strings.HasPrefix(strings.ToLower(reg.Name), prefix) {
			t.Errorf("prefix search for %q returned %q", prefix, reg.Name)
		}
	}
	t.Logf("prefix %q matched %d registries", prefix, len(byPrefix.Registries))
}
