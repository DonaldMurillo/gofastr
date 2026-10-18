#!/usr/bin/env bash
# bench-smoke: run every benchmark in the affected packages exactly once.
#
# `go test` never runs benchmarks, so a benchmark that stops working fails
# nobody's build. By 2026-10, 56 framework benchmarks failed without anyone
# noticing: generated CRUD had started requiring a session, so their requests
# answered 401 before the handler body ran, and the POST/PUT body helper
# returned a non-io.EOF error at end of input. This step proves each
# benchmark still runs; -benchtime=1x measures nothing. Postgres halves are
# skipped (BENCH_SKIP_PG=1) so it needs no database.
#
# Scope follows cmd/affected (everything on main, the affected closure on a
# pull request), narrowed to the packages that declare a benchmark so no
# other test binary is rebuilt.
set -euo pipefail
cd "$(dirname "$0")/.."

mod=$(go list -m)
match=$(git grep -l '^func Benchmark' -- '*_test.go' |
	xargs -n1 dirname | sort -u |
	awk -v m="$mod" '{ print ($0 == "." ? m : m "/" $0) }' |
	sed 's/[.]/\\./g' |
	paste -sd'|' -)

BENCH_SKIP_PG=1 exec go run ./cmd/affected -match "^(${match})\$" -- \
	go test -run='^$' -bench=. -benchtime=1x -timeout=10m "$@"
