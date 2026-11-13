#!/usr/bin/env bash
# density.sh: per-package hunt coverage and finding density (twin-sweep skill).
#   density.sh            every package with status, worst first
#   density.sh shapes     findings by round x shape
#   density.sh families   twin families by finding count
# State: $HUNT_STATE (default ~/programming/gofastr-hunt-state) holds
# coverage.tsv and findings.tsv; formats are in SKILL.md.
set -euo pipefail

STATE=${HUNT_STATE:-$HOME/programming/gofastr-hunt-state}
CHURN_LINES=${CHURN_LINES:-300}
MINOR_MAX=${MINOR_MAX:-2}
COV=$STATE/coverage.tsv
FND=$STATE/findings.tsv
REPO=$(git rev-parse --show-toplevel)
cd "$REPO"

case "${1:-table}" in
shapes)
	awk -F'\t' 'NR>1 && $7!="rejected" {
		k=$1; if (!(k in seen)) { seen[k]=1; order[++n]=k }
		c[k,$5]++; t[k]++; if ($3=="Major"||$3=="Critical") m[k]++
	} END {
		printf "round\ttotal\tmajor\ttwin\tfix-sibling\tdocs\tother\ttwin%%\n"
		for (i=1;i<=n;i++) { k=order[i]
			printf "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\n", k, t[k], m[k], c[k,"twin"], c[k,"fix-sibling"], c[k,"docs"], c[k,"other"], (t[k]? 100*c[k,"twin"]/t[k] : 0)
		}
	}' "$FND"
	exit 0 ;;
families)
	awk -F'\t' 'NR>1 && $6!="-" && $7!="rejected" { n[$6]++; r[$6]=r[$6] (r[$6]?",":"") $1":"$2 }
		END { for (f in n) printf "%d\t%s\t%s\n", n[f], f, r[f] }' "$FND" | sort -rn
	exit 0 ;;
table) ;;
*) echo "usage: density.sh [shapes|families]" >&2; exit 2 ;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Package = directory of non-test Go files. kiln and the experimental
# harness are out of hunt scope.
git ls-files -z '*.go' |
	tr '\0' '\n' |
	grep -v '_test\.go$' |
	grep -vE '^(kiln/|framework/experimental/)|(^|/)testdata/|(^|/)vendor/' |
	tr '\n' '\0' |
	xargs -0 wc -l |
	awk '$2!="total" { p=$2; if (p ~ /\//) sub(/\/[^\/]*$/, "", p); else p="."; L[p]+=$1 }
		END { for (p in L) print p "\t" L[p] }' |
	sort >"$tmp/pkgs"

# Per package: reads, last round, last base, last full read (maj, min),
# the full read before it (maj).
awk -F'\t' 'NR>1 {
	p=$3; reads[p]++; lr[p]=$1; lb[p]=$2
	if ($4=="full") { pm[p]=(p in fm)? fm[p] : -1; fm[p]=$5; fn[p]=$6 }
} END {
	for (p in reads) printf "%s\t%d\t%s\t%s\t%s\t%s\t%s\n", p, reads[p], lr[p], lb[p], (p in fm)?fm[p]:-1, (p in fn)?fn[p]:-1, (p in pm)?pm[p]:-1
}' "$COV" | sort >"$tmp/reads"

# Churn (added + deleted non-test lines in the directory) since the last
# read's base. An unknown base counts as unbounded churn.
: >"$tmp/churn"
while IFS=$'\t' read -r p _ _ base _; do
	if [ "$p" = "." ]; then spec=':(glob)*.go'; ex=':(glob,exclude)*_test.go'
	else spec=":(glob)$p/*.go"; ex=":(glob,exclude)$p/*_test.go"; fi
	if c=$(git diff --numstat "$base"..HEAD -- "$spec" "$ex" 2>/dev/null | awk '{s+=$1+$2} END {print s+0}'); then
		printf '%s\t%s\n' "$p" "$c" >>"$tmp/churn"
	else
		printf '%s\t%s\n' "$p" 999999 >>"$tmp/churn"
	fi
done <"$tmp/reads"

awk -F'\t' -v CL="$CHURN_LINES" -v MM="$MINOR_MAX" '
FILENAME==ARGV[1] { R[$1]=$0; next }
FILENAME==ARGV[2] { C[$1]=$2; next }
{
	p=$1; lines=$2
	if (!(p in R)) { printf "UNREAD\t%s\t%d\t0\t-\t-\t-\t-\t%09d\n", p, lines, 999999999-lines; next }
	split(R[p], r, "\t"); reads=r[2]; last=r[3]; fm=r[5]; fn=r[6]; pm=r[7]
	ch=C[p]+0
	if (ch > CL || (lines>0 && ch > lines/5)) { st="REREAD"; key=sprintf("%09d", 999999999-ch) }
	else if (fm==0 && pm==0 && fn<=MM && fn>=0) { st="RETIRED"; key="0" }
	else { st="OPEN"; key="0" }
	printf "%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\t%s\n", st, p, lines, reads, last, ch, (fm<0?"-":fm), (fn<0?"-":fn), key
}' "$tmp/reads" "$tmp/churn" "$tmp/pkgs" |
	awk -F'\t' 'BEGIN { o["UNREAD"]=1; o["REREAD"]=2; o["OPEN"]=3; o["RETIRED"]=4 } { print o[$1] "\t" $0 }' |
	sort -t$'\t' -k1,1n -k10,10 |
	cut -f2-9 |
	{ printf "status\tpkg\tlines\treads\tlast\tchurn\tmajors\tminors\n"; cat; }
