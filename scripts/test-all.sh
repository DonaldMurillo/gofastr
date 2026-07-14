#!/bin/bash
# Shell scripts are pinned to LF in .gitattributes so this entrypoint also
# executes directly from WSL against a Windows checkout.
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
# Resource-contention self-heal: even at -p 2 a long run can momentarily
# drain the ephemeral pool (loopback-heavy packages like battery/auth,
# battery/setup, battery/webhook landing next to a chromedp or
# subprocess suite). When — and only when — the failing run's output
# carries the kernel signature ("can't assign requested address"), the
# failed packages are re-run serially (-p 1) after a TIME_WAIT drain.
# Meridian's 16-surface visual canary can also exhaust its bounded capture
# attempts when the larger examples/site Chrome suite runs beside it (exact
# signature: "capture attempt N failed: context deadline exceeded"), and the
# evalrunner capture tests can hit their 90s deadlines under the same Chrome
# contention (looser signature: an evalrunner FAIL plus a capture/screenshot
# deadline message anywhere in the log — its tests fail through varied
# t.Fatalf texts, so this pairing accepts the same narrow residual risk as
# the port class below). Both retry serially without the port-drain delay.
# A DETERMINISTIC real failure fails the retry too (the retry re-runs the
# actual tests with -count=1), and failures without either known resource
# signature fail immediately with no retry. The residual risk is narrow:
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
LOG=$(mktemp "${TMPDIR:-/tmp}/gofastr-test-all.XXXXXX")
trap 'rm -f "$LOG"' EXIT

set +e
go test "${FLAGS[@]}" $PKGS 2>&1 | tee "$LOG"
status=${PIPESTATUS[0]}
set -e
if [ "$status" -eq 0 ]; then
  exit 0
fi

# Only self-heal the two known resource-contention signatures. Anything else
# is a real failure — surface it untouched.
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
  exit "$status"
fi

FAILED=$(awk '$1 == "FAIL" && $2 != "" {print $2}' "$LOG" | sort -u)
if [ -z "$FAILED" ]; then
  exit "$status"
fi

# Drain TIME_WAIT (2×MSL; macOS default MSL is 15s) for the port class.
# Browser starvation needs only package serialization.
if [ "$port_exhausted" -eq 1 ]; then
  echo "==> ephemeral-port exhaustion detected; draining TIME_WAIT (30s), then retrying serially:"
  sleep 30
else
  echo "==> Meridian browser capture starved beside another Chrome suite; retrying failed packages serially:"
fi
echo "$FAILED" | sed 's/^/      /'

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
