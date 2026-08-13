// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import (
	"strings"

	apperrors "github.com/NVNM-Chain/nvnm-mcp-server/internal/errors"
)

// safePrecompileReverts maps a lowercase substring of a precompile revert
// reason to a canonical, client-safe message and the sentinel it classifies
// as. The chain reports revert reasons as opaque strings with no Go sentinel
// to match on, so recognizing the safe, actionable ones requires substring
// matching at this boundary -- the one place that knows the error came from
// the anchoring precompile.
//
// Two safety rules make this acceptable despite the project's general
// "errors.Is, not string matching" preference:
//  1. Matching is on EXTERNAL chain output, which carries no Go sentinel.
//  2. Only the curated `reason` is ever surfaced -- never the raw revert
//     string -- so internal type paths (e.g. Cosmos proto paths) cannot leak
//     even if they appear alongside a matched phrase.
//
// The sentinel matters as much as the message: it decides whether
// SafeForClient passes the reason through or collapses it. A reason mapped to
// the wrong sentinel is silently discarded, which is how an on-chain role
// denial used to reach callers as a bare "upstream operation failed" while
// the equivalent denial on the role tools arrived curated.
//
// Keep this list to reasons that are (a) safe to disclose, and (b) observed
// from the precompile (do not speculate).
var safePrecompileReverts = []struct {
	match  string
	reason string
	kind   error
}{
	{
		"metadata cannot be empty",
		"metadata cannot be empty",
		apperrors.ErrPrecompileValidation,
	},
	{
		"checksum exceeds max length",
		"checksum exceeds the maximum length allowed by the registry",
		apperrors.ErrPrecompileValidation,
	},
	{
		// Version indexes are 1-based on chain. A caller that omits the
		// optional `index` sends 0 and gets this, which reads as a server
		// fault unless the 1-based rule is stated.
		"index cannot be zero",
		"index must be 1 or greater -- record version indexes start at 1; " +
			"anchor_get_records reports the index of each version",
		apperrors.ErrPrecompileValidation,
	},
	{
		// On-chain role denial. Deliberately worded to distinguish it from
		// this server's own API-key RBAC denial, which is about the caller's
		// credential rather than the signing address's registry role.
		"unauthorized",
		"on-chain authorization failed: the `from` address does not hold the " +
			"role this write requires on the target registry (creating a " +
			"registry makes you its admin; an admin grants editor with " +
			"anchor_prepare_grant_role). This is a chain-side permission " +
			"check, not this server's API-key authorization",
		apperrors.ErrPermissionDenied,
	},
}

// classifyPrecompileRevert reports whether err's text contains a known, safe
// precompile reason and, if so, returns the canonical client-facing message
// and the sentinel to wrap it with. The returned reason is drawn solely from
// safePrecompileReverts, so raw chain detail never escapes through it.
func classifyPrecompileRevert(err error) (reason string, ok bool, kind error) {
	if err == nil {
		return "", false, nil
	}
	msg := strings.ToLower(err.Error())
	for _, e := range safePrecompileReverts {
		if strings.Contains(msg, e.match) {
			return e.reason, true, e.kind
		}
	}
	return "", false, nil
}
