// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"errors"
	"strings"
	"testing"
)

func TestBoundAuditErr(t *testing.T) {
	if got := boundAuditErr(nil); got != "" {
		t.Errorf("boundAuditErr(nil) = %q, want empty", got)
	}

	short := errors.New("send transaction: nonce too low")
	if got := boundAuditErr(short); got != short.Error() {
		t.Errorf("short error was altered: %q", got)
	}

	// A hostile/MITM'd node could return an arbitrarily large error body; it
	// must be bounded before it reaches the audit column or log sink (LG-2).
	huge := errors.New(strings.Repeat("A", 5000))
	got := boundAuditErr(huge)
	if len(got) > maxAuditErrLen+len("...[truncated]") {
		t.Errorf("boundAuditErr did not cap length: got %d chars", len(got))
	}
	if !strings.HasSuffix(got, "...[truncated]") {
		t.Errorf("truncated error missing marker: %q", got[len(got)-32:])
	}
}
