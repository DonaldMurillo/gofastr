# Replay suite after the cmd/ tools sweep, 2026-11-26

Opus, `harness` condition, all replay tasks at the head of the sweep of the
nine small `cmd/` tools (`68facf7f4`), two runs per task, six for
`node-block-entity`, and two more for `index-name-collision` after one of
its cells left the reported path unfixed. Base:
`2026-11-20-replay-battery-sweep.md`. The only swept code a cell runs is
`cmd/primitives`, whose hook and stop check the `harness` condition installs;
every other cell is cut from a commit older than the registry.

## 1. Is the eval working?

**Yes**: every cell graded (22 cells, $25.71, two of them reruns).

## 2. What is working, and how much

| Opus, harness | after battery | after cmd/ tools |
|---|---|---|
| Reported path fixed | 30/30 | 21/22 |
| Other paths, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths, `form-field-typo` | 11/30 | 2/10 (1, 1) |
| Other paths, `dup-column-key` / `index-name-collision` / `outbox` | 2/2, 8/8, 6/6 | 2/2, 12/12, 6/6 |
| Other paths, `node-block-entity` | 20/24, rerun 22/24 | 22/24 |

## 3. What I think

The sweep cost nothing I can see: the one unfixed cell fixed all four sibling paths and missed the boot check, and both reruns fixed everything. `form-field-typo` is still the noisiest task, from one sibling path to all five per run. The swept stop check worked: it asked about sibling paths in 17 of the first 20 cells and sent 3 back on a failing test. I would change my mind if the reported path keeps slipping on the next batch.

## 4. Next steps

- Reword `form-field-typo`'s twin to list the entries a form drops, then rerun it.
- Run the suite again after the first `cmd/gofastr` sweep PR.
