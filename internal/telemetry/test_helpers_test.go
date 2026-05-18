// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import (
	"io"
	"log/slog"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
