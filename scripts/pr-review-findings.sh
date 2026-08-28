#!/usr/bin/env bash
# pr-review-findings.sh <pr-number> [--gate]
#
# Collects EVERY review signal on a PR — line comments, review bodies
# (where findings that couldn't attach to a line hide), and issue-level
# comments — from any reviewer, human or bot. This is CLAUDE.md hard
# rule 10 as a command: --paginate everywhere (an unpaginated fetch
# silently stops at the first page), and review BODIES are printed, not
# just counted.
#
# --gate exits 1 while any line-comment thread is unresolved, so it can
# hold a merge until every finding is triaged (resolve the thread or
# reply to it on GitHub).
set -euo pipefail

REPO="${GOFASTR_REPO:-DonaldMurillo/gofastr}"
PR="${1:?usage: pr-review-findings.sh <pr-number> [--gate]}"
GATE="${2:-}"

echo "== line comments (pulls/$PR/comments) =="
gh api --paginate "repos/$REPO/pulls/$PR/comments" --jq \
  '.[] | "• [\(.user.login)] \(.path):\(.line // .original_line // "?")\n  \(.body | split("\n") | map(select(length > 0)) | first // "" | .[0:200])"'

echo
echo "== review bodies (pulls/$PR/reviews) =="
gh api --paginate "repos/$REPO/pulls/$PR/reviews" --jq \
  '.[] | select((.body | length) > 0) | "── [\(.user.login)] \(.state) \(.submitted_at)\n\(.body)"'

echo
echo "== issue-level comments (issues/$PR/comments) =="
gh api --paginate "repos/$REPO/issues/$PR/comments" --jq \
  '.[] | "── [\(.user.login)] \(.created_at)\n\(.body | .[0:600])"'

echo
echo "== thread resolution =="
owner="${REPO%%/*}"; name="${REPO##*/}"
unresolved=$(gh api graphql -f query='
  query($owner: String!, $name: String!, $pr: Int!) {
    repository(owner: $owner, name: $name) {
      pullRequest(number: $pr) {
        reviewThreads(first: 100) {
          nodes { isResolved comments(first: 1) { nodes { path line body author { login } } } }
        }
      }
    }
  }' -F owner="$owner" -F name="$name" -F pr="$PR" --jq \
  '[.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved | not)] |
   (length | tostring) + " unresolved", (.[] | "  ! \(.comments.nodes[0].author.login) \(.comments.nodes[0].path):\(.comments.nodes[0].line // "?"): \(.comments.nodes[0].body | .[0:120])")')
printf '%s\n' "$unresolved"

if [ "$GATE" = "--gate" ]; then
  count=$(printf '%s\n' "$unresolved" | head -1 | cut -d' ' -f1)
  if [ "${count:-0}" != "0" ]; then
    echo
    echo "GATE: $count unresolved review thread(s); resolve or reply before merging."
    exit 1
  fi
  echo "GATE: all review threads resolved."
fi
