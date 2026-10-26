#!/usr/bin/env bash
# tagged-tests.sh TAG PKG... prints a `go test -run` pattern naming the
# tests that exist in the packages only under the build tag: the tests a
# `-tags TAG` run adds to the untagged suite. CI's chromium-ui shard runs
# these alone, since the main test job has already run the rest of each
# package (#598).
#
# A test counts as tagged when `go test -list` reports it with the tag and
# not without it, in the same package, so build constraints such as
# `chromium && !windows` are read by the go tool, not by this script. A
# name that is tagged in one package and untagged in another still lands
# in the pattern; -run then also runs the untagged one, which costs time
# and misses nothing.
#
# Exits 1 when the tag adds no test, so a shard cannot pass by running
# nothing.
set -euo pipefail

if [ $# -lt 2 ]; then
  echo "usage: $0 TAG PKG..." >&2
  exit 2
fi
tag=$1
shift

# list prints "<import path> <test name>" for each test go test -list
# reports. The names come before their package's ok/FAIL/? trailer line.
list() {
  go test -list . "$@" | awk '
    /^(ok|FAIL|\?)[[:space:]]/ { for (i = 0; i < n; i++) print $2 " " names[i]; n = 0; next }
    /^(Test|Example|Fuzz)[A-Za-z0-9_]*$/ { names[n++] = $0 }
  ' | LC_ALL=C sort -u
}

untagged=$(list "$@")
tagged=$(list -tags "$tag" "$@")
names=$(LC_ALL=C comm -13 <(printf '%s\n' "$untagged") <(printf '%s\n' "$tagged") |
  awk 'NF == 2 { print $2 }' | LC_ALL=C sort -u)

if [ -z "$names" ]; then
  echo "$0: -tags $tag adds no test to $*" >&2
  exit 1
fi
printf '^(%s)$\n' "$(printf '%s\n' "$names" | paste -sd '|' -)"
