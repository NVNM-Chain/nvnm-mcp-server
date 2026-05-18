#!/usr/bin/env bash
# scripts/add_license_headers.sh
#
# Idempotently prepend the SPDX license header to every .go file under
# cmd/ and internal/. Skips vendor/. A file that already has the
# expected SPDX line as its first line is left alone, so re-running
# this script is safe.
#
# Phase 9.3 (OSS Readiness): see docs/IMPLEMENTATION_PLAN.md.
# The commit hash of the bulk-header commit is recorded in
# .git-blame-ignore-revs so `git blame` skips the rewrite.

set -euo pipefail

HEADER_LINE1="// SPDX-License-Identifier: Apache-2.0"
HEADER_LINE2="// Copyright 2026 Inveniam Capital Partners"

added=0
skipped=0

while IFS= read -r -d '' file; do
    first_line=$(head -n1 "$file")
    if [[ "$first_line" == "$HEADER_LINE1" ]]; then
        skipped=$((skipped + 1))
        continue
    fi
    tmp=$(mktemp)
    {
        printf '%s\n' "$HEADER_LINE1"
        printf '%s\n' "$HEADER_LINE2"
        printf '\n'
        cat "$file"
    } > "$tmp"
    mv "$tmp" "$file"
    added=$((added + 1))
done < <(find cmd internal -name '*.go' -not -path '*/vendor/*' -print0)

echo "SPDX headers: added to $added file(s); $skipped already had the header."
