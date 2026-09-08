#!/bin/bash
# Run the adversarial red-test suite (files tagged //go:build red).
#
# A red test asserts the SECURE behaviour and fails while the finding it
# pins is still open. The normal `go test ./...` suite never sees these
# files; only this script (or `make red-tests`) opts in via -tags red.
#
# Two passes:
#   1. the plain red pass over every *_red_test.go tagged `red`;
#   2. a -race pass over the subset tagged `red && race` — concurrency
#      findings that are only deterministically observable under the race
#      detector (without it they surface as flaky unrecoverable fatals,
#      which no assertion can pin).
#
# Usage: scripts/red-tests.sh [go test -run regex]
#   RED_OUT=<path>   where the raw go test output is kept (default .gofastr/red-tests.log)
# With no regex every test in a red-tagged package runs (chromedp suites included, ~25 min);
# pass 'Red' to run only the probes.
set -uo pipefail

TAG=red
RUN="${1:-}"
OUT="${RED_OUT:-.gofastr/red-tests.log}"
PKGS="$(grep -rl --exclude-dir=.claude --exclude-dir=node_modules --exclude-dir=dist "^//go:build red" --include='*_red_test.go' . 2>/dev/null \
    | xargs -n1 dirname | sort -u | sed 's|^\.$|.|; s|^[^.]|./&|')"
mkdir -p "$(dirname "$OUT")"

# Only packages carrying red-tagged files: the tag changes nothing anywhere
# else, so testing the whole ./... tree would just re-run the slow chromedp
# and kiln suites for no signal.
if [ -z "$PKGS" ]; then
    echo "no //go:build red test files found — nothing to run."
    exit 0
fi

RUNFLAG=()
if [ -n "$RUN" ]; then RUNFLAG=(-run "$RUN"); fi
echo "==> go test -tags $TAG -count=1 ${RUNFLAG[@]+"${RUNFLAG[@]}"} in:"
echo "$PKGS" | sed 's/^/      /'
go test -tags "$TAG" -count=1 -p 2 -timeout 25m ${RUNFLAG[@]+"${RUNFLAG[@]}"} $PKGS >"$OUT" 2>&1
STATUS=$?
echo "    (raw output kept at $OUT)"

# Race-tagged probes: files tagged `red && race`. Run after the plain pass,
# appending to the same log; their failures fold into the suite verdict.
RPKGS="$(grep -rl --exclude-dir=.claude --exclude-dir=node_modules --exclude-dir=dist "^//go:build red && race\|^//go:build race && red" --include='*_red_test.go' . 2>/dev/null \
    | xargs -n1 dirname | sort -u | sed 's|^\.$|.|; s|^[^.]|./&|')"
if [ -n "$RPKGS" ]; then
    echo "==> go test -race -tags 'red race' -count=1 in:"
    echo "$RPKGS" | sed 's/^/      /'
    go test -race -tags "red race" -count=1 -p 2 -timeout 10m ${RUNFLAG[@]+"${RUNFLAG[@]}"} $RPKGS >>"$OUT" 2>&1
    RSTATUS=$?
    echo "    (race pass exit: $RSTATUS)"
    if grep -q "build failed\|setup failed" "$OUT"; then
        echo "✗ race-tagged red test files failed to COMPILE — broken findings, not open ones:"
        grep "^#\|build failed\|setup failed" "$OUT" | tail -10
        exit 1
    fi
    RFAILS=$(grep -c "WARNING: DATA RACE" "$OUT" || true)
    if [ "$RFAILS" -gt 0 ]; then
        echo "    $RFAILS race-detector finding(s), asserted by race-tagged red tests:"
        grep "WARNING: DATA RACE" "$OUT" | head -10
    fi
    STATUS=$((STATUS + RSTATUS))
fi


if grep -q "^#.*\[build failed\]\|build failed\|setup failed" "$OUT"; then
    echo "✗ red test files failed to COMPILE — broken findings, not open ones:"
    grep "^#\|build failed\|setup failed" "$OUT"
    exit 1
fi

FAILS=$(grep -c "^--- FAIL" "$OUT" || true)
PASSES=$(grep -c "^--- PASS\|^ok " "$OUT" || true)

echo
echo "==> red suite complete (combined exit: $STATUS)"
if [ "$FAILS" -gt 0 ]; then
    echo "    $FAILS open finding(s), asserted by:"
    grep "^--- FAIL" "$OUT" | sed 's/^--- FAIL: /      /' | sort
else
    echo "    no failing red tests — every pinned finding is closed."
fi
echo "    (packages fully green: $(grep -c "^ok " "$OUT" || true))"

# A partially-passing red suite is not success and not a finding list:
# it means some findings closed or some tests drifted. Surface it.
if [ "$STATUS" -ne 0 ] && [ "$FAILS" -eq 0 ]; then
    echo "✗ go test failed with no --- FAIL lines — inspect $OUT:"
    tail -40 "$OUT"
    exit 1
fi
exit 0
