---
name: component-build
description: Auto-loads when building, adding, or extending a UI component, widget, island, or any surface in the GoFastr framework — including modals, drawers, dropdowns, toasts, sidebars, banners, popovers, and any other interactive primitive. Encodes the "minimal-register + SSR-inline + hydrate" contract every component must follow. Triggers on phrases like "add a widget", "build a component", "new modal", "create dropdown", "add island", "wire a surface", or whenever the work touches core-ui/widget/, core-ui/widget/preset/, framework/ui/, or registers a widget.Definition.
---

# Building a component — the contract every primitive follows

This is the framework's pattern for any UI surface that needs server-rendered
HTML, client-side hydration, and an optional dismiss/lifecycle. Modal, Drawer,
Toast, Menu, Sidebar all follow it. New surfaces must follow it.

**Do not build a component that downloads its HTML through a JSON catalog,
constructs DOM at runtime when SSR would have done it, or fires per-mount
network calls for state it doesn't have.** Those mistakes have happened in
this repo and produce the same symptoms each time: empty view-source, slow
first-open, redundant `/state` round-trips, deep-link flicker.

## The four-rule contract

### 1. Register MINIMAL metadata, fetch chrome lazily

The registry (`/__gofastr/widgets`) is an index, not a payload. Each entry
ships only the data the runtime needs to decide what to do:

```json
{
  "name":          "components-confirm",
  "position":      "center",
  "backdrop":      true,
  "closeOnEscape": true,
  "stylePath":     "/core-ui/widget/components-confirm/style.css",
  "chromePath":    "/core-ui/widget/components-confirm/chrome",
  "statePath":     "/core-ui/widget/components-confirm/state",  // omitted when no signals
  "deepLinkKey":   "modal",
  "deepLinkValue": "user-edit",
  "deepLinkParams": ["user_id"]
}
```

Chrome HTML is fetched from `chromePath` only when the runtime actually needs
it — and only when SSR hasn't already inlined the element. The state endpoint
is omitted entirely when the widget declared no signals.

**When you add a new field to the registry**: add it to `serveWidgetList` in
`core-ui/widget/server.go`, mirror it on the runtime catalog in
`core-ui/runtime/runtime.js`, and document it.

### 2. SSR-inline whenever the URL says the surface should be open

In `framework/uihost`, every registered widget is walked per-request:

```
deeplinked widget whose key/value matches r.URL.Query()  →  inline OPEN
hidden widget that doesn't match                          →  inline with [hidden]
visible (non-hidden) widget                               →  inline OPEN
```

The widget chrome lands just before `</body>`. The runtime's `_mountByName`
checks for an existing `[data-fui-widget="<name>"]` root before fetching
`chromePath` — if one exists, the runtime hydrates in-place. View-source
shows the modal. Refreshing a deep-link paints instantly with no flicker.

When you ship a new widget preset: confirm the chrome HTML appears in
`curl http://localhost:.../some-page | grep <distinctive-text>`. Zero hits
means the SSR-inline path didn't fire.

### 3. Hydrate, don't reconstruct

`mountWidget` takes either an `existingEl` (SSR-inline path) OR a `chromeHTML`
string (lazy-fetch path). Both reach the same wired state — handlers, focus
trap, scroll lock, signal listeners. When dismissed, a hydrated widget is
**hidden in place** (`hidden` attribute), never detached, so re-opening
doesn't refetch.

Never write a runtime path that builds DOM the server could have built.

### 4. Skip the network call when there's nothing to fetch

- `cfg.statePath` is absent when the widget has no signals → no `/state` fetch.
- `cfg.chromePath` is bypassed when SSR-inline succeeded → no chrome fetch.
- Toast push is `X-Gofastr-Toast` response header on an existing RPC, not a
  separate SSE channel → no per-page connection budget cost.

If you're adding a network call to a mount path, the bar is: "can the server
have already given us this on the response that brought us here?" — almost
always yes.

## Building a new widget preset, step by step

```go
// 1. preset.go — return a Builder with sensible defaults.
func Dropdown(name string) *widget.Builder {
    return widget.New(name).
        Mount(widget.TopLeft).
        Hidden().                // click-to-open
        Role("menu").
        // …other defaults
}

// 2. host wires it once at startup.
def := preset.Dropdown("user-menu").
    LabelledBy("user-menu-title").
    Slot("body", ...).
    Build()
widget.Mount(r, &def)   // registers metadata + the three /style.css /state /chrome routes

// 3. trigger element in any screen.
<button data-fui-open="user-menu">Open</button>

// 4. (optional) deep link
preset.Modal("user-edit").
    Hidden().
    DeepLink("modal", "user-edit").
    DeepLinkParam("user_id").
    Slot("body", &UserEditForm{}).
    Build()

// 5. (optional) server response triggers a toast
ui.AddToastSuccess(w, "Saved", "Your changes are persisted.", 5000)
w.WriteHeader(http.StatusNoContent)
```

## Verifying you got it right

Before claiming a component works:

1. **Lint** — `go run ./cmd/check-csp` is clean (no inline `style=`/`<script>`).
2. **Unit** — `go test ./framework/ui/... ./core-ui/widget/...` is green.
3. **SSR inline** — `curl http://localhost:8082/<page> | grep "<distinctive
   text from the slot>"` returns ≥ 1 match.
4. **Zero ghost network calls** — open Playwright, click the trigger,
   `browser_network_requests` shows no `/state`, no `/chrome`, no `/widgets`
   request fires per open (the registry fetch is once at boot).
5. **Hydration roundtrip** — opening + closing + reopening exercises both
   paths; verify nothing is detached or refetched.
6. **A11y** — for backdrop'd surfaces: `role`, `aria-modal`, `aria-labelledby`
   set; focus moves in on open, traps via Tab, returns on close; body scroll
   locks while open.
7. **Browser-verified** — actually open the page in a browser before
   claiming done. `go test` passing is necessary, not sufficient.

## Interaction tests, not attribute tests

**Attribute checks (`role`, `aria-*`, classes) are necessary but NEVER
sufficient.** A component with the right `role="combobox"` and the right
`aria-expanded="false"` can be totally broken: the RPC might not fire,
the listbox might never open, Enter might pick the wrong option. Every
component PR must include chromedp e2e tests that:

- **Click / type / press keys** through every primary user flow.
- **Assert DOM changes** that prove the runtime hook actually ran:
  - Combobox: type → option count goes from 0 to N, `aria-expanded` flips
    to `"true"`, Enter sets `input.value` to the picked option's
    `data-value`.
  - InfiniteScroll: scrolling the sentinel into view → item count
    increases; end-of-feed (empty cursor header) removes the sentinel.
  - Tree: clicking the toggle (or pressing ArrowRight) flips
    `aria-expanded="true"` AND populates `<ul role="group">` children.
  - CopyButton: click → `.fui-copied` applied, `data-fui-copy-status`
    sibling reads `"Copied"`; if `ToastOnCopy=true`, the toast stack
    receives the title.
  - ConfirmAction: trigger click → modal visible, Cancel autofocused;
    Esc → modal closed, modal stack empty.
  - FilterChipBar: click × → chip count decreases by 1 (server-driven
    re-render via signal swap).
  - SegmentedControl: click option N → `input:checked` value matches,
    `getBoundingClientRect()` of options are equal width.
- **Use real keys**: `chromedp/kb.ArrowDown`, `kb.Enter`, `kb.Escape` —
  not literal ANSI escape strings.
- **Avoid timing flakes**: use `chromedp.Sleep` with a window 2-3x the
  debounce-or-animation duration, not 100ms because "feels enough".

When the static-shape test passes but the interaction test fails, that's
the bug you would have shipped without it. Always add both.

## CSS contract: one registry, no manual wiring

**Every styled package — `framework/ui/*`, `core-ui/patterns/*`, and
widget chrome — registers its stylesheet via `registry.RegisterStyle`
and wraps its top-level rendered element in `Style.WrapHTML(...)`.**
The runtime emits a `data-fui-comp="<name>"` marker on the wrapper,
the SSR collector scans the rendered HTML, and CSS auto-loads — one
`<link>` per used component per page, dedup'd globally.

```go
// core-ui/patterns/foo/foo.go — canonical pattern shape
var Style = registry.RegisterStyle("foo", styleFn)

func styleFn(_ style.Theme) string { return baseCSS }

func Render(cfg Config) render.HTML {
    return Style.WrapHTML(render.Tag("div", attrs(cfg), ...))
}

const baseCSS = `.foo { ... }`
```

**Do NOT export `func BaseCSS() string`** from a `core-ui/patterns/*`
package. That was the legacy contract — host apps had to import the
package AND concatenate `BaseCSS()` into their custom CSS — and a
single missed concat shipped a component without any styling on the
live site (the 2026-05-19 nestedlist incident). The rule is enforced
by `core-ui/check.LintNoPatternBaseCSS`, a build-time test that
fails CI on the next regression.

Selectors stay class-based (`.foo`, `.nested-list`) — the marker
only signals to the auto-loader "fetch this stylesheet". Apps don't
need any setup; the CSS just appears on every page that renders the
component.

## Anti-patterns this skill exists to prevent

- ❌ Exporting `BaseCSS()` from a `core-ui/patterns/*` package — use
  `registry.RegisterStyle` + `Style.WrapHTML` instead. See the CSS
  contract section above.
- ❌ Embedding full chrome HTML in `/__gofastr/widgets` JSON catalog.
- ❌ Calling `/state` on mount when the widget has no signals.
- ❌ Constructing DOM in the runtime when the server already rendered it.
- ❌ Opening per-page SSE for surfaces that fire once per session.
- ❌ Inline `style="…"` attributes (strict CSP strips them).
- ❌ "Open the modal client-side then hope SSR caught up" — SSR-inline first.
- ❌ Hand-rolling `<button class="ui-btn …">` instead of `ui.Button(...)`.
  The framework class is `ui-button` (with the `data-fui-comp` marker so
  the CSS auto-loads). `ui-btn` IS NOT a thing — it renders unstyled
  native buttons. **Always compose with the typed framework components.**
- ❌ Reading form data with `req.FormValue()` when the runtime POSTs
  JSON. `dispatchRPC` serializes non-multipart forms as JSON
  (`Content-Type: application/json`); only manual `URLSearchParams`
  POSTs (InfiniteScroll) are form-encoded. Use a helper that handles
  both.
- ❌ Writing the response body **before** setting a custom header.
  Go's `net/http` sends headers automatically on the first `Write`,
  silently dropping any later `w.Header().Set(...)`. Set headers FIRST.
- ❌ Mounting Modal/Drawer/Popover presets without `.Pages("/route")` —
  every page on the site pays for the chrome registration even if it'll
  never open them. Page-scope demo widgets.

## Related skills

- `gofastr-ui` — the broader SSR-with-hydration architecture this builds on.
- `gofastr-docs` — docs ship with the same commit as a new component.
- `verify-before-claim` — `go test` ≠ "works in a browser".
