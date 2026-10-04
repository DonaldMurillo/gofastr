#!/bin/bash
# Duplicate-code gate (issue #417): counts dupl clone groups across the
# repo's Go source (test files included) and fails when the count GROWS
# past the committed baseline in scripts/dupl-baseline.txt. dupl is
# pinned to an exact version so a release that changes its syntax-tree
# matching cannot silently move the number.
#
# Excluded from the scan: examples/ and evals/ (demo and evaluation
# trees), testdata/ fixtures, *.gen.go generated output, .claude/,
# dist/ build artifacts, vendor/.
#
# Usage:
#   ./scripts/dupl.sh                # gate: fail when the count grew
#   ./scripts/dupl.sh --rebaseline   # write the measured count to the
#                                    # baseline; commit it with the change
#                                    # that moved the number
set -euo pipefail

cd "$(dirname "$0")/.."

DUPL_VERSION=v1.1.0
BASELINE_FILE=scripts/dupl-baseline.txt
THRESHOLD=100

rebaseline=0
case "${1:-}" in
  "") ;;
  --rebaseline) rebaseline=1 ;;
  *) echo "usage: $0 [--rebaseline]" >&2; exit 2 ;;
esac

# Tracked Go files only, deterministically ordered, minus the excluded
# trees. git ls-files keeps the list identical across checkouts; sort
# guards against git ever changing its ordering.
files=$(git ls-files '*.go' |
  grep -Ev '(^|/)(examples|evals|testdata|\.claude|dist|vendor)/|\.gen\.go$' |
  sort)
if [ -z "$files" ]; then
  echo "dupl: no Go files to scan" >&2
  exit 1
fi

nfiles=$(printf '%s\n' "$files" | wc -l | tr -d ' ')
echo "==> dupl -t $THRESHOLD over $nfiles Go files"
out=$(go run github.com/mibk/dupl@"$DUPL_VERSION" -t "$THRESHOLD" $files)

# dupl ends with a summary line: "Found total N clone groups."
count=$(printf '%s\n' "$out" | sed -n 's/^Found total \([0-9][0-9]*\) clone groups\.$/\1/p')
if [ -z "$count" ]; then
  echo "dupl: could not parse a clone-group count from dupl's output:" >&2
  printf '%s\n' "$out" | tail -5 >&2
  exit 1
fi

if [ "$rebaseline" = "1" ]; then
  printf '%s\n' "$count" > "$BASELINE_FILE"
  echo "dupl: baseline written: $count clone groups ($BASELINE_FILE)"
  exit 0
fi

if [ ! -f "$BASELINE_FILE" ]; then
  echo "dupl: missing baseline $BASELINE_FILE" >&2
  echo "run scripts/dupl.sh --rebaseline and commit the file it writes" >&2
  exit 1
fi
baseline=$(cat "$BASELINE_FILE")

if [ "$count" -gt "$baseline" ]; then
  echo "FAIL: dupl clone groups grew: $count > baseline $baseline" >&2
  echo "" >&2
  printf '%s\n' "$out" >&2
  echo "" >&2
  echo "deduplicate the new clone, or accept it deliberately:" >&2
  echo "  scripts/dupl.sh --rebaseline  (and commit $BASELINE_FILE with the change)" >&2
  exit 1
fi
echo "dupl: $count clone groups (baseline $baseline, threshold -t $THRESHOLD)"
