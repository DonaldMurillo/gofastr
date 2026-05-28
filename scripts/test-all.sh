#!/bin/bash
# Run every Go test in the repo. Use before/after large refactors to
# verify nothing regressed — including the slow chromedp suite in
# examples/website and the long kiln/integration suite.
#
# Flags:
#   -count=1   bypass the test cache so the run is authoritative
#   -timeout   bumped past the default 10m to cover examples/website
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
#              chromedp, examples/website chromedp, core-ui/runtime
#              chromedp) each open dozens of sockets per test and
#              -p 4 still flakes on the M-series ephemeral range.
#              Override via TEST_PARALLELISM=N if you have a beefier
#              machine or a tuned net.inet.ip.portrange sysctl.
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
go test "${FLAGS[@]}" $PKGS
