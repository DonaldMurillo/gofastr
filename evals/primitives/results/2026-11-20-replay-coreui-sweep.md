# Replay suite after the core-ui sweep, 2026-11-20

Opus, `harness` condition, all replay tasks at `d014cde71` (both `core-ui/`
sweep groups merged), two runs per task, six for `node-block-entity`.
Base: `2026-11-20-replay-v097.md`. Five cells were run again because the
disk filled during the first pass (the Go build cache), so they never
started an agent. `form-field-typo` now reads `overlays/appspec-form`, so
it compares with `2026-11-20-form-field-typo-twin.md` rather than the
blind base. No replay task reads `core-ui/`.

## 1. Is the eval working?

**Yes**: every cell graded (25 cells, $22.95, five of them reruns). One rerun ran out of time and its tree was graded, so its cost was not recorded.

## 2. What is working, and how much

| Opus, harness | v0.97.0 | after core-ui |
|---|---|---|
| Reported path fixed | 20/20 | 20/20 |
| Other paths, entries written knowing the bugs | 12/12 | 12/12 |
| Other paths, `form-field-typo` (entry names the sites) | 13/20 | 10/10 |
| Other paths, `dup-column-key` (blind) | 1/2 | 0/2 |
| Other paths, `index-name-collision` / `node-block-entity` / `outbox` | 8/8, 22/24, 6/6 | 8/8, 24/24, 6/6 |
| Docs / upgrade note, same three tasks as before | 6/6, 5/6 | 6/6, 5/6 |

## 3. What I think

The sweep cost nothing measurable: every number held or moved by one cell. `form-field-typo` at 10 of 10 suggests naming the sites is enough more often than four runs showed. I would change my mind if `dup-column-key` stays at 0 of 2 on a rerun.

## 4. Next steps

- Give `dup-column-key` an entry naming its sibling site, as for `form-field-typo`.
- Check free disk before each cell and stop the run early.
- Sweep `battery/` next.
