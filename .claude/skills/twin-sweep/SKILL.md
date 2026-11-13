---
name: twin-sweep
description: TEMPORARY (2026-11). Changes how GoFastr bug hunts are run. (1) Twins: find places where one rule is written twice (Postgres vs SQLite, HTTP vs MCP vs Go API, SSR vs runtime JS, list vs get, JSON vs form vs multipart, memory vs DB store), then merge each pair into one implementation or pin it with a parity test. (2) Density: every hunt round records which packages it read and what it found, and the next round reads unread and changed packages first, while retired packages are skipped. Load it when planning, briefing, validating or fixing a bug hunt or bug bash, when writing a hunt LEDGER.md, or when asked about twins, parity drift, hunt coverage or bug density.
---

# Twin sweep and hunt density

Temporary skill. Delete it (and fold `density.sh` into `scripts/` if it
earned its keep) once the exit criteria at the bottom are met.

## Why it exists

Round 11 (PR #718) found 28 bugs. About 14 of them had the same shape: a
rule held on one path and was missing on its twin. Examples:
- crud Get vs List query parsing (F2-02)
- `entity_form` vs `entity_list` checks (F1-02)
- SSR stamp vs runtime paint (U2-02)
- `html.TextArea` vs `headless.Textarea` (U2-03)
- the access observer vs the hook/event recover (U4-02)

The November bashes found the same thing ("about half of that corpus was
parity drift", CLAUDE.md). 16 of the 28 came from packages no recent round
had read. So the count measures where a round looked more than how buggy
the code is. This skill goes after both causes: twins become single
implementations, and every hunt records what it read, so the next round
can be planned from that record.

## Where the history lives

- State for this skill, outside git: `~/programming/gofastr-hunt-state/`
  (`$HUNT_STATE` overrides it). It lives on the maintainer's machine on
  purpose: hunts run there, and the ledgers it is built from are not in
  the repo. Without it, `density.sh` has nothing to read.
  - `coverage.tsv`: one row per package per round that read it.
  - `findings.tsv`: one row per accepted or rejected finding.
  - `twins.md`: the twin registry (families, sites, decision, PR).
- Past ledgers, in varying formats:
  - `~/programming/gofastr-bug-bash-2026-10-27.md` (619 findings, by slice)
  - `~/programming/gofastr-bug-hunt-2026-10-31-r4.md` (rounds 4–6)
  - `~/programming/gofastr-bug-hunt-2026-11-01-r7.md`
  - `~/programming/gofastr-bug-hunt-2026-11-02-r8.md` (201 findings, 95 slices)
  - `~/programming/gofastr-bug-hunt-2026-11-{10-r9,12-r10,12-r11}/LEDGER.md`
    plus `slice-*.txt` package lists and `report-*.md` hunter text.
- Hunt threads on this Mac, through T3: search with
  `t3_thread_search "Bug hunt"` and read them with `t3_thread_read`.
  The linked Windows machine has no GoFastr hunt threads (checked
  2026-11-13). Claude Code cloud/web sessions are not reachable from here.
- Memory notes `project_bug_hunt_r*` and `project_bug_bash_2026_10_27`
  hold the decisions and rejections. Don't re-raise a rejected item.

## File formats

Tab-separated with a header row. Rows go in round order, oldest first.

`coverage.tsv`
```
round	base	pkg	depth	majors	minors
r11	23fd7aac1	core-ui/store	full	0	2
```
- `pkg` is a repo-relative directory, `.` for the root.
- `depth` takes one of three values:
  - `full`: the package and its callers were read end to end.
  - `partial`: some files were read.
  - `fix-review`: only a previous round's fix commits were read.
- Only `full` reads count toward retiring a package.
- `base` is the commit the hunters read. Churn is measured from it.
- Write a row for a read that found nothing, too. A clean read is the
  signal that retires a package.

`findings.tsv`
```
round	id	sev	pkg	shape	family	status
r11	F2-02	Minor	framework/crud	twin	crud-query-parse	fixed
```
- `shape` takes one of four values:
  - `twin`: the rule holds on one path and is missing on its twin.
  - `fix-sibling`: an earlier fix missed a sibling input or path.
  - `docs`: a doc describes behaviour that no longer holds.
  - `other`
- `family` names the twin family in `twins.md` (`-` if none).
- `status` is one of `fixed`, `rejected` or `held`.

## The density table

```
.claude/skills/twin-sweep/density.sh            # every package, worst first
.claude/skills/twin-sweep/density.sh shapes     # findings by round × shape
.claude/skills/twin-sweep/density.sh families   # twin families by finding count
```
Status per package:
- `UNREAD`: no read on record. Sorted by size, largest first.
- `REREAD`: read before, but the lines changed since that read's base
  exceed `CHURN_LINES` (default 300) or 20% of the package. Sorted by
  churn.
- `OPEN`: read, and not yet clean enough to retire.
- `RETIRED`: the last two `full` reads found no Major, and the most recent
  one found at most `MINOR_MAX` (default 2) Minors.

Hunts skip `RETIRED` packages, except in fix-review slices that read a
fix landing there.

## Running a hunt round with this skill

1. **Plan from the table.** Run `density.sh` on the hunt's base commit.
   Build slices from `UNREAD` and `REREAD` first, in table order. Add one
   fix-review slice over the previous round's fix commits. Put the table
   excerpt in the brief, so hunters know why they got the package.
2. **Brief the twin families.** Paste `twins.md`'s open families into the
   hunter brief. When a hunter finds a gap on one path, it names the twin
   and checks that path too.
3. **Validate into the files, not only the ledger.** For every slice,
   append one `coverage.tsv` row per package read, even when the read
   was clean. For every finding, append a `findings.tsv` row with shape
   and family. A `twin` finding with no family starts a new family in
   `twins.md`.
4. **Fix with the registry open.** A fixer touching a family's rule reads
   that family's sites first. Then it merges the pair (see below) or
   fixes every site in the same commit and adds the site to the
   family's parity test.
5. **Close the round.** Run `density.sh shapes` and put the round's line
   in the hunt's memory note: count, Majors, share of twin findings,
   packages retired.

## The twin work (separate from hunts)

Work the families in `twins.md` by finding count, highest first
(`density.sh families`). For each family:

1. **List every site** that enforces the rule. The ledger's "A vs B"
   validator notes name the first two. Then grep for the others: the
   same refusal message, the same `i18n.Phrase`, the same helper name
   across dialect/transport files. Sweep by the invariant, not by a name
   ([[feedback_sweep_by_invariant_not_name]]).
2. **Pick one, and record it in `twins.md`:**
   - **Merge (preferred).** One function, which every site calls. Delete
     the copies in the same change, with no shim
     ([[feedback_no_legacy_no_deprecation]]).
   - **Parity test.** Use it when the sites cannot share code (Go vs
     `runtime.js`, Postgres vs SQLite SQL). One table of inputs runs
     through every site and the outcomes are compared. The model is
     `framework/crud/readhook_parity_security_test.go`
     (`TestReadHookParityCrud`). A new site joins by adding a case.
   - **Intentional difference.** Write down the reason, and put a
     one-line comment at each site that points at the other.
3. **Prove the parity test.** Break one site, watch the test fail, then
   put the site back (Hard rule 11). A parity test nobody has seen fail
   is unproven.
4. **Batch.** 4–5 families per PR, one PR per round, never one PR per
   family ([[feedback_batch_prs_hard_rule]]).

Families to seed `twins.md` with (from rounds 10–11):
- **crud query parse:** List, Get, include, fields, where, q, trashed
  (strict decode and repeated-key refusal)
- **query-pair undecodable warning:** entityui lists vs admin Audit and
  Jobs
- **appspec block checks:** `entity_form` vs `entity_list` vs
  `entity_detail`, node vs catalog kinds
- **SSR vs runtime paint:** `store` valueString vs signals.js,
  `noderender` vs `html` builders, `html.TextArea` vs `headless.Textarea`
- **callback recover:** hook, event, access observer, and any other
  `SetObserver`/`On*` registry
- **scaffold name checks:** `new handler` vs `new route`
- **i18n of generated packages:** siteheader vs docpage

## Who does what

- Hunters: GLM read-only, briefs as in rounds 9–11.
- Validation, `coverage.tsv`/`findings.tsv` writes, and `twins.md`: the
  main loop.
- The one-time backfill of rounds 8–11 into the TSVs, and twin-family
  fixes: Opus subagents, at most 2 at once
  ([[feedback_never_sonnet_agents]], [[feedback_claude_subagent_cap_two]]).
- Backfill depth: rounds 9–11 at finding level, since their ledgers are
  tables. Round 8, rounds 4–7 and the 10-27 bash at slice level, with
  `depth` set to `partial` unless the slice heading says the whole
  package was read.

## Exit criteria (delete the skill when all hold)

- `twins.md` has no family left open: each one is merged, pinned by a
  proven parity test, or recorded as intentional.
- Every package over 2,000 lines is `RETIRED`, or was read after its
  last churn.
- Two consecutive rounds found no Major and fewer than 5 twin findings.
