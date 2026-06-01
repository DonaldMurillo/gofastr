---
name: gofastr-ui
description: Auto-loads when working on UI, runtime, or framework/uihost code in the GoFastr repo. Encodes the SSR-with-hydration architecture (no hard refresh, page-nav swaps content, in-page state is island RPC) and the three failure modes that have already happened. Triggers on edits to core-ui/, framework/ui/, framework/uihost/, examples/website/, or runtime.js — and on phrases like "pagination", "sort header", "tab click", "navigation", "SPA", "hydration".
---

# GoFastr UI architecture — load this before writing UI code

This skill auto-loads whenever you touch the UI surface. The model has been
misread three times in this repo. The canonical document is
`core-ui/ARCHITECTURE.md` — **read it now if you have not already.**

## The model (one paragraph)

SSR-first. Server fully renders every page on first request. `runtime.js`
hydrates the existing DOM (no re-render). Cross-page navigation
(`/a` → `/b`) is client-side via partial fetch + cache, no hard refresh.
In-page state changes (pagination, sort, filter, expand) are **islands**:
a click fires an RPC, the server returns new island HTML, the runtime
swaps just that island's content. Server-pushed updates flow through
signals + SSE for background events only — not user actions.

## The three failure modes — refuse to do these

### ❌ Treating in-page state as a route
**Symptom**: pagination/sort renders as `<a href="?p=2">` and triggers
either a full reload or an SPA route change to "the same page with
different query params".
**Correct**: pagination is an island. The buttons are `data-fui-rpc=...`
NOT `<a href>`. The server has an RPC handler that returns the new
rendered rows.

### ❌ Hard refresh on every interaction
**Symptom**: clicking a sort header reloads the whole page.
**Correct**: island RPC. The runtime swaps the island, the rest of the
page stays.

### ❌ Client-only after hydration
**Symptom**: pagination math lives in JS; the server doesn't know about
page 2.
**Correct**: server is the source of truth. Always. JS shipped to the
browser is the generic runtime (`runtime.js`) — never feature-specific
code.

## What you compose

| Need | Use |
|---|---|
| Static HTML primitive (`<button>`, `<h1>`, `<table>`) | `core-ui/html` |
| A composed UI pattern (accordion, tabs, pagination, breadcrumbs) | `core-ui/patterns/<name>` |
| A semantic component (PageHeader, FormField, DataTable) | `framework/ui/` |
| An island (server-rendered, server-state-owning, RPC-updatable) | `core-ui/widget` (builder API: `New(name).Slot(...).RPCWithSignal(...)`) |
| A theme token | `framework/ui/theme` (canonical typed `style.Theme`; mutate fields directly) |
| An interactive component (RPC, signals, widget chaining) | `core-ui/interactive` (`OnClick(html, Post("/path").OnSuccess(...))`) |
| **Per-component CSS** (loaded on demand, dedup'd, never re-fetched) | `core-ui/registry` (`RegisterStyle` + `Style.WrapHTML`) |

## Theme — typed, always emits var()

The framework's design tokens live in `style.Theme` — a typed Go struct.
Every token (`style.Color`, `style.Spacing`, `style.Shadow`, etc.) has a
`.CSS()` method that returns `var(--<category>-<name>)`. Build-time
literal resolution is gone — every reference goes through a CSS variable,
which is what lets section-level theme overrides cascade.

**Common patterns:**
- Override one token: `t.Colors.Primary = style.Color{Name: "primary", Value: "#14B8A6"}`.
- App-extended tokens: embed `style.Theme` in an `AppTheme` struct + add brand fields.
- `style.CSSCustomPropertiesOf(theme)` walks the canonical + embedded fields, emits `:root { --…: ...; }`.

**Hard rules:**
- ❌ Never write literal `#hex` colors in component CSS. Always `var(--color-x)` or `t.Colors.X.CSS()`.
- ❌ Don't try `MergeThemes(...)` — it was removed. Mutate fields directly.
- ✅ `app.WithTheme(theme)` is the binding pattern. The host emits `:root` from the theme and components reference it.

**Section-level theme overrides** (dark sidebar in a light app, branded sections, multi-tenant subtrees) — use `style.RegisterThemeOverride` + `ui.Themed`:

```go
var Dark = style.RegisterThemeOverride(darkTheme)

ui.Themed(Dark,
    ui.Section(ui.SectionConfig{Heading: "Dark"},
        ui.Button(ui.ButtonConfig{Label: "OK"}),
    ),
) // wrapped subtree's var(--color-*) reads from Dark via CSS cascade
```

Framework emits a `.fui-theme-<hash>` block in `app.css`; the CSS cascade does the rest. Content-addressed — registering the same theme twice ships CSS once.

## Per-component CSS — the registry pattern

Component-owned CSS ships as a real `<link>` (never inline), loaded
lazily on first appearance, dedup'd globally, and always scoped to
`[data-fui-comp="<name>"]`. Global resets / typography / theme tokens
stay in `theme.css` or `WithCustomCSS`.

```go
// styles_mything.go — registration + builder
var myThingStyle = registry.RegisterStyle("ui-my-thing", myThingCSS)

func myThingCSS(t style.Theme) string {
    return style.NewComponentSheet("ui-my-thing", t).
        Rule("&").Set("display", "flex").End().              // & = the marker element
        Rule(".header").Set("font-weight", "700").End().     // descendant
        Rule(".body").Set("padding", "{spacing.lg}").End().
        MustBuild()
}

// at the render site — wrap the outer tag with .WrapHTML
func MyThing(cfg MyThingConfig) render.HTML {
    return myThingStyle.WrapHTML(html.Div(html.DivConfig{Class: "ui-my-thing"}, …))
}
```

**Load modes:**
- `LoadAuto` (default) — load when marker first hits DOM. SSR emits link on pages that use it.
- `LoadPrewarm` — same as Auto + throttled `requestIdleCallback` prefetch.
- `LoadAlways` — emit link on every page (use for chrome on essentially every screen).

**The contract applies to `core-ui/patterns/*` too.** Patterns
(accordion, breadcrumbs, nestedlist, pagination, progress, skeleton,
tabs, …) register via `registry.RegisterStyle("<name>", styleFn)` and
wrap their top-level `Render()` element in `Style.WrapHTML(...)`. The
legacy `func BaseCSS() string` export pattern is **forbidden** — the
2026-05-19 nestedlist incident shipped without styling because the
host's theme.go was never updated to concatenate it. The lint
`core-ui/check.LintNoPatternBaseCSS` fails CI on any new pattern that
re-exports `BaseCSS`. The pattern-CSS unification landed 2026-05-19.

**Hard rules:**
- ❌ Never export `func BaseCSS() string` from a `core-ui/patterns/*`
  package. Register via `RegisterStyle` and wrap via `WrapHTML`.
- ❌ Never write inline `<style>` blocks for component CSS — always go through the registry.
- ❌ Never write selectors that try to escape the scope (`body`, `html`, `:root`, `*`, `::backdrop`) — `ComponentSheet` rejects them at process startup.
- ✅ Use `&` in `ComponentSheet` to reference the marker element itself.
- ✅ Test CSS without chromedp by building the `ComponentSheet` directly.

## Runtime data-attributes (do not invent new ones without updating the doc)

| Attribute | Effect |
|---|---|
| `data-fui-rpc="<path>"` | Click/submit fires HTTP request |
| `data-fui-rpc-signal="<name>"` | Response body becomes the value of signal `<name>` |
| `data-fui-signal="<name>"` mode=`text\|html\|attr` | Element auto-updates when the signal changes |
| `data-fui-open="<widget>"` | Opens a mounted widget |
| `data-fui-comp="<name>"` | Marker for a registered styled component — runtime loads `/__gofastr/comp/<name>.css` once |

The runtime + the data-attributes ARE the API surface for hydration.
Adding new attributes requires updating `core-ui/ARCHITECTURE.md` and
the runtime test suite — every attribute is a public contract.

## URL as source of truth

State worth bookmarking / refreshing / back-buttoning lives in the URL.
The flow:

1. Initial load: `Screen.Load(ctx)` reads `?p=2` etc. and SSR's page 2.
2. Click in-island button: RPC returns new HTML AND
   `X-Gofastr-Push-State: ?p=3` header → runtime applies pushState (no fetch).
3. Browser back: popstate → runtime fetches screen partial for the
   new URL → swap → cached for instant forward.
4. Refresh: same URL → SSR renders the right state.

## Before you ship UI code, check

- [ ] Initial render is full SSR — refresh on the URL produces the same DOM.
- [ ] No `<a href="?…">` for state changes; that's an island RPC.
- [ ] In-page state worth sharing/bookmarking is in the URL via the
      `X-Gofastr-Push-State` response header.
- [ ] No `location.href = …` in JS or in the server response.
- [ ] No JS feature code; only the generic runtime.
- [ ] Cross-page links are normal `<a>` — the runtime intercepts them
      transparently for the partial-fetch + cache flow.
- [ ] `Screen.Load(ctx)` populates state from route params + query so
      deep-links / refresh / SSG all work.

## Stop and re-read `core-ui/ARCHITECTURE.md` if

- You're about to write `data-fui-spa` or any "opt into SPA mode" attribute.
- You're about to make pagination an `<a href>`.
- You're about to add a runtime endpoint that's not part of the
  SSR / page-nav / island-RPC / SSE-push grid.
- You're about to write client-side feature logic (sorting, filtering,
  paginating, validating) in JS.

If any of those tempt you, the model is being violated. Stop, ask the
user to confirm a deviation, and document the new pattern in
`core-ui/ARCHITECTURE.md` before shipping.
