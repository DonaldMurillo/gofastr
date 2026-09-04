#!/bin/bash
# Run the adversarial red-test suite (files tagged //go:build red).
#
# A red test asserts the SECURE behaviour and fails while the finding it
# pins is still open. The normal `go test ./...` suite never sees these
# files; only this script (or `make red-tests`) opts in via -tags red.
#
# Usage: scripts/red-tests.sh [go test -run regex]
#   RED_OUT=<path>   where the raw go test output is kept (default .gofastr/red-tests.log)
# With no regex every test in a red-tagged package runs (chromedp suites included, ~25 min);
# pass 'Red' to run only the probes.
set -uo pipefail

TAG=red
RUN="${1:-}"
OUT="${RED_OUT:-.gofastr/red-tests.log}"
mkdir -p "$(dirname "$OUT")"

# Only packages carrying red-tagged files: the tag changes nothing anywhere
# else, so testing the whole ./... tree would just re-run the slow chromedp
# and kiln suites for no signal.
PKGS="$(grep -rl "^//go:build red" --include='*_red_test.go' . 2>/dev/null \
    | xargs -n1 dirname | sort -u | sed 's|^\.$|.|; s|^[^.]|./&|')"
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


if grep -q "^#.*\[build failed\]\|build failed\|setup failed" "$OUT"; then
    echo "✗ red test files failed to COMPILE — broken findings, not open ones:"
    grep "^#\|build failed\|setup failed" "$OUT"
    exit 1
fi

FAILS=$(grep -c "^--- FAIL" "$OUT" || true)
PASSES=$(grep -c "^--- PASS\|^ok " "$OUT" || true)

echo
echo "==> red suite complete (go test exit: $STATUS)"
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
