#!/usr/bin/env bash
# check-branch-protection.sh: the THIRD required-check list.
#
# Three lists decide what CI must pass, and only two of them are guarded:
#
#   1. .github/workflows/ci.yml           — what runs
#   2. scripts/release-required-checks.txt — what a TAG must have green
#   3. GitHub branch protection on main   — what a PR must have green
#
# TestReleaseManifestMatchesCI pins 1 against 2 and works. List 3 lives in
# GitHub settings, so no test in the tree can see it, and it drifts silently
# in both directions:
#
#   protected but not required     a rename orphans the context, and every PR
#                                  hangs on a check that will never report
#                                  again (PR #193 did exactly this)
#   required but not protected     the check is enforced only at release, so
#                                  a PR merges with it red and the tag is
#                                  then blocked by code already on main
#
# The second one is self-defeating: `browser e2e · chromium-ui` exists
# BECAUSE no CI step passed `-tags chromium`, and it went into the manifest
# without going into branch protection, so the check that closed a coverage
# hole was not enforced where code lands. See #348.
#
# Reading protection needs a token with repo-admin scope, which is why this
# is a script run by a human or an admin-token job rather than another case
# in TestReleaseManifestMatchesCI. It fails closed: an unreadable protection
# API is reported as a failure, not skipped, so a missing scope cannot read
# as agreement. Its comparison logic is exercised by
# cmd/gofastr/branch_protection_test.go against a stubbed gh.
#
# Usage:  check-branch-protection.sh [repo] [branch] [manifest]
#   defaults: DonaldMurillo/gofastr  main  scripts/release-required-checks.txt
# Env overrides (tests):  GH_BIN
set -euo pipefail

REPO="${1:-DonaldMurillo/gofastr}"
BRANCH="${2:-main}"
MANIFEST="${3:-scripts/release-required-checks.txt}"
GH_BIN="${GH_BIN:-gh}"

fail() { echo "::error::branch protection: $*" >&2; exit 1; }

[ -f "$MANIFEST" ] || fail "manifest $MANIFEST not found"

# Same parsing as release-gate.sh: one name per line, '#' comments and blank
# lines ignored, names matched byte-for-byte.
required=""
while IFS= read -r raw; do
	line="${raw#"${raw%%[![:space:]]*}"}"
	line="${line%"${line##*[![:space:]]}"}"
	[ -z "$line" ] && continue
	case "$line" in \#*) continue ;; esac
	required+="${line}"$'\n'
done < "$MANIFEST"
[ -n "$required" ] || fail "manifest $MANIFEST lists no required checks"

# A branch with no protection at all returns 404, and a token without admin
# returns 403. Both mean "cannot verify", and neither may pass silently.
if ! protected_raw="$("$GH_BIN" api "repos/${REPO}/branches/${BRANCH}/protection" \
	--jq '(.required_status_checks.contexts // [])[]' 2>&1)"; then
	fail "cannot read protection for ${REPO}@${BRANCH} — a token with repo-admin scope is needed to compare the third list. Response: ${protected_raw}"
fi

# jq prints nothing for an empty array; an unprotected-but-readable branch is
# still a mismatch against a non-empty manifest, and the diff below says so.
protected="$protected_raw"
[ -n "$protected" ] && protected+=$'\n'

missing="$(comm -23 <(printf '%s' "$required" | sort) <(printf '%s' "$protected" | sort))"
extra="$(comm -13 <(printf '%s' "$required" | sort) <(printf '%s' "$protected" | sort))"

if [ -z "$missing" ] && [ -z "$extra" ]; then
	n=$(printf '%s' "$required" | grep -c '')
	echo "branch protection: ${REPO}@${BRANCH} requires exactly the $n checks in $MANIFEST"
	exit 0
fi

echo "::error::branch protection on ${REPO}@${BRANCH} does not match ${MANIFEST}" >&2
if [ -n "$missing" ]; then
	echo "  in the manifest, NOT required by branch protection (a PR can merge with these red):" >&2
	printf '%s\n' "$missing" | sed 's/^/    add:    /' >&2
fi
if [ -n "$extra" ]; then
	echo "  required by branch protection, NOT in the manifest (a rename here hangs every PR):" >&2
	printf '%s\n' "$extra" | sed 's/^/    remove: /' >&2
fi
echo "  Fix in Settings → Branches → main → Require status checks, or via:" >&2
echo "    gh api -X PATCH repos/${REPO}/branches/${BRANCH}/protection ..." >&2
exit 1
