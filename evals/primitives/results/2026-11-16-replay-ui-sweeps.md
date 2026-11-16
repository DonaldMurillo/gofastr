# Replay suite after the headless, uihost and ui sweeps, 2026-11-16

Claude Code CLI 2.1.325, Opus, `harness` condition, all six replay
tasks at `3f8b2752` (the stack #744 → #745 → #746 → #747 → #748 → #749:
the #740 follow-ups, owners for the three packages, and their comment
sweeps). Two runs per task, six for `node-block-entity` (its `MinRuns`).
17 cells, $17.05; one more `node-block-entity` cell ($1.31) because one
cell's grading failed on a git error in the harness, not the agent.

| Opus, harness | step 1 | root sweep | this stack |
|---|---|---|---|
| Reported path fixed | 12/12 | 12/12 | 12/12 |
| Other paths fixed, entries written knowing the bugs | 12/12 | 12/12 | 12/12 |
| Other paths fixed, blind, `form-field-typo` and `dup-column-key` | 4/12 | 4/12 | 4/12 |
| Other paths fixed, `node-block-entity`, six runs | 20/24 | 19/24 | 19/24 |
| Updated the docs, blind-entry tasks | 6/6 | 6/6 | 6/6 |
| Added the upgrade note, blind-entry tasks | 6/6 | 6/6 | 6/6 |

`node-block-entity`, six runs each:

| Commit | Runs | Other paths fixed |
|---|---|---|
| step 1, `5d7b1376` | 4, 2, 4, 4, 4, 2 | 20 of 24 |
| root sweep, `b3d4a291` | 2, 4, 2, 4, 3, 4 | 19 of 24 |
| `main` before this stack, `d4a3828f` | 4, 4, 4, 2, 4, 4 | 22 of 24 |
| this stack, `3f8b2752` | 4, 3, 4, 4, 2, 2 | 19 of 24 |

- **The drop from `main` is noise, by construction.** A replay cell is
  cut from the task's own pre-fix commit; `-head` only supplies
  `cmd/primitives`, `internal/primitives`, `primitives/README.md` and
  the graders. Between `d4a3828f` and `3f8b2752` those differ in one
  test file's package list, so both columns ran the same environment.
  Over 24 runs at four commits the task scores 2/4 in 6 of them.
- **Nothing else moved.** Every other number matches step 1 and the
  root sweep exactly.
- **This suite cannot see the sweeps**, as `PLAN.md` notes for step 1:
  none of its tasks touches `framework/headless`, `uihost` or `ui`.
