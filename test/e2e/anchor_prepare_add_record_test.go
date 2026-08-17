// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// anchor_prepare_add_record -- anchoring a document checksum and URI.
//
// setupRecord already proved this tool produces a transaction that
// lands. What this phase covers is the output contract and, more
// interestingly, the input rules the precompile enforces that the tool
// schema calls optional.
//
// Three of those rules are load-bearing and were each found the hard way
// on a real chain: checksum_algo and metadata must be non-empty even
// though the schema marks them optional, and the literal empty JSON
// object "{}" is rejected as metadata -- older guidance told callers to
// pass exactly that. The client validates all three itself so the caller
// gets a precise message instead of an opaque revert from gas estimation.

func phasePrepareAddRecord(t *testing.T, f *flow) {
	sum := sha256.Sum256([]byte("nvnm mcp e2e contract check " + uniqueSuffix()))
	checksum := hex.EncodeToString(sum[:])

	utx := f.prepare(t, "anchor_prepare_add_record", map[string]any{
		"from":          f.address,
		"registry_id":   f.registryID,
		"uri":           "https://example.invalid/nvnm-e2e/never-broadcast",
		"checksum":      checksum,
		"checksum_algo": "sha256",
		"metadata":      `{"suite":"test/e2e"}`,
	})
	assertUnsignedTxShape(t, f, utx)

	// A 0x-prefixed digest denotes the same bytes as the bare form, and
	// the client strips the prefix so both work. The precompile caps the
	// checksum at 64 chars, so an unstripped 0x + 64 hex would be
	// rejected on chain -- meaning identical calldata here is the whole
	// point.
	prefixed := f.prepare(t, "anchor_prepare_add_record", map[string]any{
		"from":          f.address,
		"registry_id":   f.registryID,
		"uri":           "https://example.invalid/nvnm-e2e/never-broadcast",
		"checksum":      "0x" + checksum,
		"checksum_algo": "sha256",
		"metadata":      `{"suite":"test/e2e"}`,
	})
	if prefixed.Data != utx.Data {
		t.Error("a 0x-prefixed checksum produced different calldata than the bare form; " +
			"the two denote the same digest and must normalize to the same call")
	}

	// Rejections, all validated before any broadcast.
	base := func() map[string]any {
		return map[string]any{
			"from":          f.address,
			"registry_id":   f.registryID,
			"uri":           "https://example.invalid/nvnm-e2e/rejected",
			"checksum":      checksum,
			"checksum_algo": "sha256",
			"metadata":      `{"suite":"test/e2e"}`,
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
		want   string // a distinctive fragment the message should carry
	}{
		{"registry_id 0", func(a map[string]any) { a["registry_id"] = 0 }, "registry_id"},
		{"no checksum", func(a map[string]any) { delete(a, "checksum") }, "checksum"},
		{"no checksum_algo", func(a map[string]any) { delete(a, "checksum_algo") }, "checksum_algo"},
		{"no metadata", func(a map[string]any) { delete(a, "metadata") }, "metadata"},
		{"empty JSON object metadata", func(a map[string]any) { a["metadata"] = "{}" }, "metadata"},
		{"padded empty JSON object", func(a map[string]any) { a["metadata"] = "  {}  " }, "metadata"},
	} {
		args := base()
		tc.mutate(args)

		got, errText := f.tryPrepare(t, "anchor_prepare_add_record", args)
		if got != nil {
			t.Errorf("%s: the tool returned a transaction instead of rejecting the call", tc.name)
			continue
		}
		// The message is the deliverable here: an agent has to be able to
		// fix its own call from it, which an opaque "upstream operation
		// failed" does not allow.
		if !strings.Contains(strings.ToLower(errText), tc.want) {
			t.Errorf("%s: rejected with %q, which does not mention %q -- a caller cannot tell "+
				"which field to fix", tc.name, errText, tc.want)
			continue
		}
		t.Logf("%s: rejected -- %s", tc.name, errText)
	}
}
