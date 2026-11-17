# Replay suite after the contracts, entityui, migrate, embed, entity and access sweeps, 2026-11-17

Claude Code CLI, Opus, `harness` condition, all seven replay tasks at
the head of stack #770 (#762 → #769: the #753 follow-ups, owners for
the six packages, and their comment sweeps). Two runs per task, twelve
for `node-block-entity` (two sets of its six `MinRuns`), and the six-run
base measurement at `main`, `b14eabd5`. $28.57 at the head, $9.52 at
the base. Three cells errored on a held `.git/index.lock` and were run
again after the harness fix below; one `node-block-entity` cell ran out
of time and its tree was graded.

| Opus, harness | root sweep | ui sweeps | this stack |
|---|---|---|---|
| Reported path fixed | 12/12 | 12/12 | 14/14 |
| Other paths fixed, entries written knowing the bugs | 12/12 | 12/12 | 12/12 |
| Other paths fixed, blind, `form-field-typo` and `dup-column-key` | 4/12 | 4/12 | 4/12 |
| Other paths fixed, blind, `index-name-collision` (new) | | | 8/8 |
| Other paths fixed, `node-block-entity`, six runs | 19/24 | 19/24 | 14/24, then 20/24 |
| Updated the docs, blind-entry tasks | 6/6 | 6/6 | 8/8 |
| Added the upgrade note, blind-entry tasks | 6/6 | 6/6 | 8/8 |

`node-block-entity`, six runs each:

| Commit | Runs | Other paths fixed |
|---|---|---|
| step 1, `5d7b1376` | 4, 2, 4, 4, 4, 2 | 20 of 24 |
| root sweep, `b3d4a291` | 2, 4, 2, 4, 3, 4 | 19 of 24 |
| ui sweeps, `3f8b2752` | 4, 3, 4, 4, 2, 2 | 19 of 24 |
| `main` before this stack, `b14eabd5` | 4, 4, 4, 2, 4, 4 | 22 of 24 |
| this stack, `144caf3d`, first set | 2, 4, 2, 2, 2, 2 | 14 of 24 |
| this stack, `144caf3d`, second set | 4, 4, 4, 4, 2, 2 | 20 of 24 |

- **The first set's 14 is noise, not a drop.** A cell is cut from the
  task's own pre-fix commit; `-head` only adds `cmd/primitives`,
  `internal/primitives` and `primitives/README.md`. Between `main` and
  this stack those differ in two lines of one test's package list, and
  no agent ran that test. Every agent at both commits hit the same
  failing `TestAuthFormActionFollowsBasePath`; a 2/4 run is one that
  widened the new refusal to the `entity_*` blocks only, a 4/4 run also
  to `login_form` and `signup_form`. The second set, under the same
  conditions as the base, scored 20 of 24; the twelve head runs pool to
  34 of 48.
- **`index-name-collision` is new**, cut from the bug `366d9a40` fixed
  in `framework/migrate`: two tables that each declare an index with
  the same name boot without a word, and only the first gets its index. Its registry entries were written
  by an agent that had not seen the fix. It scored 4/4 in both runs.
- **The harness now waits out and ends what the agent left running.**
  A background `go test` from the agent outlived the CLI and held the
  cell's `.git/index.lock`, so grading failed. The harness now kills
  every process whose working directory is in the cell (Linux) and
  waits up to 30 s for the lock before restoring tests.
- **Nothing else moved**, and as before this suite cannot see the
  sweeps: no task touches the six swept packages except
  `index-name-collision`, which is new.
