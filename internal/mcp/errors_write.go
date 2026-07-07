// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import "errors"

// ErrSignerBlacklisted is returned when the recovered signer of a keyless
// write is on the per-signer ban list (SignerBlacklistStore). Input class:
// surfaced verbatim to the caller.
var ErrSignerBlacklisted = errors.New("signer is blacklisted")

// ErrSignerQuotaExceeded is returned when the recovered signer of a keyless
// write has reached SignerWriteRate broadcasts within the current
// SignerWriteWindow (SignerQuotaStore). Input class: surfaced verbatim to
// the caller.
var ErrSignerQuotaExceeded = errors.New("per-signer write quota exceeded")
