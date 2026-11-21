# dup-column-key with an entry naming its sibling sites, 2026-11-20

Opus, `harness` condition, `dup-column-key` only, four runs, reading
`overlays/uinode-dupkey` (the blind uinode entries plus one twin naming
`DataTableProps.validate`, `renderDataTable` and the two tables that panic
on an empty or repeated column key). Base: the same task with the blind
entries, `2026-11-20-replay-v097.md` and `2026-11-20-replay-coreui-sweep.md`.

## 1. Is the eval working?

**Yes**: every cell graded (4 cells, $3.25).

## 2. What is working, and how much

| Opus, harness, `dup-column-key` | blind entry | entry names the sites |
|---|---|---|
| Reported path fixed | 4/4 | 4/4 |
| Other path fixed (the renderer on a hand-built tree) | 1/4 | 4/4 |
| Runs that saw the twin notice | | 4/4 |

## 3. What I think

Naming the sites closed the gap here: every run read the twin from the hook and fixed the renderer too. The note worked because it said what slips through (an empty or repeated key on a hand-built tree), the lesson `form-field-typo` taught. The evidence is four runs of one task; I would change my mind if a fresh blind task scored like this without its sites named.

## 4. Next steps

- Run the full replay suite after the `battery/` sweep with this overlay in place.
- Keep naming the dropped shapes, not only the sites, in every new twin.
