# Replay suite after the battery/ sweep, 2026-11-20

Opus, `harness` condition, all replay tasks at the head of the battery
sweep (`4d0ce8f5b`, the four groups merged), two runs per task, six for
`node-block-entity`. Base: `2026-11-20-replay-coreui-sweep.md`.
`dup-column-key` now reads `overlays/uinode-dupkey`, so it compares with
`2026-11-20-dup-column-key-twin.md`. `form-field-typo` was run four more
times after its two cells. No replay task reads the swept code: each cell
is cut from a commit older than the registry and gets a fixed overlay, so
`undecodable-filter` sees the old `battery/admin` and its comments, not
this sweep.

## 1. Is the eval working?

**Yes**: every cell graded (24 cells, $22.61, four of them the `form-field-typo` rerun).

## 2. What is working, and how much

| Opus, harness | after core-ui | after battery |
|---|---|---|
| Reported path fixed | 20/20 | 24/24 |
| Other paths, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths, `form-field-typo` (entry names the sites) | 10/10 | 11/30 (1, 1, 2, 5, 1, 1) |
| Other paths, `dup-column-key` | 0/2 blind, 4/4 once named | 2/2 named |
| Other paths, `index-name-collision` / `node-block-entity` / `outbox` | 8/8, 24/24, 6/6 | 8/8, 20/24, 6/6 |

## 3. What I think

The sweep cost nothing it could show: nothing a cell reads changed but one test's list of owned packages. `form-field-typo` fixes either all of its sibling paths or about one per run, 34 of 60 across the three batches that read this entry, and `node-block-entity` lost two runs at 2 of 4 after scoring 22 to 24 before, so both moves look like run-to-run spread, not the sweep. I would change my mind if `node-block-entity` stayed below 22 of 24 on a rerun.

## 4. Next steps

- Rerun `node-block-entity` six times to see whether 20 of 24 holds.
- Reword `form-field-typo`'s twin to list the entries a form drops, then rerun it four times.
- Regenerate `undecodable-filter`'s overlay from the swept `admin` entries if this suite should measure sweeps (needs your decision).
