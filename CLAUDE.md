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
  The 2026-09-04 round split the filesystem-root shape in two.
  `rootwrite` widened to the sinks the storage symlink probe slipped
  past (battery/storage Save/Delete): a same-package helper that
  RETURNS the root-joined path counts as root-derived at the caller,
  os.Rename/Link/Symlink DESTINATIONS, os.Remove, and MkdirAll of
  filepath.Dir(<such path>). New `rootread` is the read twin:
  os.Open/ReadFile/Stat/Remove and read-only OpenFile under a
  root-joined path, plus caller-supplied fs.FS reads (.Open,
  fs.ReadFile, fs.Stat) in HTTP-serving functions. The same round's
  reviewer mutation (core/upload Save with both EvalSymlinks removed
  stayed silent behind sanitizeKey) stripped the sanitizer shield from
  `rootwrite` too: in EITHER twin only resolution postures gate —
  EvalSymlinks on the chain or its Dir, symlink-named guards, an
  O_NOFOLLOW flag, or an Lstat+ModeSymlink leaf check — and a
  sanitized join is never one (the upload probes escaped through
  exactly one). `os.Root` is the fix spelling of first resort on
  Go 1.27: os.OpenRoot plus root.Open/ReadFile/Stat/Create/OpenFile/
  Mkdir(All)/Remove/Rename confines reads and writes in the kernel —
  no symlink escape, no TOCTOU — and both rules are quiet on it.
  The 2026-09-04 round widened `emitident` with the JS/TS declaration
  slots (`export const %s`, `readonly %s:`, `interface %s`,
  `type %s`, `function %s(`, `class %s`, and unquoted object keys at
  line start, each demanding code evidence after the name so prose
  stays quiet): the generated client.d.ts emitted camelCased table
  names with only toCamelCase — a transform, not a gate — in front,
  while the .js had already moved to the quoted `this[%q]` spelling;
  the package field memo is now type-qualified (a check on one
  struct's Table field no longer silences a different struct's Table),
  and a composite of constants gates like the constants it holds. The
  same round added `negdur` (`d <= 0` substituting a default, or a
  `d > 0`/`d != 0` decision arming expiry — an ExpiresAt/expiry/
  deadline assignment or a time.After/.Add(d) call — on a
  caller-supplied time.Duration, with or without an accompanying
  "0 means default" arm: an unrejected negative silently becomes the
  strongest setting, default-or-forever; reject `d < 0` or clamp to
  zero/expired first). "Caller-supplied" means parameter-rooted:
  receiver fields and config/options-named struct fields are
  developer configuration (a host footgun, not an attacker-reachable
  inversion) and stay quiet, as do a diverging refusal, a dominating
  negative check — including one inside a package-local validator the
  function calls with the same value (IssueToken →
  validateTokenSpec) — a max(d, 0) clamp, a zero/disabled-sentinel
  substitution, and an override-only-when-positive option setter
  whose skipped arm keeps the pre-set default.
  Three more came from the 2026-09-04 red-probe round, two of them in
  the contracts pipeline because they read SQL strings, not types:
  `GOFASTR1408 absattempts` (never write a retry counter from a
  host-side value — `attempts = $1` in an UPDATE SET; overlapping
  claimants both read N and both write N+1, so the attempt budget
  under-counts. Increment in SQL at claim, `attempts = attempts + 1`,
  the battery/queue spelling, and let settle write state, not
  arithmetic) and `GOFASTR1409 unfencedclaim` (on a table whose
  completion paths match `claim_token = $n`, every claim-state
  UPDATE/DELETE needs its own fence — a token predicate, a
  `claimed_at <= $n` staleness bound, or a terminal status guard; the
  v0.66 fencing reached Ack/Nack and missed `release`). The third is
  the vet analyzer `credfetch`: an http.Client with no CheckRedirect
  carrying a credential-bearing request (a client_secret/token/code
  form, an Authorization header, or a token-endpoint URL) re-sends the
  credential to whatever host a redirect names — refuse redirects the
  way battery/auth oidcNoRedirect does. The same analyzer reports the
  unbounded decode of such a fetch's response
  (`json.NewDecoder(resp.Body)` / `io.ReadAll(resp.Body)` with no
  `io.LimitReader`), which is exactly where `unboundedbody`
  deliberately stays silent. Both postures skip `_test.go` files
  outright (2026-09-04 posture): a test client POSTing a fixture
  code to an httptest.Server is not a credential flow, and a client
  assignment in a test file must not decide a production field's
  verdict — battery/auth/oauth2_test.go:657/:679 kept firing after
  the oidcNoRedirect fix because the test's bare-client field note
  won the merge and the report node pointed into the test file.
  The 2026-09-04 red-probe round grew the browser-runtime lint family
  in `core-ui/check/runtimeshapes.go` to eight: `storagekeyraw` fires
  when a Web-storage key (`localStorage`/`sessionStorage`
  setItem/getItem/removeItem, or a `document.cookie` write) uses a
  data-fui-* attribute value raw — injected markup then writes or
  clobbers any key on the origin; the fix is namespace AND component-
  encode spelled at the sink (`PREFIX + encodeURIComponent(v)`,
  banner.js's dismissKey shape; encoding alone leaves dots and hyphens
  alive, so the prefix is load-bearing). Its live-tree probe is
  `TestStorageKeysEncodeAttrValues` in `core-ui/runtime/
  runtime_security_test.go`. The same round added the generated-code
  gate `TestGeneratedCLIPassesRepoVettool` (`cmd/gofastr`): it
  regenerates a CLI from a fixture spec into a temp module and runs the
  repo vettool over the EMITTED code, plus a direct check that
  terminal-control summaries never ship live — that round's
  control-bytes sink exists only inside the Go source template in
  `generate_cli.go` (printUsage prints `command.summary` raw), so no
  analyzer over this repo can see it.

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
