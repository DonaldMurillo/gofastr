#!/bin/bash
# Run every Go test in the repo. Use before/after large refactors to
# verify nothing regressed — including the slow chromedp suites in
# examples/website and examples/core-ui-demo and the long
# kiln/integration suite.
#
# Flags:
#   -count=1   bypass the test cache so the run is authoritative
#   -timeout   bumped past the default 10m to cover core-ui-demo (~5m)
#              and kiln/integration (~1.5m) on slower machines
#   -race      optional, opt-in via RACE=1 (slows full run ~2x)
#
# Usage:
#   ./scripts/test-all.sh                       # full run, no race
#   RACE=1 ./scripts/test-all.sh                # with race detector
#   ./scripts/test-all.sh ./core-ui/...         # scope to a subtree
#   SHORT=1 ./scripts/test-all.sh               # -short (skip slow tests
#                                                 in packages that honor it)
set -euo pipefail

cd "$(dirname "$0")/.."

PKGS=${*:-./...}
FLAGS=(-count=1 -timeout=20m)
[ "${RACE:-0}" = "1" ] && FLAGS+=(-race)
[ "${SHORT:-0}" = "1" ] && FLAGS+=(-short)

echo "==> go build $PKGS"
go build $PKGS

echo "==> go vet $PKGS"
go vet $PKGS

echo "==> go test ${FLAGS[*]} $PKGS"
go test "${FLAGS[@]}" $PKGS
