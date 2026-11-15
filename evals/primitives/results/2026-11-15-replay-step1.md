# Replay suite after step 1, 2026-11-15

Claude Code CLI 2.1.325, Opus, `harness` condition, all six replay
tasks, two trials each, at `5d7b1376` (main after #724 to #729: the
`applies` field, the new owners, the guide in `AGENTS.md`, and the stop
check's notices for CSS-only changes). This is the check `PLAN.md` step 1
asks for: the replay numbers must not drop.

Five of the first twelve cells hit a full disk while six sweep agents
were building in the same container, and failed before or during
grading. They were rerun on the same commit once the build cache was
cleared. The table counts the seven clean cells of the first run and the
five reruns: 12 cells, $17.43 in all, $5.54 of it on cells the disk
spoiled.

| Opus, harness | before (2026-11-14) | after |
|---|---|---|
| Reported path fixed | 18/18 | 12/12 |
| Other paths fixed, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths fixed, entries written blind | 10/20 | 12/20 |
| Updated the docs, blind-entry tasks | 6/6 | 6/6 |
| Added the upgrade note, blind-entry tasks | 4/4 | 6/6 |

By task, other paths fixed:

| Task | Run 1 | Run 2 |
|---|---|---|
| `repeated-query-key` (3 paths) | 3/3 | 3/3 |
| `observer-panic` (1 path) | 1/1 | 1/1 |
| `undecodable-filter` (2 paths) | 2/2 | 2/2 |
| `form-field-typo` (5 paths) | 1/5 | 1/5 |
| `node-block-entity` (4 paths) | 4/4 | 4/4 |
| `dup-column-key` (1 path) | 1/1 | 1/1 |

- **Nothing dropped.** Every number held or rose. Step 1 changed the
  tooling the harness condition uses, and the harness still delivers
  what the registry holds.
- **`form-field-typo` is still the weak task.** Both runs fixed the
  reported name and one of the five other entries. Its registry entry
  was written blind and does not name the hidden, read-only,
  server-filled and repeated entries the real fix covered.
- **`observer-panic` updated its docs in one run of two and added no
  upgrade note in either.** The 2026-11-14 runs did not report these
  two habits for this task, so there is nothing to compare them with.

The replay suite cannot see the shorter guide itself (see `PLAN.md`);
`2026-11-15-guide.md` and `2026-11-15-guide-agents.md` measured that,
12 of 12 for both guides.
