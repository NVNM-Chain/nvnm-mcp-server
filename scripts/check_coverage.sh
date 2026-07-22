#!/usr/bin/env bash
# scripts/check_coverage.sh
#
# Fails if total statement coverage in a Go coverage profile is below
# the required threshold. CI invokes this after the test step; locally
# it runs as part of `make coverage-check`.
#
# Usage:
#   scripts/check_coverage.sh [profile] [threshold]
#     profile    coverage profile path (default: coverage.out)
#     threshold  minimum total coverage percent (default: 80)
#
# The threshold can also be set via COVERAGE_THRESHOLD.

set -euo pipefail

PROFILE="${1:-coverage.out}"
THRESHOLD="${2:-${COVERAGE_THRESHOLD:-80}}"

if [[ ! -f "$PROFILE" ]]; then
    echo "ERROR: coverage profile '$PROFILE' not found." >&2
    echo "Run: go test -race -coverprofile=$PROFILE ./..." >&2
    exit 1
fi

total=$(go tool cover -func="$PROFILE" | awk '/^total:/ {sub(/%/, "", $NF); print $NF}')

if [[ -z "$total" ]]; then
    echo "ERROR: could not parse total coverage from '$PROFILE'." >&2
    exit 1
fi

echo "Total statement coverage: ${total}% (threshold: ${THRESHOLD}%)"

# awk handles the float comparison; bash arithmetic is integer-only.
if awk -v t="$total" -v min="$THRESHOLD" 'BEGIN { exit !(t < min) }'; then
    echo "FAIL: total coverage ${total}% is below the required ${THRESHOLD}%." >&2
    echo "Add tests for the uncovered code before merging. To see gaps:" >&2
    echo "  go tool cover -func=$PROFILE | grep -v '100.0%' | sort -t$'\t' -k3 -n" >&2
    echo "  go tool cover -html=$PROFILE -o coverage.html" >&2
    exit 1
fi

echo "OK: coverage meets the ${THRESHOLD}% threshold."
