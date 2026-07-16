---
name: gofastr-docs
description: Auto-loads when adding, changing, or removing any GoFastr feature or exported API. Encodes the doc topology (README + ARCHITECTURE + framework/docs/content/*.md) and the change→doc mapping. Docs are embedded in the gofastr binary at build time — `gofastr docs` browses them and the MCP `framework_docs_*` tools expose them to agents. Triggers on edits to framework/*.go, core/, battery/, cmd/, kiln/, core-ui/ — and on phrases like "add", "implement", "build", "refactor", "rename", "remove", "deprecate", "new feature", "new endpoint", "new field type", "API change", "expose", "wire up". Goal: docs ship in the same commit as the code, not "later".
---

# GoFastr docs — load this before changing the public surface

If you are about to add, rename, remove, or refactor anything that an
external user (or a future Claude) would discover from the docs, this
skill applies. Docs are part of the change, not a follow-up.

## Doc topology — know what lives where

| File | Role | Update when |
|------|------|-------------|
| `README.md` | Canonical entry point. The "Surfaces" table & quickstart are the source of truth for what the framework advertises. | A new surface is added/renamed/removed. A new auto-generated entity behaviour is added. CLI changes. |
| `core-ui/ARCHITECTURE.md` | UI/runtime contract. Authoritative for SSR/hydration/island/SSE model. | Any change to `core-ui/`, `framework/ui/`, `framework/uihost/`, `runtime.js`, or the `data-fui-*` attribute set. |
| `framework/docs/content/entity-declarations.md` | JSON + Go entity declaration reference. | New field type, new option in `EntityConfig`, new declaration loader, new validator. |
| `framework/docs/content/query-dsl.md` | Query DSL parser surface. | New operator, new clause, new column type, change to DSL grammar. |
| `framework/docs/content/migrations.md` | SQL-file migrate directives + CLI subcommands. | New directive, new CLI subcommand, dialect change. |
| `framework/docs/content/search.md` | `battery/search` interface + backends. | New backend, interface change, new query option. |
| `framework/docs/content/security.md` | Default middleware stack + security headers. | New default middleware, header change, new policy primitive. |
| `framework/docs/content/widgets.md` | `core-ui/widget` builder API. | New widget, new preset, new theme hook. |
| `examples/*/README.md` | Per-example walkthrough. | When the example's behaviour or wiring changes. |

There is no `docs/api-reference/` — the public API is documented via
README's "Surfaces" table + per-feature reference page + the
`framework/doc.go` package doc. Keep these consistent.

## The change→doc mapping (use this as a checklist)

For each kind of change, the listed docs MUST be revisited in the same
commit. "Revisited" means: open the file, decide whether it needs a
change, and either edit it or note in the commit message why no edit
was needed.

**You added a new exported func/type/const in `framework/`:**
- README "Surfaces" table or per-feature section
- The matching `docs/<feature>.md` (or create it if the surface is
  new enough to warrant its own page)
- If the new symbol is referenced from a JSON entity declaration:
  `framework/docs/content/entity-declarations.md`

**You added a new entity field type** (e.g. `schema.JSON`, `schema.Money`):
- `framework/docs/content/entity-declarations.md` — field type table
- README "Declare an entity" example if it changes the canonical shape
- `framework/entity/column.go` column constructors — same PR

**You added a new auto-generated route or endpoint pattern**
(e.g. `_batch`, `_events`, `_search`):
- README's per-entity surfaces table
- A reference page under `docs/` if the surface has its own semantics
- `examples/api-tour/main.go` should exercise it; update the example
  README

**You added a new `EntityConfig` option** (e.g. `SoftDelete`, `MCP`,
`CursorField`, `Endpoints`):
- `framework/docs/content/entity-declarations.md`
- README "Declare an entity (Go)" example if it changes the default
  story

**You changed the query DSL grammar or operator set:**
- `framework/docs/content/query-dsl.md`
- `framework/dsl/dsl.go` parser table — same PR

**You changed migration directives or the migrate CLI:**
- `framework/docs/content/migrations.md`
- `cmd/gofastr/migrate_cmd.go` (+ `migrate_generate.go`) — same PR

**You changed `core-ui/` or `framework/uihost/`:**
- `core-ui/ARCHITECTURE.md` — this is mandatory; the doc is the contract
- If you added a new `data-fui-*` attribute: update the runtime test
  suite as well (rule from `CLAUDE.md`)

**You added/changed a battery package (`auth`, `cache`, `email`,
`queue`, `search`, `storage`):**
- The matching `docs/<battery>.md` (create if missing)
- README "battery/" section if the package list changed

**You added a new CLI subcommand:**
- README "cmd/gofastr — CLI" section
- A reference page under `docs/cli-<subcommand>.md` if non-trivial

**You ran an architecture review / security review:**
- Findings become fixes with pinning tests in the same pass; the
  keep/flip/delete rationale goes in the commit message and a comment
  beside the surviving test. No review-log or ledger files — git
  history is the record.

**You removed or renamed an exported symbol:**
- grep the docs for the old name and replace
- `git grep '<OldName>' README.md docs/ examples/`
- Don't leave dangling code samples

**You landed a BREAKING change / you're cutting a release:**
- CHANGELOG.md entry (BREAKING items called out explicitly)
- `cmd/gofastr/upgrades.yml` — every release PR bumps `through`; a
  release with BREAKING or migration-relevant changes also adds its
  entry (one-line guidance, optional `detect` regex; this parser has
  no block scalars). `TestUpgradeRegistryThroughMatchesChangelog`
  gates it against the CHANGELOG's latest heading.
- SECURITY.md "Supported versions" — the latest-minor line
- If the release adds host-facing surface (a new battery, uihost
  option, or CLI capability a host-app agent should reach for):
  `.claude/skills/gofastr-host/SKILL.md` recipe table + trigger
  phrases, then copy to `cmd/gofastr/embedded/gofastr-host-skill.md`
  (`TestEmbeddedHostSkillMatchesRepo` pins the sync)

## Hard rules

1. **Doc updates ship in the same commit as the code change.** Not a
   follow-up PR, not a TODO. If the PR is rejected for doc reasons it
   gets fixed in place, not deferred.

2. **Don't invent documentation for code that doesn't exist yet.** Read
   the actual implementation before writing the doc. Stale or wrong
   docs are worse than missing ones.

3. **Don't document private implementation details.** The reference
   pages describe the public surface a user touches: exported types,
   exported funcs, HTTP routes, CLI flags, JSON declaration fields,
   `data-fui-*` attributes. Skip internal helpers.

4. **No time estimates, no roadmaps in reference docs.** Status
   ("pre-alpha") goes in the README only, once. Reference pages
   describe what is true now.

5. **One canonical example per concept.** If a snippet exists in the
   README, the reference page should link to or extend it, not
   re-state a different version that will drift.

6. **Stub pages are a defect.** A 22-line doc that just shows one code
   snippet is not a reference page. Either flesh it out with: full
   API surface + every option + at least one failure mode, or fold
   it into the README and delete the stub.

7. **Don't claim coverage you can't show.** "Production-ready" /
   "battle-tested" / "fully-featured" — never. State what the code
   does, in present tense, with no marketing.

## When you are unsure whether a doc needs updating

Run this mental check:

- Could a user of this library *reach* the thing I just changed by
  reading the docs and following the obvious path? If yes → doc.
- Did I add a new HTTP route, CLI subcommand, JSON field, env var,
  middleware, hook point, or auto-generated artifact? → doc.
- Did I only change internals that nobody outside the package would
  touch? → no doc, but make sure no existing doc *contradicts* the
  new behaviour.

## Anti-patterns observed in this repo (don't repeat them)

- **Stub doc disease.** `framework/docs/content/search.md`, `framework/docs/content/security.md`,
  `framework/docs/content/migrations.md`, `framework/docs/content/query-dsl.md` were each ~20 lines —
  enough to be discoverable but not enough to answer real questions.
  Fix: expand or delete; don't leave half-done.
- **Surfaces drift.** README's "Surfaces" table listed batch endpoints
  and SSE streams that had no reference page anywhere. If the README
  advertises it, `docs/` should cover it.
- **Roadmap drift.** Forward-looking work lives in `ROADMAP.md`.
  Keep it accurate — when an item ships, delete it from the roadmap
  and move per-feature truth into `docs/<feature>.md`.

## Definition of done for a doc-touching PR

- [ ] Every doc listed in the change→doc mapping above has been opened
- [ ] README "Surfaces" table reflects reality
- [ ] No dangling references to removed symbols (`git grep` clean)
- [ ] No new TODO in docs that aren't tracked elsewhere
- [ ] If you created a new `docs/<name>.md`, it has at least:
  a one-paragraph intro, a code sample that runs as-is against the
  current code, the full option/parameter table, and one "common
  mistake" callout
- [ ] If you renamed/removed something, the old name does not appear
  in any doc

Read `CLAUDE.md`, `README.md`, and `core-ui/ARCHITECTURE.md` if you
have not yet this session. They are the source of truth.
