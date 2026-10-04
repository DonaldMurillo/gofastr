#!/bin/bash
# Shell scripts are pinned to LF in .gitattributes so this entrypoint also
# executes directly from WSL against a Windows checkout.
# Run every Go test in the repo. Use before/after large refactors to
# verify nothing regressed, including the slow chromedp suite in
# examples/site and the long kiln/integration suite.
#
# Two passes, CI's shape (the blocking job runs the deterministic
# packages in parallel; the browser-e2e job runs each heavy suite
# isolated on its own runner):
#   1. every non-heavy package in parallel at -p $TEST_PARALLELISM;
#   2. the heavy browser suites (HEAVY_RE below), after the parallel
#      pass, serialized at -p 1, plus cmd/gofastr's four browser tests
#      via -run (they are -skip'ped in pass 1; the rest of the package
#      rides the parallel pass).
#
# Flags:
#   -count=1   bypass the test cache so the run is authoritative
#   -timeout   bumped past the default 10m to cover examples/site
#              chromedp (~2.5m) and kiln/integration (~1.5m) on
#              slower machines
#   -p N       caps package-level parallelism of the PARALLEL pass. Go
#              defaults to GOMAXPROCS (10 on M-series), which lets
#              dozens of httptest servers race for the macOS ephemeral
#              port range (49152-65535 = ~16K ports, 15s TIME_WAIT).
#              The symptom is intermittent "bind: can't assign
#              requested address" / "connect: can't assign requested
#              address" on tests that succeed in isolation. Capping at
#              2 keeps intra-package t.Parallel() at full GOMAXPROCS
#              while keeping the kernel ephemeral pool from saturating.
#              The heavy chromedp suites no longer lean on this cap as
#              their only defence: they run alone in the serialized
#              pass at -p 1. Override via TEST_PARALLELISM=N if you
#              have a beefier machine or a tuned
#              net.inet.ip.portrange sysctl.
#
# Resource-contention self-heal, shared by BOTH passes (one helper, one
# log): even at -p 2 the parallel pass can momentarily drain the
# ephemeral pool (loopback-heavy packages like battery/auth,
# battery/setup, battery/webhook landing next to a chromedp or
# subprocess suite). When, and only when, the failing run's output
# carries the kernel signature ("can't assign requested address"), the
# failed packages are re-run serially (-p 1) after a TIME_WAIT drain.
# Meridian's 16-surface visual canary can also exhaust its bounded capture
# attempts when the larger examples/site Chrome suite runs beside it (exact
# signature: "capture attempt N failed: context deadline exceeded"), and
# the evalrunner capture tests can hit their 90s deadlines under the same Chrome
# contention (looser signature: an evalrunner FAIL plus a capture/screenshot
# deadline message anywhere in the log, its tests fail through varied
# t.Fatalf texts, so this pairing accepts the same narrow residual risk as
# the port class below). Both retry serially without the port-drain delay.
# A DETERMINISTIC real failure fails the retry too (the retry re-runs the
# actual tests with -count=1), and failures without either known resource
# signature fail immediately with no retry. The residual risk is narrow:
# a genuinely flaky race in package A that happens to co-occur with a
# port message from package B could pass on the serial retry, acceptable
# versus the alternative of a suite that can't complete in parallel at all.
#   -race      optional, opt-in via RACE=1 (slows full run ~2x)
#
# Usage:
#   ./scripts/test-all.sh                       # full run, no race
#   RACE=1 ./scripts/test-all.sh                # with race detector
#   TEST_PARALLELISM=8 ./scripts/test-all.sh    # bump parallelism cap
#   ./scripts/test-all.sh ./core-ui/...         # scope BOTH passes to a subtree
#   SHORT=1 ./scripts/test-all.sh               # -short (skip slow tests
#                                                 in packages that honor it)
#   GOFASTR_TESTALL_DRYRUN=1 ./scripts/test-all.sh [tree]
#                                              # print the commands instead
#                                              # of running them (go list
#                                              # still runs, it builds the
#                                              # two package lists)
set -euo pipefail

cd "$(dirname "$0")/.."

PKGS=${*:-./...}
PARALLEL=${TEST_PARALLELISM:-2}

# The heavy set: the chromedp/browser packages CI isolates one suite per
# runner in its browser-e2e job. Import-path regex; the same spelling
# .githooks/pre-push and .github/workflows/ci.yml use. Keep in sync.
HEAVY_RE='gofastr/(examples/site|examples/meridian|examples/tracker|examples/layoutlab|kiln/integration|core-ui/runtime|core-ui/widget|evals/ui-quality/internal/evalrunner|framework/headless)$'
# The four cmd/gofastr tests CI runs serialized in its browser-e2e
# gofastr-cli shard: three drive a real Chrome over the dev livereload
# websocket, one builds and boots every example. Skipped in the parallel
# pass, run alone at -p 1 in the serialized pass.
CLI_BROWSER_TESTS='TestBlueprintCLIGeneratesEntireWorkingAppE2E|TestE2E_HotReload_BrowserAutoRefreshes|TestLivereloadSameBuildIDDoesNotReload|TestE2E_DevLoop_Examples'

FLAGS=(-count=1 -timeout=20m)
[ "${RACE:-0}" = "1" ] && FLAGS+=(-race)
[ "${SHORT:-0}" = "1" ] && FLAGS+=(-short)

dryrun() { [ "${GOFASTR_TESTALL_DRYRUN:-0}" = "1" ]; }

echo "==> go build $PKGS"
dryrun || go build $PKGS

echo "==> go vet $PKGS"
dryrun || go vet $PKGS

# Split the scoped package set into the two passes. go list resolves the
# subtree argument once; the regex split is the hook's and CI's heavy set.
all_pkgs=$(go list $PKGS)
light_pkgs=$(printf '%s\n' "$all_pkgs" | grep -Ev "$HEAVY_RE" || true)
heavy_pkgs=$(printf '%s\n' "$all_pkgs" | grep -E "$HEAVY_RE" || true)
cli_pkg=$(printf '%s\n' "$all_pkgs" | grep -E 'gofastr/cmd/gofastr$' || true)

LOG=$(mktemp "${TMPDIR:-/tmp}/gofastr-test-all.XXXXXX")
trap 'rm -f "$LOG"' EXIT

# go_test_selfheal ARGS...: run `go test "$@"` teeing to $LOG, then apply
# the resource-contention self-heal described in the header. The log is
# truncated per call, so a pass's serial retry re-runs only that pass's
# failures, not the earlier pass's.
go_test_selfheal() {
  if dryrun; then
    printf 'go test %s\n' "$*"
    return 0
  fi
  : > "$LOG"
  set +e
  go test "$@" 2>&1 | tee "$LOG"
  local status=${PIPESTATUS[0]}
  set -e
  if [ "$status" -eq 0 ]; then
    return 0
  fi

  # Only self-heal the two known resource-contention signatures. Anything
  # else is a real failure, surface it untouched.
  port_exhausted=0
  browser_starved=0
  grep -q "can't assign requested address" "$LOG" && port_exhausted=1
  if grep -q '^FAIL[[:space:]].*github.com/DonaldMurillo/gofastr/examples/meridian' "$LOG" &&
     grep -q 'capture attempt [0-9][0-9]* failed: context deadline exceeded' "$LOG"; then
    browser_starved=1
  fi
  if grep -q '^FAIL[[:space:]].*github.com/DonaldMurillo/gofastr/evals/ui-quality/internal/evalrunner' "$LOG" &&
     grep -qE '(capture|screenshot).*context deadline exceeded' "$LOG"; then
    browser_starved=1
  fi
  if [ "$port_exhausted" -eq 0 ] && [ "$browser_starved" -eq 0 ]; then
    return "$status"
  fi

  local failed
  failed=$(awk '$1 == "FAIL" && $2 != "" {print $2}' "$LOG" | sort -u)
  if [ -z "$failed" ]; then
    return "$status"
  fi

  # Drain TIME_WAIT (2×MSL; macOS default MSL is 15s) for the port class.
  # Browser starvation needs only package serialization.
  if [ "$port_exhausted" -eq 1 ]; then
    echo "==> ephemeral-port exhaustion detected; draining TIME_WAIT (30s), then retrying serially:"
    sleep 30
  else
    echo "==> Meridian browser capture starved beside another Chrome suite; retrying failed packages serially:"
  fi
  echo "$failed" | sed 's/^/      /'

  # Rebuild the flag set with -p 1 replacing the parallel cap.
  local retry=() skip_next=0 f
  for f in "$@"; do
    if [ "$skip_next" = "1" ]; then skip_next=0; continue; fi
    if [ "$f" = "-p" ]; then skip_next=1; continue; fi
    retry+=("$f")
  done
  retry+=(-p 1)

  echo "==> go test ${retry[*]} <failed packages>"
  # shellcheck disable=SC2086
  go test "${retry[@]}" $failed
}

# ---- Pass 1: parallel, non-heavy packages ----
# cmd/gofastr rides here with its four browser tests skipped, so its
# deterministic tests stay in the parallel pass.
if [ -n "$light_pkgs" ]; then
  echo "==> pass 1: parallel (-p $PARALLEL), non-heavy packages"
  # shellcheck disable=SC2086
  go_test_selfheal "${FLAGS[@]}" -p "$PARALLEL" -skip "$CLI_BROWSER_TESTS" $light_pkgs
else
  echo "==> pass 1: no non-heavy packages in scope"
fi

# ---- Pass 2: serialized heavy pass (-p 1) ----
if [ -n "$heavy_pkgs" ]; then
  echo "==> pass 2: serialized (-p 1), heavy browser packages"
  # shellcheck disable=SC2086
  go_test_selfheal "${FLAGS[@]}" -p 1 $heavy_pkgs
else
  echo "==> pass 2: no heavy packages in scope"
fi
if [ -n "$cli_pkg" ]; then
  echo "==> pass 2: serialized (-p 1), cmd/gofastr browser tests"
  # shellcheck disable=SC2086
  go_test_selfheal "${FLAGS[@]}" -p 1 -run "$CLI_BROWSER_TESTS" $cli_pkg
fi
