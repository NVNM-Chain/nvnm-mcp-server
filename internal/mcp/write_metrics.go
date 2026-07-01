// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"

	"github.com/NVNM-Chain/nvnm-mcp-server/internal/telemetry"
)

// WriteMetrics records bounded detection counters for the broadcast write
// path. Every label value is a fixed enum set by server code -- never a
// signer, address, or other caller-derived string -- which keeps the
// Prometheus /metrics surface bounded in cardinality and free of addresses
// (see .local/SLICE_4D_DETECTION_METRICS_DESIGN.md, sections 2-3). A nil
// WriteMetrics is a valid no-op.
type WriteMetrics interface {
	// RecordBroadcast counts a broadcast attempt that passed relay scope.
	// outcome is "ok" or "failed".
	RecordBroadcast(ctx context.Context, outcome string)
	// RecordRelayReject counts a pre-broadcast rejection. cause is
	// "relay_scope", "decode", or "anchor_misconfig".
	RecordRelayReject(ctx context.Context, cause string)
}

// Compile-time guard: the concrete telemetry recorder satisfies WriteMetrics
// so the two never drift.
var _ WriteMetrics = (*telemetry.Metrics)(nil)
