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
# A `TAG` file and a `!TAG` file can both define TestFoo. Both lists then
# hold the name, so the comparison drops it, and the tagged body would
# never run. The pattern therefore also names every test defined in a
# file that compiles only with the tag, kept only where the tagged list
# reports it in that package: an Example with no output comment
# compiles and never runs, and must not stand in for a test.
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

# files prints "<import path> <file>" for each test file go list includes.
files() {
  go list -e -f '{{$p := .ImportPath}}{{$d := .Dir}}{{range .TestGoFiles}}{{$p}} {{$d}}/{{.}}{{"\n"}}{{end}}{{range .XTestGoFiles}}{{$p}} {{$d}}/{{.}}{{"\n"}}{{end}}' "$@" |
    LC_ALL=C sort -u
}

untagged=$(list "$@")
tagged=$(list -tags "$tag" "$@")
# defined prints "<import path> <name>" for each test function declared in
# a test file that compiles only with the tag.
defined=$(LC_ALL=C comm -13 <(files "$@") <(files -tags "$tag" "$@") |
  while IFS=' ' read -r pkg f; do
    sed -nE 's/^func ((Test|Example|Fuzz)[A-Za-z0-9_]*)\(.*/\1/p' "$f" | sed "s|^|$pkg |"
  done | LC_ALL=C sort -u)
names=$({
  LC_ALL=C comm -13 <(printf '%s\n' "$untagged") <(printf '%s\n' "$tagged")
  LC_ALL=C comm -12 <(printf '%s\n' "$defined") <(printf '%s\n' "$tagged")
} | awk 'NF == 2 { print $2 }' | LC_ALL=C sort -u)

if [ -z "$names" ]; then
  echo "$0: -tags $tag adds no test to $*" >&2
  exit 1
fi
printf '^(%s)$\n' "$(printf '%s\n' "$names" | paste -sd '|' -)"
