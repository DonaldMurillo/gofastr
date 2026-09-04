# Claude / agent instructions for the GoFastr repo

**Before writing any UI, runtime, `framework/uihost`, OR code that *emits*
UI, read [`core-ui/ARCHITECTURE.md`](core-ui/ARCHITECTURE.md). Emitting UI
covers the blueprint generator (`cmd/gofastr`), batteries, and anything
that produces markup or CSS. This is mandatory.
Emitting a styled `<div>` is writing UI. A generator that ships CSS is a
generator writing UI badly.**

**Before adding, moving, or extracting anything under `framework/`, read
[`framework/ARCHITECTURE.md`](framework/ARCHITECTURE.md).** It captures the
package layout, the layering rules, the cycle-breaking interfaces
(`entity.Registry`, `db.Executor`), and the recipe for new extractions.

The architecture document is the contract. It captures failure *classes*
caught the hard way so they don't repeat:
- **Navigation**: three wrong attempts before the SSR/island/SSE model
  was written down.
- **Styling/structure ownership**: caught 2026-06 building the Meridian
  flagship. The blueprint generator accreted ~70 bespoke CSS rules
  (`.mrd-*`, `.gofastr-*`, a `BlueprintBaseCSS` string) and hand-rolled
  markup that duplicated, or worked around, components that already
  existed (`ui.Grid`, `ui.Stack`, `ui.ThemeToggle`, the `--font-heading`
  token). The fix was to delete all of it and compose/extend the design
  system. The blueprint now ships **zero** CSS. See Hard rules 7–9.

**Before adding, renaming, or removing any exported API, route, CLI
subcommand, JSON field, or auto-generated artifact, the `gofastr-docs`
skill at `.claude/skills/gofastr-docs/SKILL.md` auto-loads. Docs ship
in the same commit as the code change, not a follow-up. The docs
live in `framework/docs/content/*.md` and are embedded into the
`gofastr` binary at build time. `gofastr docs` browses them; the
MCP tools `framework_docs_list` / `framework_docs_get` /
`framework_docs_search` expose them to agents connected to a live app.**

## TL;DR of the architecture (read the full doc anyway)

- **SSR-first**. Every page is fully server-rendered on initial load.
- **Hydration**, not re-render. `runtime.js` attaches handlers to the
  existing DOM after first paint.
- **Cross-page nav is client-side** with cache. No hard refreshes when
  going from `/a` to `/b`.
- **In-page state changes are islands**: a click fires an RPC, the
  server returns new island HTML, the runtime swaps just that island.
- **Passive freshness is polled, not pushed**: `data-fui-poll` /
  `widget Builder.Poll` for dashboards/counters/statuses. No held
  connection, no fanout dependency.
- **Server-pushed updates** (SSE, the single `/__gofastr/sse` bus) are
  the last resort: presence, collaboration, sub-second updates. Genuine
  background events, never user actions. The full ladder is
  `framework/docs/content/reactivity.md`.
- **The interactive layer is stateless**: state lives in the DB or the
  client signal store, never in server RAM. Sessions are signed tokens
  (`WithSecret` / `GOFASTR_SECRET`); any replica serves any request.

## Hard rules

1. Never make in-page state changes (sort, paginate, expand) into routes.
   They are islands.
2. Never re-implement pagination/sort/filter math in JS. Server-side.
3. Never use SSE to deliver responses to user actions. SSE is push-only,
   lives on the single `/__gofastr/sse` bus (never a bespoke
   `EventSource` on app surfaces. Dev-mode tooling like
   `framework/dev` livereload and kiln's build-mode reload ships its
   own, and that's the whole exception class), and is reserved for
   presence/collab/sub-second semantics. Passive freshness polls
   instead (`data-fui-poll`).
4. Never add `location.href = …` or full reloads as a "fix".
5. Never add new `data-fui-*` attributes without updating
   `core-ui/ARCHITECTURE.md` and the runtime test suite.
6. Never expose an entity holding per-user data via auto-CRUD without
   setting `EntityConfig.Scope.OwnerField`. See
   `framework/docs/content/entity-declarations.md` → "Per-user scoping".
7. **One styling surface.** Generators and apps ship ZERO bespoke CSS and
   ZERO hand-rolled structural markup. ALL styling + layout lives in the
   design system: `framework/ui` components (CSS via
   `registry.RegisterStyle`), `core-ui/app` layouts, `core-ui/style`
   tokens. A surface that needs styling the design system doesn't provide
   is a MISSING component / layout / token: add it *upstream* and compose
   it. Tripwires that mean STOP, you're drifting: a `*BaseCSS` string
   accreting rules; an invented `.mrd-*`/`.gofastr-*` class; setting a CSS
   property where a `var(--*)` token belongs; overriding a component's
   internals from outside instead of giving the component a config/variant.
8. **Survey before you build.** Before hand-rolling any UI markup or CSS,
   `grep` `framework/ui` + `core-ui` for an existing primitive. The
   catalog is large (Hero, Grid, Stack, Cluster, DetailList, Form,
   FormField, AuthCard, SiteHeader, Sidebar, ThemeToggle, Card, Section,
   DataTable, StatCard, charts, PageHeader, …). Reinventing or
   CSS-overriding an existing component is the #1 failure mode here. If
   it's genuinely missing, add it to the design system, not locally.
9. **Pixels, not probes, and dogfood the flagship.** Never claim a UI
   "works / polished / verified" from a DOM dump, a11y tree, or
   computed-style probe: those cannot see layout or composition.
   Screenshot the *rendered* page and read it (use `chromedp` →
   `FullScreenshot` to a PNG if Playwright hangs on the SSE connection).
   For any framework/design-system change, verify `examples/meridian`
   end-to-end: marketing, app, auth, admin, mobile, light + dark. Meridian
   is the design-system completeness canary: a surface there that needs
   CSS the components don't provide is a gap to fix upstream, never a patch.
10. **Read the PR's line-level review comments before merging.** After
    opening a PR and after every push to it, run
    `./scripts/pr-review-findings.sh <N>` — it collects line comments,
    review bodies, and issue comments (paginated) from every reviewer,
    and `--gate` exits non-zero while any review thread is unresolved,
    so triage is enforced, not remembered: reply to each thread with the
    accept/reject disposition, resolve it, and only then merge. The raw
    calls behind it, for when you need them:
    `gh api --paginate repos/DonaldMurillo/gofastr/pulls/<N>/comments`. The
    `--paginate` is load-bearing: without it the fetch silently stops at the
    first page, so a multi-page review (exactly the miss this rule exists to
    catch) looks empty. The line-level review lives there, NOT in
    `repos/DonaldMurillo/gofastr/issues/<N>/comments`, which returns only
    issue-level comments. Read `/pulls/<N>/reviews` too, and read the BODY,
    not just its "Actionable comments posted" count: findings the bot could
    not attach to a line sit there under "Outside diff range comments", and
    PR #206 hid five that way, including a Major on the change set's own
    anti-vacuity gate. A review bot showing `pass`
    in `gh pr checks` means it RAN, not that it found nothing: PR #198
    showed `CodeRabbit pass` while holding 12 unread comments, six of
    them Major. Triage every finding on merit and say which you rejected
    and why. Silence is not triage.

11. **Prove the guard, don't reason about it.** A condition you argued was
    correct is untested until you have watched it fail. Break it, run the
    test, put it back. v0.67.0's review found three guards written that same
    week that nobody had made fail: a DSN check that read a bare key as
    consent nobody gave, a scope exemption where a cross-tenant grant cleared
    an owner refusal, and an anti-vacuity gate that skipped its own assertion
    on a populated table. Each took one mutation to expose and none had cost
    more than a minute to prove.

## Common operations

- **Build / run the example website**: `./scripts/dev-watch.sh` (auto-rebuild + livereload, port `:8082`). Dev-watch writes to `/tmp/` because the watched tree must stay clean.
- **Build canonical binaries**: `make build` (→ `dist/gofastr`, `dist/kiln`) or `make build-all` (also builds every example into `dist/examples/`). The `dist/` directory is the **only** sanctioned build output location and is gitignored.
- **Test all packages**: `go test ./...`.
- **Repo analyzers (type-aware invariants as vet checks)**: `make
  analyze` builds `cmd/vettool` and runs it over the tree; it also runs
  in the pre-commit hook and CI's vet step. Registered: `mapwriter`
  (never range a map while writing output — iterate
  `slices.Sorted(maps.Keys(m))`, so SSR/email/prompt bytes are
  deterministic), `unboundedbody`, `errleak`, `fieldtypeswitch`,
  `reqparamlimit`, `discardmutator`, and the two `hygiene` passes. Each
  analyzer's own doc comment carries the bug that produced it and the
  postures it deliberately stays silent on. New invariants go in
  `internal/analyzers/` ONLY when they need type information;
  pattern-shaped rules belong in the contracts pipeline
  (`framework/contracts`, `gofastr verify`), which already owns bespoke
  CSS, hard navigation, bespoke EventSource, and inline style/script.
  Twelve more arrived with the 2026-09 adversarial probe audit, one per
  bug SHAPE the 419 probes kept finding; each fires on the pre-fix
  site and stays quiet on the fix:
  `controlbytes` (request-derived strings pass a scrub before
  slog/otel/header/stdio sinks — name the helper scrub/sanitize/… or
  byte-index the value), `callbackunderlock` (never invoke func-typed
  fields or map callbacks between Lock and Unlock — snapshot under the
  lock, call outside), `recovercallback` (registry callbacks on
  goroutine/read-loop paths need a recover guard), `compositekey`
  (never key a map or keyed store on a control-character-joined
  concatenation — struct keys or length prefixes), `emitident` (never
  Sprintf an ungated name into an identifier slot of emitted code — Go
  declarations, SQL DDL, CSS string slots, route paths; toCamelCase
  transforms but does not validate), `asciifold` (never key a registry
  lookup on `strings.ToLower`/`ToUpper` — Unicode folding maps
  homoglyphs onto ASCII, ſ → S; refuse non-ASCII first), `laxcoerce` (a
  failed type assertion on a `map[string]any` entry is not absence —
  split presence from type, or return an error), `rootwrite` (writes
  under a root contained by `filepath.Join` + prefix only, with no
  `EvalSymlinks` on the chain, and zip entry names with no
  `path.Clean`), `divlimit` (integer division by a caller-supplied
  limit/pageSize/n with no zero-or-one guard), `intwrap`
  (unsigned→signed conversion and MinInt negation without a dominating
  bound check), `reflectset` (reflect `Set*` on a `Field`-derived value
  with no `CanSet`), and `discardederr` (an error dropped from a
  three-plus-result method call while the kept results march on).
  Five more came from the 2026-09-03 red-probe round, again one per
  repeated bug shape: `worldreadable` (state or secret files and dirs
  created with group/other bits — 0644/0755/os.Create — where the
  repo's discipline is 0600/0700; public artifacts prove it by path
  provenance, never by content guessing), `fixedtmp` (a constant or
  pid-named path under the shared temp root reaching mkdir/create/
  exec; a pid is not entropy, use MkdirTemp/CreateTemp), `secretcompare`
  (a credential-named string compared with ==/!= against another
  credential or an input read; use subtle.ConstantTimeCompare or
  hmac.Equal), `timestampid` (an id/token/session/key minted from
  time.Now() formatted into a string, rand-failure fallbacks included;
  mint from crypto/rand), and `discardeddecode` (`_ = json.Unmarshal`,
  `_ = Decode`, `_ = ParseForm`: the zero value marches on as data).
  The same round widened `unboundedbody` (ParseForm/FormValue with no
  MaxBytesReader on the request), `controlbytes` (SMTP envelope and
  header-line writes, slog attrs built in battery/log, child-stderr
  detail strings, http.Redirect Location; a helper counts as a scrub
  only when its body walks the C0 range), and `recovercallback`
  (interface-method callbacks on host-installed fields and the func
  results they return, on dispatch paths).
  Every registration is wrapped in `allow.Guard`
  (`internal/analyzers/allow`): a site that is the shape ON PURPOSE
  carries `//gofastr:allow(<analyzer>) <why>` on its line or the line
  above, the same marker spelling the contracts pipeline uses; the
  reason is mandatory and a bare marker silences nothing. That is the
  only exception mechanism — never add a per-site silent posture to a
  rule for one caller.
  An analyzer package that is written but not registered must say why
  in `cmd/vettool/main.go` — `TestEveryAnalyzerIsWiredOrExplained`
  fails on one that is neither registered nor explained.
- **Run the adversarial red probes**: `make red-tests` (or
  `scripts/red-tests.sh Red` to run only the probes). Red probes are
  `*_red_test.go` files tagged `//go:build red` that assert the SECURE
  behaviour and fail while the finding is open; `go test ./...` never
  sees them. A fix converts its probe into a permanent
  `*_security_test.go`; a probe that is deleted says why in the commit
  message and beside the sibling test that carries the contract. The raw
  `go test` output is kept at `.gofastr/red-tests.log`.
- **Run the FULL repo suite (build + vet + test, no cache, generous timeout)**: `./scripts/test-all.sh`. Use this before/after large refactors: it covers the slow chromedp suite (`examples/site`) and `kiln/integration`. `RACE=1`, `SHORT=1`, and a trailing package path are all supported.
- **Test the site end-to-end (chromedp)**: `go test ./examples/site/ -run TestE2E`.
- **Clean build artifacts**: `make clean` (wipes `dist/`, `bin/`, `gen/`, `.gofastr/`).
- **Audit no-binaries-committed**: `find . -maxdepth 3 -type f -size +500k ! -path "./.git/*" ! -path "./dist/*" ! -name "*.go" ! -name "*.md"`. Anything in the result is a stray binary in the source tree; either move it to `dist/` or remove it before commit.

## Where to look first

- Reviewing maturity or choosing roadmap work? Check
  [`docs/agent-notes.md`](docs/agent-notes.md) before trusting an older status
  section at face value.
- New UI / any styling decision? It goes in the design system, full stop
  (Hard rules 7–8). Start in `framework/ui/` if it composes intent
  (PageHeader, FormField, DataTable, Hero, AuthCard). Use `core-ui/html`
  if it maps 1:1 to an HTML tag, `core-ui/patterns/` for a composed
  pattern (accordion, tabs, pagination…), `core-ui/app` for layout shells
  (the centered container, sidebar row: see `LayoutBaseCSS`), and
  `core-ui/style` for tokens (incl. `DarkColors`).
- Using a general design skill (`/impeccable`, `/shape`)? Its "implement
  working code with aesthetic detail" means **extend `framework/ui` +
  the theme tokens** here: never inline CSS into a page, an app, or the
  generator. Aesthetics ship as components + tokens, so every surface
  inherits them.
- New island? Use `core-ui/widget` builder.
- Theme tokens? `framework/ui/theme` for the canonical theme;
  `core-ui/style` for the underlying machinery.
- Entity model, columns, relations, validators, EntityDeclaration?
  `framework/entity/`. Most other framework subpackages depend on it.
- HTTP CRUD handler / batch / cursor / stream / upload / typed query /
  MCP tool generator / eager loading / includes? `framework/crud/`.
- Filtering, sorting, pagination, DSL parsing? `framework/filter/`,
  `framework/pagination/`, `framework/dsl/` (each is its own pkg).
- Lifecycle hooks (BeforeCreate/AfterUpdate/etc.)? `framework/hook/`.
- Auto-migration / schema diffing / dialect detection?
  `framework/migrate/`.
- Multi-tenancy, soft delete, RBAC? `framework/{tenant,softdelete,access}/`.
- Cron, events, file-field upload helper, slow-query logger?
  `framework/{cron,event,file,slowquery}/`.
- App lifecycle, plugins, registry, typed hooks? Stay in `framework/`
  root. These are the App spine. See `framework/ARCHITECTURE.md` for
  why each remaining root file is glued to App.
