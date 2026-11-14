# Primitives eval

Does the primitives registry (`primitives/`) carry what code comments
carried? This eval gives a fresh agent the same request against three
versions of the tree and checks what it leaves behind.

| Condition | Tree | What the agent can read |
|---|---|---|
| `comments` | `-base` (default `origin/main`) | the original comments, no registry |
| `registry` | `-head` (default `HEAD`) | the registry, comments swept |
| `bare` | `-head` with the registry, its command and its `CLAUDE.md` paragraph removed | neither |

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

Conditions are `plain` (the report alone) and `nudge` (the report plus
one sentence asking the agent to check other paths for the same defect).
Graders are held-out tests under `cmd/primitives-eval/graders/`, written
so each fails before the real fix and passes after it. A cell is `fixed`
when the reported path passes, and `twins fixed` counts the other paths.

## Run

```sh
go run ./evals/primitives/cmd/primitives-eval -model sonnet -runs 2
```

It needs the Claude Code CLI (`-claude`, default `claude`) signed in, and
a committed `-head`: cells are built with `git archive`, so uncommitted
work is not in them. `-task` and `-condition` narrow the run; both
repeat. `-cell-budget` caps one cell and `-budget` stops new cells once
the run has spent that much, both in USD.

Artifacts land in `dist/primitives-eval/<timestamp>/`: `results.json`,
`RESULTS.md`, and one `<task>-<condition>-<run>.jsonl` transcript per
cell. Workspaces go to a temp directory outside the repository, because
a cell inside it would read this checkout's `CLAUDE.md`.

## How a cell is isolated

- Each cell is a fresh one-commit repository, so the agent cannot read
  this checkout's history, where the deleted comments still are.
- `evals/primitives/` is removed from every cell: it holds the prompts
  and the grading.
- The agent runs with project settings only and no MCP servers.

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
