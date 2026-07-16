#!/bin/bash
# Enforce coverage floors for the audited packages so their coverage
# can't drift silently. This script is the single source of truth for
# the floors.
#
# Methodology: own-package coverage only (`go test -cover ./<pkg>/ -count=1`),
# the cheapest reproducible measurement. The audited packages sit below a
# literal 100% by design: defensive fail-closed guards that are unreachable
# today are kept (not rewritten to chase the number), and CLI serve-loops /
# interactive entry points + a few OS-IO fault branches are accepted as
# untestable without real listeners or stdin.
#
# Each floor is set ~2 points below the value measured on 2026-06-10. The
# slack absorbs ordinary churn (a refactor that adds a handful of uncovered
# defensive lines shouldn't block CI) while still catching real regressions —
# a deleted test file or a newly untested feature moves coverage far more
# than 2 points. If you intentionally change a package's coverage profile,
# re-measure and update the floor here in the
# same commit.
#
# Exclusions:
#   cmd/gofastr (claimed 84% full-suite) is NOT gated here: its suite is
#   dominated by slow, environment-sensitive e2e tests (subprocess hot-reload
#   servers, chromedp, an installed `gofastr` binary on PATH, ~7 min) that
#   the CI test/browser-e2e jobs already run — re-running them for a floor
#   check would double the longest part of the blocking job and import their
#   flake surface into this gate.
#
# Usage:
#   ./scripts/coverage-floors.sh

set -euo pipefail

cd "$(dirname "$0")/.."

# pkg<space>floor.
FLOORS="
./core/migrate/ 98.0
./core/schema/ 97.0
./framework/ 95.5
./framework/crud/ 96.9
./framework/entity/ 86.5
./framework/migrate/ 73.5
./framework/tenant/ 85.5
"

fail=0
while read -r pkg floor; do
  [ -z "$pkg" ] && continue
  if ! out=$(go test -cover -count=1 "$pkg" 2>&1); then
    echo "$out"
    echo "FAIL  $pkg — tests failed (no coverage measurement)"
    fail=1
    continue
  fi
  cov=$(echo "$out" | grep -Eo 'coverage: [0-9.]+%' | tail -1 | grep -Eo '[0-9.]+')
  if [ -z "$cov" ]; then
    echo "$out"
    echo "FAIL  $pkg — could not parse a coverage percentage"
    fail=1
    continue
  fi
  if awk -v c="$cov" -v f="$floor" 'BEGIN { exit !(c + 0 < f + 0) }'; then
    echo "FAIL  $pkg — coverage ${cov}% is below floor ${floor}%"
    fail=1
  else
    echo "ok    $pkg — ${cov}% (floor ${floor}%)"
  fi
done <<EOF
$FLOORS
EOF

if [ "$fail" -ne 0 ]; then
  echo
  echo "Coverage floor violated. Either restore the lost tests or, if the"
  echo "drop is intentional, update the floor here."
  exit 1
fi
echo "All coverage floors hold."
