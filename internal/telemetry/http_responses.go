// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

// Phase 10 RD3/RD4 (docs/PHASE_10_DESIGN.md § 14): HTTP response counter with a
// `class` label so operators can alert on the SLI ratio at rule-evaluation
// time without recomputing. The four classes intentionally collapse the full
// status-code space into the categories operators actually page on:
//
//   - server_fault    — 5xx; "the server broke", the canonical error-rate SLI
//   - customer_impact — 429 (overload) and 408 (server-timed-out request);
//                       not the server's "fault" but the customer experiences
//                       failure, so they belong in the customer-impact SLI
//   - client_error    — other 4xx; the client sent bad input, not a server
//                       reliability signal — useful as a side metric, NOT in
//                       the error-rate alert numerator
//   - success         — 2xx, 3xx, and 1xx for completeness
//
// Keep this enum in sync with deploy/prometheus/alerts.yaml; the alert PromQL
// references these label values verbatim.

// Response class label values for the mcp_http_responses_total counter.
const (
	// ResponseClassServerFault is the class label for 5xx responses — the
	// canonical "the server broke" error-rate SLI numerator.
	ResponseClassServerFault = "server_fault"
	// ResponseClassCustomerImpact is the class label for 429 (overload) and
	// 408 (request timeout) — the customer experiences failure even though
	// the server did not fault in the traditional 5xx sense.
	ResponseClassCustomerImpact = "customer_impact"
	// ResponseClassClientError is the class label for other 4xx responses —
	// bad input from the caller, not a server reliability signal.
	ResponseClassClientError = "client_error"
	// ResponseClassSuccess is the class label for 1xx/2xx/3xx responses.
	ResponseClassSuccess = "success"
)

// ClassifyStatus maps an HTTP status code to one of the four SLI class label
// values used on the mcp_http_responses_total counter.
//
// Status codes outside the documented HTTP range (< 100 or >= 600) are
// classified as ResponseClassServerFault on the principle that anything we
// cannot categorize is more likely an internal bug than a routine response —
// fail loud, surface it on the error SLI rather than silently bucketing it as
// success. This matches the project preference for fail-fast over silent
// defensive defaults (see ~/.claude/CLAUDE.md).
func ClassifyStatus(status int) string {
	switch {
	case status >= 500 && status < 600:
		return ResponseClassServerFault
	case status == 429 || status == 408:
		return ResponseClassCustomerImpact
	case status >= 400 && status < 500:
		return ResponseClassClientError
	case status >= 100 && status < 400:
		return ResponseClassSuccess
	default:
		// Out-of-range codes (negative, zero, >= 600) — treat as server
		// fault so the anomaly surfaces on the error-rate SLI.
		return ResponseClassServerFault
	}
}
