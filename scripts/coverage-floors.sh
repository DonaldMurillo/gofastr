#!/bin/bash
# Enforce coverage floors for the audited packages so their coverage
# can't drift silently. This script is the single source of truth for
# the floors.
#
# Methodology: own-package coverage (`go test -coverprofile ./<pkg>/`),
# the cheapest reproducible measurement. No -count=1: coverage runs are
# served from Go's content-addressed test cache when a package's sources
# and deps are unchanged, same inputs, same percentage, so repeat runs
# (locally and in CI, which persists GOCACHE per-SHA) skip re-execution. A
# floor may gate a
# whole package OR a file-filtered *bucket* within one. See below. The
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
# (processmodule_*.go, #37), 87% of the package by volume, and dominated by
# subprocess spawn/kill, OS-specific sandbox backends, and Postgres-only DDL
# that no portable test env can exercise, is held to a floor matching its
# nature (the same way framework/migrate is floored at 73.5 and
# framework/entity at 86.5). Both buckets are computed from ONE profile.
#
# Each floor is set ~2 points below the value measured when the bucket was
# last (re)baselined. The slack absorbs ordinary churn (a refactor that adds
# a handful of uncovered defensive lines shouldn't block CI) while still
# catching real regressions, a deleted test file or a newly untested
# feature moves coverage far more than 2 points. If you intentionally change
# a bucket's coverage profile, re-measure and update the floor here in the
# same commit.
#
# 2026-08-16 extension sweep: every remaining package the blocking CI job
# runs (i.e. not the chromedp-isolated packages ci.yml's Test step excludes,
# not cmd/gofastr per below) was measured with `go test -cover`; every one
# at ≥70% got a floor at measured − 1.5. Packages below 70% were left
# unfloored deliberately: a floor under weak coverage pins the weakness
# instead of catching drift.
#
# Exclusions:
#   cmd/gofastr (claimed 84% full-suite) is NOT gated here: its suite is
#   dominated by slow, environment-sensitive e2e tests (subprocess hot-reload
#   servers, chromedp, an installed `gofastr` binary on PATH, ~7 min) that
#   the CI test/browser-e2e jobs already run, re-running them for a floor
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
./battery/admin/ 79.4
./battery/auth/ 77.5
./battery/cache/ 80.1
./battery/log/ 78.1
./battery/notify/ 81.7
./battery/print/ 87.6
./battery/queue/ 78.9
./battery/setup/ 81.2
./battery/storage/ 79.8
./battery/webhook/ 82.9
./cmd/check-embed/embedcheck/ 78.0
./cmd/mutate/ 28.8
./cmd/mutate/guardmut/ 96.3
./cmd/repolint/ 76.7
./codegen/ 77.4
./core-ui/app/ 87.1
./core-ui/check/ 84.6
./core-ui/component/ 88.2
./core-ui/compute/ 95.2
./core-ui/di/ 95.6
./core-ui/html/ 90.6
./core-ui/interactive/ 93.5
./core-ui/island/ 85.4
./core-ui/node/ 91.0
./core-ui/patterns/accordion/ 93.4
./core-ui/patterns/breadcrumbs/ 85.5
./core-ui/patterns/combobox/ 96.9
./core-ui/patterns/disclosure/ 69.9
./core-ui/patterns/multiselect/ 85.0
./core-ui/patterns/nestedlist/ 89.1
./core-ui/patterns/pagination/ 70.4
./core-ui/patterns/progress/ 88.0
./core-ui/patterns/scrollspy/ 81.8
./core-ui/patterns/skeleton/ 90.2
./core-ui/patterns/sortablelist/ 87.4
./core-ui/patterns/tabs/ 90.2
./core-ui/patterns/tree/ 92.5
./core-ui/registry/ 84.8
./core-ui/runtime/minify/ 85.5
./core-ui/seo/ 92.3
./core-ui/store/ 79.3
./core-ui/style/ 82.8
./core-ui/uinodev1/ 80.8
./core-ui/urlsafe/ 94.1
./core-ui/widget/preset/ 93.5
./core-ui/widget/theme/ 98.5
./core/config/ 82.0
./core/dotenv/ 93.3
./core/fanout/ 93.8
./core/featureflag/ 78.6
./core/fuzzy/ 98.5
./core/handler/ 85.9
./core/i18n/ 86.3
./core/markdown/ 90.8
./core/mcp/ 82.1
./core/middleware/ 80.7
./core/migrate/ 98.0
./core/moduleproto/ 81.2
./core/netguard/ 94.2
./core/openapi/ 85.8
./core/query/ 94.3
./core/render/ 92.8
./core/router/ 90.7
./core/schema/ 97.0
./core/static/ 80.1
./core/stream/ 89.5
./core/upload/ 78.2
./core/yaml/ 84.1
./examples/backoffice/ 73.5
./framework/ 84.0 match:/processmodule
./framework/ 95.5 nomatch:/processmodule
./framework/access/ 80.5
./framework/agentsinv/ 98.5
./framework/axecov/ 84.5
./framework/contracts/ 72.0
./framework/contracts/analyzers/ 70.5
./framework/cron/ 84.0
./framework/crud/ 96.9
./framework/datexport/ 98.0
./framework/dev/ 81.3
./framework/docs/ 81.8
./framework/dsl/ 72.7
./framework/embed/ 87.0
./framework/entity/ 86.5
./framework/event/ 80.5
./framework/experimental/harness/ 79.5
./framework/experimental/harness/client/tui/ 78.5
./framework/experimental/harness/context/ 78.2
./framework/experimental/harness/control/auth/ 72.3
./framework/experimental/harness/engine/ 75.9
./framework/experimental/harness/hook/ 75.0
./framework/experimental/harness/internal/ulid/ 83.5
./framework/experimental/harness/logging/ 82.5
./framework/experimental/harness/memory/ 86.7
./framework/experimental/harness/profile/ 75.3
./framework/experimental/harness/provider/ 79.7
./framework/experimental/harness/provider/credstore/ 77.5
./framework/experimental/harness/provider/helper/ 90.8
./framework/experimental/harness/provider/internal/openai/ 74.5
./framework/experimental/harness/secrets/ 84.5
./framework/experimental/harness/session/ 75.3
./framework/experimental/harness/session/sqlite/ 74.6
./framework/experimental/harness/skill/ 78.3
./framework/experimental/harness/skill/skillmd/ 75.6
./framework/experimental/harness/slash/ 88.9
./framework/experimental/harness/tool/ 73.5
./framework/experimental/harness/tool/builtins/ 76.2
./framework/experimental/harness/tool/permission/ 79.3
./framework/experimental/harness/tracing/ 84.2
./framework/factory/ 81.0
./framework/file/ 73.7
./framework/filter/ 88.9
./framework/hook/ 82.2
./framework/i18nui/ 90.5
./framework/image/ 81.8
./framework/image/internal/vp8l/ 89.5
./framework/imagefield/ 83.9
./framework/isolation/ 74.6
./framework/lifecycle/ 79.9
./framework/migrate/ 73.5
./framework/openapi/ 93.4
./framework/outbox/ 76.2
./framework/owner/ 92.9
./framework/pagination/ 68.7
./framework/pluginhost/ 94.0
./framework/ratelimit/ 91.0
./framework/routegroup/ 85.4
./framework/sdk/ 87.3
./framework/sdkdocs/ 77.1
./framework/semcov/ 87.0
./framework/tenant/ 85.5
./framework/ui/ 84.0
./framework/ui/theme/ 90.7
./framework/uihost/ 85.5
./framework/uihost/internal/sessiontoken/ 93.7
./framework/uihost/uinoderender/ 87.7
./internal/fileperm/ 98.5
./kiln/agent/acp/ 78.1
./kiln/agent/mcp/ 73.5
./kiln/effect/ 75.1
./kiln/expr/ 71.7
./sqlite/stdlib/ 81.8
./stability/ 91.4
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
    if ! go test -coverprofile="$prof" "$pkg" >"$profdir/$key.log" 2>&1; then
      # stderr, not stdout. This function is called as $(profile_for ...), so
      # anything on stdout is captured into the caller's variable instead of
      # reaching the log, which is how a filtered package could fail a
      # BLOCKING gate while printing nothing but "tests failed (no coverage
      # measurement)". The failing test name was simply swallowed.
      cat "$profdir/$key.log" >&2
      # Go writes a partial coverprofile even when the run fails. Leaving it
      # behind let the package's SECOND bucket skip the re-run, read the
      # partial data, and report a comfortable "ok NN%" for a suite that had
      # just failed. Remove it so every bucket of a failed package reports the
      # failure.
      rm -f "$prof"
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
    if ! out=$(go test -cover "$pkg" 2>&1); then
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
