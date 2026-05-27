// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "github.com/NVNM-Chain/nvnm-mcp-server/internal/config"

// RuntimeInfo bundles the operator-config values surfaced by the
// onboarding tools (lobby, wizard, wallet_status) and any tool that
// needs to render an env-aware token name. It is constructed once at
// server startup and passed by value to tool registrations so handlers
// do not reach into *config.Config directly.
type RuntimeInfo struct {
	ChainEnvironment config.ChainEnvironment
	ChainID          int64
	AnchorAddress    string
	DocsURL          string
	ExplorerURL      string
	BridgeURL        string
}

// RuntimeInfoFromConfig copies the relevant fields from a fully-loaded
// Config into a RuntimeInfo bundle. The result is a value, not a
// pointer; callers pass it by value to handler constructors.
func RuntimeInfoFromConfig(cfg *config.Config) RuntimeInfo {
	return RuntimeInfo{
		ChainEnvironment: cfg.ChainEnvironment,
		ChainID:          cfg.ChainID,
		AnchorAddress:    cfg.AnchorAddress,
		DocsURL:          cfg.DocsURL,
		ExplorerURL:      cfg.ExplorerURL,
		BridgeURL:        cfg.BridgeURL,
	}
}

// TokenNaming returns the customer-facing token symbols for the active
// environment. Convenience method so tool handlers do not have to
// import the config package just to render a balance string.
//
// RuntimeInfo is a small config-style struct; value receiver matches the
// pass-by-value convention used at call sites.
//
//nolint:gocritic // hugeParam: see comment above
func (r RuntimeInfo) TokenNaming() config.TokenNaming {
	return config.NamingFor(r.ChainEnvironment)
}
