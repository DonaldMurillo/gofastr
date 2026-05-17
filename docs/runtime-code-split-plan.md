# Runtime code-split plan

> Branch: `runtime-code-split`.
>
> Goal: shrink the parser-blocking JS payload on a typical page from
> ~31 KB gz (one bundle, everything) to ~10 KB gz (core only) +
> lazy modules loaded on hover / idle / first-use.

## The status quo

The entire client runtime is a single IIFE in `core-ui/runtime/runtime.js`:

- 2,545 lines of source.
- 108 KB raw / 31 KB gzip / 26 KB brotli.
- Served as `/__gofastr/runtime.js`, parser-blocking, in every page.

Lighthouse-equivalent projection at slow 3G: full critical path
(HTML + CSS + runtime + widget chrome) takes ~4 s end-to-end on
the wire as-shipped today (no compression). With gzip enabled
that drops to ~1.7 s. With code-split + gzip it could drop further
to ~1 s.

The wire-compression problem is orthogonal and a separate phase
(see "Phase 0" below). The runtime-architecture work in this
branch focuses on what code we ship vs defer.

## Three load classes

| Class | Examples | Wire timing |
|---|---|---|
| **Core** — needed before first interaction | namespace, signals, dispatchRPC, SPA router, screen cache, component CSS catalog, disclosure Esc, `data-fui-confirm`, MutationObserver, module loader | parser-blocking, in `<head>` |
| **Hover-prefetch** — triggered by a known marker | popover, menu, fileupload, toast push, modal open | `mouseover`/`focusin` on the trigger fires `loadModule()`; `click` awaits the promise |
| **Idle-load** — needed soon but with no clear trigger | widgets chrome (for SSR-inlined widgets), SSE consumer, form primitives (autogrow / charcount / persist / etc.) | `requestIdleCallback` after FCP |

## Proposed module split

| Module | Contents | Est. gzip | Load |
|---|---|---:|---|
| `core.js` | namespace, signals, dispatchRPC, SPA router (cache + popstate), CSS catalog scan + idle prefetch, disclosure (Esc + SPA-nav close), `data-fui-confirm`, `data-fui-open` delegator (queues), `data-fui-prefetch` delegator, **module loader**, MutationObserver, active-link aria-current | **~10 KB** | parser-blocking |
| `widgets.js` | `mountWidget` chrome + dismiss, modal stack, backdrop, focus trap, deep-link push/strip, Esc handler (modals + popovers), `_popoverStack` non-modal dismiss | ~8 KB | idle (always) + hover (`data-fui-open`) |
| `sse.js` | `connectSSE` + island stream consumer | ~1 KB | idle (only if `<meta name="gofastr-sse">` present) |
| `popover.js` | `_anchorPopover` + arrow + scroll/resize tracking + trigger-active class | ~2 KB | hover (`data-fui-popover-anchor`) |
| `toasts.js` | toast stack, `__gofastr.toast()`, `X-Gofastr-Toast` header parser, TTL + hover-pause + click-dismiss, `data-fui-toast` delegator | ~2.5 KB | idle (when a toast-stack widget mounts) + eager (first toast) |
| `menu.js` | roving focus, Home/End, type-ahead, Tab-to-close | ~2 KB | hover (`data-fui-menu`) |
| `fileupload.js` | drag/drop wiring, filename render, image thumbnail | ~1 KB | hover (`data-fui-fileupload`) |
| `forms.js` | autogrow, charcount, persist-storage, fill-input, clear-on-esc, submit-on-enter, disable-when-invalid, copy-text-from, tick-elapsed, flash-on-update, scroll-bottom-on-update, rpc-after-text/disable/scroll-to, shortcuts | ~5 KB | idle (many small features; not worth per-marker hover) |

**Total when everything is needed:** ~31.5 KB gz (matches today's size).

**First-load JS on a typical page (no popover/menu/toast/fileupload):**
core only = **~10 KB gz**, then widgets + forms + sse arrive on idle ≈ 14 KB more,
**none of which is parser-blocking**.

## Component-driven dependency tracking

Components know what runtime modules they need at render time.
The renderer collects a per-request set; the host emits one
`<link rel="modulepreload">` per needed module so the browser
fetches them in parallel with the HTML parse.

```go
// framework/ui/popover.go (illustrative)
func Popover(name string) *widget.Builder {
    runtime.Need("widgets", "popover")   // server-side marker
    return widget.New(name).Mount(widget.TopRight).Hidden()
}
```

```html
<!-- emitted in <head> for this page -->
<link rel="modulepreload" href="/__gofastr/runtime/core.js?v=abc">
<link rel="modulepreload" href="/__gofastr/runtime/widgets.js?v=abc">
<link rel="modulepreload" href="/__gofastr/runtime/popover.js?v=abc">
<script src="/__gofastr/runtime/core.js?v=abc"></script>
<!-- core.js fetches widgets.js + popover.js on idle / hover -->
```

The trigger element gets `data-fui-prefetch="popover"`. Core's
single `pointerover` + `focusin` capture-phase delegator calls
`loadModule("popover")` on first hover. By the time the user
clicks, the module is loaded.

## Phase ladder (each phase ships independently)

### Phase 0 (optional, separate branch): HTTP compression
A 30-line gzip middleware on `/__gofastr/*`, `text/html`,
`application/json`. Independent 3.5–4× wire-size win. Not in this
branch.

### Phase 1 — internal carve (no wire change)
Reorganize `runtime.js` in place:
- One block per future-module, with a labelled header.
- Shared state hoisted into a "core state" block at the top.
- Add `__gofastr.loadModule(name)` API that returns an
  immediately-resolved Promise (the modules all live in one file
  still, so the call is a no-op behavior-wise).
- Tests + behavior unchanged.

**Validates that the boundary holds before we add HTTP weight.**

### Phase 2 — file split
- Move sections to `core-ui/runtime/src/core.js`, `widgets.js`,
  `popover.js`, `toasts.js`, `menu.js`, `fileupload.js`,
  `forms.js`, `sse.js`.
- Go-side server emits `/__gofastr/runtime/<module>.js?v=<hash>`,
  content-addressed, `Cache-Control: public, max-age=31536000, immutable`.
- `loadModule(name)` becomes real dynamic `<script>` injection with
  cached promises.
- Host emits only `<script src=".../core.js?v=…">` in `<head>` — no
  modulepreload yet.
- `_pendingFor` queue in core: a `data-fui-open` click before
  `widgets.js` has loaded queues; replays after load.

### Phase 3 — server-side dep registration + preload tags
- `framework/ui` components and `core-ui/widget/preset` builders
  call `runtime.Need(modules...)`.
- `uihost` reads the per-request set and emits `<link
  rel="modulepreload">` tags.
- Trigger elements get `data-fui-prefetch="<module>"`.

### Phase 4 — hover prefetch
- Core attaches one `pointerover` + `focusin` capture-phase
  delegator.
- On first match, calls `loadModule(name)`.
- Replaces the old "parse the marker on click" path; click handlers
  now `await loadModule()` before executing.

### Phase 5 — idle fallback
- Modules without a hover-trigger marker (`widgets.js` for
  SSR-inlined widgets, `sse.js`, `forms.js`) scheduled via
  `requestIdleCallback` after FCP. Falls back to `setTimeout(0)`
  on browsers without rIC.

### Phase 6 — tests
- Drift test extended: every `data-fui-*` attribute the runtime
  reads belongs to exactly one module.
- E2E asserts a page with no `data-fui-popover-anchor` NEVER
  fetches `popover.js` (`browser_network_requests` filter).
- E2E asserts hovering a `data-fui-prefetch` trigger fetches its
  module within one frame.
- Runtime-size budget split: core ≤ 12 KB gz, each demand module
  ≤ 3 KB gz.

## Risks I want to call out

- **Click-before-module-load on first hover** — a user who lands on
  a page and immediately clicks a popover trigger (no hover) waits
  one round trip. Acceptable: under 100ms on 4G, ~300ms on slow 3G.
  Touch devices (no hover) always hit this path. Mitigation: keep
  modules small (≤3 KB gz) and serve with `immutable` cache so
  repeat visits are free.
- **Boundary leakage** — a feature added later forgets to declare
  its module. The drift test (Phase 6) catches this.
- **Idempotency under SPA nav** — every module must register a
  `(root) => void` scanner. Core's MutationObserver invokes
  scanners on inserted DOM; modules deduplicate via per-element
  `__fuiWired` flags.
- **Server-side dep tracking adds touch points** — every renderer
  that emits a marker needs to call `runtime.Need()`. Risk of
  miss → preload tag missing → first interaction stalls one RTT.
  Drift test (Phase 6) catches missing declarations.
- **Old browsers without `requestIdleCallback`** — Safari < 16.2,
  Firefox < 55. Fallback to `setTimeout(0, …)` is fine; idle
  becomes "next tick" which is still after FCP.

## Status

| Phase | Status |
|---|---|
| 0 — HTTP compression | not in scope of this branch |
| 1 — internal carve | **in progress** |
| 2 — file split | pending |
| 3 — server-side dep registration | pending |
| 4 — hover prefetch | pending |
| 5 — idle fallback | pending |
| 6 — tests | pending |
