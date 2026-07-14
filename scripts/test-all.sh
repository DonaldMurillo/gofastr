#!/bin/bash
# Run every Go test in the repo. Use before/after large refactors to
# verify nothing regressed — including the slow chromedp suite in
# examples/site and the long kiln/integration suite.
#
# Flags:
#   -count=1   bypass the test cache so the run is authoritative
#   -timeout   bumped past the default 10m to cover examples/site
#              chromedp (~2.5m) and kiln/integration (~1.5m) on
#              slower machines
#   -p N       cap package-level parallelism. Go defaults to GOMAXPROCS
#              (10 on M-series), which lets dozens of httptest servers
#              and chromedp browsers race for the macOS ephemeral port
#              range (49152-65535 = ~16K ports, 15s TIME_WAIT). The
#              symptom is intermittent "bind: can't assign requested
#              address" / "connect: can't assign requested address" on
#              tests that succeed in isolation. Capping at 2 keeps
#              intra-package t.Parallel() at full GOMAXPROCS while
#              keeping the kernel ephemeral pool from saturating —
#              picked 2 (not higher) because the heaviest packages
#              (cmd/gofastr blueprint subprocesses, kiln/integration
#              chromedp, examples/site chromedp, core-ui/runtime
#              chromedp) each open dozens of sockets per test and
#              -p 4 still flakes on the M-series ephemeral range.
#              Override via TEST_PARALLELISM=N if you have a beefier
#              machine or a tuned net.inet.ip.portrange sysctl.
#
# Port-exhaustion self-heal: even at -p 2 a long run can momentarily
# drain the ephemeral pool (loopback-heavy packages like battery/auth,
# battery/setup, battery/webhook landing next to a chromedp or
# subprocess suite). When — and only when — the failing run's output
# carries the kernel signature ("can't assign requested address"), the
# failed packages are re-run serially (-p 1) after a TIME_WAIT drain.
# A DETERMINISTIC real failure fails the retry too (the retry re-runs the
# actual tests with -count=1), and failures WITHOUT the kernel signature
# fail the script immediately with no retry. The residual risk is narrow:
# a genuinely flaky race in package A that happens to co-occur with a
# port message from package B could pass on the serial retry — acceptable
# versus the alternative of a suite that can't complete in parallel at all.
#   -race      optional, opt-in via RACE=1 (slows full run ~2x)
#
# Usage:
#   ./scripts/test-all.sh                       # full run, no race
#   RACE=1 ./scripts/test-all.sh                # with race detector
#   TEST_PARALLELISM=8 ./scripts/test-all.sh    # bump parallelism cap
#   ./scripts/test-all.sh ./core-ui/...         # scope to a subtree
#   SHORT=1 ./scripts/test-all.sh               # -short (skip slow tests
#                                                 in packages that honor it)
set -euo pipefail

cd "$(dirname "$0")/.."

PKGS=${*:-./...}
PARALLEL=${TEST_PARALLELISM:-2}
FLAGS=(-count=1 -timeout=20m -p "$PARALLEL")
[ "${RACE:-0}" = "1" ] && FLAGS+=(-race)
[ "${SHORT:-0}" = "1" ] && FLAGS+=(-short)

echo "==> go build $PKGS"
go build $PKGS

echo "==> go vet $PKGS"
go vet $PKGS

echo "==> go test ${FLAGS[*]} $PKGS"
LOG=$(mktemp -t gofastr-test-all)
trap 'rm -f "$LOG"' EXIT

set +e
go test "${FLAGS[@]}" $PKGS 2>&1 | tee "$LOG"
status=${PIPESTATUS[0]}
set -e
if [ "$status" -eq 0 ]; then
  exit 0
fi

# Only self-heal the macOS ephemeral-port exhaustion class. Anything
# else is a real failure — surface it untouched.
if ! grep -q "can't assign requested address" "$LOG"; then
  exit "$status"
fi

FAILED=$(awk '$1 == "FAIL" && $2 != "" {print $2}' "$LOG" | sort -u)
if [ -z "$FAILED" ]; then
  exit "$status"
fi

# Drain TIME_WAIT (2×MSL; macOS default MSL is 15s) so the retry starts
# with a full ephemeral pool, then re-run the failed packages serially.
echo "==> ephemeral-port exhaustion detected; draining TIME_WAIT (30s), then retrying serially:"
echo "$FAILED" | sed 's/^/      /'
sleep 30

# Rebuild the flag set with -p 1 replacing the parallel cap.
RETRY_FLAGS=()
skip_next=0
for f in "${FLAGS[@]}"; do
  if [ "$skip_next" = "1" ]; then skip_next=0; continue; fi
  if [ "$f" = "-p" ]; then skip_next=1; continue; fi
  RETRY_FLAGS+=("$f")
done
RETRY_FLAGS+=(-p 1)

echo "==> go test ${RETRY_FLAGS[*]} <failed packages>"
# shellcheck disable=SC2086
go test "${RETRY_FLAGS[@]}" $FAILED
