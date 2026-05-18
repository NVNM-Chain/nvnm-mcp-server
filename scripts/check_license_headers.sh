#!/usr/bin/env bash
# scripts/check_license_headers.sh
#
# Fails if any .go file under cmd/ or internal/ is missing the
# required SPDX license header on its first line. CI invokes this
# script to enforce that new files include the header.
#
# Fix: run scripts/add_license_headers.sh, then commit.

set -euo pipefail

REQUIRED="// SPDX-License-Identifier: Apache-2.0"
missing=()
total=0

while IFS= read -r -d '' file; do
    total=$((total + 1))
    first_line=$(head -n1 "$file")
    if [[ "$first_line" != "$REQUIRED" ]]; then
        missing+=("$file")
    fi
done < <(find cmd internal -name '*.go' -not -path '*/vendor/*' -print0)

if [[ ${#missing[@]} -gt 0 ]]; then
    echo "ERROR: ${#missing[@]} of $total .go file(s) under cmd/ or internal/ are missing the SPDX header:"
    printf '  %s\n' "${missing[@]}"
    echo ""
    echo "Expected first line:"
    echo "  $REQUIRED"
    echo ""
    echo "Fix: run scripts/add_license_headers.sh and commit."
    exit 1
fi

echo "SPDX header check: OK ($total file(s) verified)."
