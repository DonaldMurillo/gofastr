# GoFastr — Roadmap

Forward-looking work that isn't built yet (or isn't finished yet). Shipped
features live in `docs/<feature>.md` and the two architecture documents
(`framework/ARCHITECTURE.md`, `core-ui/ARCHITECTURE.md`).

Each section ends with a status note. When something ships, delete it from
here and add the `docs/<feature>.md` it now belongs in.

---

## 1. Route groups

**Status:** proposed (2026-05-21).

Cluster routes under a shared prefix, middleware stack, and access policy
without repeating boilerplate at each registration site. Groups nest and
compose with the existing router + middleware pipeline.

**Sketch**

- `Group(prefix, opts...)` on the App / router surface
- `g.Use(mw...)` applies group-scoped middleware to all children
- Nested groups inherit parent prefix and middleware
- Group-scoped access policy that composes with entity access
- Auto-CRUD entities mount cleanly inside a group
- OpenAPI tags and MCP tool namespacing reflect the group structure

**Why now:** precondition for screen groups (§2) and API versioning (§3).

**Acceptance**

- Routes registered inside a group are reachable at `<parent-prefix><group-prefix><route>`
- Middleware order outer→inner, matches direct registration semantics
- Removing a group cleanly unregisters all child routes
- OpenAPI spec groups routes under the same tag as their parent group

---

## 2. Screen groups and sub-layouts

**Status:** proposed (2026-05-21). Depends on §1.

Screens-side analogue of route groups. A group declares a shared layout
(header / sidebar / chrome) that wraps every child screen. Layouts nest,
and the SSR-first + island hydration contract from `core-ui/ARCHITECTURE.md`
is preserved: navigating between siblings inside the same layout swaps only
the inner content region, not the layout shell.

**Sketch**

- `ScreenGroup(prefix, layout, opts...)`
- `Layout` component contract: receives children, can host islands
- Nested groups → composed layouts, outermost → innermost
- Sibling-screen nav swaps inner content; cross-group nav swaps the
  appropriate layout boundary
- Islands declared inside a layout persist across child navigations
- Page-data and breadcrumbs flow up through the layout chain

**Acceptance**

- Initial load fully SSR-rendered, including all wrapping layouts
- Sibling-screen nav does not re-render the parent layout (DOM-stable)
- Layout islands keep their state across child navigations
- chromedp e2e asserts layout DOM node identity is preserved across sibling
  navigations

---

## 3. API versioning

**Status:** proposed (2026-05-21). Depends on §1.

First-class versioning for the auto-generated HTTP/CRUD surface, MCP tools,
and OpenAPI spec. Multiple versions of the same entity coexist; deprecations
are explicit and machine-readable.

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

Tier 1 (idempotency, health/ready, feature flags, outbound webhooks) and
Tier 2 (i18n, notifications, factories, admin UI) have all landed — see
`docs/{idempotency,health-checks,feature-flags,webhooks,i18n,notifications,factories,admin}.md`.

These remain:

### 4a. WebSocket primitive

SSE is excellent for push. Bidirectional surfaces (collab cursors, presence,
multiplayer islands) need a WebSocket equivalent in `core/stream/` with the
same backpressure rules as the existing SSE primitive.

### 4b. CLI scaffolding beyond kiln

`kiln` is the high-level live builder. A lower-level
`gofastr new entity Post --fields ...` for users who don't want the visual
flow.

### 4c. Configuration management

No first-class config loader (env + file + secret-source). Apps roll their
own with `os.Getenv`. A `core/config` with typed binding and validation
removes a class of bugs.

### 4d. Health-aware graceful shutdown contract

`App.Shutdown(ctx)` exists but there's no published contract for "drain
in-flight requests, stop accepting new ones, flush queues." A documented
lifecycle (and a `framework/lifecycle/` plugin hook) would let batteries
cooperate.

### 4e. i18n surface coverage

The `core/i18n` primitive shipped. Framework surfaces still emit hardcoded
English: entity field labels, validator error messages, `framework/ui`
defaults (Pagination, ValidationSummary, EmptyState, Banner / Toast / Modal
copy), `framework/crud` error response bodies, `battery/admin` page chrome,
and OpenAPI / `llm.md` auto-gen. Tier 2.5 follow-up: add `LabelKey` hooks to
entity configs, shift validators to error codes, replace literal strings in
`framework/ui` defaults with `i18n.T` calls behind English fallbacks.

### 4f. Battery follow-ups

- **Idempotency**: Redis backend (memory + SQL shipped).
- **Feature flags**: Redis store (memory + SQL shipped).
- **Webhooks**: Passkeys for auth (OAuth, magic link, 2FA already in
  `battery/auth`).

---

## 5. UI components — deferred / pick up later

Wave 1–7 have shipped. See `docs/ui-new-components.md` for the running list
of landed components. These remain deferred (design or scope work needed):

### 5a. Calendar / date picker

- **Layer:** `core-ui/widget/preset/`
- **Why deferred:** Big surface (single date / range / time / locale /
  min-max / disabled-dates) and design needs to settle before committing to
  an API.
- **Shape sketch:** anchored Popover preset with a server-rendered calendar
  island. RPC fetches month grids; selection submits via `Bind` to the
  underlying `<input>`. Must work with native `<input type="date">` as
  graceful fallback.
- **Pre-reqs:** Popover preset (already shipped).

### 5b. Dynamic form repeater

- **Layer:** `core-ui/patterns/`
- **Why deferred:** Form-array indexing and partial-island re-render
  contract need an explicit design pass — risk of leaking a half-baked array
  shape across the framework.
- **Shape sketch:** `Repeater(name, template)` pattern. Add/Remove buttons
  fire RPCs that re-render the list island; submission collects nested
  fields as `name[i].field`.
- **Pre-reqs:** May want a typed form-state helper in `framework/ui/form`
  before building this on top.

### 5c. Form step wizard

- **Layer:** `core-ui/patterns/`
- **Why deferred:** Needs a server-side step-state story (session? signed
  cookie? hidden cumulative form?) before picking an API. Overlaps with the
  upcoming form-state helper.
- **Shape sketch:** `Wizard(steps...)` with per-step RPC validation and
  Next/Back actions; final submit posts the accumulated payload.
- **Pre-reqs:** Form-state helper; possibly the repeater (§5b) for steps
  that contain arrays.

### 5d. Inline edit field

- **Layer:** `framework/ui/`
- **Why deferred:** Focus-management contract between SSR swap and the new
  input needs care; we want the runtime to grow a "post-swap focus" hint
  first so every island-replacing component benefits.
- **Shape sketch:** `InlineEdit(cfg)` renders a span; click swaps to an
  input, Enter saves via RPC, Escape reverts, blur saves. Validation errors
  render inline below the input.
- **Pre-reqs:** Runtime post-swap focus directive (`data-fui-focus`-style).

### 5e. Lightbox pinch-to-zoom

Touch event story (multi-touch, gesture cancellation). Punted from the
Wave-4 follow-up that landed the standalone Lightbox.

### 5f. BottomSheet drag-to-dismiss

Touch event story (drag with velocity threshold, snap-back).

### 5g. Carousel virtual-scroll

Current render emits all slides upfront; fine for product reels, costly for
image-heavy archive views (>50 slides).

### 5h. Form module follow-ons

Still unshipped from the form-module plan:

- **PasswordInput** — password field with show/hide toggle. Runtime toggles
  `type` between `password`/`text`. `aria-label` on toggle, announced state
  change.
- **SearchInput** — text input with search icon + clear button. Clear is
  `type="reset"` or JS clear.
- **InputGroup** — wrapper that prepends/appends text or icons to an input.
  Flex container with visual join. Config: Prepend (render.HTML), Append
  (render.HTML), Input (render.HTML).

---

## 6. Typed theme + variant system

**Status:** plan; implementation in progress on `worktree-core-ui-css`.

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
10. Migrate `examples/website/theme.go` to the new shape.
11. Docs + drift: `core-ui/ARCHITECTURE.md` typed-theme section, the
    `gofastr-ui` skill, extended drift test.

### Out of scope

- Per-variant tree-shaking.
- Multi-tenant runtime theme swapping (different themes per request based
  on tenant ID).
- Replacing the `ClientJS`-based action system with `data-fui-*` primitives.

---

## 7. Performance opportunities

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

**7c. FilteredList is +127% slower vs hand-rolled `net/http`.**
`BenchmarkT7_FilteredList_GoFastr/sqlite` is 161µs / 3187 allocs vs 71µs /
1881 allocs hand-rolled. Same SQL, same JSON output. The framework's list
handler does include parsing, filter parsing, soft-delete check, tenant
scope, projection, JSON casing — all on every request. Precompute per-
entity at registration time; skip parsers for entities that haven't opted
into the feature; pool `[]map[string]any` via `sync.Pool`. Target: halve
the gap (-127% → -60%).

**7d. JSON case conversion: 26 allocations per row, twice on write paths.**
`BenchmarkJSONCasing/snake→camel` is 19µs / 1048 B / 26 allocs for a
10-key row. List endpoints run snake→camel once per row × rows-per-page;
writes run both. Pre-compute the camel↔snake mapping at `Define()` time;
pool a `strings.Builder`; reuse `mapToCamelCase`'s output map. Target:
drop snake→camel below 10 allocs.

### P1 — solid wins, contained scope

**7e. SchemaDiff at 59ms for 50 entities on Postgres.**
`BenchmarkSchemaDiff/postgres/N=50` does N round-trips to
`information_schema.columns`. One bulk `WHERE table_name IN (...)` query
would do it. Target: 5-10× faster.

**7f. AutoMigrate idempotent re-run at 7.5ms for 50 entities (Postgres).**
Same root cause as 7e — one existence check per entity. Single bulk
`pg_tables` query before the per-entity loop. Target: sub-1ms regardless
of entity count.

**7g. CronTick allocates 1471 times per minute for 1000 jobs.**
`BenchmarkCronTick/N=1000` reports 175µs / 213KB / 1471 allocs per tick.
The snapshot copy + per-job match check allocates fresh slices. Replace
the copy with a read-locked iteration if mutations during tick are rare;
pre-sort by next-fire time so the tick breaks early. Target: ≤1 alloc per
tick regardless of N.

**7h. DSL parser allocates on every call.**
`BenchmarkDSLParse/complex` is 6µs / 7 allocs. Agents often issue the same
query template repeatedly; parsed `DSLQuery` could be cached by input
string in a bounded `sync.Map` LRU. Target: ~50ns / 0 allocs on cache hit.

### P2 — broader scope, real benefit

**7i. SSE backpressure drops half the burst.**
`BenchmarkSSE_BackpressureDropRate`: drop_rate **0.99** at 5000 events
through a 32-buffer + slow consumer. `BenchmarkT9_SSEEventStream`:
delivery_ratio **0.48** at 500 events end-to-end. The 32-event buffer is
hardcoded. Options: per-subscriber buffer via query param/header
(`?buffer=128`); per-entity default via `EntityConfig.EventBuffer int`;
`?slow=block` mode for clients willing to trade latency for delivery.

**7j. UI host page render is ~15× a bare JSON encode.**
`BenchmarkT9_UIHostPageRender` is 7.6µs / 580 bytes for a trivial page
vs 500ns for `BenchmarkT7_JSON_GoFastr`. The factor is the HTML tree
build + runtime script injection. Pool the `strings.Builder` that backs
`render.HTML`; cache the runtime script tag string; pre-flatten the
layout shell so only the screen render varies per request. Target: halve
render time for trivial pages.

**7k. Island RPC tail latency at parallelism=64.**
`BenchmarkT9_IslandRPC_Concurrency/parallelism=64`: p50 13µs, p99 **65ms**.
Wide gap is contention through the recorder + render allocations. Pool
the rendered output buffer at handler entry; pre-render static page
chrome once at startup. Target: p99 at par=64 should drop below 10ms.

**7l. Filtered list overhead allocations.**
Re-stated from 7c: 3187 allocs vs 1881 hand-rolled. Pool the response
writer's byte buffer; switch `[]map[string]any` to a typed result struct
when the entity has a generated model (the `framework/typed_query.go`
path already supports this — auto-detect and route to it).

### Doc-only fixes (no code change)

**7m. SQLite write serialisation under load.**
`BenchmarkT6_CreateConcurrency/sqlite3/parallelism=64`: p99 climbs to
112ms; only 10 writes complete out of 5072 ops in `BenchmarkT6_MixedRW`.
Update `docs/migrations.md` and `docs/security.md` with a "Concurrency
model" callout for SQLite. The framework already sets `MaxOpenConns(1)`
in test helpers; users should know to do the same in production or pick
Postgres.

**7n. cgo SQLite costs ~4MB binary + 440MB build RAM.**
Resource bench: `crud` is 12.9MB / 760MB build RAM; `minimal` is 8.8MB /
311MB. Document `modernc.org/sqlite` as a pure-Go alternative in
`docs/migrations.md`. Trade-offs: pure-Go a few % slower at query time,
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

**Status:** Phase 1 in progress on `runtime-code-split` branch.

Goal: shrink the parser-blocking JS payload on a typical page from ~31 KB
gz (one bundle, everything) to ~10 KB gz (core only) + lazy modules loaded
on hover / idle / first-use.

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
- **Phase 3** — server-side dep registration + preload tags.
  `framework/ui` and `core-ui/widget/preset` builders call
  `runtime.Need(modules...)`; `uihost` reads the per-request set and emits
  `<link rel="modulepreload">`. Trigger elements get `data-fui-prefetch`.
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
