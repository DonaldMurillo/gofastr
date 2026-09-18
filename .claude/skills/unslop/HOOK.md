# The two guards this plugin ships, and how to turn them off

Installing `dtl-developers` registers two hooks. **They are on by default.** That is a change
from the earlier opt-in design, and it is deliberate: the whole lesson of this plugin is that
a rule nobody is forced to follow does not get followed. `unslop` banned em dashes for months
while 898 of them accumulated in the skills around it.

Plugin installs register both through `hooks/hooks.json`. Repository installs use the host
mechanisms each checkout supports: `.claude/settings.json` for Claude Code and
`.codex/hooks.json` for Codex. Those commands resolve the git root and run the vendored scripts
under `.dtl/tracker/scripts`, with no plugin cache or home-directory dependency.

Both hosts require workspace trust for repository hooks. Codex also asks you to approve the
exact hook definition. A changed hook definition needs approval again. Until approval, the
local `board.py` command works but the raw-write guard is not active.

## `unslop_hook.py`, a Stop hook

Checks the **final assistant message** against the unslop **rules**: em dash, en dash, curly
quote, and emoji outside the seven-glyph allowlist. Fenced code blocks and inline backticks are
exempt, so quoting a log line that really contains an em dash is fine.

**It never checks guidelines.** Length, bold density, heading density, and title shape are not
enforced in chat. A long answer is often correct, you can scroll, and a hook that argued about
answer length would be switched off within a day. Those are gated where they matter, on the way
to GitHub.

Kill switch: `UNSLOP_HOOK=off`.

## `gh_write_guard.py`, a PreToolUse hook on Bash

Refuses a raw `gh` write that would skip the secret and slop gates:

| Blocked | Use instead |
| --- | --- |
| `gh issue create` | `board.py add` |
| `gh issue comment` | `board.py comment` |
| `gh issue edit` | `board.py edit` / `set` / `label` / `check` |
| `gh issue close` | `board.py close` |
| `gh issue transfer` | `board.py transfer` |
| `gh pr create` | `board.py pr` |
| `gh pr comment` | `board.py pr-comment` |
| `gh pr edit` | `board.py pr-edit` |
| `gh pr merge` | `board.py pr-merge` |
| `gh api` POST/PATCH/PUT/DELETE on an issues or pulls path | `board.py edit` / `pr-edit` |

Reads are never blocked: `gh issue list`, `gh issue view`, `gh pr diff`, `gh pr view`,
`gh project`, and any `gh api` GET go through untouched. A read cannot leak a secret or
publish slop.

REST `gh api` writes were unmatched until 0.11.0, and this page said so. That was an honest signpost on a
hole, and a signpost is not a gate: repairing PR data-platform#621's placeholder body meant
`gh api --method PATCH .../pulls/621 -f body=...`, which reaches neither scanner. Now that
`board.py edit` and `board.py pr-edit` exist, the hole has no reason to, so it is matched.
Mutation is inferred from the method flag and from the field flags, because `gh api <path>
-f k=v` POSTs without naming a method. Raw GraphQL mutations are also blocked. `board.py`
can still use GraphQL because its subprocess calls do not pass through the shell hook.

**Two limits worth knowing.**

It blocks you as well as the agent. A PreToolUse hook cannot tell whether a Bash command came
from the model or from you typing `!gh issue comment`. That cost was accepted on purpose,
because the version that can be skipped is the version that produced the 898.

Heredoc bodies and shell comments are stripped before matching, so documenting a blocked
command is not the same as running one. Quoted strings are stripped too, except when checking
`gh api`, where the path is the only part that says which surface is being written.

Kill switch: `GH_WRITE_GUARD=off`.

## Both fail open

Any error, a missing final message or transcript, malformed JSON, or an import failure: exit 0,
silently. A guard that wedges a session is worse than an em dash. `stop_hook_active` is honoured
so the Stop hook fires once per turn rather than nagging in a loop.

## Turning them off

1. **This session:** `export UNSLOP_HOOK=off` or `export GH_WRITE_GUARD=off`.
2. **Permanently for a plugin:** uninstall it, or delete `hooks/hooks.json` from the installed
   copy, which a plugin update will restore.
3. **Permanently for a repository:** remove the managed entries from `.claude/settings.json`
   and `.codex/hooks.json`. The next canonical sync restores them.

If you find yourself reaching for the kill switch often, that is data about a threshold being
wrong, not a reason to leave it off. Say which command you were trying to run.

## A third guard, not shipped here

`board_guard.py` in the `dtl-work` skill is a separate Stop hook that refuses to end a turn
which changed code without updating the board. It is **not** in `hooks/hooks.json`: it arms only
under `/dtl-work`, which is deprecated, and it is registered by hand if at all. It goes when
`dtl-work` does.
