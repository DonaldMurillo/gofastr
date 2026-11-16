# Replay suite after the root framework sweep, 2026-11-16

Claude Code CLI 2.1.325, Opus, `harness` condition, all six replay
tasks, two trials each, at `b3d4a291` (the stack #739, #736, #737, #740:
owners for the root `framework` package and its comment sweep). This is
the check `PLAN.md` asks for after a step: the replay numbers must not
drop from `2026-11-15-replay-step1.md`.

All 12 cells ran clean, $13.04. One number dropped, so `node-block-entity`
was run four more times on the same commit, $4.81.

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

`node-block-entity` over six runs on this commit: 2/4, 4/4, 2/4, 4/4,
3/4, 4/4, which is 19 of 24.

- **The drop is one task, and it is the task's spread, not the stack.**
  A low run refuses `entity_list`, `entity_form` and `entity_detail`
  under a node block and misses `login_form` and `signup_form`. Nothing a
  replay cell reads changed between the step 1 commit and this one: the
  registry tooling, the harness and its overlays are the same files, and
  a cell starts from the pre-fix commit, so the swept `framework` code
  never reaches it. Six runs on identical inputs range from 2/4 to 4/4.
  Step 1's two runs both landed at the top of that range.
- **Two runs per task cannot hold a 12/20 baseline.** One run of this
  task moves the blind-entry total by two. Compare this task on its
  six-run figure, 19 of 24, from here on.
- **`form-field-typo` is unchanged and still the weak task**, 1 of 5 in
  both runs, as in step 1.
- **`observer-panic` updated its docs in 2 runs of 2** (1 of 2 in step
  1) and added no upgrade note in either, as before.

The blind entry for `node-block-entity` does not name the two form
blocks. Naming them would be writing the entry knowing the bug, which is
what the blind tasks exist to avoid; the finding stands as it is.
