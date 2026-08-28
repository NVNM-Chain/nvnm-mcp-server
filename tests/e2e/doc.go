// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

//go:build e2e

// Package e2e is the harness for the deployment hot-path test.
//
// TestE2E_HotPath_AnchorDocument (package e2e_test) walks prepare → sign
// → broadcast → confirm → read back against NVNM_MCP_TEST_SERVER_URL, or
// an in-process server with real chain clients when that URL is unset.
// Signing is local: the server never receives a private key.
//
// Tool-level regression belongs in internal/mcp
// (TestMCP_Tools), not here.
package e2e
