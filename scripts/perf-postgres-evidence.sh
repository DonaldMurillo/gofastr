#!/bin/sh
set -eu

BENCH_OUT="${BENCH_OUT:-dist/bench}"
BENCHTIME="${BENCHTIME:-100ms}"
BENCH_COUNT="${BENCH_COUNT:-1}"
BENCH_TIMEOUT="${BENCH_TIMEOUT:-240s}"
BENCH_PATTERN="${BENCH_PATTERN:-Benchmark(T9_StreamingVsBuffered_RealVolume|SchemaDiff|AutoMigrate_Idempotent)$}"

mkdir -p "$BENCH_OUT"
tmp="${TMPDIR:-/tmp}/gofastr-postgres-evidence-$$.txt"
trap 'rm -f "$tmp"' EXIT

if go test -v ./framework -run='^$' -bench="$BENCH_PATTERN" -benchmem \
	-benchtime="$BENCHTIME" -count="$BENCH_COUNT" -timeout="$BENCH_TIMEOUT" >"$tmp" 2>&1; then
	cat "$tmp"
else
	cat "$tmp"
	exit 1
fi

if ! grep -E 'Benchmark.*postgres/.+-[0-9]+' "$tmp" >/dev/null; then
	echo "Postgres evidence missing: set TEST_POSTGRES_DSN or run with Docker/testcontainers access." >&2
	exit 1
fi

cp "$tmp" "$BENCH_OUT/postgres-evidence.txt"
echo "wrote $BENCH_OUT/postgres-evidence.txt"
