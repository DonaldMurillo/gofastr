---
name: gofastr-docs
description: Auto-loads when adding, changing, or removing any GoFastr feature or exported API. Encodes the doc topology (README + ARCHITECTURE + framework/docs/content/*.md) and the change→doc mapping. Docs are embedded in the gofastr binary at build time; `gofastr docs` browses them and the MCP `framework_docs_*` tools expose them to agents. Triggers on edits to framework/*.go, core/, battery/, cmd/, kiln/, core-ui/, and on phrases like "add", "implement", "build", "refactor", "rename", "remove", "deprecate", "new feature", "new endpoint", "new field type", "API change", "expose", "wire up". Goal: docs ship in the same commit as the code, not "later".
---

# GoFastr docs: load this before changing the public surface

If you are about to add, rename, remove, or refactor anything that an
external user (or a future Claude) would discover from the docs, this
skill applies. Docs are part of the change, not a follow-up.

## Doc topology: know what lives where

| File | Role | Update when |
|------|------|-------------|
| `README.md` | Canonical entry point. The quickstart, the "The code you don't write" table (auto-generated entity behaviour), and the "The repo in 60 seconds" table (batteries, CLI) are the source of truth for what the framework advertises. | A new auto-generated entity behaviour is added/renamed/removed. A battery or CLI subcommand is added/renamed/removed. |
| `framework/docs/content/README.md` | The curated docs index: it links most reference pages, not all of them. | A file in `framework/docs/content/` is added, renamed or removed. |
| `core-ui/ARCHITECTURE.md` | UI/runtime contract. Authoritative for SSR/hydration/island/SSE model. | Any change to `core-ui/`, `framework/ui/`, `framework/uihost/`, `runtime.js`, or the `data-cui-*` attribute set. |
| `framework/docs/content/entity-declarations.md` | JSON + Go entity declaration reference. | New field type, new option in `EntityConfig`, new declaration loader, new validator. |
| `framework/docs/content/query-dsl.md` | Query DSL parser surface. | New operator, new clause, new column type, change to DSL grammar. |
| `framework/docs/content/migrations.md` | SQL-file migrate directives + CLI subcommands. | New directive, new CLI subcommand, dialect change. |
| `framework/docs/content/search.md` | `battery/search` interface + backends. | New backend, interface change, new query option. |
| `framework/docs/content/security.md` | Default middleware stack + security headers. | New default middleware, header change, new policy primitive. |
| `framework/docs/content/widgets.md` | `core-ui/widget` builder API. | New widget, new preset, new theme hook. |
| `framework/docs/content/reactivity.md` | The reactivity ladder (client signals → RPC → poll → SSE push) + the stateless-interactive-layer contract. | Any change to signals, polling, the SSE bus, session tokens, `WithSecret`, or which rung a surface should use. |
| `framework/docs/content/contracts.md` | `gofastr verify`: the rule catalog, capabilities, config/suppression, baselines, semantic coverage, and the machine-readable output. | A rule is added/renamed/retired, a capability changes, a `verify` flag or exit code changes, the JSON/SARIF shape changes, or a contract MCP tool is added. |
| `examples/site/docs_catalog.go` | The public docs site's curated catalog. Every embedded topic must appear here or `/docs/<slug>` 404s. | **Any** new file in `framework/docs/content/`; there is a test for this, and it is the one people forget. |
| `examples/*/README.md` | Per-example walkthrough. | When the example's behaviour or wiring changes. |

The public API is documented via README's "The code you don't write"
table + the per-feature reference pages in `framework/docs/content/` + the
`framework/doc.go` package doc. Keep these consistent. The repo-root
`docs/` holds agent notes, proposals and reviews, not reference pages.

## The change→doc mapping (use this as a checklist)

For each kind of change, the listed docs MUST be revisited in the same
commit. "Revisited" means: open the file, decide whether it needs a
change, and either edit it or note in the commit message why no edit
was needed.

**You added a new exported func/type/const in `framework/`:**
- README "The code you don't write" table or per-feature section
- The matching `framework/docs/content/<feature>.md` (or create it if the
  surface is new enough to warrant its own page)
- If the new symbol is referenced from a JSON entity declaration:
  `framework/docs/content/entity-declarations.md`

**You added a new entity field type** (e.g. `schema.JSON`, `schema.Money`):
- `framework/docs/content/entity-declarations.md`: field type table
- README "The code you don't write" `app.Entity` example if it changes
  the canonical shape
- `framework/entity/column.go` column constructors, same PR

**You added a new auto-generated route or endpoint pattern**
(e.g. `_batch`, `_events`, `_search`):
- README's "The code you don't write" table
- A page under `framework/docs/content/` updated in the same change;
  a reference page of its own if the surface has its own semantics
- `examples/api-tour/main.go` should exercise it; update the example
  README

**You added a new `EntityConfig` option** (e.g. `SoftDelete`, `MCP`,
`CursorField`, `Endpoints`):
- `framework/docs/content/entity-declarations.md`
- README "The code you don't write" `app.Entity` example if it changes
  the default story

**You changed the query DSL grammar or operator set:**
- `framework/docs/content/query-dsl.md`
- `framework/dsl/dsl.go` parser table, same PR

**You added a contract rule, or changed `gofastr verify`:**
- `framework/contracts/catalog.go`: the rule is data; ID, Why, Fix, and a
  bad/good example pair are all required, and a malformed entry panics at
  init rather than shipping half-documented
- `framework/docs/content/contracts.md`: the capability table and any
  behaviour a reader would be surprised by
- An analyzer claiming a rule that is not in the catalog panics at init.
  The reverse, a catalog rule no analyzer emits, is caught by
  `TestEveryCatalogRuleHasAnAnalyzer` in the analyzers package (it has to
  live there: the catalog deliberately does not import its analyzers, so
  the same test in `framework/contracts` could only ever skip)
- The catalog is checked against itself: no rule may fire on its own good
  example, and a rule's bad example must produce it. An example that needs
  more context than one file can express goes in
  `examplesNeedingMoreContext` WITH the reason
- `README.md` quotes the rule count.
  `TestAdvertisedRuleCountMatchesTheCatalog` fails the build until it
  matches, so adding a rule updates that number. A release entry in
  CHANGELOG.md states the count at that release and is never rewritten

**You added ANY file to `framework/docs/content/`:**
- `examples/site/docs_catalog.go`: add the slug to an intent group, or
  the site's A–Z index links a page that 404s. `TestEveryEmbeddedDocIsInCatalog`
  catches it, but only in the slow `examples/site` suite, so it is easy to
  miss locally

**You changed migration directives or the migrate CLI:**
- `framework/docs/content/migrations.md`
- `cmd/gofastr/internal/migratecmd/migrate_cmd.go` (+ `migrate_generate.go`), same PR

**You changed `core-ui/` or `framework/uihost/`:**
- `core-ui/ARCHITECTURE.md`: this is mandatory; the doc is the contract
- If you added a new `data-cui-*` attribute: update the runtime test
  suite as well (rule from `CLAUDE.md`)

**You added/changed a battery package (anything under `battery/`):**
- The battery's existing reference page in `framework/docs/content/`
  (find it through the docs index; create `<battery>.md` only when the
  battery has no page yet)
- README "The repo in 60 seconds" `battery/` row if the package list
  changed

**You added a new CLI subcommand:**
- `framework/docs/content/cli.md`, which maps every subcommand to its doc
- README "The repo in 60 seconds" `cmd/gofastr` row
- A reference page under `framework/docs/content/` if non-trivial

**You ran an architecture review / security review:**
- Findings become fixes with pinning tests in the same pass; the
  keep/flip/delete rationale goes in the commit message and a comment
  beside the surviving test. No review-log or ledger files; git
  history is the record.

**You removed or renamed an exported symbol:**
- grep the docs for the old name and replace
- `git grep '<OldName>' README.md docs/ framework/docs/content/ examples/`
  (`docs/public-api.md` lists exported symbols and their stability tiers)
- Don't leave dangling code samples

**You landed a BREAKING change / you're cutting a release:**
- CHANGELOG.md entry (BREAKING items called out explicitly). Keep it to
  what changed and what a reader must do, in a few lines, and link the
  PR or docs page for the rest.
- A v0.N0.0 release moves the ten minor versions before it out of
  CHANGELOG.md into `changelog/v0.A0-v0.A9.md`, unchanged except that
  repo-relative links gain `../`, and links the new file from
  CHANGELOG.md's header (`TestChangelogArchiveLayout`).
- `internal/upgrade/`: every release PR bumps `through` in
  `registry.yml`; a release with BREAKING or migration-relevant changes
  also adds `releases/<version>.yml` (one-line guidance, a `find` block
  of typed matchers, or a
  one-line `nodetect` reason when no spelling differs; the parser has
  no block scalars). `TestThroughMatchesChangelog`
  gates it against the CHANGELOG's latest heading. Between releases a
  feature PR may add the note to a pending `releases/<next version>.yml`
  above `through`: exactly one such file, its version the next patch,
  minor or major of `through` (`TestThroughCoversNewestEntry`); the
  release PR keeps or renames it to the version that ships, and every
  file at or below `through` must name a CHANGELOG release. The
  dev-mode retired-markup table (`internal/retired/retired_table.gen.go`)
  copies every kit class or attribute (`ui-*`, `fui-*`, `data-cui-*`,
  `data-fui-*`, `data-hui-*`) a breaking note names, with the note's
  version and change text. Run `go generate ./internal/retired` after
  adding or dropping such a name, editing the change text of a note that
  names one, or renaming a release file that holds one (including the
  pending file the release PR renames). `TestTableIsCurrent` names the
  command when the table is out of date.
- SECURITY.md "Supported versions": the latest-minor line
- If the release adds host-facing surface (a new battery, uihost
  option, or CLI capability a host-app agent should reach for):
  `.claude/skills/gofastr-host/SKILL.md` recipe table + trigger
  phrases, then copy to `cmd/gofastr/internal/agentscmd/embedded/gofastr-host-skill.md`
  (`TestEmbeddedHostSkillMatchesRepo` pins the sync)
- **Publish the GitHub Release: a pushed tag is NOT a release.** After
  the PR merges, tag the merge commit AND run
  `gh release create vX.Y.Z --title "…" --notes-file <the CHANGELOG
  section>`. `git push origin vX.Y.Z` only creates a tag object; the
  repo's Releases page (and anyone watching for releases) shows nothing
  until the Release object exists. The release is not "done" at the
  tag; it's done when `gh release view vX.Y.Z` succeeds and it reads
  `Latest`. (v0.27.0 and v0.28.0 are tag-only for exactly this reason;
  backfill them if the Releases page needs to be complete.)

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
   `data-cui-*` attributes. Skip internal helpers.

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
   "battle-tested" / "fully-featured": never. State what the code
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

- **Surfaces drift.** If the README advertises a behaviour, a page in
  `framework/docs/content/` should cover it.
- **Roadmap drift.** Forward-looking work lives in `ROADMAP.md`.
  Keep it accurate: when an item ships, delete it from the roadmap
  and move per-feature truth into `framework/docs/content/<feature>.md`.

## Definition of done for a doc-touching PR

- [ ] Every doc listed in the change→doc mapping above has been opened
- [ ] README "The code you don't write" and "The repo in 60 seconds"
  tables reflect reality
- [ ] No dangling references to removed symbols (`git grep` clean)
- [ ] No new TODO in docs that aren't tracked elsewhere
- [ ] If you created a new `framework/docs/content/<name>.md`, it has at least:
  a one-paragraph intro, a code sample that runs as-is against the
  current code, the full option/parameter table, and one "common
  mistake" callout
- [ ] If you renamed/removed something, the old name does not appear
  in any doc

Read `CLAUDE.md`, `README.md`, and `core-ui/ARCHITECTURE.md` if you
have not yet this session. They are the source of truth.
