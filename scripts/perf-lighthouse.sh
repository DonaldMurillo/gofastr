#!/usr/bin/env bash
# perf-lighthouse.sh [--runs N] [--out DIR] <name=url>...
#
# First-load numbers for one or more pages under two network/CPU
# profiles, as a table: the median of N Lighthouse runs per page and
# profile. Every comparison in a perf PR (GoFastr before/after, GoFastr
# against another framework's page) goes through this one script so the
# numbers share a method.
#
#   mobile  Lighthouse's default mobile profile: simulated slow 4G
#           (150 ms RTT, ~1.6 Mbps) and a 4x CPU slowdown.
#   slow    a low-end phone on a poor link: 300 ms RTT, 400 kbps and a
#           6x CPU slowdown.
#
# Columns: score, FCP and LCP in seconds, total blocking time, CLS,
# transferred KiB in total and per type, request count, and script
# execution time ("bootup"). Raw Lighthouse JSON is kept in DIR.
#
# Needs node (npx fetches a pinned Lighthouse) and a local Chrome.
#
#   scripts/perf-lighthouse.sh home=http://localhost:8090/ astro=https://astro.build/
set -euo pipefail

RUNS=3
OUT="${TMPDIR:-/tmp}/gofastr-lighthouse"
while [ $# -gt 0 ]; do
	case "$1" in
	--runs | --out)
		[ $# -ge 2 ] || { echo "$1 needs a value" >&2; exit 2; }
		if [ "$1" = --runs ]; then RUNS="$2"; else OUT="$2"; fi
		shift 2
		;;
	--) shift; break ;;
	-*) echo "unknown flag $1" >&2; exit 2 ;;
	*) break ;;
	esac
done
case "$RUNS" in '' | *[!0-9]* | 0*) echo "--runs wants a positive integer, got '$RUNS'" >&2; exit 2 ;; esac
[ $# -gt 0 ] || { sed -n 2,22p "$0" >&2; exit 2; }
mkdir -p "$OUT"
LH="lighthouse@12.8.2"

profile_flags() {
	case "$1" in
	mobile) ;;
	slow) echo "--throttling.rttMs=300 --throttling.throughputKbps=400 --throttling.cpuSlowdownMultiplier=6" ;;
	esac
}

for target in "$@"; do
	name="${target%%=*}"
	url="${target#*=}"
	for profile in mobile slow; do
		for i in $(seq 1 "$RUNS"); do
			# A failed run must not leave an earlier report in its place.
			rm -f "$OUT/$name.$profile.$i.json"
			# shellcheck disable=SC2046 # profile flags are word-split on purpose
			npx -y "$LH" "$url" $(profile_flags "$profile") --quiet \
				--chrome-flags="--headless=new" --only-categories=performance \
				--output=json --output-path="$OUT/$name.$profile.$i.json" >/dev/null 2>&1 ||
				echo "lighthouse failed: $name $profile run $i" >&2
		done
	done
done

node - "$OUT" "$RUNS" "$@" <<'EOF'
const fs = require('fs'), path = require('path');
const [dir, nruns, ...targets] = process.argv.slice(2);
const median = (xs) => { const s = [...xs].sort((a, b) => a - b); return s[Math.floor(s.length / 2)]; };
const kib = (b) => Math.round(b / 1024);
const head = ['page', 'profile', 'score', 'FCP s', 'LCP s', 'TBT ms', 'CLS', 'KiB', 'JS', 'CSS', 'HTML', 'reqs', 'bootup ms'];
const rows = [head];
for (const t of targets) {
  const name = t.split('=')[0];
  for (const profile of ['mobile', 'slow']) {
    // This invocation's runs only: a larger --runs into the same --out
    // earlier leaves higher-numbered reports behind.
    const runs = Array.from({ length: Number(nruns) }, (_, i) => path.join(dir, `${name}.${profile}.${i + 1}.json`))
      .filter((f) => fs.existsSync(f))
      .map((f) => JSON.parse(fs.readFileSync(f)))
      .filter((r) => r.categories && r.categories.performance.score !== null);
    if (!runs.length) { rows.push([name, profile, 'failed']); continue; }
    const m = (fn) => median(runs.map(fn));
    const byType = (type) => m((r) => ((r.audits['network-requests'].details || {}).items || [])
      .filter((i) => i.resourceType === type).reduce((s, i) => s + (i.transferSize || 0), 0));
    rows.push([
      name, profile,
      m((r) => r.categories.performance.score * 100).toFixed(0),
      (m((r) => r.audits['first-contentful-paint'].numericValue) / 1000).toFixed(1),
      (m((r) => r.audits['largest-contentful-paint'].numericValue) / 1000).toFixed(1),
      m((r) => r.audits['total-blocking-time'].numericValue).toFixed(0),
      m((r) => r.audits['cumulative-layout-shift'].numericValue).toFixed(3),
      kib(m((r) => r.audits['total-byte-weight'].numericValue)),
      kib(byType('Script')), kib(byType('Stylesheet')), kib(byType('Document')),
      m((r) => ((r.audits['network-requests'].details || {}).items || []).length),
      m((r) => r.audits['bootup-time'].numericValue || 0).toFixed(0),
    ]);
  }
}
const w = head.map((_, c) => Math.max(...rows.map((r) => String(r[c] ?? '').length)));
for (const r of rows) console.log(r.map((v, c) => String(v ?? '').padEnd(w[c])).join('  '));
EOF
