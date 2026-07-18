#!/bin/bash
# Enforce coverage floors for the audited packages so their coverage
# can't drift silently. This script is the single source of truth for
# the floors.
#
# Methodology: own-package coverage (`go test -coverprofile ./<pkg>/`,
# `-count=1`), the cheapest reproducible measurement. A floor may gate a
# whole package OR a file-filtered *bucket* within one — see below. The
# audited packages sit below a literal 100% by design: defensive
# fail-closed guards that are unreachable today are kept (not rewritten to
# chase the number), and CLI serve-loops / interactive entry points + a few
# OS-IO fault branches are accepted as untestable without real listeners or
# stdin.
#
# Per-bucket floors: a package that hosts two subsystems with genuinely
# different testability can gate each separately instead of blending them
# into one diluted number that hides a regression in either. `framework`
# does this: the App spine (app/module/plugin/battery/...) is held to a
# strict floor, while the process-isolated-module subsystem
# (processmodule_*.go, #37) — 87% of the package by volume, and dominated by
# subprocess spawn/kill, OS-specific sandbox backends, and Postgres-only DDL
# that no portable test env can exercise — is held to a floor matching its
# nature (the same way framework/migrate is floored at 73.5 and
# framework/entity at 86.5). Both buckets are computed from ONE profile.
#
# Each floor is set ~2 points below the value measured when the bucket was
# last (re)baselined. The slack absorbs ordinary churn (a refactor that adds
# a handful of uncovered defensive lines shouldn't block CI) while still
# catching real regressions — a deleted test file or a newly untested
# feature moves coverage far more than 2 points. If you intentionally change
# a bucket's coverage profile, re-measure and update the floor here in the
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

# pkg<space>floor[<space>filter].
#   filter absent           → whole-package coverage.
#   filter "match:REGEX"    → only statements in files whose path matches REGEX.
#   filter "nomatch:REGEX"  → only statements in files whose path does NOT match.
# Buckets sharing a pkg reuse a single cached coverprofile (the suite runs once).
FLOORS="
./core/migrate/ 98.0
./core/schema/ 97.0
./framework/ 95.5 nomatch:/processmodule
./framework/ 84.0 match:/processmodule
./framework/crud/ 96.9
./framework/entity/ 86.5
./framework/migrate/ 73.5
./framework/tenant/ 85.5
"

profdir=$(mktemp -d)
trap 'rm -rf "$profdir"' EXIT

# profile_for PKG → path to a cached coverprofile for PKG (runs the suite
# once per package, reused across that package's buckets).
profile_for() {
  local pkg="$1"
  local key
  key=$(echo "$pkg" | tr -c 'A-Za-z0-9' '_')
  local prof="$profdir/$key.out"
  if [ ! -f "$prof" ]; then
    if ! go test -coverprofile="$prof" -count=1 "$pkg" >"$profdir/$key.log" 2>&1; then
      cat "$profdir/$key.log"
      return 1
    fi
  fi
  echo "$prof"
}

# bucket_cov PROFILE FILTER → coverage percentage of the filtered statements.
# Each profile line after the mode header is:
#   <file>:<sL>.<sC>,<eL>.<eC> <numStmts> <count>
# Bucket coverage = covered statements / total statements, matching what
# `go test -cover` reports for the whole file set.
bucket_cov() {
  local prof="$1" filter="$2"
  awk -v filter="$filter" '
    NR==1 { next }                       # skip "mode:" header
    {
      n = split($0, a, " ")
      stmts = a[n-1]; cnt = a[n]
      keep = 1
      if (filter ~ /^match:/)   { re = substr(filter,7);  keep = ($0 ~ re) }
      if (filter ~ /^nomatch:/) { re = substr(filter,9);  keep = ($0 !~ re) }
      if (keep) { tot += stmts; if (cnt+0 > 0) cov += stmts }
    }
    END { if (tot > 0) printf "%.1f", 100*cov/tot; else print "NA" }
  ' "$prof"
}

fail=0
while read -r pkg floor filter; do
  [ -z "$pkg" ] && continue
  label="$pkg"
  [ -n "$filter" ] && label="$pkg [$filter]"

  if [ -z "$filter" ]; then
    # Whole-package fast path.
    if ! out=$(go test -cover -count=1 "$pkg" 2>&1); then
      echo "$out"; echo "FAIL  $label — tests failed (no coverage measurement)"; fail=1; continue
    fi
    cov=$(echo "$out" | grep -Eo 'coverage: [0-9.]+%' | tail -1 | grep -Eo '[0-9.]+')
  else
    # File-filtered bucket: compute from a cached coverprofile.
    if ! prof=$(profile_for "$pkg"); then
      echo "FAIL  $label — tests failed (no coverage measurement)"; fail=1; continue
    fi
    cov=$(bucket_cov "$prof" "$filter")
  fi

  if [ -z "$cov" ] || [ "$cov" = "NA" ]; then
    echo "FAIL  $label — could not compute a coverage percentage"; fail=1; continue
  fi
  if awk -v c="$cov" -v f="$floor" 'BEGIN { exit !(c + 0 < f + 0) }'; then
    echo "FAIL  $label — coverage ${cov}% is below floor ${floor}%"; fail=1
  else
    echo "ok    $label — ${cov}% (floor ${floor}%)"
  fi
done <<EOF
$FLOORS
EOF

if [ "$fail" -ne 0 ]; then
  echo
  echo "Coverage floor violated. Either restore the lost tests or, if the"
  echo "drop is intentional, re-measure and update the floor here."
  exit 1
fi
echo "All coverage floors hold."
