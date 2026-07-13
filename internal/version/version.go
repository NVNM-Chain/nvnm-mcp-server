// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package version

// Version is the server version string, injected at build time. All packages
// reference this single variable so the reported version is atomic.
//
// Release builds set it from the git tag:
//
//	go build -ldflags "-X github.com/NVNM-Chain/nvnm-mcp-server/internal/version.Version=1.2.3"
//
// The fallback is deliberately "dev", NOT a plausible release number. A
// hardcoded version here would be a value that should be DERIVED but is instead
// RESTATED -- and it drifts: this fallback previously read "1.0.0-rc10" while
// the Dockerfile omitted the -X flag entirely, so every container image from
// rc11 onward confidently reported itself as rc10 and nothing looked broken.
// "dev" makes a missing injection obvious the first time anyone looks.
var Version = "dev"
