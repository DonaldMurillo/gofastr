# Plan

What this work is for: when someone, human or agent, changes this
repository, they do the whole job, and the repository tells them what
that is at the moment it matters. Three ideas started it: far fewer
comments, the framework's parts and decisions written down as data, and
one way to do each thing.

## What the evals found

They are in `evals/primitives/results/`. In short:

- A test protects a rule. A comment, a line in `CLAUDE.md` or a registry
  entry informs and does not bind.
- Deleting comments cost nothing where a test enforced what they said.
- An agent fixes what it was pointed at and stops. Told at the right
  moment what else a change needs, Opus did it: docs in 6 of 6 runs
  (7 of 12 before), the upgrade note in 4 of 4 (0 of 7 before), the
  other sites of a recorded twin in 12 of 12 (2 of 12 before).
- A registry entry helps only where someone recorded the right thing.
  Entries written without knowing the bugs helped with none of them.

So the registry is a memory of what a change needs, the harness is what
delivers it, and tests are what enforce it.

## What is in place

- The registry format, its linter and lookup (`cmd/primitives`), with
  `framework/crud` described.
- `.claude/settings.json`: deny rules, the same-job hook, the stop
  check.
- The replay eval (`evals/primitives`), six past bugs.
- The two duplicate implementations the registry recorded in
  `framework/crud`, merged.

## What comes next, in order

Each step is its own pull request or series of them. Do not start a
step before the one above it is done.

### 1. Shrink CLAUDE.md

Move each rule out of `CLAUDE.md` and the two `ARCHITECTURE.md` files
into the primitive it governs: an invariant where a test, analyzer or
contract rule enforces it, an `on_change` line where nothing can. Start
with the Hard rules. What stays in `CLAUDE.md` is how to find the
registry and what no primitive can own.

Done when `CLAUDE.md` is under 60 lines and the replay eval on Opus
shows no drop from the numbers above.

### 2. Sweep the comments, one package at a time

Follow the `primitives-sweep` skill. History and narration go. A rule
with a proven test loses its comment. A rule with no test gets the test
first. Exported doc comments, directives and test comments stay. Order:
`framework/crud` first, then the rest of `framework/`, `core/`,
`core-ui/`, `battery/`, `cmd/`, `internal/`.

Done when non-test Go code is under 8% comment lines, from 24% today.
The first sweep adds the script that counts them.

### 3. Bring the known twins in, and merge them

Follow the `twin-sweep` skill. Every open family from the bug-hunt
ledgers becomes a `twins` entry, then a merge. A twin stays only where
code cannot be shared, with a parity test.

Done when `go run ./cmd/primitives lint` counts no twin whose reason is
work not yet done.

### 4. Cover the rest of the framework

A primitive for each part of `framework/`, `core-ui/` and `core/`, each
with its docs and its `on_change` lines. This is what makes the stop
check useful outside `framework/crud`.

Done when `go run ./cmd/primitives for <path>` finds an owner for every
non-test Go file under those three trees.

## Rules for all four

- Use Opus for the work and for the eval runs.
- After each step, rerun the replay eval and add the numbers to
  `evals/primitives/results/`. A step that makes them worse is not done.
- When a bug hunt finds a miss the harness should have caught, add it:
  a twin, an `on_change` line, or a replayed bug in the eval.
- Report an eval with the `eval-report` skill.

## Not planned

- Generating runtime code from the registry. It generates import tests
  and nothing has asked for more.
- A model that judges whether code follows an invariant. The `judge`
  field and `primitives judge` exist; no eval has shown a rule that only
  a judge could hold.
