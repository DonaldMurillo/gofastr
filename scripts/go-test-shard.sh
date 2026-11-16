#!/usr/bin/env bash
# go-test-shard.sh K/N PKG [go test flags...] runs shard K of N of one
# package's top-level tests: every name `go test -list` reports, sorted,
# whose index i has i%N == K. K is 0-based, the same k/n spelling as
# GOFASTR_AXE_SHARD. CI splits a package too slow for one runner across N
# matrix rows with it; the N shards together run every test exactly once.
#
# Sorted round-robin, not contiguous ranges: tests that share a name
# prefix tend to share a file and a cost, and round-robin deals them out
# across the shards instead of stacking them on one.
#
# A shard that selects no test exits 1, so a row whose N outnumbers the
# tests cannot pass by running nothing.
#
# Set GOFASTR_TEST_SHARD_LIST=1 to print the shard's names, one per line,
# instead of running them.
set -euo pipefail

if [ $# -lt 2 ]; then
  echo "usage: $0 K/N PKG [go test flags...]" >&2
  exit 2
fi
spec=$1
pkg=$2
shift 2

if ! [[ $spec =~ ^([0-9]+)/([0-9]+)$ ]]; then
  echo "go-test-shard: want K/N, got '$spec'" >&2
  exit 2
fi
k=$((10#${BASH_REMATCH[1]}))
n=$((10#${BASH_REMATCH[2]}))
if [ "$n" -lt 1 ] || [ "$k" -ge "$n" ]; then
  echo "go-test-shard: want 0 <= K < N, got '$spec'" >&2
  exit 2
fi

# -list prints the names and then an "ok" line; Benchmark names are left
# out because -run never runs a benchmark.
listing=$(go test -list '.*' "$pkg")
mapfile -t all < <(printf '%s\n' "$listing" | grep -E '^(Test|Example|Fuzz)[^ ]*$' | LC_ALL=C sort -u)

picked=()
for i in "${!all[@]}"; do
  if [ $((i % n)) -eq "$k" ]; then
    picked+=("${all[$i]}")
  fi
done

if [ "${#picked[@]}" -eq 0 ]; then
  echo "go-test-shard: shard $spec of $pkg selected none of ${#all[@]} tests" >&2
  exit 1
fi

if [ "${GOFASTR_TEST_SHARD_LIST:-}" = 1 ]; then
  printf '%s\n' "${picked[@]}"
  exit 0
fi

echo "go-test-shard: shard $spec of $pkg runs ${#picked[@]} of ${#all[@]} tests" >&2
pattern=$(IFS='|'; printf '%s' "${picked[*]}")
exec go test -run "^(${pattern})\$" "$@" "$pkg"
