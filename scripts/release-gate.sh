#!/usr/bin/env bash
# release-gate.sh — the publish gate for a release tag.
#
# The ONLY normal way a GitHub release is created here is the release.yml
# workflow, which invokes this script. The supported human/agent flow is:
#
#     merge the release PR to main  →  push the v* tag on the green main head
#
# The tag push triggers the workflow; this gate VALIDATES ONLY (it never
# creates the release — the workflow step after a passing gate does) and
# refuses to publish unless:
#
#   1. SECURITY.md names the minor being released as the supported line
#      (vX.Y.Z tag -> `X.Y.x` in the "Supported versions" section),
#   2. no release for the tag already exists (a manual `gh release create`
#      bypassed the gate, or a prior run already shipped),
#   3. the tag commit IS the current head of main (not a stale re-tag, not an
#      unmerged-PR commit), and
#   4. EVERY required blocking check named in the manifest ran green on that
#      commit's main-push CI run (ci.yml has no v* trigger; check runs attach
#      to the commit, so the main-push run carries them).
#
# Fail-closed by construction: a red / cancelled / skipped / neutral /
# timed-out check, a missing check, a stale or unmerged SHA, a stale
# SECURITY.md, or a pre-existing release all abort before anything is
# published.
#
# Why a script (not inline YAML): the gate is behavioral and must be testable
# locally — GitHub Actions cannot run as a PR check. The logic lives here and
# is exercised by cmd/gofastr/release_gate_test.go against a stubbed gh/git,
# so the contract is pinned by a test, not by hope.
#
# Usage:  release-gate.sh <tag> <repo> <sha> <manifest>
# Env overrides (tests / tuning):
#   GH_BIN         gh binary             (default: gh)
#   GIT_BIN        git binary            (default: git)
#   MAIN_REF       ref to validate on    (default: origin/main)
#   GATE_TIMEOUT   seconds to wait       (default: 3600)
#   POLL_INTERVAL  seconds between polls (default: 30)
#   SECURITY_MD    security policy file  (default: SECURITY.md)
set -euo pipefail

TAG="${1:?usage: release-gate.sh <tag> <repo> <sha> <manifest>}"
REPO="${2:?missing repo (owner/repo)}"
SHA="${3:?missing sha}"
MANIFEST="${4:?missing manifest path}"
GH_BIN="${GH_BIN:-gh}"
GIT_BIN="${GIT_BIN:-git}"
MAIN_REF="${MAIN_REF:-origin/main}"
GATE_TIMEOUT="${GATE_TIMEOUT:-3600}"
POLL_INTERVAL="${POLL_INTERVAL:-30}"
SECURITY_MD="${SECURITY_MD:-SECURITY.md}"

fail() { echo "::error::release gate: $*" >&2; exit 1; }

# --- 0. Load the required-check manifest -------------------------------------
# One check name per line; blank lines and '#' comments are ignored. Names are
# matched EXACTLY (byte-for-byte) against the check runs' `name` field, so the
# list is the single source of truth — a check whose name drifted is "missing".
REQUIRED=()
while IFS= read -r raw; do
	line="${raw#"${raw%%[![:space:]]*}"}"   # trim leading whitespace
	line="${line%"${line##*[![:space:]]}"}" # trim trailing whitespace
	[ -z "$line" ] && continue
	case "$line" in \#*) continue ;; esac
	REQUIRED+=("$line")
done < "$MANIFEST"
if [ "${#REQUIRED[@]}" -eq 0 ]; then
	fail "manifest $MANIFEST lists no required checks"
fi

# --- 1. SECURITY.md must name the minor being released -----------------------
# The "Supported versions" section promises fixes for the latest minor only,
# and it drifts silently on release (it still named 0.63.x after v0.64.0
# shipped). Derive the supported line from the tag (vX.Y.Z -> X.Y.x) and
# require SECURITY.md to contain it. The workflow always invokes this script
# from the checkout root (the scripts/ path could not resolve otherwise), so
# there a missing SECURITY.md fails rather than skips; the one caller NOT at
# the root is cmd/gofastr/release_gate_test.go, which drives the script from
# its own package dir against stub tags — neither path below exists there, so
# the policy check stays out of that harness.
if [ -f "$SECURITY_MD" ] || [ -f scripts/release-gate.sh ]; then
	supported="${TAG#v}"
	supported="${supported%.*}.x"
	if [ ! -f "$SECURITY_MD" ]; then
		fail "$SECURITY_MD not found — the supported-versions policy must exist and name $supported before $TAG ships."
	fi
	if ! grep -qF "$supported" "$SECURITY_MD"; then
		fail "$SECURITY_MD does not name $supported as the supported minor for $TAG — update its 'Supported versions' section, merge that to main, and re-tag."
	fi
fi

# --- 2. A release must not pre-exist -----------------------------------------
# The workflow is the publisher. An existing release means someone ran
# `gh release create` by hand (bypassing this gate entirely — the old flow
# documented exactly that, and the old "already exists" check ran only AFTER
# the CI gate) or a prior run already shipped. Either way: stop loud.
if "$GH_BIN" release view "$TAG" --json tagName >/dev/null 2>&1; then
	fail "a release for $TAG already exists. The workflow must be the only publisher — delete the release (gh release delete $TAG --yes) and let the gate create it; do not run 'gh release create' by hand."
fi

# --- 3. The tag commit must BE the current head of main ----------------------
# Equality is the rule. The ancestor check runs first only to give a sharper
# error for a tag pointing at a commit that never landed on main at all
# (unmerged PR / wrong branch); the equality check then catches a stale re-tag
# that IS on main but behind the head. Both must hold to publish.
if ! "$GIT_BIN" merge-base --is-ancestor "$SHA" "$MAIN_REF" 2>/dev/null; then
	fail "$SHA ($TAG) is not an ancestor of $MAIN_REF — the tag points at an unmerged commit (or a different branch). Merge it to main and re-tag."
fi
main_head="$("$GIT_BIN" rev-parse "${MAIN_REF}^{commit}")"
if [ "$main_head" != "$SHA" ]; then
	fail "$SHA ($TAG) is behind $MAIN_REF (head is $main_head). Re-tag on the current main head."
fi

# --- 4. Every required blocking check must be present + green ----------------
# Fetch the tag commit's check runs and reduce them to one (status, conclusion)
# per name — NEWEST wins, by descending id, so a red re-run of an
# already-green check blocks the release (and a green re-run unblocks a
# previously red one). The reduction happens AFTER paginate concatenates
# every page: --jq streams rows, sort/awk fold them globally. A per-page jq
# group_by would reduce each page independently. Then classify each entry:
#   missing  — no run for that name (race while CI uploads, or a rename)
#   running  — present but not yet completed
#   bad      — completed with a non-success conclusion (failure/cancelled/
#              skipped/neutral/timed_out/action_required/stale) — terminal, no
#              point waiting; CI will not re-run itself.
#   green    — completed + success.
# Terminal-bad aborts immediately. Missing/running wait up to GATE_TIMEOUT so a
# tag pushed the instant CI starts (or a workflow_dispatch re-run while CI is
# mid-flight) can still converge; anything still outstanding at the deadline
# fails naming exactly which checks never arrived.
classify() {
	# $1 = name. Prints one of: green | running | bad:<conclusion> | missing .
	local name="$1" sc status concl
	sc=$(awk -F'\t' -v want="$name" '$1==want {print $2"\t"$3; exit}' <<<"$map_data")
	if [ -z "$sc" ]; then
		echo "missing"; return
	fi
	status="${sc%%$'\t'*}"
	concl="${sc#*$'\t'}"
	if [ "$status" != "completed" ]; then
		echo "running"; return
	fi
	if [ "$concl" != "success" ]; then
		echo "bad:$concl"; return
	fi
	echo "green"
}
deadline=$((SECONDS + GATE_TIMEOUT))
while :; do
	map_data="$(
		"$GH_BIN" api "repos/${REPO}/commits/${SHA}/check-runs" --paginate \
			--jq '(.check_runs // [])[] | [.id, .name, .status, (.conclusion // "")] | @tsv' \
			| sort -t"$(printf '\t')" -k1,1nr \
			| awk -F'\t' '!seen[$2]++ {print $2"\t"$3"\t"$4}'
	)"

	missing=() running=() bad=()
	for want in "${REQUIRED[@]}"; do
		state=$(classify "$want")
		case "$state" in
			green) ;;
			running) running+=("$want") ;;
			missing) missing+=("$want") ;;
			bad:*)   bad+=("${want}[${state#bad:}]") ;;
		esac
	done

	if [ "${#bad[@]}" -gt 0 ]; then
		fail "blocking checks are not green on $SHA: ${bad[*]}"
	fi
	if [ "${#missing[@]}" -eq 0 ] && [ "${#running[@]}" -eq 0 ]; then
		echo "release gate: all ${#REQUIRED[@]} required checks green on $SHA"
		exit 0
	fi
	if [ "$SECONDS" -ge "$deadline" ]; then
		detail=""
		[ "${#missing[@]}" -gt 0 ] && detail+=" missing: ${missing[*]};"
		[ "${#running[@]}" -gt 0 ] && detail+=" still running: ${running[*]};"
		fail "timed out after ${GATE_TIMEOUT}s waiting for blocking checks on $SHA;${detail} re-run this workflow via workflow_dispatch once CI has finished."
	fi
	echo "release gate: waiting on $SHA — ${#missing[@]} missing, ${#running[@]} running of ${#REQUIRED[@]} required checks"
	sleep "$POLL_INTERVAL"
done
