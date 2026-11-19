# Replay suite after the core/ sweeps, 2026-11-19

Opus, `harness` condition, all replay tasks at the head of the core stack
(#810 → #820, `d90a7f8`), two runs per task, six for `node-block-entity`.
Base for the comparison: the previous report,
`2026-11-18-replay-framework-rest-sweeps.md`. Three cells were run again:
two that timed out and so recorded no cost, and one scored broke because
another cell had emptied the shared build cache.

## 1. Is the eval working?

**Yes**, after two harness fixes: a git lock left by a killed process failed 5 of the first run's 10 finished cells, and one cell's `go clean -cache` broke a neighbour's grader (23 cells, $19.25, plus a $9 first run stopped at 10 of 20 cells).

## 2. What is working, and how much

| Opus, harness | previous | this stack |
|---|---|---|
| Reported path fixed | 19/19 | 20/20 |
| Other paths, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths, blind (`form-field-typo` + `dup-column-key`) | 2/12 | 3/12 |
| Other paths, `index-name-collision` / `node-block-entity` / `outbox` | 8/8, 22/24, 6/6 | 8/8, 23/24, 6/6 |
| Docs / upgrade note, blind-entry tasks | 6/6, 5/6 | 6/6, 4/6 |

## 3. What I think

The core sweeps cost nothing measurable: every number held or moved by one cell. The one drop, the upgrade note, is `index-name-collision` missing it in all 3 runs, the same task that missed it before. I would change my mind if a task whose entry names its upgrade step started missing it.

## 4. Next steps

- Add the upgrade-note step to `index-name-collision`'s registry entry, then re-run that task.
- Give the blind twin tasks entries that name a sibling path.
- Merge stack #810 → #820 once you ask.
