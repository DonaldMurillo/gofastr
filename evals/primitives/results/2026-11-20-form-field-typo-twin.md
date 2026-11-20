# form-field-typo with an entry naming its sibling sites, 2026-11-20

Opus, `harness` condition, `form-field-typo` only, four runs, reading
`overlays/appspec-form` (the blind appspec entries plus one twin naming
the `entity_list` and `entity_form` checks and what the generator and
entityui forms draw). Base: the same task with the blind entries,
`2026-11-20-replay-v097.md`. One cell failed at setup and was run again.

## 1. Is the eval working?

**Yes**: every cell graded (5 cells, $4.83, one of them a rerun of a cell whose setup failed).

## 2. What is working, and how much

| Opus, harness, `form-field-typo` | blind entry | entry names the sites |
|---|---|---|
| Reported path fixed | 2/2 | 4/4 |
| Other paths fixed | 2/10 | 13/20 (5, 5, 2, 1) |
| Runs that saw the twin notice | | 4/4 |

## 3. What I think

Naming the sites helps, but less than it did on the tasks whose entries were written knowing the bugs: every run read the twin, and two still fixed only the typo, reasoning that the request asked for no more. The twin names where the check lives, not which entries the form drops, and I think that is the gap. I would change my mind if a twin that lists the dropped entries (hidden, read_only, server-filled, repeated) still left runs at 1 or 2 of 5.

## 4. Next steps

- Reword the twin to list the entries a form drops, then rerun four times.
- Write the same rule into the live registry when `cmd/` is swept.
