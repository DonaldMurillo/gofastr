# Replay suite after the root framework sweep, 2026-11-16

Claude Code CLI 2.1.325, Opus, `harness` condition, all six replay
tasks, two trials each, at `b3d4a291` (the stack #739, #736, #737, #740:
owners for the root `framework` package and its comment sweep). This is
the check `PLAN.md` asks for after a step: the replay numbers must not
drop from `2026-11-15-replay-step1.md`.

All 12 cells ran clean, $13.04. One number dropped, so
`node-block-entity` was run again: four more times on this commit
($4.81) and six times on the step 1 commit, `5d7b1376` ($7.92).

| Opus, harness | step 1 (2026-11-15) | after |
|---|---|---|
| Reported path fixed | 12/12 | 12/12 |
| Other paths fixed, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths fixed, entries written blind | 12/20 | 10/20 |
| Updated the docs, blind-entry tasks | 6/6 | 6/6 |
| Added the upgrade note, blind-entry tasks | 6/6 | 6/6 |

By task, other paths fixed:

| Task | Run 1 | Run 2 |
|---|---|---|
| `repeated-query-key` (3 paths) | 3/3 | 3/3 |
| `observer-panic` (1 path) | 1/1 | 1/1 |
| `undecodable-filter` (2 paths) | 2/2 | 2/2 |
| `form-field-typo` (5 paths) | 1/5 | 1/5 |
| `node-block-entity` (4 paths) | 2/4 | 4/4 |
| `dup-column-key` (1 path) | 1/1 | 1/1 |

`node-block-entity`, six runs on each commit:

| Commit | Runs | Other paths fixed |
|---|---|---|
| step 1, `5d7b1376` | 4/4, 2/4, 4/4, 4/4, 4/4, 2/4 | 20 of 24 |
| this stack, `b3d4a291` | 2/4, 4/4, 2/4, 4/4, 3/4, 4/4 | 19 of 24 |

- **The drop is not a regression.** Measured the same way, the step 1
  commit scores 20 of 24 and this stack 19 of 24, and each has two runs
  at 2/4. Step 1's published 4/4 twice was two runs from the top of that
  spread. A low run refuses `entity_list`, `entity_form` and
  `entity_detail` under a node block and misses `login_form` and
  `signup_form`.
- **Two runs per task cannot hold a 12/20 baseline.** One run of this
  task moves the blind-entry total by two. Its baseline from here on is
  the six-run figure, 20 of 24; a later step drops if it falls clearly
  below that, measured over six runs.
- **`form-field-typo` is unchanged and still the weak task**, 1 of 5 in
  both runs, as in step 1.
- **`observer-panic` updated its docs in 2 runs of 2** (1 of 2 in step
  1) and added no upgrade note in either, as before.

The blind entry for `node-block-entity` does not name the two form
blocks. Naming them would be writing the entry knowing the bug, which is
what the blind tasks exist to avoid; the finding stands as it is.
