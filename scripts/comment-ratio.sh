#!/usr/bin/env bash
# comment-ratio.sh [dir...]: the share of non-blank lines in non-test,
# non-generated Go files that are // comments, directives excluded.
# primitives/PLAN.md step 2 tracks it: under 8% when the sweep is done.
# With no argument it counts the whole repository; testdata and vendor
# trees are skipped.
set -euo pipefail

[ "$#" -gt 0 ] || set -- .
files=$(mktemp); trap 'rm -f "$files"' EXIT
find "$@" -name '*.go' ! -name '*_test.go' \
  ! -path '*/testdata/*' ! -path '*/vendor/*' ! -path '*/node_modules/*' -print0 |
  xargs -0 grep -L -E '^// Code generated .* DO NOT EDIT\.$|^//gofastr:generated' >"$files" || true
tr '\n' '\0' <"$files" | xargs -0 cat | awk '
    { line = $0; sub(/^[ \t]+/, "", line) }
    line == "" { next }
    line ~ /^\/\/(go:|gofastr:|nolint|line |export )/ || line ~ /^\/\/ ?\+build/ { code++; next }
    line ~ /^\/\// { comment++; next }
    { code++ }
    END {
      total = comment + code
      if (total == 0) { print "no Go lines"; exit }
      printf "%d comment lines of %d (%.1f%%)\n", comment, total, 100 * comment / total
    }'
