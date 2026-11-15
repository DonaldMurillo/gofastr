# Guide eval on the AGENTS.md layout, 2026-11-15

Claude Code CLI 2.1.325, Opus. `guide-long` is the 282-line CLAUDE.md
from `main` (`fa3e5077`). `guide-short` is the #729 layout (`011f85c0`):
CLAUDE.md holds only `@AGENTS.md`, and AGENTS.md holds the 51-line guide.
The tasks are the four traps from `2026-11-15-guide.md`, with 3 runs per
condition. That makes 24 cells, $9.01.

## Cells that kept the rule

| Task | Rule it traps | guide-long | guide-short |
|---|---|---|---|
| `hn-instant-pager` | no list math in JS (2) | 3/3 | 3/3 |
| `hn-live-comments` | SSE is push-only (3) | 3/3 | 3/3 |
| `hn-title-style` | one styling surface (7) | 3/3 | 3/3 |
| `runtime-select-attr` | new `data-cui-*` is documented and tested (5) | 3/3 | 3/3 |

None of the transcripts shows contamination. No `guide-long` transcript
names AGENTS.md or a registry id that only the short guide lists. Every
`guide-short` transcript names at least one of them. That also shows the
`@AGENTS.md` import loaded in every short cell.

## What the cells show

- **The import lost nothing.** Both conditions held 12 of 12, the same
  as the run with the guide in CLAUDE.md.
- **The traps are still too easy.** None of the 12 cells on
  `hn-instant-pager` and `hn-live-comments` changed source; most
  answered in five tool calls or fewer.
- **The short guide still sends agents to the registry.** `guide-short`
  cells made 24 registry calls, against 6 for `guide-long`. Tool calls
  were close (127 against 136), and so was cost ($4.61 against $4.39).
- One `runtime-select-attr` `guide-long` cell ran out of time. Its tree
  was graded and held, but its cost was not recorded, so the
  `guide-long` total is low by about $0.90.

## What this does not show

As before, a tie at the ceiling cannot tell the two guides apart. It
shows that moving the guide behind an import does no harm on rules
Opus already follows.
