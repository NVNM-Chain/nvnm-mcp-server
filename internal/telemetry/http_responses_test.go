// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package telemetry

import "testing"

// TestClassifyStatus pins the four-class mapping defined in Phase 10 RD3/RD4
// (docs/PHASE_10_DESIGN.md § 14). Boundaries matter — the alert rules in
// deploy/prometheus/alerts.yaml partition the status-code space by these
// labels, so a mis-categorized boundary (e.g. 408 as client_error) would
// silently move customer-impact responses off the customer-impact SLI.
func TestClassifyStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   string
	}{
		// Success boundaries.
		{name: "100 informational is success", status: 100, want: ResponseClassSuccess},
		{name: "200 OK is success", status: 200, want: ResponseClassSuccess},
		{name: "299 success upper boundary", status: 299, want: ResponseClassSuccess},
		{name: "300 redirect is success", status: 300, want: ResponseClassSuccess},
		{name: "399 redirect upper boundary", status: 399, want: ResponseClassSuccess},

		// 4xx — client_error vs customer_impact split.
		{name: "400 Bad Request is client_error", status: 400, want: ResponseClassClientError},
		{name: "407 just below 408 is client_error", status: 407, want: ResponseClassClientError},
		{name: "408 Request Timeout is customer_impact", status: 408, want: ResponseClassCustomerImpact},
		{name: "409 just above 408 is client_error", status: 409, want: ResponseClassClientError},
		{name: "428 just below 429 is client_error", status: 428, want: ResponseClassClientError},
		{name: "429 Too Many Requests is customer_impact", status: 429, want: ResponseClassCustomerImpact},
		{name: "430 just above 429 is client_error", status: 430, want: ResponseClassClientError},
		{name: "499 client_error upper boundary", status: 499, want: ResponseClassClientError},

		// 5xx — server_fault.
		{name: "500 Internal Server Error is server_fault", status: 500, want: ResponseClassServerFault},
		{name: "599 server_fault upper boundary", status: 599, want: ResponseClassServerFault},

		// Out-of-range fail-loud.
		{name: "0 unset status is server_fault", status: 0, want: ResponseClassServerFault},
		{name: "negative status is server_fault", status: -1, want: ResponseClassServerFault},
		{name: "99 below HTTP range is server_fault", status: 99, want: ResponseClassServerFault},
		{name: "600 above HTTP range is server_fault", status: 600, want: ResponseClassServerFault},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyStatus(tc.status)
			if got != tc.want {
				t.Errorf("ClassifyStatus(%d) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}
