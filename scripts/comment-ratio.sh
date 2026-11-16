#!/usr/bin/env bash
# comment-ratio.sh [dir...]: the share of non-blank lines that are //
# comments, directives excluded, in non-generated Go files. The first
# line counts non-test files, the number primitives/PLAN.md step 2 tracks
# (under 8% when the sweep is done). The second counts _test.go files, so
# a rule swept out of production code cannot come back as prose above a
# test unseen. With no argument it counts the whole repository; testdata
# and vendor trees are skipped.
set -euo pipefail

[ "$#" -gt 0 ] || set -- .
files=$(mktemp); trap 'rm -f "$files"' EXIT

count() {
  local label="$1"; shift
  find "${dirs[@]}" -name '*.go' "$@" \
    ! -path '*/testdata/*' ! -path '*/vendor/*' ! -path '*/node_modules/*' -print0 |
    xargs -0 -r grep -L -E '^// Code generated .* DO NOT EDIT\.$|^//gofastr:generated' >"$files" || true
  tr '\n' '\0' <"$files" | xargs -0 -r cat | awk -v label="$label" '
      { line = $0; sub(/^[ \t]+/, "", line) }
      line == "" { next }
      line ~ /^\/\/(go:|gofastr:|nolint|line |export )/ || line ~ /^\/\/ ?\+build/ { code++; next }
      line ~ /^\/\// { comment++; next }
      { code++ }
      END {
        total = comment + code
        if (total == 0) { printf "%s: no Go lines\n", label; exit }
        printf "%s: %d comment lines of %d (%.1f%%)\n", label, comment, total, 100 * comment / total
      }'
}

dirs=("$@")
count code ! -name '*_test.go'
count tests -name '*_test.go'
