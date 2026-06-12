// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package version

// Version is the canonical server version string. All packages reference this
// single constant so bumps are atomic.
//
// Override at build time with:
//
//	go build -ldflags "-X github.com/NVNM-Chain/nvnm-mcp-server/internal/version.Version=1.2.3"
var Version = "1.0.0-rc6"
