# Replay suite after the rest-of-framework sweeps, 2026-11-18

Opus, `harness` condition, all replay tasks at the head of stack #787
(#777 → #786), two runs per task, six for `node-block-entity`. Eight
cells that errored at setup (disk full, held `index.lock`) were run
again. Base: `form-field-typo`, `dup-column-key` and
`index-name-collision` at `origin/main`, two runs each, same
concurrency. New task: `outbox-lagging-consumer`, cut from the real fix
`05aa78e1c` (a lagging consumer's deliveries; the blind entry is the
pre-fix outbox registry).

## 1. Is the eval working?

**Yes**: every cell graded. On the two numbers that fell, main scores
3/12 against the head's 2/12 on the blind twins and 5/6 against 5/6 on
the upgrade note. One cell in twelve is within run-to-run noise (about
$24 at the head, $6.74 at main).

## 2. What is working, and how much

| Opus, harness | main | this stack |
|---|---|---|
| Reported path fixed | 6/6 | 19/19 |
| Other paths fixed, entries written knowing the bugs | | 12/12 |
| Other paths fixed, blind, `form-field-typo` + `dup-column-key` | 3/12 | 2/12 |
| Other paths fixed, `index-name-collision` | 8/8 | 8/8 |
| Other paths fixed, `node-block-entity`, six runs (4,2,4,4,4,4) | 22/24 (earlier) | 22/24 |
| Other paths fixed, `outbox-lagging-consumer` (new) | | 6/6 |
| Updated the docs, blind-entry tasks | 6/6 | 6/6 |
| Added the upgrade note, blind-entry tasks | 5/6 | 5/6 |

## 3. What I think

The sweeps cost nothing measurable. The blind twins sit at 2 to 4 of 12
on every commit, and the one missed upgrade note is the same
`index-name-collision` run 2 at main, so both drops are noise. I would
change my mind if a re-run at the head fell below 2/12 while main held.

## 4. Next steps

- Make the blind twin tasks harder to miss: the registry entry they read names no sibling path.
- Fix the harness's held `index.lock` at cell setup; it still errors cells.
- Merge stack #787 once the review gate passes (needs your ask).
