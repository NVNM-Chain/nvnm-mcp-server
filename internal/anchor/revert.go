// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package anchor

import "strings"

// safePrecompileReverts maps a lowercase substring of a precompile revert
// reason to a canonical, client-safe message. The chain reports revert
// reasons as opaque strings with no Go sentinel to match on, so recognizing
// the safe, actionable ones requires substring matching at this boundary --
// the one place that knows the error came from the anchoring precompile.
//
// Two safety rules make this acceptable despite the project's general
// "errors.Is, not string matching" preference:
//  1. Matching is on EXTERNAL chain output, which carries no Go sentinel.
//  2. Only the curated `reason` is ever surfaced -- never the raw revert
//     string -- so internal type paths (e.g. Cosmos proto paths) cannot leak
//     even if they appear alongside a matched phrase.
//
// Keep this list to reasons that are (a) caller-input validation, (b) safe to
// disclose, and (c) observed from the precompile (do not speculate). Anything
// not listed falls through to the generic upstream-failure collapse.
var safePrecompileReverts = []struct{ match, reason string }{
	{"metadata cannot be empty", "metadata cannot be empty"},
	{"checksum exceeds max length", "checksum exceeds the maximum length allowed by the registry"},
}

// classifyPrecompileRevert reports whether err's text contains a known, safe
// precompile input-validation reason and, if so, returns the canonical
// client-facing message for it. The returned reason is drawn solely from
// safePrecompileReverts, so raw chain detail never escapes through it.
func classifyPrecompileRevert(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	msg := strings.ToLower(err.Error())
	for _, e := range safePrecompileReverts {
		if strings.Contains(msg, e.match) {
			return e.reason, true
		}
	}
	return "", false
}
