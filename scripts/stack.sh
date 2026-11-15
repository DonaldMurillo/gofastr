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
      '.[]? // . | "stack #\(.number) on \(.base.ref), open=\(.open)", (.pull_requests[] | "  #\(.number) \(.state)")'
    ;;
  merge)
    [ "$#" -ge 1 ] && [ "$#" -le 2 ] || usage
    pr="$1"; method="${2:-merge}"
    uuid=$(jq -nc --arg m "$method" '{merge_method: $m, merge_action: "default"}' |
      gh api -X PUT "repos/$REPO/pulls/$pr/merge-async" --input - --jq '.details.uuid')
    for _ in $(seq 1 60); do
      status=$(gh api "repos/$REPO/pulls/$pr/merge-async/$uuid" --jq '.status')
      [ "$status" != pending ] && break
      sleep 10
    done
    echo "merge $uuid: $status"
    [ "$status" = merged ] || [ "$status" = enqueued ]
    ;;
  *) usage ;;
esac
