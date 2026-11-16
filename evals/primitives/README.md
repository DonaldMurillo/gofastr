# Primitives eval

Does the primitives registry (`primitives/`) carry what code comments
carried? This eval gives a fresh agent the same request against three
versions of the tree and checks what it leaves behind.

| Condition | Tree | What the agent can read |
|---|---|---|
| `comments` | `-base` (default `origin/main`) | the original comments, no registry |
| `registry` | `-head` (default `HEAD`) | the registry, comments swept |
| `bare` | `-head` with the registry, its command and its guide paragraph (`AGENTS.md`, or `CLAUDE.md` in an older tree) removed | neither |

`bare` is the control: it shows what deleting comments costs when nothing
replaces them. Test files keep their comments in every condition.

## Tasks

Three change requests each ask for something a recorded rule forbids.
The agent is free to refuse, and the prompt says so. A fourth task is a
question about why the code is the way it is.

| Task | The request | The rule it runs into | Graded by |
|---|---|---|---|
| `scope-equality` | make an unknown read-scope operator fall back to equality | `crud.scope/unknown-operator-matches-nothing` | `TestRenderReadScopeOps`, `TestReadScope_EveryOperatorFiltersRealRows` |
| `hook-projection` | let a child hook return rows without the primary key | `crud.read-hooks/child-hook-results-match-by-primary-key` | `TestReattach*`, from `-head` |
| `upload-dimensions` | call `framework/image` from the crud upload path | the `crud.uploads` import ban | `TestPrimitiveImports` (`TestUploadPathDoesNotLinkImageCodecs` at `-base`) |
| `fk-question` | is the missing `relationReachable` gate on foreign-key writes an oversight? | `crud.write/foreign-key-skips-relation-reachable` | four regular expressions over the answer |

A change task is graded on the tree, not on what the agent says. The
harness restores every `_test.go` file to what the condition shipped,
removes test files the agent added, copies in the task's grader files
from `-head`, and runs the named tests. A grader file is held out: every
cell starts with the `-base` copy, so a test written to grade a task is
not something the agent can read. `held`
means they pass; `broke` means one fails or the package no longer
builds. The report also records whether the agent changed source or test
files, and how many tool calls named `primitives/` or `cmd/primitives`.

## The replay suite

`-suite replay` asks a different question: on ordinary work, does the
agent do the whole job? Each task replays a real past bug from this
repository, starting from the commit just before its fix. The prompt
reports one path. The real fix reached others with the same defect.

| Task | Starts at | Reported path | Other paths the real fix reached |
|---|---|---|---|
| `repeated-query-key` | `81850a28^` | a repeated `fields` key on a list | repeated `where` and `q` on a list, `fields` on read-one |
| `observer-panic` | `ae4a44a9^` | a panicking permission observer | a panicking router serve hook |
| `undecodable-filter` | `830f31b8^` | an undecodable filter on the admin audit page | the admin jobs and search pages |
| `form-field-typo` | `5cbb3145^` | a misspelled name in an `entity_form` fields list | the same on an edit form; a hidden, read-only, server-filled or repeated entry |
| `node-block-entity` | `289df96a^` | an `entity_list` under a node block | `entity_form`, `entity_detail`, `login_form` and `signup_form` there, nested or spelled `type:` |
| `dup-column-key` | `765ffef4^` | `Validate` accepting repeated data-table column keys | the renderer panicking on a hand-built tree |

The registry entries for the first three tasks were written knowing the
bugs. Those for the last three were written by a separate agent that was
given the pre-fix code and the format, and not told what the bugs were.

Conditions are `plain` (the report alone), `nudge` (the report plus one
sentence asking the agent to check other paths for the same defect) and
`registry` (the report alone, in a tree given the registry tooling, a
`twins` entry naming the paths, and the pointer in the guide; the
entries are under `cmd/primitives-eval/overlays/`), `hook` (the same
tree, plus the registry's hook, so the notice arrives without the agent
asking) and `harness` (the `hook` tree plus the stop check: tests for
the changed packages, the question about sibling paths, and a changed
part's `on_change` lines and untouched docs).
Graders are held-out tests under `cmd/primitives-eval/graders/`, written
so each fails before the real fix and passes after it. A cell is `fixed`
when the reported path passes, and `twins fixed` counts the other paths.

## The guide suite

`-suite guide` asks whether moving the Hard rules out of `CLAUDE.md` and
into the registry keeps agents to them. Each task asks for something a
Hard rule forbids, in a tree the rule governs. Both conditions run the
full harness (the registry's hook and stop check):

| Condition | Tree |
|---|---|
| `guide-long` | `-base`: the long `CLAUDE.md` that spelled every rule out |
| `guide-short` | `-head`: the short `CLAUDE.md`, the rules as registry entries reaching their trees through `applies` |

| Task | The request | Hard rule | Graded by |
|---|---|---|---|
| `hn-instant-pager` | page the hn story list in the browser | 2 (no list math in JS), 1 | no changed `.js` file; no GOFASTR1004/1801/1802/1804/1805 in a changed file; the hn tests |
| `hn-live-comments` | stream new comments to an EventSource of its own | 3 (one SSE bus, push only) | no GOFASTR1802/1803/1805 in a changed file; the hn tests |
| `hn-title-style` | bold titles and a grey meta line | 7 (one styling surface) | no GOFASTR1801/1804/1806/1807/1810/1817/1823 in a changed file; the hn tests |
| `runtime-select-attr` | a new `data-cui-select-on-focus` attribute | 5 (attributes documented) | `TestRuntimeAttrsAreDocumented`; `unfixed` when no runtime file gains the attribute |

`held` means the rule held and the tests pass; `broke` names each rule
found, as `rule@file`. Findings come from `gofastr verify --changed`,
built once from this checkout so both conditions are graded by the same
catalog. Refusing, or meeting the need another way (an owned style
sheet, `data-cui-poll`, the `/__gofastr/sse` bus), holds.

## Run

```sh
go run ./evals/primitives/cmd/primitives-eval -runs 2
```

It needs the Claude Code CLI (`-claude`, default `claude`) signed in, and
a committed `-head`: cells are built with `git archive`, so uncommitted
work is not in them. `-task` and `-condition` narrow the run; both
repeat. `-runs` is the trial count for every task, except that
`node-block-entity` always gets at least six: one of its runs moves the
blind-entry total by two, so its baseline is a six-run figure (20 of 24,
`results/2026-11-16-replay-root-sweep.md`). `-task-runs <task>=<n>`
sets one task's count outright and repeats. `-cell-budget` caps one cell and `-budget` stops new cells once
the run has spent that much, both in USD.

Artifacts land in `dist/primitives-eval/<timestamp>/`: `results.json`,
`RESULTS.md`, and one `<task>-<condition>-<run>.jsonl` transcript per
cell. Workspaces go to a temp directory outside the repository, because
a cell inside it would read this checkout's `CLAUDE.md`.

## How a cell is isolated

- Each cell is a fresh one-commit repository, so the agent cannot read
  this checkout's history, where the deleted comments still are.
- `evals/primitives/` is removed from every cell: it holds the prompts
  and the grading. So is `.claude/settings.json`: a cell must not
  inherit this repository's hooks and deny rules. The `hook` and
  `harness` conditions write their own.
- The agent runs with project settings only and no MCP servers.
- Tests are restored, and changes measured, against the cell's first
  commit. An agent that commits inside the cell changes neither.

A cell is not a sandbox. It is an agent with a shell on the machine that
runs the eval. Every cell gets a small deny policy (no pushes, no `ssh`
or `gh`, no reading of SSH keys or cloud credentials), which stops the
usual spellings and nothing more. Run the eval somewhere disposable: a
cloud session or a container, not a workstation with credentials on it.

`-budget` stops new cells from starting. Cells already running finish,
so a run can pass it by `-concurrency` times `-cell-budget`.

## What it does not show

- Four tasks in one package. The rules were picked because their
  enforcing tests are proven by mutation, not because they are typical.
- A `held` cell does not say why it held: an agent that never found the
  code scores the same as one that refused for the right reason. Read
  the transcript.
- The question task is graded by pattern match. It rewards naming the
  right facts, not being right.
- Cost and tool-call counts vary between runs. Use `-runs 3` or more
  before comparing them.
- A cell stopped by its timeout records no cost: the CLI reports what a
  session cost in its last message, and a stopped session never sends
  one. A run's total leaves those cells out.
