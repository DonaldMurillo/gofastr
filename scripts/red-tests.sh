#!/bin/bash
# Run the adversarial red-test suite (files tagged //go:build red).
#
# A red test asserts the SECURE behaviour and fails while the finding it
# pins is still open. The normal `go test ./...` suite never sees these
# files; only this script (or `make red-tests`) opts in via -tags red.
#
set -uo pipefail

TAG=red
OUT="$(mktemp -t gofastr-red-tests)"
trap 'rm -f "$OUT"' EXIT

# Only packages carrying red-tagged files: the tag changes nothing anywhere
# else, so testing the whole ./... tree would just re-run the slow chromedp
# and kiln suites for no signal.
PKGS="$(grep -rl "^//go:build red" --include='*_red_test.go' . 2>/dev/null \
    | xargs -n1 dirname | sort -u | sed 's|^\.$|.|; s|^[^.]|./&|')"
if [ -z "$PKGS" ]; then
    echo "no //go:build red test files found — nothing to run."
    exit 0
fi

echo "==> go test -tags $TAG -count=1 in:"
echo "$PKGS" | sed 's/^/      /'
go test -tags "$TAG" -count=1 -p 2 -timeout 25m $PKGS >"$OUT" 2>&1
STATUS=$?


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
    echo "✗ go test failed with no --- FAIL lines — inspect $OUT-style output:"
    cat "$OUT"
    exit 1
fi
exit 0
