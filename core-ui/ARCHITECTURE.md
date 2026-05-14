# GoFastr UI Architecture

> **Read this before writing any UI, runtime, or `framework/uihost` code.**
> Misunderstanding the model has caused multiple round-trips of rework.
> This document is the contract.

---

## The model in one paragraph

Every page is **server-rendered (SSR)** on first request — full HTML, full
data, no skeleton, no client-side data fetch on initial load. The browser
receives the rendered page and `runtime.js` **hydrates** the existing DOM:
attaches event listeners, signal bindings, SSE streams. After hydration,
**page-to-page navigation is client-side** (Angular-router style: pushState,
partial fetch, swap content, with cache so back is instant — no hard
refreshes). **Interactions that change state inside the current page are
handled by islands**: a click triggers an RPC to the server-side island
handler, which returns the updated island HTML; the runtime swaps just
that island's content. The rest of the page stays put. **Server-pushed
updates** (e.g. another user changed something) flow through signals + SSE
to update bound DOM nodes without any client action.

---

## The four scenarios

| Scenario | What runs | What's on the wire |
|---|---|---|
| **Initial load** of any URL | Full SSR via `framework/uihost` → `app.RenderPage` → `Screen.Load(ctx)` → `Screen.Render()` | One HTML response with everything inline |
| **Page → page navigation** (`/a` → `/b`) | Client-side router intercepts `<a>` click, fetches partial via `X-Gofastr-Navigate: 1`, swaps `<main>`, caches the previous page for instant back | One small partial HTML response (just the screen content); no full chrome re-fetch |
| **In-page state change** (sort, paginate, expand a row, open a tab) | Click on an island element → RPC to the island's handler → server returns new island HTML → runtime swaps just the island's slot | One small RPC response with the changed island HTML |
| **Server-pushed update** (background event, another user's action) | Server-side signal update → SSE event → runtime updates `[data-fui-signal="…"]` nodes | SSE frames over a single long-lived connection |

**Forms and mutations** follow the in-page pattern: POST to the island's
RPC handler, response carries the new island HTML.

---

## What is an island?

An island is a **server-rendered, server-driven component** with its own
RPC endpoints and (optionally) signal bindings. It owns:

1. Its rendered HTML (SSR).
2. Its server-side state (in memory or DB).
3. Its update logic (handlers that re-render and respond).

Pagination is an island. A sortable table is an island. A "favorite" toggle
on a card is an island. A page header that needs to react to user-scope
changes is an island. **Inline content that never changes** (a static
heading, a piece of marketing copy, a footer) is **not** an island — it's
just rendered HTML.

The framework primitives live in:

- `core-ui/widget` — the builder API (`widget.New(name).Mount(...).Slot(...).Signal(...).RPC(...).RPCWithSignal(...)`)
- `core-ui/island` — the runtime-side island manager (registration, SSE push, slot lookup)
- `core-ui/signal` — reactive state values that trigger SSE pushes
- `core-ui/runtime/runtime.js` — the client-side hydration runtime

---

## Runtime primitives (the wiring)

The runtime understands a small set of `data-fui-*` attributes on the
hydrated DOM. **You don't write JavaScript** — you compose these on the
server side and the runtime does the work.

| Attribute | Purpose |
|---|---|
| `data-fui-rpc="<path>"` | Click / form-submit fires a request to `<path>` |
| `data-fui-rpc-method="GET\|POST\|…"` | HTTP method (default POST) |
| `data-fui-rpc-signal="<name>"` | The response body is treated as a signal value and broadcast to bound nodes |
| `data-fui-rpc-close` | Containing widget closes on 2xx |
| `data-fui-rpc-reset` | Containing form resets on 2xx |
| `data-fui-signal="<name>"` | This node's content/attribute updates when the named signal changes |
| `data-fui-signal-mode="text\|html\|attr"` | How to apply the signal value (default `text`) |
| `data-fui-signal-attr="<attr>"` | Attribute name when mode is `attr` |
| `data-fui-open="<widget-name>"` | Click opens a registered widget surface |
| `data-fui-push-state="<path>"` | After the RPC succeeds, apply this URL via `history.pushState` (no re-fetch). Useful when the button knows the canonical URL ahead of time (e.g. pagination button "page 3" → `data-fui-push-state="?p=3"`). Server-supplied `X-Gofastr-Push-State` header takes precedence. |
| `data-fui-confirm="<message>"` | Pre-flight `window.confirm(<message>)` before firing the RPC. Cancel aborts. Use for destructive actions (delete, revoke). |
| `data-fui-rpc-trigger="input"` | On a `<form data-fui-rpc=…>`, dispatch the RPC on every `input` event from any control inside, after a debounce window. |
| `data-fui-rpc-debounce-ms="<ms>"` | Debounce window for `data-fui-rpc-trigger="input"`. Default 250. |
| `data-fui-rpc-after-text="<text>"` | On 2xx RPC, replace the trigger's text content with `<text>`. One-shot — idempotent on re-click via `data-fui-rpc-after-done`. |
| `data-fui-rpc-after-disable` | On 2xx RPC, mark the trigger as `aria-disabled="true"` and (for `<button>`/`<input>`) set `disabled=true` permanently. Use with `after-text` for "Saved ✓" / "Revealed ✓" feedback. |
| `data-fui-rpc-scroll-to="<selector>"` | On 2xx RPC, smooth-scroll the matching element into view. Use to direct the user's eye at newly-inserted content. |
| `data-fui-comp="<name>"` | Marks an instance of a registered styled component. The runtime scans for it on every DOM insertion and lazily loads `/<__gofastr/comp/<name>.css>` once per session via a `<link data-fui-style="<name>">` (dedup'd, never re-fetched). See "Component CSS" below. |
| `data-fui-bundle="<a,b,c>"` | Set on the SSR-emitted bundle `<link>` to list the components it covers. The runtime reads it at boot and seeds `_pendingLinks` so the per-component scan never double-loads anything already in the bundle. |
| `data-fui-disclosure` | Marks a `<details>` element as a dismissible disclosure (mobile hamburger nav, popover, etc.). The runtime closes it automatically on SPA navigation and when Escape is pressed anywhere on the page (native `<details>` only handles Escape when the `<summary>` itself has focus). |
| `data-fui-action="<name>"` | Marks an element as a server-action trigger. Used together with `data-fui-rpc` to dispatch a named action. |
| `data-fui-widget="<name>"` | Marks a registered widget instance — the runtime mounts behavior on it after first paint. |
| `data-fui-backdrop` | Marks an element as a click-to-dismiss overlay backdrop. Pairs with `data-fui-open` to make the floating surface dismissible. |
| `data-fui-style="<name>"` | Set on the runtime-injected `<link rel="stylesheet">` so duplicates are dedup'd by component name. |
| `data-fui-shortcut-click="<chord>"` / `data-fui-shortcut-focus="<chord>"` | Global keyboard shortcut: e.g. `Meta+K` or `/` focuses or clicks the target element. |
| `data-fui-submit-on-enter` | On a `<form>`, Enter inside any child textarea submits the form. |
| `data-fui-clear-on-esc` | On an `<input>`/`<textarea>`, Escape clears the value. |
| `data-fui-autogrow` | On a `<textarea>`, height auto-grows with content. |
| `data-fui-charcount-source="<id>"` | An element that displays the live character count of the referenced input. |
| `data-fui-copy-text-from="<selector>"` | A button that copies the source element's text to the clipboard on click. |
| `data-fui-fill-input="<selector>"` / `data-fui-fill-text="<selector>"` | A button that fills the target input or text node with this element's `data-value` (or text content). |
| `data-fui-disable-when-invalid` | On a submit button: disabled while any field in the surrounding `<form>` reports `:invalid`. |
| `data-fui-persist-storage="<key>"` | The element's value persists across reloads in `localStorage` under the given key. |
| `data-fui-flash-on-update` / `data-fui-flash-duration-ms="<ms>"` | A signal-bound element flashes (CSS class `is-fui-flash`) for `<ms>` after each update. |
| `data-fui-scroll-bottom-on-update` | A signal-bound scroll container auto-scrolls to the bottom on each update (chat / log views). |
| `data-fui-tick-elapsed="<unix-ms>"` | Element's text updates once per second with the elapsed human-readable interval since the given epoch. |
| `data-fui-rpc-body="<json>"` | Static JSON body for `data-fui-rpc` requests that don't come from a `<form>`. |
| `data-fui-rpc-after-done` | Internal marker — set by the runtime after a one-shot `after-text` / `after-disable` fires so re-clicks are idempotent. |

For the authoritative list, grep `data-fui-` in `core-ui/runtime/runtime.js`. Adding a new attribute requires updating this table AND adding a runtime test.

**Response headers the runtime understands:**

| Header | Effect |
|---|---|
| `X-Gofastr-Push-State: <path>` | Apply via `history.pushState` after the RPC succeeds (URL update without re-fetch) |
| `X-Gofastr-Partial: true` | Body is a screen-partial (used by the cross-page nav path) |
| `X-Gofastr-Title: <text>` | Update `document.title` after partial swap |

**The flow for an in-page update** (e.g. clicking "page 2" on a pagination island):

```
[click]
  → button has data-fui-rpc="/island/customers/page" data-fui-rpc-method="POST" data-fui-rpc-signal="customers-rows"
  → runtime POSTs {"page": 2}
  → server handler computes new rows, renders HTML, returns it
  → runtime treats the response body as the new value of signal "customers-rows"
  → every node with data-fui-signal="customers-rows" data-fui-signal-mode="html" gets innerHTML replaced
  → no URL change, no <main> swap, no other DOM touched
```

---

## The three failure modes (do not repeat)

These are the misreadings of the model that have already cost rework.
Future agents: **if you find yourself doing one of these, stop and re-read this doc.**

### Failure 1: "Just intercept all link clicks for SPA"
> Symptom: every `<a href>` is hijacked by the runtime to do partial fetches.

That breaks the model for two reasons:
- **State-change interactions** like pagination should NOT be `<a href="?p=2">` links at all. They are island RPCs. The fact that I built pagination as a route-changing link is the root cause; "fixing" the link interception only papered over it.
- **Cross-page navigation** (a real `/a` → `/b`) IS supposed to be intercepted (Angular-router style with cache). Don't disable that to "fix" pagination — pagination wasn't a route in the first place.

### Failure 2: "Make every interaction a server round-trip via full nav"
> Symptom: clicking a sort header reloads the entire page.

That's a hard refresh. The model rules it out for in-page state. Use an
island RPC.

### Failure 3: "Make every interaction client-only after hydration"
> Symptom: pagination only works for datasets that fit in one render; the
> client manages pagination state in JS.

That's a different model (Stimulus-style). It conflicts with "islands are
server-driven" and "the server is the source of truth". For datasets
larger than first paint it just breaks. Use an island RPC and let the
server do the math.

---

## Why this model

- **Stability**: islands are stateless per-request HTTP. RPC is easy to
  retry, easy to log, easy to observe. SSE handles only push (no
  user-action-via-SSE), so connection-pool concerns stay manageable.
- **Scalability**: HTTP/2 multiplexes RPCs; a paginating user fires ~1
  request per click. SSE per session is one long-lived connection only
  for the genuine push channel — it is NOT how user actions reach the
  server. Mixing the two is fine; do not collapse them.
- **Server is source of truth**: pagination math, sort comparators,
  filtering rules, validation — all live in Go. The client never
  re-implements them. JS code shipped to the browser stays small and
  generic (the runtime is a few hundred lines of vanilla JS).
- **Hydration on SSR**: the first paint is a fully-rendered, accessible,
  scrape-able HTML document. Clients without JS get the same content,
  just without the interactivity layer. SEO + accessibility come for free.
- **No hard refreshes**: cross-page nav swaps content; in-page nav swaps
  islands. The runtime stays loaded; signals/SSE/cache survive.

---

## Recipes

### How to build a page

1. Implement `component.Component` (`Render() render.HTML`). Optionally also:
   - `app.ScreenTitler` / `app.ScreenDescriber` / `app.ScreenTyper` — individual optional interfaces so `app.Register(path, comp, layout)` reads just the metadata the component declares. Implement one, two, or all three (the combined `app.ScreenSpec` embeds all three for convenience). `ScreenTyper` defaults to `ScreenPage` when not implemented.
   - `app.ScreenLoader` — `Load(ctx) error` runs once per request after DI injection, before render
   - `app.ParamSetter` — `SetParams(map[string]string)` receives route params from dynamic paths
   (`Screen` itself is a struct value the router holds — not the interface you implement on your component.)
2. Inside Render, compose `core-ui/html` (1:1 tag primitives) +
   `core-ui/patterns` (accordion, tabs, pagination…) + `framework/ui`
   (semantic components like PageHeader, FormField, DataTable).
3. Anything that changes state in response to a user action → wrap it in
   an **island** (see below).
4. Register on the app router.

### How to build an island

1. Define a `widget.Definition` via the builder:
   ```go
   def := widget.New("customer-list").
       Slot("rows", &CustomerListRows{State: …}).
       RPCWithSignal("POST", "/islands/customers/page", pageHandler, "customer-list-rows")
   ```
2. The slot's component renders the current state. The handler reads
   request data, mutates state, returns the new HTML.
3. The runtime sees `data-fui-rpc="/islands/customers/page"` on the
   pagination button and `data-fui-signal="customer-list-rows"` with
   mode=html on the rows wrapper. Click → RPC → response → swap.
4. No `<a href>`. No URL change unless you opt into deep-linking via
   `pushState` from the handler's response (still no full reload).

### URL params are the source of truth

This is non-negotiable. Every island state that you want to be
**refreshable, shareable, bookmarkable, or back-button-able** must
round-trip through the URL. Refresh = same view. Share-link = same
view. Browser-back = previous view.

The flow:

1. **Initial load** (`/customers?p=2`):
   `Screen.Load(ctx)` reads `app.QueryFromContext(ctx).Get("p")` and
   pre-builds the island in its page-2 state. SSR ships fully-rendered
   page 2. The user can refresh and get exactly this — no JS required.

2. **Click "page 3" inside the island**:
   - The button is `data-fui-rpc="/islands/customers/page" data-fui-rpc-method="POST"`.
   - The RPC handler reads `{"page": 3}`, mutates server-side state,
     renders the new rows, and returns the HTML.
   - **The handler also returns an `X-Gofastr-Push-State: ?p=3` header.**
     The runtime applies it via `history.pushState` — URL becomes
     `?p=3` — but **does not** trigger a fetch. Just URL update + island swap.
   - Result: URL and DOM are now consistent. Bookmark works. Share works.

3. **Browser back**:
   - `popstate` fires with the previous URL (`?p=2`).
   - The runtime fetches the screen partial for the new URL via
     `X-Gofastr-Navigate: 1` (the same flow as cross-page nav) and
     swaps `<main>`. The screen's `Load(ctx)` reads `?p=2` again, the
     island is re-rendered server-side at page 2, partial response
     comes back, runtime swaps. The cache makes typical back-stack
     transitions feel instant.
   - This means popstate triggers a full screen-partial re-fetch, not
     a per-island patch. It's coarser than the click path but it's
     simpler and correct — and it's still way cheaper than a hard
     reload.

4. **Refresh / share / bookmark**: trivially correct because step 1
   handles the case.

This is "URL as the canonical state, RPC for the fast path, partial
re-fetch for popstate". You do not need to re-implement state in JS.
You do not need to teach the runtime about pagination. The runtime
just knows: *RPC response can carry a `pushState` header; popstate
triggers a screen-partial fetch.*

---

## Theme

The framework's design tokens live in `core-ui/style.Theme` — a
**typed Go struct** with a fixed canonical field set: `Colors`,
`Spacing`, `Radii`, `Fonts`, `Breakpoints`, `Shadows`, `ZIndex`,
`Durations`, `Typography`. Every token carries a `Name` (the CSS
custom property identifier) and a `Value` (the concrete value):

```go
t := style.DefaultTheme()
t.Colors.Primary // → style.Color{Name: "primary", Value: "#4F46E5"}
t.Colors.Primary.CSS()   // → "var(--color-primary)"
t.Colors.Primary.Value   // → "#4F46E5"
```

### The var-only contract

**Components always emit `var(--*)` references.** Build-time
resolution of `{tokens.text}` to literal hex values has been
removed; every reference is a CSS variable indirection. This is
required for section-level theme overrides via the CSS cascade —
a parent `.fui-theme-<hash> { --color-text: #f4f4f5 }` overrides
every descendant's `var(--color-text)` automatically. The hash is
content-derived from the overridden tokens (see `RegisterThemeOverride`),
so apps don't pick the class name — they pass an override struct and
get a stable hash back.

### Overriding tokens

Apps mutate fields directly — there's no MergeThemes helper:

```go
t := style.DefaultTheme()
t.Colors.Primary = style.Color{Name: "primary", Value: "#14B8A6"}
app.WithTheme(t)
```

`framework/ui/theme.Default(theme.Overrides{Primary: "#…"})`
wraps this pattern as a convenience for the most common cases.

### Apps with extra tokens

Embed `style.Theme` in your own type and add fields:

```go
type AppTheme struct {
    style.Theme
    Brand struct { Logo, Glow style.Color }
}

var App = AppTheme{Theme: style.DefaultTheme()}
App.Brand.Logo = style.Color{Name: "brand-logo", Value: "#FF00FF"}
```

`style.CSSCustomPropertiesOf(App)` walks both canonical and
embedded fields via reflection, emitting a unified `:root` block.
Framework code only sees the embedded `style.Theme`; app-local
components reference `theme.App.Brand.Logo` directly.

### Section-level theme overrides

Need a dark sidebar in an otherwise-light app? Branded callouts?
Per-tenant subtree theming? Use `style.RegisterThemeOverride` +
`ui.Themed`:

```go
// Register at package init — content-addressed, so re-registering
// the same theme returns the same handle and ships CSS only once.
var Dark = style.RegisterThemeOverride(func() style.Theme {
    t := style.DefaultTheme()
    t.Colors.Background = style.Color{Name: "background", Value: "#0a0a0a"}
    t.Colors.Text       = style.Color{Name: "text",       Value: "#f4f4f5"}
    return t
}())

// Wrap any subtree to apply the override.
ui.Themed(Dark,
    ui.Section(ui.SectionConfig{Heading: "Settings"},
        ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
    ),
)
```

The framework emits one `.fui-theme-<hash> { --color-…: …; }` block
in `app.css` for every registered override. The wrapped `<div
class="fui-theme-<hash>">` scopes the override via CSS variable
cascade — no per-component changes, no inline `<style>`, no extra
HTTP requests beyond the always-present app.css.

### app.css — one asset, one request

The framework serves a single `/__gofastr/app.css` per app:
theme :root custom properties concatenated with `WithCustomCSS`
payload. Replaces the legacy `theme.css` + `styles.css` split.
SSG output emits the same single file. (`/__gofastr/theme.css`
and `/__gofastr/styles.css` are retained as `410 GONE` so stale
references surface clearly.)

---

## Component CSS

Every component-owned stylesheet ships as a real `<link>` —
**never inline** — loaded lazily per-component, dedup'd globally,
and **always scoped** to `[data-fui-comp="<name>"]`. There is no
"unscoped component CSS"; global rules (resets, typography, theme
tokens) live in `theme.css` / `WithCustomCSS`.

### The model in one paragraph

A component declares its CSS by calling
`registry.RegisterStyle(name, fn)` in a package var; the handle's
`.Render(c)` wraps the component's output and injects
`data-fui-comp="<name>"` onto its outermost tag (no extra DOM
node). The SSR host string-scans the final rendered HTML for those
markers and emits **one** `<link rel="stylesheet">` in `<head>` for
the page's exact set of components. After hydration, the runtime
scans newly inserted DOM (cross-page swap, island response, widget
mount) and lazy-loads any new component's CSS as a `<link>` once
per session, dedup'd by `data-fui-style="<name>"`. The browser
caches the stylesheet by URL (`/__gofastr/comp/<name>.css?v=<hash>`)
under `immutable` headers in prod — content-addressed via the
component's CSS hash, so a deploy that changes the sheet busts the
URL automatically.

### Three load modes

| Mode         | First-paint cost           | Behavior                                                                              |
| ------------ | -------------------------- | ------------------------------------------------------------------------------------- |
| `LoadAlways` | 1 request, render-blocking | SSR emits `<link>` in `<head>` on every page, regardless of whether the page renders the component. Use for chrome that's on essentially every screen. |
| `LoadAuto` (default) | 0 (deferred)               | SSR collector emits `<link>` only on pages that actually render the component. After hydration, the on-demand scanner picks up newly-inserted markers from partial responses. |
| `LoadPrewarm`| 0 (deferred)               | `LoadAuto` plus a throttled `requestIdleCallback` prefetch (serialized, one in flight). Use for components that are likely soon (a hotkey-opened palette). |

All three converge on `loadComponentCSS(name)`. The function is
**synchronous** — no `await` between the existence check and
`appendChild`, plus a `_pendingLinks` guard — so promoting a
component across modes or having two scans race never produces a
duplicate request.

### The bundle endpoint

When a single SSR page references ≥2 components, the host emits
one bundled `<link rel="stylesheet" href="/__gofastr/comp-bundle.css?names=a,b,c&v=<combinedHash>">`
instead of N individual links. The bundle handler concatenates the
per-component scoped CSS in the sorted order embedded in the URL.
Content-addressed via the SHA of the concatenated component
versions, served `immutable` in prod. After hydration, the on-
demand path uses single-component `<link>`s; the bundle is just a
first-paint optimization.

### Runtime catalog

The host SSR-embeds an inert JSON block in `<head>`:

```html
<script type="application/json" id="gofastr-catalog">
{ "<name>": { "stylePath": "/__gofastr/comp/<name>.css", "version": "…", "loadMode": "auto" } }
</script>
```

The runtime reads `JSON.parse(document.getElementById('gofastr-catalog').textContent)`
at boot, so `loadComponentCSS` can resolve a marker name to a URL.
This is strict-CSP-clean (no inline JS, no separate script src) —
the legacy `/__gofastr/catalog.js` endpoint now returns 410 GONE.

### Adding a styled component

```go
// modal/modal.go
var Style = registry.RegisterStyle("modal", modalCSS)

func modalCSS(t style.Theme) string {
    return style.NewComponentSheet("modal", t).
        Rule(".header").Set("font-weight", "{fonts.weight.bold}").End().
        Rule(".body").  Set("padding",     "{spacing.lg}").End().
        MustBuild()
}

type Modal struct { Title string }
func (m *Modal) Render() render.HTML {
    return render.Tag("div", attrs("modal"), render.HTML(`
        <div class="header">`+m.Title+`</div>
        <div class="body">…</div>`))
}

// at a render site:
modal.Style.Render(&modal.Modal{Title: "Hi"})
```

`registry.RegisterStyle` panics at process startup on conflicting
re-registration (different StyleFn under the same name) and on
unscopable selectors (`body`, `html`, `:root`, `*`, `::backdrop`,
`::view-transition-*`). Authors `go test` a sheet without chromedp
by building the `ComponentSheet` directly and asserting on bytes.

### What about widgets?

The `core-ui/widget` registry continues to drive widgets (their
position chrome, slot composition, RPC endpoints). Widgets that
host styled components benefit from the same on-demand loader: the
mounted chrome HTML is scanned in `mountWidget` and any new
`data-fui-comp` triggers a load. Widget chrome CSS itself still
serves from `/core-ui/widget/<name>/style.css` for backwards
compatibility; future work may collapse the two paths.

Both registries coexist safely: they share the
`data-fui-style="<name>"` link dedup key on the client, so a widget
and a registered styled component can never double-load CSS even
if a future change merges them. Widgets surface through
`/__gofastr/widgets`; styled components surface through
the inline `<script id="gofastr-catalog">` JSON block and `/__gofastr/comp/<name>.css`.

---

## What lives where

```
core-ui/
  app/         — screen + router + layout + request-in-context helpers
                 (the URL → rendered page pipeline)
  di/          — dependency injection container (used by app)
  html/        — semantic HTML primitives, 1:1 with HTML tags
                 (Div, Button, Heading, Form, Table…)
  patterns/    — composed UI patterns (not 1:1 with HTML):
                 accordion, breadcrumbs, pagination, progress,
                 skeleton, tabs
  component/   — Component / InteractiveComponent interfaces (the contract
                 every renderable satisfies)
  widget/      — island/widget builder + registration
  widget/preset/ — opinionated mounting shortcuts (Modal, Toast, Drawer…)
  widget/theme/ — page-level theme tokens + utility classes
  signal/      — reactive state + SSE push
  island/      — runtime-side island manager
  runtime/     — runtime.js (client) + Go embed wrapper
  style/       — theme structs, stylesheet builder, token resolution
  check/       — .ui.go linter

framework/
  uihost/      — wires core-ui app onto framework.App router; serves
                 runtime.js, theme.css, styles.css; handles SSE; client-side
                 navigation partial-fetch endpoint
  static/      — SSG builder (renders every screen at build time)
  ui/          — opinionated semantic components on top of core-ui (PageHeader,
                 FormField, DataTable, EmptyState…)
  ui/theme/    — canonical framework theme tokens
```

---

## Hard rules

1. **Never** treat in-page state changes as routes. No `<a href="?p=2">` for pagination.
2. **Never** re-implement pagination/sort/filter logic in JS. Server-side, always.
3. **Never** make user-action-driven updates flow through SSE. SSE is for server-pushed updates only. RPC is for user-initiated updates.
4. **Never** introduce a hard refresh as a fix. If you find yourself doing `location.href = …`, stop.
5. **Never** add new `data-fui-*` attributes without updating this doc and the runtime test suite.
6. **Always** start with `Screen.Load(ctx)` reading initial state (route params, query) and SSR-ing the first paint correctly.
7. **Always** prefer composing existing widget/preset shortcuts over building a new island from scratch.
