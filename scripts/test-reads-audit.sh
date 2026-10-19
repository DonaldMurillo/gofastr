#!/usr/bin/env bash
# test-reads-audit.sh: check that the affected selection (cmd/affected)
# picks up every file a test reads.
#
# Runs the suite with each test binary writing Go's test log, the record
# of every file the binary opens itself that the test cache keys on (a
# subprocess's reads and build-tagged tests stay unseen), then runs
# cmd/affected's TestRecordedReadsSelectTheirReader over the logs: every
# tracked file a package's tests opened must select that package when it
# changes, through the import graph or a //gofastr:test-reads directive.
# A miss is a gate the affected run skips; #536's missing timeout reached
# main through one.
#
#   scripts/test-reads-audit.sh                  # every package
#   scripts/test-reads-audit.sh ./core-ui/...    # a subset
#
# GOFASTR_TESTLOG_DIR keeps the logs in a directory you choose, which must
# be empty (default: a fresh temp dir). A failing test still writes its
# log, so ordinary test failures are reported and the audit still runs.
# A package that did not build left no log at all, and that fails the
# script, and the audit fails on a log whose binary exited any other way
# than pass or fail: Go flushes the log only when the tests finish, so a
# panic leaves it cut off.
set -euo pipefail

# The -exec wrapper: go test runs this script with the test binary first.
if [ "${1:-}" = "--exec" ]; then
  shift
  bin="$1"
  shift
  base=$(mktemp "$GOFASTR_TESTLOG_RECORD/t.XXXXXXXX")
  printf '%s\n' "$PWD" > "$base.dir"
  # Not exec: the exit status is how the audit tells a log Go flushed at
  # the end of the run (0 pass, 1 a failed test) from one a panic cut off.
  status=0
  "$bin" -test.testlogfile="$base.log" "$@" || status=$?
  printf '%s\n' "$status" > "$base.status"
  exit "$status"
fi

root=$(git rev-parse --show-toplevel)
cd "$root"
dir="${GOFASTR_TESTLOG_DIR:-$(mktemp -d)}"
mkdir -p "$dir"
if [ -n "$(ls -A "$dir")" ]; then
  echo "test-reads-audit: $dir is not empty; older logs would be audited as this run's" >&2
  exit 2
fi
if [ "$#" -eq 0 ]; then
  set -- ./...
fi

echo "test-reads-audit: recording into $dir" >&2
# GOFASTR_TESTLOG_DIR is unset while recording: the audit test itself is
# in ./... and must skip there, or its own walk of every test file would
# be recorded and audited.
suite=0
env -u GOFASTR_TESTLOG_DIR GOFASTR_TESTLOG_RECORD="$dir" \
  go test -count=1 -timeout 45m -exec "$root/scripts/test-reads-audit.sh --exec" "$@" \
  > "$dir/run.txt" 2>&1 || suite=$?
if [ "$suite" -ne 0 ]; then
  echo "test-reads-audit: the suite had failures (see $dir/run.txt); auditing the logs it wrote" >&2
fi

audit=0
GOFASTR_TESTLOG_DIR="$dir" go test -count=1 -run '^TestRecordedReadsSelectTheirReader$' ./cmd/affected || audit=$?

if grep -E '\[(build|setup) failed\]' "$dir/run.txt" >&2; then
  echo "test-reads-audit: the lines above left packages with no complete log, so their reads went unaudited" >&2
  exit 1
fi
exit "$audit"
