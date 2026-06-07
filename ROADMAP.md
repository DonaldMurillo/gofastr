# GoFastr — Roadmap

Forward-looking work that isn't built yet (or isn't finished yet). Shipped
features live in `framework/docs/content/<feature>.md` (also embedded into
the binary — run `gofastr docs` to browse) and the two architecture
documents (`framework/ARCHITECTURE.md`, `core-ui/ARCHITECTURE.md`).

Each section ends with a status note. When something ships, delete it from
here and add the `docs/<feature>.md` it now belongs in.

---

## 1. Route groups

**Status:** implemented (2026-05-21) — `framework/routegroup/`.

Cluster routes under a shared prefix, middleware stack, and access policy
without repeating boilerplate at each registration site. Groups nest and
compose with the existing router + middleware pipeline.

**Implementation**

- `framework/routegroup/` — `RouteGroup` type with prefix, middleware, access policy, OpenAPI tag, MCP namespace
- `App.Group(prefix, opts...)` creates a group on the app's router
- `App.GroupEntity(group, name, config)` registers an entity into a group
- `core/router.Group()` already supported prefix + middleware nesting
- Groups support `WithAccess()`, `WithOpenAPITag()`, `WithMCPNamespace()`, `WithMiddleware()`
- Nested groups compose prefixes and middleware (outer → inner)

**Files**: `framework/routegroup/group.go`, `framework/routegroup/group_test.go`, `framework/reexports_routegroup.go`

**Sketch**

- `Group(prefix, opts...)` on the App / router surface
- `g.Use(mw...)` applies group-scoped middleware to all children
- Nested groups inherit parent prefix and middleware
- Group-scoped access policy that composes with entity access
- Auto-CRUD entities mount cleanly inside a group
- OpenAPI tags and MCP tool namespacing reflect the group structure

**Why now:** precondition for screen groups (§2) and API versioning (§3).

**Acceptance**

- Routes registered inside a group are reachable at `<parent-prefix><group-prefix><route>` ✓
- Middleware order outer→inner, matches direct registration semantics ✓
- ~~Removing a group cleanly unregisters all child routes~~ — OUT OF SCOPE (2026-05-22). The underlying core router has no route-removal API; hot route-swap is not a normal Go web framework primitive and is not worth the refactor cost. Routes live until process exit.
- OpenAPI spec groups routes under the same tag as their parent group ✓ (`OpenAPITag` accessor + nested namespace composition verified by `TestNestedMCPNamespaceComposes`)

**Production hardening (2026-05-22):** nested access composition, access-before-middleware ordering, and nested MCP namespace composition now have explicit tests.

---

## 2. Screen groups and sub-layouts

**Status:** implemented (2026-05-21) — `core-ui/app/screen_group.go`.

Screens-side analogue of route groups. A group declares a shared layout
(header / sidebar / chrome) that wraps every child screen. Layouts nest,
and the SSR-first + island hydration contract from `core-ui/ARCHITECTURE.md`
is preserved: navigating between siblings inside the same layout swaps only
the inner content region, not the layout shell.

**Implementation**

- `core-ui/app/screen_group.go` — `ScreenGroup` type with layout, prefix, nested sub-groups
- `ScreenGroup.Screen()` registers screens with resolved paths and inherited layouts
- `ScreenGroup.SubGroup()` creates nested groups
- `ComposeLayouts()` wraps content from innermost to outermost
- `data-fui-screen-group` attribute on layout wrappers for runtime DOM stability
- Runtime: `findCommonScreenGroup()` checks if current and target paths share a group
- Runtime: `swapScreenGroupContent()` swaps only inner content during sibling nav

**Files**: `core-ui/app/screen_group.go`, `core-ui/app/screen_group_test.go`, runtime.js

**Sketch**

- `ScreenGroup(prefix, layout, opts...)`
- `Layout` component contract: receives children, can host islands
- Nested groups → composed layouts, outermost → innermost
- Sibling-screen nav swaps inner content; cross-group nav swaps the
  appropriate layout boundary
- Islands declared inside a layout persist across child navigations
- Page-data and breadcrumbs flow up through the layout chain

**Acceptance**

- Initial load fully SSR-rendered, including all wrapping layouts ✓
- Sibling-screen nav does not re-render the parent layout (DOM-stable) ✓ — `findCommonScreenGroup` picks deepest matching prefix (longest-prefix-wins) so the innermost preserved layout is the right one.
- Layout islands keep their state across child navigations ✓ — by virtue of the DOM swap only touching the inner content region.
- chromedp e2e asserts layout DOM node identity is preserved across sibling navigations — DEFERRED to a follow-up branch with a real demo screen group; the SSR contract is now covered by `TestNestedGroupRendersNestedLayoutShells`.

**Production hardening (2026-05-22):**
- Nested groups now actually nest at SSR time. Previously, `g.Screen()` set `screen.Layout = g.layout` (single layout, no composition), so a sub-group's screen lost the outer group's layout shell entirely. Now `Screen` carries an unexported `group` reference and the renderer calls `ComposeLayouts(screen.group, content)`, emitting nested `data-fui-screen-group` markers from outer→inner. Verified by `TestNestedGroupRendersNestedLayoutShells`.
- `findCommonScreenGroup` runtime fix (deepest match) — fixed in the prior hardening pass; verified by `TestScreenGroupPicksDeepestMatch`.

---

## 3. API versioning

**Status:** EXPERIMENTAL (2026-05-22) — moved to `framework/experimental/apiversions/`. Speculative without a real v1↔v2 in-tree case study to shape the projection machinery (include/exclude/rename). Revisit when a real consumer surfaces the shape.

First-class versioning for the auto-generated HTTP/CRUD surface, MCP tools,
and OpenAPI spec. Multiple versions of the same entity coexist; deprecations
are explicit and machine-readable.

**Implementation**

- `framework/apiversions/` — `Version` type wrapping route groups with version metadata
- URL prefix scheme (`/v1`, `/v2`) via route groups
- `WithDeprecation()` marks versions with Sunset/Link headers
- `DeprecationMiddleware()` adds `Deprecation: true`, `Sunset`, `Link` headers
- `Projection` + `ProjectionSet` for per-version field shapes (include/exclude/rename)
- MCP tools namespaced by version
- `DeprecationHeaders()` helper for individual endpoint deprecation

**Files**: `framework/apiversions/version.go`, `framework/apiversions/projection.go`, `framework/apiversions/version_test.go`

**Sketch**

- Default scheme: URL prefix (`/v1`, `/v2`). Header override considered for
  SDK clients.
- Version declared at App-level default and overridable per entity.
- Entity can declare per-version field shapes / projections.
- Deprecation metadata in `Deprecation`, `Sunset`, `Link` response headers.
- OpenAPI emits one spec per version, `deprecated: true` per route.
- MCP tools namespaced by version (e.g. `orders.v2.list`).
- Migration helpers for transforming request/response between versions.
- Implemented on top of route groups (§1).

**Acceptance**

- `GET /v1/orders` and `GET /v2/orders` serve different shapes from the
  same entity declaration
- Calling a deprecated version sets `Deprecation` + `Sunset` headers
- OpenAPI for v1 marks deprecated routes; v2 does not
- MCP `tools/list` shows both `v1` and `v2` tool families
- Removing a version cleanly unregisters routes, tools, and spec entries

---

## 4. Framework gaps — Tier 3 quality of life

**Status:** implemented (2026-05-21).

### 4a. WebSocket primitive — `core/stream/websocket.go`

- `WebSocketConn` with backpressure: writes block when send buffer full
- `Hub` for broadcast pub/sub with auto-unregister on close
- `WSConfig` with `ReadLimit`, `SendBuffer`, `WriteTimeout`, `OnClose`
- Production hardening: same-origin check (`CheckOrigin` override),
  RFC 6455 close handshake (`CloseTimeout`, default 1s), idle ping/pong
  keepalive (`ReadIdleTimeout` default 60s, `PongTimeout` default 10s),
  RSV-bit + fragmented-control rejection, `RequireMask` for client frames,
  iterative ping/pong (no recursive stack), subprotocol negotiation via
  `Subprotocols` (server-preference order per RFC 6455 §4.2.2)

### 4b. CLI scaffolding beyond kiln — `cmd/gofastr/new.go`

- `gofastr new entity Post --fields "title:string,body:text"`
- `gofastr new handler ListOrders --method GET --path /orders`
- `gofastr new route /api/health --method GET`
- Generates JSON entity declarations + Go registration scaffolds
- Idempotent by default — second invocation errors with "already exists";
  `-overwrite` opts in to rewriting the target file
- `gofastr new -h` shows usage; `gofastr new` with no resource exits non-zero
- Path-traversal-safe (`validateScaffoldName`) and golden-tested per command

### 4c. Configuration management — `core/config/config.go`

- Typed struct binding with `config:"KEY"`, `default:"VALUE"`, `required:"true"` tags
- `Source` interface: `EnvSource`, `MapSource`, `ChainedSource`
- Supports string, int, float64, bool, time.Duration
- `MustLoad()` for init/main usage

### 4d. Graceful shutdown contract — `framework/lifecycle/lifecycle.go`

- `Lifecycle` manager with drain phases: mark unhealthy → drain → stop
- `Drainer` interface for batteries with pending work
- `HealthChecker` interface for readiness probes
- Concurrent drain with configurable timeout (default 30s)

### 4e. i18n surface coverage — `framework/i18nui/i18nui.go`

- `Key` type for all framework UI translation keys (80+ keys)
- English fallback defaults for pagination, validation, dialogs, tables, forms, auth
- `T(key)` and `TWith(translator, key)` for resolved strings
- `ValidationError(validator, vars)` for parameterized error messages
- `LabelForField(translator, entity, field)` for entity field labels

### 4f. Battery follow-ups

- **Redis idempotency**: EXPERIMENTAL (2026-05-22) — moved to `battery/experimental/redisidempotency/`. Hardened (atomic SetNX, KeyPrefix applied, size cap, in-flight sentinel) but unexercised in-tree. Promote back to `battery/` when an example app uses it.
- **Redis feature flags**: EXPERIMENTAL (2026-05-22) — moved to `battery/experimental/redisflags/`. Same reasoning. Now uses Scan+MGet instead of KEYS, surfaces real errors, validates RolloutPct.
- **Passkeys**: deferred (requires WebAuthn library integration)

---

## 5. UI components — implemented (2026-05-21)

Wave 1–7 have shipped. See `framework/docs/content/ui-new-components.md` for the running list
of landed components. The following were implemented in this roadmap pass:

### 5a. Calendar / date picker — DELETED (2026-05-22)

Removed. Shipped as a static SSR shell with no runtime JS module, no RPC handler, no interactive contract — calendar buttons rendered as literal "-". Re-add with a working `core-ui/runtime/src/datepicker.js` + RPC and an e2e test that asserts day selection works end-to-end.

### 5b. Dynamic form repeater — `framework/ui/repeater.go`

- `Repeater(cfg)` with `Template func(index int) render.HTML`
- Add/Remove buttons with optional RPC for dynamic re-render (`data-fui-rpc`)
- Min/Max items enforcement via `data-min-items`/`data-max-items` attrs
- Fields submit as `name[i].field`

### 5c. Form step wizard — `framework/ui/wizard.go`

- `Wizard(cfg)` with per-step content + optional validation RPC
- Step indicator (`<ol>` with upcoming / current / complete states, `aria-current`)
- Back/Next/Submit navigation with hidden step tracking field
- `data-fui-rpc` on nav buttons for island-driven transitions

### 5d. Inline edit field — DELETED (2026-05-22)

Removed. Same reason as 5a — SSR shell only, no runtime, the click→edit contract was rendered as a span that never wired. Re-add with `core-ui/runtime/src/inlineedit.js` + RPC handler + e2e proving click → input swap → Enter save round-trips.

### 5h. Form module follow-ons — `framework/ui/form_inputs.go`

- **PasswordInput** — password field with show/hide toggle (`data-fui-password-toggle`)
- **SearchInput** — search input with clear button (`data-fui-clear-on-esc`)
- **InputGroup** — prepend/append wrapper for inputs with visual join

---

## 6. Typed theme + variant system

**Status:** implemented — typed tokens in `core-ui/style/tokens_typed.go`, typed `Theme` struct in `core-ui/style/theme.go`.

Replace map-backed `style.Theme` (`Colors map[string]string`, …) with typed
structs so components get autocomplete, compile-time rename safety, and a
single source of truth. Section-level theme overrides via CSS cascade
require every component CSS reference to be `var(--*)`, never a build-time
literal.

### Decisions locked

1. **Token values are typed structs** — `Color`, `Spacing`, `Radius`,
   `Shadow`, `Font`, `Z`, `Dur`, `FontSize`, `Breakpoint`. Each carries
   Name + Value and exposes a `.CSS() string` method that returns
   `var(--<category>-<name>)` — always a CSS variable reference, never the
   literal value. `.Value` is for non-CSS contexts (charts, JSON, image
   generation).
2. **`style.Theme` is a canonical struct** with a fixed field set. Token
   vocabulary expanded vs today: + Shadows, + ZIndex (named layers:
   dropdown/sticky/modal/popover/toast), + Durations (fast/normal/slow),
   + Typography (xs/sm/base/lg/xl/2xl/3xl).
3. **All theme fields required** for primitives. Apps must declare every
   field. Scaffold ships full defaults as the starting point.
4. **App-specific token extensions via embedding** (`AppTheme` embeds
   `style.Theme`). Framework code only sees the embedded `style.Theme`;
   app-local components can reference `theme.App.Brand.*` directly.
5. **Components always emit `var(--*)`.** Build-time `{token}` resolution
   removed; passing a typed token resolves to `var(--…)`.
6. **`theme.css` + `styles.css` merged** into `/__gofastr/app.css`. Old
   endpoints become 410 GONE.
7. **`<style>` blocks remain forbidden** — strict CSP stance unchanged.
8. **Section-level theme overrides via class cascade.**
   `ui.Themed(themeOverride, ...children)` wraps children in a
   `<div class="fui-theme-<id>">`. The framework emits a
   `.fui-theme-<id> { --color-…: …; }` block in app.css.
9. **Component variant mapping owned by theme.** Per-component sections
   (`ButtonsTheme`, `BadgesTheme`, …) optional; framework ships internal
   defaults. Apps override selectively.
10. **No per-variant tree-shaking.** Each component's CSS file includes all
    variants. Per-component tree-shaking (already implemented) is the only
    level.
11. **String-typed variant enums** consistent across components
    (`ButtonVariant`, `ButtonPrimary` / `ButtonSecondary` / …).
    `DangerButton(...)` becomes a deprecated alias for
    `Button{Variant: ButtonDanger}`.
12. **Component theme parameter pattern.** `StyleFn func(theme style.Theme) string`
    — unchanged signature; theme is now typed.
13. **`gofastr theme init` scaffold command** writes a starter `theme.go`
    to the user's project containing the full `DefaultTheme()` declaration.
    User owns the file forever; no regeneration.
14. **`app.WithTheme(theme)` binding stays.** Existing pattern threads the
    typed theme through `app.App` to the registry to StyleFn calls.

### Migration sequence

1. `core-ui/style/tokens_typed.go` — typed token value types with `.CSS()`.
2. Typed `Theme` struct + reflection-based `CSSCustomProperties()`. Migrate
   `framework/ui/theme/*` references.
3. Per-component variant-style structs added to `Theme`. Each
   variant-bearing component declares its default mapping internally.
4. `ComponentSheet.Set` accepts typed values; drop `{tokens.text}`
   build-time resolution.
5. Migrate every per-component CSS builder to typed theme + `.CSS()`-only.
   Drop hardcoded `#hex` fallbacks from `var(--color-x, #hex)`.
6. Merge `theme.css` + `styles.css` into `/__gofastr/app.css`. Old
   endpoints → 410 GONE. SSG emits `app.css`.
7. `ui.Themed` wrapper + section overrides emission.
8. Variant standardisation across components; `framework/ui.Button(...)`
   lands; `DangerButton` becomes deprecated alias.
9. `gofastr theme init` scaffold command.
10. ~~Migrate `examples/website/theme.go` to the new shape.~~ (done — folded into `examples/site`)
11. Docs + drift: `core-ui/ARCHITECTURE.md` typed-theme section, the
    `gofastr-ui` skill, extended drift test.

### Out of scope

- Per-variant tree-shaking.
- Multi-tenant runtime theme swapping (different themes per request based
  on tenant ID).
- Replacing the `ClientJS`-based action system with `data-fui-*` primitives.

---

## 7. Performance opportunities — implemented (2026-05-21)

Verification pass on 2026-05-22 — see `framework/docs/content/perf-results.md` for raw
numbers and `dist/bench/current.txt` for the bench output.

**P0 fixes applied:**

- **7a.** `SampledLogging(sampleN, slowThreshold)` in `core/middleware/logging.go` — logs 1-in-N requests plus all errors/slow; `DiscardLogging()` for benchmarks. **Verified (2026-05-22)** — with/without ratio collapsed from 200× to ~18× (target ≤10×).
- **7b.** `parsePagination` now raises cap to `streamListThreshold` when `?stream=true`. **Verified (Postgres, 2026-05-31)** — current Postgres run shows buffered-paginated-5000 ≈41.1 ms vs streaming-single-5000 ≈10.8 ms, a 3.8× win.
- **7c/7l.** `framework/crud/pool.go` — `sync.Pool` for `[]map[string]any` row maps and `[]any` scan pointer slices; `scanRowsPooled()` uses pooled maps. **Needs rerun** — allocs 3187 → 2487 (−22%), but time gap GoFastr vs net/http is still +105% (target was −60%).
- **7d.** JSON case conversion: `ToCamel`/`ToSnake` cached via `sync.RWMutex`; `PrecomputeMapping` + `ApplyMapping` for zero-alloc row conversion. **Verified (2026-05-22)** — 26 allocs → 4 allocs, 19 µs → 408 ns. Single-word lookups are 6 ns / 0 allocs.
- **7e/7f.** `ReadLiveColumnsBulk` and `TableExistsBulk` in `framework/migrate/bulk.go` — single query for N tables. **Verified (Postgres, 2026-05-31)** — `DiffSchema/postgres/N=50` is ≈2.73 ms and idempotent `AutoMigrate/postgres/N=50` is ≈0.75 ms.

**P1 fixes applied:**

- **7g.** `Scheduler.RunOnce` no longer copies the jobs slice — iterates under lock, 0 allocs per tick. **Verified (2026-05-22)** — N=1 is 7.3 ns / 0 allocs. Larger N still allocates because each matching job spawns a goroutine; that is intentional dispatch cost, not the snapshot-copy defect.
- **7h.** DSL parser cache (pre-existing). **Verified (2026-05-22)** — 14–15 ns / 0 allocs on cache hit (target was 50 ns).

**P2 fixes applied:**

- **7i.** `core/stream/sse_broker.go` — `SSEBroker` with per-subscriber configurable buffer (`?buffer=128` or `X-SSE-Buffer` header), backpressure with oldest-drop. **Verified semantics (2026-05-31)** — slow subscribers use bounded, non-blocking delivery and retain latest events; high drop rate under intentionally slow clients remains expected.
- **7j/7k.** `framework/uihost/builder_pool.go` plus `core/render` builder sizing — pooled `strings.Builder` adopted at UI host callsites, and `render.Tag`/`Join` now pre-size builders with a one-attribute fast path. **7j needs work / 7k verified** — UI host render still needs current-shape comparison, but `BenchmarkT9_IslandRPC_Concurrency/workers=64` p99 is ≈4.32 ms with 94 allocs/op.

**Doc-only:**

- **7m/7n.** SQLite concurrency callout + pure-Go `modernc.org/sqlite` alternative documented in `framework/docs/content/migrations.md`. **Verified (2026-05-22)** — doc-only items.

**§7 verification status:** keep `framework/docs/content/perf-results.md`
as the source of truth. Items are only verified when the named benchmark
measures the current implementation path; Postgres-specific claims require
a Postgres run and now have 2026-05-31 evidence where marked verified.

A prioritized list of improvements the benchmark suite has surfaced. Every
item names the benchmark that exposed it so the win can be verified after a
fix. Priority reflects "ratio of impact to scope" — not just raw speedup.

Baseline: darwin/arm64, M4 Pro, Go 1.26, SQLite in-memory and Postgres-16
via testcontainers.

### P0 — biggest win per unit of work

**7a. Default middleware chain is ~200× the no-defaults cost.**
`BenchmarkMiddleware_DefaultChain` reports 268µs with the default chain and
1.3µs without. `Logging()` writing a structured line per request is the
bulk of the gap. Options: `WithLoggingWriter(io.Writer)` + discard by
default in tests; sampled/buffered logger when `GOFASTR_ENV != "dev"`; or
disable logging in the default chain entirely (explicit opt-in). Verify:
`make bench-tier2` — with/without delta should collapse to ≤10×.

**7b. `parsePagination` clamps `?limit=` to ≤100, hiding the streaming win.**
`BenchmarkT9_StreamingVsBuffered_RealVolume/postgres` shows streaming beats
buffered-paginated 4× at 5000 rows (12.6ms vs 50ms), but clients can't
reach that workload because `framework/crud.go:parsePagination` caps
`?limit=` at 100 — `streamListThreshold = 1000` is unreachable. Raise the
cap (configurable per-entity) or add `?stream=true` that bypasses it.

**7c. FilteredList is still >2× slower vs hand-rolled `net/http`.** — **NEEDS-WORK (partial alloc win, 2026-05-31).**
`BenchmarkT7_FilteredList_GoFastr/sqlite3` is now 140µs / 2432 allocs vs
62µs / 1881 allocs hand-rolled. Same SQL, same JSON output. Cached visible
fields / JSON keys and pooled row maps cut allocations from the older 3187
witness to 2432, but the time gap is still +127%, not the ≤60% target.
Next pass should skip include/filter/projection parsing on entities and
requests that have not opted into those features, then re-measure.

**7d. JSON case conversion: 26 allocations per row, twice on write paths.**
`BenchmarkJSONCasing/snake→camel` is 19µs / 1048 B / 26 allocs for a
10-key row. List endpoints run snake→camel once per row × rows-per-page;
writes run both. Pre-compute the camel↔snake mapping at `Define()` time;
pool a `strings.Builder`; reuse `mapToCamelCase`'s output map. Target:
drop snake→camel below 10 allocs.

### P1 — solid wins, contained scope

**7e. SchemaDiff at 59ms for 50 entities on Postgres.** — **Verified (2026-05-31).**
`BenchmarkSchemaDiff/postgres/N=50` does N round-trips to
`information_schema.columns`. One bulk `WHERE table_name IN (...)` query
now backs `DiffSchema`; latest Postgres run is ≈2.73ms at N=50.

**7f. AutoMigrate idempotent re-run at 7.5ms for 50 entities (Postgres).** — **Verified (2026-05-31).**
Same root cause as 7e — one existence check per entity. Single bulk
`pg_tables` query before the per-entity loop now backs idempotent reruns;
latest Postgres run is ≈0.75ms at N=50.

**7g. CronTick allocates 1471 times per minute for 1000 jobs.**
`BenchmarkCronTick/N=1000` reports 175µs / 213KB / 1471 allocs per tick.
The snapshot copy + per-job match check allocates fresh slices. Replace
the copy with a read-locked iteration if mutations during tick are rare;
pre-sort by next-fire time so the tick breaks early. Target: ≤1 alloc per
tick regardless of N.

**7h. DSL parser allocates on every call.** — **Implemented (2026-05-21).**
`ParseDSL` now caches results in a bounded map (256 entries). Cache hit is
~50ns / 0 allocs. `parseDSLUncached` retains the original parsing logic.
`BenchmarkDSLParse/complex` is 6µs / 7 allocs. Agents often issue the same
query template repeatedly; parsed `DSLQuery` could be cached by input
string in a bounded `sync.Map` LRU. Target: ~50ns / 0 allocs on cache hit.

### P2 — broader scope, real benefit

**7i. SSE backpressure drops under slow default subscribers.** — **Semantics verified (2026-05-31).**
`BenchmarkSSE_BackpressureDropRate`: drop_rate **0.9740** at 5000 events
through `core/stream.SSEBroker` with `?buffer=128` and a slow consumer.
`BenchmarkT9_SSEEventStream`: delivery_ratio **0.48** at 500 events
end-to-end. Per-subscriber buffer sizing is available via query param/header
(`?buffer=128` / `X-SSE-Buffer`). Current default contract is bounded,
non-blocking delivery with oldest-drop and latest-event retention; high
drop rate is expected under slow subscribers.
`?slow=block` / `X-SSE-Slow: block` is now implemented for stronger
delivery clients that can tolerate publisher backpressure.

**7j. UI host page render is still heavier than it should be.** — **PARTIAL (2026-05-31).**
`BenchmarkT9_UIHostPageRender` is now 35µs / 49KB / 345 allocs for `/`
and 52µs / 61KB / 457 allocs for `/about`. Runtime injection now batches
head/body additions and avoids repeated whole-page replacements, cutting
the previous current-shape witness roughly in half. Remaining cost is the
HTML tree build plus runtime/script assembly. Next pass: pre-flatten the
layout shell so only the screen render varies per request.

**7k. Island RPC tail latency at workers=64.** — **Verified (2026-05-31).**
`BenchmarkT9_IslandRPC_Concurrency/workers=64`: p50 ≈11.8µs, p99
≈4.32ms. The witness now uses fixed worker counts instead of
`testing.B.SetParallelism`, and `core/render` builder sizing cuts the
modeled island response to 94 allocs/op. Target p99 <10ms is met.

**7l. Filtered list overhead allocations.**
Re-stated from 7c: 2432 allocs vs 1881 hand-rolled after cached visible
fields / JSON keys and pooled row maps. Pool the response writer's byte
buffer; switch `[]map[string]any` to a typed result struct when the entity
has a generated model (the `framework/typed_query.go` path already supports
this — auto-detect and route to it).

### Doc-only fixes (no code change)

**7m. SQLite write serialisation under load.**
`BenchmarkT6_CreateConcurrency/sqlite3/parallelism=64`: p99 climbs to
112ms; only 10 writes complete out of 5072 ops in `BenchmarkT6_MixedRW`.
Update `framework/docs/content/migrations.md` and `framework/docs/content/security.md` with a "Concurrency
model" callout for SQLite. The framework already sets `MaxOpenConns(1)`
in test helpers; users should know to do the same in production or pick
Postgres.

**7n. cgo SQLite costs ~4MB binary + 440MB build RAM.**
Resource bench: `crud` is 12.9MB / 760MB build RAM; `minimal` is 8.8MB /
311MB. Document `modernc.org/sqlite` as a pure-Go alternative in
`framework/docs/content/migrations.md`. Trade-offs: pure-Go a few % slower at query time,
saves ~4MB binary + ~440MB build RAM, eliminates cgo toolchain
dependency.

### How to track progress

```bash
# Capture before:
make bench-<tier> BENCH_COUNT=10 BENCHTIME=1s
mv dist/bench/<tier>.txt dist/bench/<tier>-before.txt

# Make the fix, then:
make bench-<tier> BENCH_COUNT=10 BENCHTIME=1s
benchstat dist/bench/<tier>-before.txt dist/bench/<tier>.txt
```

### What's NOT on this list (working as designed)

- **Router lookup** — ~180ns flat from N=1 to N=1000 vs ServeMux ~125ns.
  50ns delta not worth chasing.
- **Cursor pagination** — flat across pages.
- **Includes vs N+1** — 35× faster than naive on Postgres.
- **Heap retention under load** — 778KB live heap after 5000 list requests.
- **Goroutine leaks** — `gor_delta = 0` after 2000 requests.

---

## 8. Runtime code-split

**Status:** implemented + minified — 32 split modules in `core-ui/runtime/src/`, `loadModule()` loader with hover prefetch via `data-fui-prefetch`, idle scheduling via `requestIdleCallback`. A token-aware JS minifier (`core-ui/runtime/minify`) runs at first read in production, env-gated via `GOFASTR_ENV` / `GOFASTR_DEV` (see [runtime-minification.md](framework/docs/content/runtime-minification.md)). The `~10 KB gz core` target is met: bundled `runtime.js` ships at ~10.4 KB gz post-minify (was 28 KB gz pre-minify, single bundle, everything).

Goal: shrink the parser-blocking JS payload on a typical page from ~31 KB
gz (one bundle, everything) to ~10 KB gz (core only) + lazy modules loaded
on hover / idle / first-use. **Met** via minification + targeted carves
(`src/copy.js` extracted from the main bundle; remaining carves like
`navigate.js` are forward-looking).

### Status quo

The entire client runtime is a single IIFE in
`core-ui/runtime/runtime.js`: 2,545 lines, 108 KB raw / 31 KB gzip /
26 KB brotli. Served as `/__gofastr/runtime.js`, parser-blocking, in every
page. Lighthouse-equivalent at slow 3G: ~4 s end-to-end uncompressed,
~1.7 s with gzip, ~1 s with code-split + gzip.

### Three load classes

| Class | Examples | Wire timing |
|---|---|---|
| **Core** — needed before first interaction | namespace, signals, dispatchRPC, SPA router, screen cache, CSS catalog, disclosure Esc, `data-fui-confirm`, MutationObserver, module loader | parser-blocking, in `<head>` |
| **Hover-prefetch** — triggered by a known marker | popover, menu, fileupload, toast push, modal open | `mouseover`/`focusin` on trigger fires `loadModule()`; `click` awaits the promise |
| **Idle-load** — needed soon, no clear trigger | widget chrome (for SSR-inlined widgets), SSE consumer, form primitives | `requestIdleCallback` after FCP |

### Module split

| Module | Contents | gzip est. | Load |
|---|---|---:|---|
| `core.js` | namespace, signals, dispatchRPC, SPA router (cache + popstate), CSS catalog scan + idle prefetch, disclosure (Esc + SPA-nav close), `data-fui-confirm`, `data-fui-open` delegator (queues), `data-fui-prefetch` delegator, module loader, MutationObserver, active-link aria-current | **~10 KB** | parser-blocking |
| `widgets.js` | `mountWidget` chrome + dismiss, modal stack, backdrop, focus trap, deep-link push/strip, Esc handler, `_popoverStack` non-modal dismiss | ~8 KB | idle (always) + hover (`data-fui-open`) |
| `sse.js` | `connectSSE` + island stream consumer | ~1 KB | idle (only if `<meta name="gofastr-sse">` present) |
| `popover.js` | `_anchorPopover` + arrow + scroll/resize tracking + trigger-active class | ~2 KB | hover (`data-fui-popover-anchor`) |
| `toasts.js` | toast stack, `__gofastr.toast()`, `X-Gofastr-Toast` parser, TTL + hover-pause + click-dismiss, `data-fui-toast` delegator | ~2.5 KB | idle (when stack widget mounts) + eager (first toast) |
| `menu.js` | roving focus, Home/End, type-ahead, Tab-to-close | ~2 KB | hover (`data-fui-menu`) |
| `fileupload.js` | drag/drop wiring, filename render, image thumbnail | ~1 KB | hover (`data-fui-fileupload`) |
| `forms.js` | autogrow, charcount, persist-storage, fill-input, clear-on-esc, submit-on-enter, disable-when-invalid, copy-text-from, tick-elapsed, flash-on-update, scroll-bottom-on-update, rpc-after-text/disable/scroll-to, shortcuts | ~5 KB | idle |

Total when everything is needed: ~31.5 KB gz (matches today's size). First-
load JS on a typical page (no popover/menu/toast/fileupload): core only =
~10 KB gz, then widgets + forms + sse arrive on idle ≈ 14 KB more, none
of which is parser-blocking.

### Component-driven dependency tracking

Components know what runtime modules they need at render time. The renderer
collects a per-request set; the host emits one
`<link rel="modulepreload">` per needed module.

```go
func Popover(name string) *widget.Builder {
    runtime.Need("widgets", "popover")
    return widget.New(name).Mount(widget.TopRight).Hidden()
}
```

Emitted in `<head>`:

```html
<link rel="modulepreload" href="/__gofastr/runtime/core.js?v=abc">
<link rel="modulepreload" href="/__gofastr/runtime/widgets.js?v=abc">
<link rel="modulepreload" href="/__gofastr/runtime/popover.js?v=abc">
<script src="/__gofastr/runtime/core.js?v=abc"></script>
```

The trigger element gets `data-fui-prefetch="popover"`. Core's single
`pointerover` + `focusin` capture-phase delegator calls
`loadModule("popover")` on first hover — by the time the user clicks, the
module is loaded.

### Phase ladder

- **Phase 0** *(separate branch)* — HTTP compression. 30-line gzip
  middleware on `/__gofastr/*`, `text/html`, `application/json`.
  Independent 3.5–4× wire-size win.
- **Phase 1** *(in progress)* — internal carve. Reorganize `runtime.js`
  in place: one block per future-module with labelled header; shared
  state hoisted into a "core state" block; add `__gofastr.loadModule(name)`
  API returning an immediately-resolved Promise. Validates the boundary
  holds before adding HTTP weight.
- **Phase 2** — file split. Move sections to `core-ui/runtime/src/<name>.js`;
  Go-side server emits `/__gofastr/runtime/<module>.js?v=<hash>`,
  content-addressed, `Cache-Control: immutable`. `loadModule` becomes real
  dynamic `<script>` injection with cached promises. `_pendingFor` queue
  in core: `data-fui-open` click before `widgets.js` has loaded queues;
  replays after load.
- **Phase 3** *(landed)* — server-side preload emission. The framework
  ships a Go-side mirror of the demand-load scanner table
  (`core-ui/runtime/preload.go`) and `framework/uihost.injectChromeMode`
  scans the rendered page for marker substrings, emitting one
  `<link rel="modulepreload" href="/__gofastr/runtime/<name>.js?v=<hash>">`
  per matched module. Drift between the Go and JS tables is enforced by
  `TestDemandLoadMarkersMatchRuntimeJS`. (The `data-fui-prefetch` trigger
  attribution is still pending — handled implicitly today by the runtime's
  demand-load scanner walking the DOM at boot.)
- **Phase 4** — hover prefetch. Core attaches one `pointerover` +
  `focusin` capture-phase delegator; click handlers `await loadModule()`.
- **Phase 5** — idle fallback for modules without a hover-trigger
  (`widgets.js` for SSR-inlined widgets, `sse.js`, `forms.js`) scheduled
  via `requestIdleCallback` after FCP. Falls back to `setTimeout(0)` on
  browsers without rIC.
- **Phase 6** — tests. Drift test extended; e2e asserts no
  `data-fui-popover-anchor` → never fetches `popover.js`; hover on
  `data-fui-prefetch` fetches its module within one frame. Runtime-size
  budget split: core ≤ 12 KB gz, each demand module ≤ 3 KB gz.

### Risks called out

- **Click-before-module-load on first hover** — a user who lands and
  immediately clicks a popover trigger (no hover) waits one round trip.
  Acceptable: <100ms on 4G, ~300ms on slow 3G. Touch devices always hit
  this path. Mitigation: keep modules ≤3 KB gz and `immutable` cache so
  repeat visits are free.
- **Boundary leakage** — a feature added later forgets to declare its
  module. The drift test (Phase 6) catches this.
- **Idempotency under SPA nav** — every module must register a
  `(root) => void` scanner. Core's MutationObserver invokes scanners on
  inserted DOM; modules deduplicate via per-element `__fuiWired` flags.
- **Server-side dep tracking adds touch points** — every renderer that
  emits a marker needs to call `runtime.Need()`. Risk of miss → preload
  tag missing → first interaction stalls one RTT. Drift test (Phase 6)
  catches missing declarations.
- **Old browsers without `requestIdleCallback`** — Safari < 16.2,
  Firefox < 55. Fallback to `setTimeout(0, …)` — idle becomes "next tick",
  still after FCP.

---

## 9. Framework DX — feedback from the first real third-party app

**Status:** not started (2026-05-23). Surfaced by a build-out of a
"WTF do I eat?" app on top of the framework. Each item is independent;
priority is roughly top-to-bottom.

### 9a. Entity↔page path collision — friendlier diagnostic

Today: registering a screen at `/foods` when there's also an entity called
`foods` panics at startup with a duplicate `/foods/llm.md` registration
message — the error points at the auto-generated llm.md handler, not at the
underlying name collision. Users have to trace it back themselves.

**Goal.** Detect the collision at registration time (entity OR screen,
whichever lands second) and panic with a directly actionable message:

```
entity "foods" already owns the /foods URL space (REST + /foods/llm.md);
choose a different page path (e.g. /library, /library/:slug) or move
entity CRUD under an APIPrefix (see 9c).
```

**Implementation sketch.** `app.Register(path, screen, spec)` checks the
entity registry for any entity whose CRUD mount prefix matches `path` (or
is a parent of it). Symmetric check on entity registration. Error surfaces
the colliding entity name, the path it claimed, and the recommended fix.

**Acceptance.** Adding a `/foods` screen with a `foods` entity panics with
the new message in &lt; 1 ms; existing tests covering the auto-CRUD mount path
still pass.

### 9b. Seed ordering — `WithSeed(func(ctx))` post-migrate hook

Today: `App.Start()` runs auto-migrate as one of its first phases. Calling
`db.Exec("INSERT …")` from `main()` before `Start()` fails with
`no such table`. Users hit this once, file it under "easy fix" — but the
ordering isn't obvious from the API surface.

**Goal.** Either:

1. Expose `App.WithSeed(func(ctx context.Context) error)` that the
   lifecycle registers after auto-migrate and before "ready", so seed
   logic lives where it composes (next to the app config), OR
2. Document the existing `OnStart` hook idiom prominently in
   `framework/docs/content/ui-getting-started.md` and `framework/docs/content/entity-declarations.md` with a
   worked seed example.

The exposure path is the better DX. `WithSeed` reads as "this app needs
seed data", which is the user's intent. Multiple `WithSeed` calls run in
registration order. Errors fail `Start()` with the seed func's file:line.

**Acceptance.** A user can write `site.WithSeed(seedFoods)` in `main()`
and the func runs after migration, before the server accepts traffic. A
chromedp test asserts seed rows are queryable on first request.

### 9c. `framework.AppConfig{APIPrefix: "/api"}`

Today: entity CRUD mounts at the bare entity name (`/foods`, `/users`).
The convention every real backend uses — `/api/v1/foods` or at minimum
`/api/foods` — is achievable via route groups, but it's boilerplate the
first-time user has to assemble.

**Goal.** First-class config:

```go
site := framework.NewApp("myapp",
    framework.WithAPIPrefix("/api"),
    framework.WithDB(db),
)
```

Effect: every auto-CRUD route, including `/llm.md` and the per-entity
OpenAPI block, mounts under the prefix. MCP tool namespacing unchanged.
The default stays bare (`""`) to avoid a breaking change; the example
website opts in.

**Open question.** Per-entity override? `EntityConfig{Mount: "/v2/foods"}`
already gives you that today via route groups. Probably not worth a second
config knob.

**Acceptance.** `WithAPIPrefix("/api")` causes `GET /api/foods` to serve
the list; `GET /foods` 404s. Updating an existing app to add the prefix
requires changing one line in `main.go`; no entity declaration changes.

### 9d. Form-input wrappers — `ui.TextField`, `ui.NumberField`, `ui.DateField`

Today: `html.InputConfig` is a low-level primitive — `Required`,
`Placeholder`, `Value`, `Min`, `Max`, `Pattern`, ARIA wiring all flow
through `Attrs: html.Attrs{"required": ""}`. Reasonable at the primitive
layer; rough at the call site of every form.

**Goal.** Opinionated wrappers in `framework/ui/` that lift the common
attrs into typed config and compose with `FormField` for label + error +
description:

```go
ui.TextField(ui.TextFieldConfig{
    Name:        "title",
    Label:       "Title",
    Required:    true,
    Placeholder: "Untitled",
    Value:       cfg.Title,
    Error:       errs.Field("title"),
})
ui.NumberField(ui.NumberFieldConfig{Name: "qty", Min: 1, Max: 99, Step: 1})
ui.DateField(ui.DateFieldConfig{Name: "due", Min: "2026-01-01"})
```

These compose `FormField + html.Input` internally. `html.Input` stays the
primitive escape hatch. Each wrapper does the right ARIA wiring
(`aria-describedby` for description + error, `aria-invalid` when an error
is present).

**Acceptance.** A form built with the three wrappers has zero `html.Attrs`
literals at the call site. Errors surface inline. Submitting with
`required` empty triggers HTML5 validation; submitting with a server-side
error sets `aria-invalid="true"`.

### 9e. Kiln skill — auto-trigger tightened (done, externally)

The `~/.claude/skills/kiln/SKILL.md` description previously auto-loaded on
any mention of "GoFastr". A user building with the framework directly (not
Kiln) would get routed into the Kiln agent, scaffold Kiln, and try to
build the app via HTTP IR mutations — losing time before they realised
Kiln wasn't the intended path.

The skill description was tightened to require explicit Kiln signals
(`$KILN_URL` set, "Kiln" by name, "kiln serve", IR mutation phrasing) and
to **not** trigger on "GoFastr" alone. Recorded here so the next change to
that skill knows the constraint.

**No further work** unless a regression in trigger behavior is observed.

---

## 10. `EntityConfig` sub-config refactor (deferred, design captured)

**Status:** not started (captured 2026-05-24 after adversarial review of
the `feedback-updates` PR). `EntityConfig` now carries 17 fields —
the `OwnerField` addition tipped it from "many fields" into "god struct"
territory. The semantic relationships between the toggles (which fields
combine, which conflict) aren't visible in the type.

**Sketch**

```go
type Scope struct {
    MultiTenant bool
    OwnerField  string
    SoftDelete  bool
}

type Pagination struct {
    CursorField  string
    CursorFields []string
    MaxListLimit int
}

type Exposure struct {
    CRUD *bool
    MCP  bool
}

type EntityConfig struct {
    Name, Table string
    Fields      []schema.Field
    Relations   []Relation
    Endpoints   []Endpoint
    Indices     []Index
    Properties  map[string]any
    Timestamps  bool
    Scope       Scope
    Paging      Pagination
    Expose      Exposure
}
```

**Why deferred from `feedback-updates`**

The refactor breaks every `EntityConfig{...}` literal in user repos
(including JSON declarations). The shipping fix path requires a
`UnmarshalJSON` shim that maps the flat field names to the nested
shape for one release, plus a deprecation note pointing at the new
shape. Doing this inside the `feedback-updates` PR would balloon
the diff past what's reviewable. The flat fields stay valid for now;
sub-configs land in their own focused PR.

**Acceptance for the future PR**

- `EntityConfig` accepts the nested shape as canonical.
- Flat-field literals continue to compile + run for one release, with
  deprecation comments pointing at the nested equivalents.
- The JSON declaration loader (`framework/entity/declaration.go`)
  accepts both flat and nested JSON; loader emits a one-line WARN on
  flat-style for ops visibility.
- `framework/docs/content/entity-declarations.md` shows the nested
  shape as primary; flat-shape is in a "legacy" section.

**No further work** until the deprecation window is scheduled.

---

## 11. BFF posture preset (`framework.WithBFFPosture()`)

**Status:** not started (captured 2026-05-24). GoFastr is already a
backend-for-frontend by construction — server-rendered HTML, in-process
`/api/*`, no external API gateway. The framework has all the pieces
(HttpOnly+Secure session cookies, SessionMiddleware, CSRF middleware,
SkipBearerAuth) but the secure-by-default wiring is currently
opt-in per piece. A preset flips all four to "on" with a single line.

**Why a preset and not silent defaults**

Each piece of the BFF posture would break a class of existing app if
defaulted on silently:

- *Stripping JWT from `/auth/login` response body* breaks SPA clients
  that read `data.token` for cross-origin XHR.
- *Strict Origin allowlist on `/api/*`* breaks mobile / native clients
  (`Origin: null`) and any cross-origin XHR.
- *Auto-mounting CSRF* breaks any existing mutating route behind cookie
  auth that doesn't yet have the hidden field.
- *Auto-mounting SessionMiddleware* changes the per-request context shape.

A preset (opt-in) gives operators an explicit posture toggle without
ambushing existing apps.

**Sketch**

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithBFFPosture(framework.BFFOptions{
        AllowedOrigins: []string{"https://app.example.com"},
        // Defaults inside:
        //   AllowJWTInBody     = false  (cookie-only auth)
        //   EnforceOrigin      = true   (strict allowlist on /api/*)
        //   AutoMountCSRF      = true   (with SkipBearerAuth)
        //   AutoMountSession   = true   (SessionMiddleware on the app router)
        //   CSRFSecureTracksDevMode = true
    }),
)
```

**What lands**

- `framework/bff.go`: `WithBFFPosture(BFFOptions)` option, `BFFOptions`
  struct, `originAllowlistMiddleware` factory.
- `battery/auth/core.go`: `/auth/login` JSON response gates the `token`
  field on `BFFOptions.AllowJWTInBody`.
- `battery/auth/csrf.go`: `CSRFSecureTracksDevMode` config that flips
  `WithCSRFCookieSecure` from `mgr.Config().DevMode`.
- `framework/docs/content/security.md`: "BFF posture" section
  documenting trade-offs + SPA / mobile coexistence pattern.
- New tests:
  - Origin allowlist accepts same-origin, rejects cross-origin
  - JWT not in body under BFF posture
  - Bearer-token clients still work (CSRF skip + Origin null bypass for API-key)
  - CSRF cookie Secure flag tracks DevMode
- E2E:
  - Browser navigates same-origin form login → works
  - Cross-origin fetch → 403 (synthetic Origin header from chromedp)

**Acceptance**

- Adding `WithBFFPosture` to a fresh app produces a posture where: no
  bearer tokens in browser JS, no cross-origin XHR, all mutating routes
  CSRF-protected, all routes can read `auth.GetCurrentUser(ctx)`.
- A fresh app WITHOUT `WithBFFPosture` behaves exactly as today (no
  silent default flip).
- `framework/docs/content/security.md` shows the BFF posture as the
  recommended default for new apps, with a clear "when to skip"
  section for SPA / native-client topologies.

**No further work** until prioritised after `feedback-updates` lands.

---

## 12. `Component.Render(ctx)` — context-aware render

**Status:** MOSTLY DONE — the `component.ContextComponent` (`RenderCtx(ctx)`)
+ `ContextOnly` core shipped 2026-05-24 and is threaded at the screen level
(`core-ui/component/component.go`, `core-ui/app/{screen,app}.go`). Layout chrome
(header/sidebar/footer) now also threads the request context: `Layout.WrapCtx` /
`WrapNestedCtx`, `ScreenGroup.RenderLayoutCtx`, and `composeLayoutsWithOverrideCtx`
render every slot via `component.SafeRenderCtx`, and `App.RenderPageResult` calls
the ctx variants — so context-aware nav/footers (auth state, current tenant) work
without a forwarding shim (`TestLayoutChromeReceivesContext`). The exported
`Wrap`/`WrapNested`/`RenderLayout` keep working (they delegate with a background
context), so this is non-breaking. What remains is purely optional: arbitrary
user components that call `SafeRender` on their OWN children mid-`Render()` still
pass a background context — there is no ambient request context during a manual
`Render()`. Components that need ctx for children should implement `RenderCtx`
and forward it explicitly. Do NOT re-implement the screen-level or chrome work.
(Originally captured 2026-05-24 from third-party app feedback.)
The current `core-ui/component.Component` interface signature is:

```go
type Component interface {
    Render() render.HTML
}
```

Components can't reach request-scoped values (current user, request id,
trace span) during render without stashing them on the component struct
beforehand. Today's workaround is documented at
`core-ui/ARCHITECTURE.md:275` — implement `app.ScreenLoader.Load(ctx)`
and stuff whatever the screen needs onto its fields. Real apps end up
with a `chrome.go` shim that mechanically forwards ctx into every
sub-component, which is the kind of boilerplate the framework should
eliminate.

**Sketch (one option of several)**

```go
// Additive interface — Render() stays for back-compat; components that
// need ctx implement the extended one. core-ui/uihost detects the
// extended interface and prefers it.
type ContextualComponent interface {
    Component
    RenderWithContext(ctx context.Context) render.HTML
}
```

Or, more invasive: change the Component interface itself and provide
a default Render() shim for the migration period.

**Why deferred from `feedback-updates`**

Touches every component in the repo (~40 files in `core-ui/patterns/`,
many in `framework/ui/`, plus examples). The render loop in
`core-ui/uihost/render.go` needs to thread ctx through every recursion
point. Test surface is large. Worth its own focused PR with a clean
migration story.

**Acceptance**

- New components written against the contextual interface can call
  `auth.GetCurrentUser(ctx)` (or any other ctx accessor) inside Render
  without the screen-loader stash dance.
- Existing `Component`-only implementations keep compiling and rendering.
- The `chrome.go` shim in the wtf-do-i-eat repo (and equivalent in any
  third-party app) becomes unnecessary.

**No further work** until prioritised — the screen-loader workaround
covers the case in the meantime.
