#!/bin/bash
# Fail when a `go test` run over an unchanged tree re-ran any package.
#
# Usage: scripts/test-cache-replay.sh <go test output> [start marker]
#
# CI runs the test step's command a second time and hands its output
# here. Every package should come back "(cached)": a package that re-ran
# reads an input that changes when no code did (a stat of .git, a file
# the suite itself rewrites inside the module, a directory listing that
# picks up run state), and every later run pays for it again. The fix is
# to stop reading that input, not to re-run.
#
# Given a file touched before the first run, a failure also lists the
# module files changed since. A package that walks the repo hashes every
# directory listing it read, entry mtimes included, so a file another
# test writes or rewrites during the first run is the usual culprit. Go
# also declines to cache a run whose opened files changed under 2s
# before it finished.
set -euo pipefail

log=${1:?usage: test-cache-replay.sh <go test output> [start marker]}
marker=${2:-}

if [ ! -r "$log" ]; then
	echo "test-cache-replay: cannot read $log" >&2
	exit 1
fi
# A run that tested nothing replays nothing and proves nothing.
total=$(grep -cE '^ok[[:space:]]' "$log" || true)
if [ "$total" -eq 0 ]; then
	echo "test-cache-replay: $log has no passing package; the second run did not test anything" >&2
	exit 1
fi

reran=$(grep -E '^ok[[:space:]]' "$log" | grep -v '(cached)' || true)
if [ -n "$reran" ]; then
	echo "test-cache-replay: these packages re-ran on an unchanged tree:" >&2
	echo "$reran" >&2
	if [ -n "$marker" ] && [ -r "$marker" ]; then
		echo "Module files changed since the first run started:" >&2
		find . -path ./.git -prune -o -newer "$marker" -print | sed -n 1,50p >&2
	fi
	echo "Find the input: GODEBUG=gocachetest=1 go test <pkg> twice, then read the input list Go saved." >&2
	exit 1
fi
echo "test-cache-replay: every package replayed from the cache ($total packages)"
