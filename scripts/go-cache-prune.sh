#!/bin/bash
# Keep a saved Go build cache to the entries one run used.
#
#   go-cache-prune.sh age   [dir]   right after the cache is restored
#   go-cache-prune.sh prune [dir]   right before it is saved
#
# Go drops a cache entry only after five days without use, and CI saves
# the cache it restored plus what the run added, so every commit that
# rebuilds a widely imported package leaves a generation of objects no
# later run reads. The go-test- entry reached 5.1 GB (2026-11-16) where a
# fresh one is 1.0 GB: 164 s to restore in each of the 18 jobs that read
# it, and with it the lint and race entries no longer fit the repo's
# 10 GB, so their jobs ran cold.
#
# Go marks an entry used by setting its mtime to now, unless the mtime is
# under an hour old. `age` sets every restored entry's mtime to two hours
# ago, so a use always moves it, and leaves a marker dated one hour ago.
# `prune` removes the entries still older than the marker: the ones no
# step read or wrote. Two hours is far inside Go's own five-day limit, so
# its trim removes nothing first.
#
# An entry is a file or directory named *-a or *-d, one level below the
# cache root, as in Go's own trim. A cached executable is a directory
# marked by its own mtime, so entries are aged and removed whole.
#
# dir defaults to ~/.cache/go-build, the path the workflow caches.
set -euo pipefail

mode=${1:-}
dir=${2:-$HOME/.cache/go-build}
marker=${GO_CACHE_PRUNE_MARKER:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/go-cache-prune.marker}

entries() {
	find "$dir" -mindepth 2 -maxdepth 2 \( -name '*-a' -o -name '*-d' \) "$@"
}

size_mb() {
	du -sm "$dir" | cut -f1
}

# ago NOW SECONDS prints a UTC timestamp SECONDS before NOW in touch -t
# form. BSD touch has no -d '1 hour ago'; the two date families spell
# "the epoch N seconds back" differently (BSD -r, GNU -d @N), so the
# probe picks the spelling the host understands. CI runs GNU
# coreutils; dev laptops run BSD tools.
ago() {
	local epoch=$(($1 - $2))
	if date -v-1m +%s >/dev/null 2>&1; then
		date -r "$epoch" +%Y%m%d%H%M.%S
	else
		date -d "@$epoch" +%Y%m%d%H%M.%S
	fi
}

case "$mode" in
age)
	# Use UTC for both formatting and parsing, including the repeated
	# local hour when daylight saving time ends.
	export TZ=UTC0
	now=$(date +%s)
	touch -t "$(ago "$now" 3600)" "$marker"
	if [ ! -d "$dir" ]; then
		echo "go-cache-prune: no cache at $dir, nothing to age"
		exit 0
	fi
	entries -exec touch -c -t "$(ago "$now" 7200)" {} +
	echo "go-cache-prune: aged $(entries | wc -l) entries, $(size_mb) MB"
	;;
prune)
	if [ ! -f "$marker" ]; then
		echo "go-cache-prune: no marker at $marker; run 'age' before the build, or every entry counts as used" >&2
		exit 1
	fi
	if [ ! -d "$dir" ]; then
		echo "go-cache-prune: no cache at $dir, nothing to prune"
		exit 0
	fi
	before=$(size_mb)
	entries ! -newer "$marker" -exec rm -rf {} +
	echo "go-cache-prune: kept $(size_mb) MB of $before MB"
	;;
*)
	echo "usage: go-cache-prune.sh age|prune [dir]" >&2
	exit 2
	;;
esac
