#!/usr/bin/env bash
# stack.sh: GitHub stacked PRs over REST only.
#
#   stack.sh create <pr> <pr> [<pr>...]   link open PRs into a stack, bottom to top
#   stack.sh view <pr>                    show the stack a PR belongs to
#   stack.sh merge <pr> [merge|squash|rebase]
#                                         merge the stack up to and including <pr>,
#                                         all or nothing, and wait for the result
#
# `gh stack` does the same, but it also calls GraphQL, which Claude Code
# cloud sessions cannot reach. Each PR must already be based on the one
# below it; `gh stack` or plain `gh pr create --base` sets that up.
set -euo pipefail

REPO="${GOFASTR_REPO:-DonaldMurillo/gofastr}"
usage() { sed -n '2,8p' "$0" >&2; exit 2; }

cmd="${1:-}"; shift || true
case "$cmd" in
  create)
    [ "$#" -ge 2 ] || usage
    prs=$(printf '%s\n' "$@" | jq -R 'tonumber' | jq -sc '{pull_requests: .}')
    gh api -X POST "repos/$REPO/stacks" --input - <<<"$prs" \
      --jq '"stack #\(.number) on \(.base.ref): " + ([.pull_requests[] | "#\(.number)"] | join(" -> "))'
    ;;
  view)
    [ "$#" -eq 1 ] || usage
    gh api "repos/$REPO/stacks?pull_request=$1" --jq \
      'if length == 0 then "#'"$1"' is in no stack" else .[] | "stack #\(.number) on \(.base.ref), open=\(.open)", (.pull_requests[] | "  #\(.number) \(.state)") end'
    ;;
  merge)
    [ "$#" -ge 1 ] && [ "$#" -le 2 ] || usage
    pr="$1"; method="${2:-merge}"
    # The body is read even when the call fails: a 409 means a merge of
    # this stack is already running, and its body names that merge.
    resp=$(jq -nc --arg m "$method" '{merge_method: $m, merge_action: "default"}' |
      gh api -X PUT "repos/$REPO/pulls/$pr/merge-async" --input - 2>/dev/null) || true
    status=$(jq -r '.status // empty' <<<"$resp" 2>/dev/null || true)
    uuid=$(jq -r '.details.uuid // empty' <<<"$resp" 2>/dev/null || true)
    if [ "$status" = merged ] || [ "$status" = enqueued ]; then
      echo "merge: $status"
      exit 0
    fi
    if [ -z "$uuid" ]; then
      echo "merge request refused: ${resp:-no response}" >&2
      exit 1
    fi
    for _ in $(seq 1 60); do
      poll=$(gh api "repos/$REPO/pulls/$pr/merge-async/$uuid")
      status=$(jq -r '.status' <<<"$poll")
      [ "$status" != pending ] && break
      sleep 10
    done
    echo "merge $uuid: $status"
    if [ "$status" != merged ] && [ "$status" != enqueued ]; then
      jq -r '.details.message // .message // empty' <<<"$poll" >&2
      exit 1
    fi
    ;;
  *) usage ;;
esac
