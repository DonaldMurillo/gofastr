# Replay suite after the battery/ sweep, 2026-11-20

Opus, `harness` condition, all replay tasks at the head of the battery
sweep (`4d0ce8f5b`, the four groups merged), two runs per task, six for
`node-block-entity`. Base: `2026-11-20-replay-coreui-sweep.md`.
`dup-column-key` now reads `overlays/uinode-dupkey`
(`2026-11-20-dup-column-key-twin.md`). `form-field-typo` was run four more
times and `node-block-entity` six more. No replay task reads the swept
code: each cell is cut from a commit older than the registry and gets a
fixed overlay, so `undecodable-filter` sees the old `battery/admin` with
its comments.

## 1. Is the eval working?

**Yes**: every cell graded (30 cells, $30.13, ten of them reruns).

## 2. What is working, and how much

| Opus, harness | after core-ui | after battery |
|---|---|---|
| Reported path fixed | 20/20 | 30/30 |
| Other paths, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths, `form-field-typo` (entry names the sites) | 10/10 | 11/30 (1, 1, 2, 5, 1, 1) |
| Other paths, `dup-column-key` | 0/2 blind | 2/2 named |
| Other paths, `index-name-collision` / `outbox` | 8/8, 6/6 | 8/8, 6/6 |
| Other paths, `node-block-entity` | 24/24 | 20/24, rerun 22/24 |

## 3. What I think

The sweep cost nothing measurable; nothing a cell reads changed except one test's package list. `form-field-typo` swings between one sibling path and all five per run, 34 of 60 across three batches on this entry, so its drop is noise. I would change my mind if it stays near 11 of 30 on the next batch.

## 4. Next steps

- Reword `form-field-typo`'s twin to list the entries a form drops, then rerun it.
- Decide whether to regenerate `undecodable-filter`'s overlay from the swept `admin` entries, so the suite measures sweeps (your call).
