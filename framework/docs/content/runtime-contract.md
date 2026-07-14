# Runtime contract — SSR, hydration, islands, and the `data-fui-*` reference

<!--
  SYNC NOTE: this doc is an embedded extract of core-ui/ARCHITECTURE.md,
  which is the repo's source of truth for the UI/runtime contract. The
  two must be kept in sync: when the model or the attribute table
  changes there, update this file in the same commit.
  framework/docs/doc_sync_test.go fails when an attribute documented in
  core-ui/ARCHITECTURE.md is missing here.
-->

This page carries the runtime contract for readers of the embedded
docs (`gofastr docs`, the `framework_docs_*` MCP tools): the
SSR/hydration/island/SSE model and the full `data-fui-*` attribute
reference. If you are working inside the framework repo, read
`core-ui/ARCHITECTURE.md` instead — it is the authoritative version
and adds the recipes, the styling contract, and the component
cheat sheet.

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

Islands are built with the `core-ui/widget` builder (see
[widgets](widgets.md)) or wired by hand with the attributes below (see
[interactive-patterns](interactive-patterns.md) → "Writing a
hand-written island, end to end").

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
| `data-fui-rpc-open="<widget-name>"` | A registered widget opens on 2xx (e.g. "save in drawer → open results sheet") |
| `data-fui-rpc-navigate="<path>"` | Client-side SPA navigation to `<path>` on 2xx. Bypasses the screen cache and re-renders even when `<path>` is the current page — the RPC mutated server state, so the destination must be fetched fresh |
| `data-fui-signal="<name>"` | This node's content/attribute updates when the named signal changes |
| `data-fui-signal-mode="text\|html\|attr"` | How to apply the signal value (default `text`) |
| `data-fui-signal-attr="<attr>"` | Attribute name when mode is `attr` |
| `data-fui-signal-set="<name>[:<value>]"` | Click sets the named signal to `<value>` purely client-side (no RPC). Omit `:<value>` to set the empty string. Used by `framework/ui.Tabs` buttons (`<name>:<index>`). |
| `data-fui-signal-inc="<name>[:<delta>]"` | Click increments the named signal by `<delta>` (default `1`; negative decrements) client-side. Used by `framework/ui.Counter`. |
| `data-fui-signal-toggle="<name>"` | Click flips the named boolean signal client-side. Used by `framework/ui.SignalToggle` and `interactive.ToggleLocal`. |
| `data-fui-tab-index="<n>"` | Set on `framework/ui.Tabs` buttons and panels to associate each with its zero-based index. CSS keys the active-button highlight and visible panel off the wrapper's `data-active` matching this index. When the wrapper's `data-active` attribute is updated through a signal (`data-fui-signal-mode="attr"`), the core runtime also mirrors the new index into `aria-selected` on every `[role="tab"][data-fui-tab-index]` descendant so assistive tech tracks the selection, not just the CSS highlight. |
| `data-fui-computed="<reducer>"` | Marks a `core-ui/store` computed slice. The `computed` runtime module subscribes the node to its dependency signals and, on any change, runs the host-registered JS reducer `window.__gofastr._reducers[<reducer>]` over the current dep values and broadcasts the result to this node's `data-fui-signal`. CSP-safe — the reducer is a real function the host registers (no `eval`). |
| `data-fui-computed-deps="<a,b>"` | Comma-separated dependency signal names a `data-fui-computed` node recomputes from. |
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
| `data-fui-layout="<name>"` | Set by the outermost layout shell on its root `<div>` with the layout's name (e.g. `app`, `marketing`). On SPA navigation the runtime compares the destination route's layout (from the route manifest's `layout` field) with the current shell's marker; when they differ — cross-layout nav, where the chrome itself changes — it fetches the full page and swaps the whole shell instead of just `<main>`, so the new screen renders in the right chrome without a hard reload. |
| `data-fui-disclosure` | Marks a `<details>` element as a dismissible disclosure (mobile hamburger nav, popover, etc.). The runtime closes it automatically on SPA navigation and when Escape is pressed anywhere on the page (native `<details>` only handles Escape when the `<summary>` itself has focus). |
| `data-fui-disclosure-trap` | Opt-in modifier on a `data-fui-disclosure` `<details>` element: when open, the runtime sets `inert` on every sibling so focus is trapped inside the disclosure body. Use for mobile drawer / full-sheet popover patterns that need modal-style focus containment (vs. the default non-trapping inline disclosure). |
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
| `data-fui-copy-status` | Marks an element (typically a visually-hidden `role="status"` span) that receives a textual announcement when its sibling/ancestor `data-fui-copy-text-from` button succeeds. Pairs with `data-fui-copy-announce` (default "Copied"). |
| `data-fui-copy-announce="<msg>"` | Overrides the announcement text written into the matching `data-fui-copy-status` element on copy success. |
| `data-fui-copy-toast="<json>"` | When set on a `data-fui-copy-text-from` button, the runtime dispatches a toast via `window.__gofastr.toast(<json>)` on copy success. Use for "Copied to clipboard" notifications without per-button JS. |
| `data-fui-os` *(on `<html>`)* | Set by the runtime at boot to `"mac"` or `"other"` based on best-effort platform detection. Used by `framework/ui.ShortcutHint` to display platform-correct mod-key glyphs purely in CSS (no per-component JS). Functional shortcut matching does not depend on this attribute. |
| `data-fui-static` *(on `<html>`)* | Injected **only** by the static exporter (`framework/static.Builder`) onto `<html>`. When present, the runtime enters static mode: it fetches the dumped catalog file (`/__gofastr/widgets.json`) instead of the live session-gated endpoint, and a `data-fui-rpc` click/submit surfaces a "Needs the Go server" notice (via the CSP-clean `#fui-nav-toast` mini toast) instead of firing a dead request — so a visitor who tries a server-backed demo learns why it's inert and how to run it locally. `data-fui-open` is **not** gated — overlays resolve against the widget catalog + chrome HTML the exporter dumps as query-free files, so navigation surfaces (command palette, section-menu drawers) work. Client-only features (theme toggle, copy, signal mutations) are unaffected. Live pages never carry it, so every static-mode guard is a no-op in the normal server-backed app. |
| `data-fui-static-options` *(on a combobox listbox `<ul>`)* | Set by `core-ui/patterns/combobox` when the combobox carries a static `Options` list (no RPC). The combobox runtime module filters the inline `<li role="option">` rows client-side on input — hide non-matches, show all when the query clears — instead of firing a search endpoint. Use for small fixed command sets (docs/nav palette) so search works on a serverless export with no backend. Because the option rows render visible at SSR, the input ships `aria-expanded="true"` so the SSR state matches reality and the module's Escape / outside-click dismissal works before the first keystroke. An option's `Href` (written to `data-fui-push-state`) is scheme-filtered at render time — `javascript:` and friends are dropped. |
| `data-fui-infinite-scroll="<rpc-path>"` | Marks an infinite-scroll wrapper. The runtime POSTs to `<rpc-path>` (form-encoded body with `cursor=<token>`) when the contained `data-fui-infinite-sentinel` enters the viewport, then appends the HTML response into the items container. Pair with `data-fui-infinite-cursor`, `data-fui-infinite-items` (optional, CSS selector — default the wrapper itself), and `data-fui-infinite-root-margin` (default `200px`). Response carries `X-Gofastr-Infinite-Cursor: <next>` for the next call; empty/missing → end of feed, sentinel removed, observer disconnected. `aria-busy` toggles during fetch. |
| `data-fui-infinite-sentinel` | Marks the IntersectionObserver target inside an infinite-scroll wrapper. The sentinel is removed when end-of-feed is reached. |
| `data-fui-infinite-cursor="<token>"` | Initial cursor token on the infinite-scroll wrapper. Updated in-place after every fetch. |
| `data-fui-infinite-items="<selector>"` | Optional CSS selector identifying the child container into which new items are appended. Defaults to the wrapper itself. |
| `data-fui-infinite-root-margin="<px>"` | Optional IntersectionObserver `rootMargin` value. Default `200px`. |
| `data-fui-tree-toggle` | Marks the expand/collapse button inside a `core-ui/patterns/tree` treeitem row. The keyboard nav (ArrowRight to expand, ArrowLeft to collapse, Enter/Space to toggle) clicks this button so any `data-fui-rpc` on it fires lazy-load. |
| `data-fui-fill-input="<selector>"` / `data-fui-fill-text="<selector>"` | A button that fills the target input or text node with this element's `data-value` (or text content). |
| `data-fui-disable-when-invalid` | On a submit button: disabled while any field in the surrounding `<form>` reports `:invalid`. |
| `data-fui-persist-storage="<key>"` | The element's value persists across reloads in `localStorage` under the given key. |
| `data-fui-flash-on-update` / `data-fui-flash-duration-ms="<ms>"` | A signal-bound element flashes (CSS class `fui-flash`) for `<ms>` after each update. |
| `data-fui-scroll-bottom-on-update` | A signal-bound scroll container auto-scrolls to the bottom on each update (chat / log views). |
| `data-fui-tick-elapsed="<unix-ms>"` | Element's text updates once per second with the elapsed human-readable interval since the given epoch. |
| `data-fui-rpc-body="<json>"` | Static JSON body for `data-fui-rpc` requests that don't come from a `<form>`. |
| `data-fui-rpc-after-done` | Internal marker — set by the runtime after a one-shot `after-text` / `after-disable` fires so re-clicks are idempotent. |
| `data-fui-deeplink="<k1=v1&k2=v2>"` | On a `data-fui-open` button: per-click overrides for the opened widget's declared `DeepLinkParams`. The runtime mirrors the pairs into the widget's signals on open AND pushes them onto the URL (alongside the widget's `DeepLinkKey=DeepLinkValue`) so refresh / share / back-button preserve the open modal AND its data. Used for row-level "Edit user 42" flows. |
| `data-fui-toast="<json>"` | On a clickable element: clicking fires a toast with the given config (variant/title/body/ttl/stack). The runtime's global click delegator parses + dispatches via `__gofastr.toast()`. |
| `data-fui-toast-id="<id>"` | Marks one item inside a `preset.ToastStack` rendered list. The value is the toast id assigned by `__gofastr.toast()`; click-to-dismiss targets it. |
| `data-fui-toast-stack="<name>"` | Marks the container into which `__gofastr.toast()` appends items. The name matches the widget name passed to `preset.ToastStack`. |
| `data-fui-toast-ttl-ms="<n>"` | On a toast item: auto-dismiss after `n` milliseconds. Hovering or focusing the item pauses the timer; leaving resumes from where it stopped. Omit (or 0) for persistent toasts that require explicit dismissal. |
| `data-fui-toast-dismiss` | Click target inside a toast item that triggers dismiss. Pairs with the runtime's CSS-driven fade-out animation. |
| `data-fui-toast-fallback` | Marks the degraded inline container core injects when the `toasts` module fails to load (transient 5xx, network hiccup). Used by `__gofastr._fallbackToast(cfg)` so an X-Gofastr-Toast payload still reaches the user even when the full module is unavailable. Unstyled-but-visible; no TTL, no animation. |
| `data-fui-menu` | Marks a `<details data-fui-disclosure>` as a `framework/ui.Menu` dropdown. The runtime focuses the first `[role=menuitem]` when the disclosure opens; arrow keys / Home / End / type-ahead navigate within the panel; Tab closes the menu and lets focus escape naturally. |
| `data-fui-menu-panel` | Emitted by `framework/ui.Menu` on the `role="menu"` panel `<div>`. No runtime or CSS consumer today (the menu module scopes by `data-fui-menu` + `.ui-menu__panel`); emit-only structural marker. |
| `data-fui-match-prefix` | On a `<nav> <a>` link: opts the link into prefix-matching for active-route highlighting. The runtime tags it `aria-current="page"` + `.active` whenever the current URL starts with the link's href (only when href ends in `/` — `/components/` lights up on `/components/accordion`). Without this attribute the runtime does exact-href matching only, so breadcrumbs and sidebars (where multiple links share prefixes) keep the server-rendered single active item. Root `/` is never a prefix match. |
| `data-fui-fileupload` | Marks the drag-drop zone surrounding a `framework/ui.FileUpload` `<input type="file">`. The runtime wires dragover/dragleave/drop handlers that forward dropped File objects into the input's `files` property and dispatch a `change` event so form RPC pipelines fire uniformly whether the user clicked-to-pick or dragged-to-drop. |
| `data-fui-popover-anchor` | On a `data-fui-open` trigger button: opt the opened widget into trigger-anchored positioning. The value is the preferred side — `"top"`, `"bottom"`, `"left"`, `"right"`, or empty / `"auto"` (= bottom-first, then top, right, left). The runtime measures both rects after open and applies inline `position: fixed; top; left` so the popover sits next to the trigger; if the preferred side would overflow the viewport (8px margin), it auto-flips to the opposite. Re-runs on `window.resize` AND `window.scroll` (capture, rAF-throttled) so the popover tracks the trigger when the page scrolls. Distinct from `preset.Modal`'s deep-link affordances — popovers are click-driven and don't deep-link. |
| `data-fui-banner-dismiss` | On the X button inside a `framework/ui.Banner`: clicking sets `hidden` on the nearest `[data-fui-comp="ui-banner"]` ancestor. The runtime delegates the click globally so dismissal survives partial-island swaps. |
| `data-fui-scrollspy` | Marks a scrollspy container. The runtime observes section heading targets via IntersectionObserver and tags the matching `data-fui-scrollspy-target` link with `aria-current="true"` as the user scrolls. |
| `data-fui-scrollspy-target` | On a nav link inside a scrollspy: the value identifies the section heading the link tracks. Updated to `aria-current="true"` when its target enters the active band. |
| `data-fui-optimistic-idle` / `data-fui-optimistic-success` / `data-fui-optimistic-endpoint` / `data-fui-optimistic-method` | On an OptimisticAction button: the runtime flips the visible label between the idle and success copy as it dispatches a fetch to the endpoint+method, rolling back on error. Used by `framework/ui.OptimisticAction` for "Save / Saved!" patterns without per-button JS. |
| `data-fui-toggle-endpoint` / `data-fui-toggle-method` / `data-fui-toggle-allow-untoggle` / `data-fui-toggle-untoggle-endpoint` | On a three-state ToggleAction button (`framework/ui.ToggleAction`, `framework/ui/toggleaction.go`): `endpoint`+`method` (default POST) hit when toggling from idle → committed; `allow-untoggle="true"` lets a second click reverse the action, hitting `untoggle-endpoint` (same method) if set — with NO untoggle endpoint configured the button flips back to idle locally without issuing any request. Driven by `runtime/src/toggleaction.js` with a three-state mutex so rapid clicks can't race. |
| `data-fui-toggle-idle` / `data-fui-toggle-committed` | Markers on the two label spans inside a ToggleAction button. The runtime shows/hides them as the button transitions between idle and committed states. SSR ships the initial visible state. |
| `data-fui-toggle-group="<key>"` | Joins a ToggleAction button to a client-side mutex: committing any button with the same group key optimistically reverts the previously-committed sibling (no extra RPC — the server stays the source of truth and a later navigation refreshes from server state). Maps from `ToggleActionConfig.Group`. |
| `data-fui-network-retry-threshold` / `data-fui-network-retry-health` / `data-fui-network-retry-button` / `data-fui-network-retry-sse-silence` | On a NetworkRetryBanner element: threshold = number of consecutive fetch failures before the banner shows; health = the URL the runtime probes to detect recovery; button = the retry trigger; sse-silence = grace period (ms) after the last SSE frame before the banner considers the link unhealthy — the runtime polls `window.__gofastr.sseStatus.lastEventAt` (kept current by the SSE module on every frame) and, on a `gofastr:sse-status` reconnect, re-probes `health` so the banner can dismiss. |
| `data-fui-network-retry-demo-trigger` / `data-fui-network-retry-demo-recover` | Demo-only attributes (`examples/site` NetworkRetryBanner page): trigger forces the banner into the failed state for screenshot/dev purposes; recover restores it. Not used in production wiring. |
| `data-fui-banner-dismiss-id="<id>"` | Optional companion to `data-fui-banner-dismiss`. When set, the dismissal is recorded in `localStorage` under `gofastr.banner-dismiss.<id>` and the same banner is auto-hidden on every subsequent page load until the key is cleared. Use for "deprecation notice — got it" banners. |
| `data-fui-slider-mirror` | On an `<input type="range">` inside a `framework/ui.Slider`: opt the slider into runtime value-mirroring. The slider module listens for `input` events on these elements and writes the current value into the associated `<output for="<id>">` so the displayed number tracks the thumb as the user drags. Auto-emitted when `SliderConfig.ShowValue` is true. |
| `data-fui-number-step="<delta>"` | On a button inside a `framework/ui.NumberInput`: clicking the button increments the linked `<input type="number">` by `<delta>` (signed). Honors the input's `min` / `max` / `step` and dispatches an `input` + `change` event after writing the new value so form-RPC pipelines see the change. Pair with `data-fui-number-for`. |
| `data-fui-number-for="<input-id>"` | On a `data-fui-number-step` button: the id of the `<input type="number">` it controls. |
| `data-fui-multiselect` | Marks a `core-ui/patterns/multiselect` disclosure root. The `multiselect` runtime module scopes its chip rebuild + remove handling to descendants of this element. |
| `data-fui-multiselect-chips` | On the chips strip inside a `core-ui/patterns/multiselect`: the runtime rebuilds the chip list inside this element after every `change` event on a descendant `.ui-multiselect__check` checkbox. `aria-live="polite"` ships on the same element so SR users hear updates. |
| `data-fui-multiselect-placeholder="<text>"` | Empty-state placeholder shown via `::before` when no chips are rendered. |
| `data-fui-multiselect-remove="<input-id>"` | On a chip's × button: clicking unchecks the linked checkbox (which fires `change` and re-renders chips). |
| `data-fui-multiselect-name="<field>"` | Emitted by `core-ui/patterns/multiselect` on the disclosure root with the form-field name. No runtime or CSS consumer today; emit-only marker (the module scopes by `data-fui-multiselect`). |
| `data-fui-dropdown` | On a dropdown trigger button. The `dropdown` runtime module toggles `aria-expanded` and shows/hides the paired panel on click, closes on outside-click / Escape / SPA navigation, and is the singleton-by-default (opening one closes the others). |
| `data-fui-dropdown-wrap` | On the wrapper around a `data-fui-dropdown` trigger + `data-fui-dropdown-panel`. Scopes open/close to one dropdown instance; the runtime sets/clears `data-fui-dropdown-open` on it to track state. |
| `data-fui-dropdown-panel` | On the floating panel sibling of a `data-fui-dropdown` trigger. The runtime toggles its `hidden` attribute as the dropdown opens/closes. |
| `data-fui-dropdown-open` | Runtime-written marker on a `data-fui-dropdown-wrap` while its dropdown is open. CSS keys the open state off it; the runtime uses it to find and close open dropdowns. |
| `data-fui-animate-signal="<name>"` | On an element wired by the `animate` runtime module: names the signal to watch. When the signal becomes truthy the runtime adds `data-fui-animate-class`; falsy removes it. Initial state is applied on wire. |
| `data-fui-animate-class="<class>"` | The CSS class the `animate` module toggles on the element as its `data-fui-animate-signal` value flips between truthy and falsy. |
| `data-fui-reveal="<type>"` | Marks an element for the `reveal` runtime module's scroll-into-view animation. The element gets `fui-hidden` immediately; when it enters the viewport the runtime swaps in `fui-revealed` + `fui-reveal-<type>` (e.g. `data-fui-reveal="fade-up"` → `fui-reveal-fade-up`). One-shot. |
| `data-fui-dropzone-preview` | On a `<input type="file">` inside a `framework/ui.FileDropzone`: opt the input into image-preview rendering. After each `change`, the dropzone runtime FileReader-reads each selected image and renders `<img>` tags into the sibling `[data-fui-dropzone-preview-for="<input-id>"]` container. |
| `data-fui-dropzone-preview-for="<input-id>"` | On the previews strip element: links it to the input it should display previews for. |
| `data-fui-range-slider="<id>"` | On each `<input type="range">` of a `framework/ui.RangeSlider` pair: marks the pair so the runtime cross-clamps min ≤ max on every input event. Both inputs in the pair share the same id. |
| `data-fui-range-slider-value="<id>"` | On the `<output>` element of a RangeSlider with `ShowValue=true`: the runtime mirrors `min – max` into this element as the user drags. |
| `data-fui-tag-input="<form-field-name>"` | On the text `<input>` of a `framework/ui.TagInput`: the runtime listens for Enter / comma to commit a new chip (creates a sibling `<input type=hidden>` with this name + the typed value), and Backspace-on-empty to remove the last chip. |
| `data-fui-tag-input-zone` | Wrapper containing the chips + the text input. Used by the runtime to scope chip insertion / removal. |
| `data-fui-tag-input-id="<input-id>"` | Emitted by `framework/ui.TagInput` on the text input but currently consumed by NOTHING — `runtime/src/taginput.js` never reads it, no `<output>` is emitted, and no CSS selects on it. Emit-only; a candidate for removal on the Go side. |
| `data-fui-animated-counter="<target>"` | On a `framework/ui.AnimatedCounter`: the runtime ticks the inner `.ui-animated-counter__value` from `<from>` to `<target>` over `<ms>` once the element scrolls into view. Respects `prefers-reduced-motion` (no-op). Pair with `data-fui-animated-counter-from` and `data-fui-animated-counter-ms`. |
| `data-fui-animated-counter-from="<n>"` | AnimatedCounter starting value during the animation. |
| `data-fui-animated-counter-ms="<n>"` | AnimatedCounter animation duration in milliseconds. |
| `data-fui-theme-toggle` | On a `framework/ui.ThemeToggle`: marks the trigger. Empty/icon/label variants toggle through light/dark/auto on click; `data-fui-theme-toggle="pill"` scopes segmented option buttons. |
| `data-fui-theme-toggle-opt="<light\|auto\|dark>"` | On a ThemeToggle pill option: selects and persists the requested color-scheme mode. |
| `data-fui-back-to-top` | On a `framework/ui.BackToTop`: marks the fixed scroll button so the split runtime module can show/hide it and scroll to the configured target. |
| `data-fui-btt-threshold="<px>"` | BackToTop scroll threshold before the control becomes visible. |
| `data-fui-btt-target="<selector>"` | Optional BackToTop target selector. Empty means scroll the document root/window. |
| `data-fui-btt-scroll="smooth\|instant"` | BackToTop scroll behavior. Defaults to smooth. |
| `data-fui-btt-visible` | Runtime-written BackToTop visibility marker. CSS uses it to reveal the control once the threshold is crossed. |
| `data-fui-cond-disabled` | Runtime-written marker on controls disabled by `framework/ui.ConditionalField`; it lets the runtime distinguish fields it disabled from fields that were already disabled by the app. |
| `data-fui-toc="<selector>"` | On a `framework/ui.TableOfContents` nav: the runtime walks the matching content region after first paint and emits an `<li><a>` per `<h2>` / `<h3>` with an `id`. Active-section tracking via IntersectionObserver. Pair with `data-fui-toc-levels`. |
| `data-fui-toc-levels="<csv>"` | Optional comma-separated list of heading levels to harvest (default `"2,3"`). |
| `data-fui-toc-for="<heading-id>"` | Internal — set by the TOC runtime on each emitted `<a>` linking it back to its source heading. Used by the active-section tracker. |
| `data-fui-sortable` | Marks the `<ol>` of a `core-ui/patterns/sortablelist` as reorderable. Pair with `data-fui-sortable-rpc`. |
| `data-fui-sortable-rpc="<path>"` | POST endpoint that receives `order=<comma-separated-keys>` after every successful reorder; non-2xx response reverts the DOM. |
| `data-fui-sortable-item` | Marks an `<li>` as a drag-and-drop item inside a `data-fui-sortable` list. Pair with `data-fui-sort-key` and (typically) `draggable="true"` + `tabindex="0"`. Keyboard: Space grabs / drops; Arrow Up/Down moves while grabbed; Esc cancels. |
| `data-fui-sort-key="<key>"` | Stable identifier the server uses to apply the new order. |
| `data-fui-sortable-group="<id>"` | Board id shared by linked `data-fui-sortable` columns (kanban). Lists with the same non-empty group id allow cross-container drag and keyboard moves between them; lists with no group (or different groups) stay isolated. Back-compat: existing single lists have no group → unchanged behavior. |
| `data-fui-sortable-container="<id>"` | Per-column id emitted on each `data-fui-sortable` `<ol>` of a linked board. Sent as the `container` body field in a cross-container move payload so the server knows which column the item landed in. Distinct from `data-fui-sortable-group` (the board id) because a board has one group but N containers — the server needs both to route the write. |
| `data-fui-sortable-version="<token>"` | Optional optimistic-concurrency token. When set, appended as a `version` body field to every commit POST. A 409 response then fires the conflict path (refetch `data-fui-sortable-conflict` HTML) instead of a blanket rollback. Without this attr, 409 is treated like any other non-2xx (rollback) — back-compat. |
| `data-fui-sortable-conflict="<rpc>"` | GET endpoint refetched on a 409 response (only when `data-fui-sortable-version` is set). The response body replaces the destination list's `innerHTML` — server-rendered reconciliation. Without this attr, a 409 falls back to rollback + a `console.warn`. |
| `data-fui-shortcut-target="<selector>"` | Optional companion to a page-level `data-fui-shortcut-focus` on a non-focusable wrapper: when the chord fires, the runtime focuses the element matched by this selector instead of the wrapper itself. Used by `framework/ui.GlobalSearch` where the chord lives on the wrapper but the focus target is the inner `<input>`. |
| `data-fui-lightbox="<name>"` | On the slot wrapper of a `framework/ui.Lightbox`: identifies the open viewer for the runtime. Pair with optional `data-fui-lightbox-nav="true"` to enable Prev/Next + ArrowLeft/Right keyboard nav across siblings sharing `data-fui-lightbox-group`. |
| `data-fui-lightbox-nav="true"` | On the lightbox slot wrapper: opts into the runtime's arrow-key + Prev/Next button navigation. |
| `data-fui-lightbox-group="<id>"` | On a trigger anchor that opens a Lightbox: identifies the gallery group whose siblings the runtime walks during Prev/Next nav. |
| `data-fui-lightbox-prev` / `data-fui-lightbox-next` | On Prev/Next buttons inside the open Lightbox: clicking steps to the previous/next image in the gallery group. |
| `data-fui-carousel` | Marks a `framework/ui.Carousel` root. The runtime wires Prev/Next clicks, pagination dot clicks, ArrowLeft/Right keyboard nav, and optional AutoRotate. |
| `data-fui-carousel-track` | The inner scrolling `<ul>` of a carousel. The runtime reads its `scrollLeft` + slide offsets to compute the current index. |
| `data-fui-carousel-slide="<i>"` | Marks a slide `<li>` inside the carousel track with its index. |
| `data-fui-carousel-prev` / `data-fui-carousel-next` | On Prev/Next buttons: stepping by one slide. Auto-disabled at the ends when Loop is off. |
| `data-fui-carousel-dot="<i>"` | On a pagination dot button: clicking scrolls to slide `<i>`. The active dot carries `aria-current="true"`. |
| `data-fui-carousel-autorotate="<ms>"` | Opt-in auto-advance interval. The runtime pauses on hover, focus-within, prefers-reduced-motion, and Page Visibility hidden. |
| `data-fui-carousel-loop="true"` | Wrap-around: Next on the last slide goes to first, Prev on the first goes to last. |
| `data-fui-carousel-defer="<idx>"` | On a virtual-scroll carousel slide that hasn't been hydrated yet. The runtime IntersectionObserves the slide and swaps in the real HTML from the matching `<script data-fui-carousel-deferred-for>` entry the first time it enters the viewport (plus one read-ahead window). The attribute is removed on hydration. |
| `data-fui-carousel-deferred-for="<carousel-id>"` | On the `<script type="application/json">` element that ships alongside a virtual-scroll carousel. The script body is a JSON map of slide index → HTML; the runtime parses it to hydrate placeholder slides on demand. |
| `data-fui-popover-side` | Written by the runtime onto the anchored popover's widget root after placement — value is the final chosen side (`"top"`, `"bottom"`, `"left"`, `"right"`, post auto-flip). CSS uses it to position the directional arrow (`::before`) and to apply the anchored chrome (border, shadow, max-inline/block-size). Cleared on dismiss. |
| `data-fui-popover-trigger` | Written by the runtime onto the originating trigger button while its anchored popover is open. The runtime also adds the `.is-popover-trigger-active` class so the trigger can be highlighted while its popover is the currently-active surface. Both are stripped on dismiss or when the popover re-anchors to a different trigger. |
| `data-fui-prefetch="<module>"` | On any element: opt the page into hover/focus-prefetch of a split runtime module (e.g. `data-fui-prefetch="fileupload"`). On the first `pointerover` or `focusin` (capture phase, once per element) the runtime fires `__gofastr.loadModule(<module>)` so the module is ready by the time the user clicks. Multiple modules can be listed space-separated. Used to keep typical pages on `core.js` only while still feeling instant on interaction. See `ROADMAP.md` §8. |
| `data-fui-screen-group="<prefix>"` | On the layout wrapper div emitted by `ScreenGroup.RenderLayout`. Identifies the screen group boundary so the runtime can preserve the layout shell during sibling-screen navigation (swap only inner content, not the layout). The prefix matches the group's URL prefix. |
| `data-fui-inline-edit="<id>"` | On the display span of an `InlineEdit` component. Clicking it triggers edit mode (span hides, input shows). Enter saves, Escape reverts. |
| `data-fui-password-toggle="<input-id>"` | On the toggle button inside a `PasswordInput`. The runtime toggles the linked input's type between `password` and `text`. |
| `data-fui-pane-host` | Emitted by `framework/ui.PaneHost` on its root `<div>` (alongside `data-fui-comp="ui-pane-host"`). Marker the demand-loaded `panehost` runtime module scans for to wire open/close/swap triggers and the responsive overlay-drawer collapse. |
| `data-fui-pane="primary\|secondary\|tertiary"` | Emitted by `PaneHost` on each of its three slot children. The runtime addresses a pane by this value when opening/closing it; CSS keys the open-state grid columns and the overlay-drawer chrome off the combination of the root's open modifier classes and this slot marker. |
| `data-fui-pane-open="secondary\|tertiary"` | On a button: click opens the named side pane (reveals the column, hands focus to the pane's first focusable, remembers the trigger for restore). The trigger resolves its host via the nearest `[data-fui-pane-host]` ancestor, or via `data-fui-pane-host-target` for triggers outside the host. |
| `data-fui-pane-close="secondary\|tertiary"` | On a button: click hides the named pane and restores focus to the element that opened it. Value may be omitted (bare attribute) to close the topmost open pane. |
| `data-fui-pane-swap="secondary\|tertiary"` | On a button: click opens the named pane AND closes the other secondary/tertiary sibling — the "opening a link fills the third pane instead of navigating" flow that swaps which side pane is shown. |
| `data-fui-pane-host-target="<id>"` | On an open/close/swap trigger that lives OUTSIDE its `[data-fui-pane-host]` ancestor: the `id` of the host the trigger drives. Without it the runtime resolves the host by `closest('[data-fui-pane-host]')`. |
| `data-fui-pane-mode="overlay"` | Written by the `panehost` runtime module onto the host when `matchMedia('(max-width: 768px)')` matches AND a pane is open. CSS flips the open pane to a fixed overlay drawer (backdrop scrim via `::before`, right edge, full height) and the module applies a focus trap + scroll lock + ESC/backdrop-to-close. Cleared when the viewport widens or the pane closes. |
| `data-fui-repeater="<name>"` | On the items container of a `Repeater` component. The runtime uses `data-min-items` and `data-max-items` attributes to enforce item count limits during dynamic add/remove. |
| `data-wizard-steps="<n>"` | On the `<form>` wrapper of a `Wizard` component. The runtime uses this to know the total number of steps for navigation. |
| `data-fui-drag-dismiss="true"` | On a widget root whose Definition has `DragDismiss=true` (e.g. `preset.BottomSheet`). Driven by the demand-loaded `runtime/src/dragdismiss.js` module (the marker itself is the load trigger — present at boot for SSR-inlined sheets; dynamically-opened chrome is caught by the MutationObserver scan). Drag starts only from the `data-fui-drag-handle` bar; the module follows pointer Y movement with `transform: translateY` and closes the widget on `pointerup` when distance > 80px or downward velocity > 0.5 px/ms. Snaps back otherwise. While dragging, `data-fui-dragging` is set on the root (used by CSS to suppress conflicting animations). |
| `data-fui-drag-handle="true"` | On the visible drag-handle bar rendered at the top of a drag-dismiss-enabled widget. Marks the affordance for cursor styling; the actual pointer logic is delegated from the widget root. |
| `data-fui-zoomed` | Written by the runtime onto a `.ui-lightbox__full` image when the user has pinch-zoomed past 1×. CSS uses it to flip the cursor from `zoom-in` to `grab` and to enable single-pointer panning. Cleared on snap-back and on lightbox close. |
| `data-fui-trusted` | Marks a server-emitted region as trusted to host the legacy `data-kiln-tool` click/submit delegators. Without this ancestor (or `<body class="kiln-app">`), the legacy delegator refuses to dispatch — preventing stored-XSS content from forging authenticated kiln-tool POSTs. Apply only to chrome you fully control. |
| `data-fui-sidebar` | Emitted by `framework/ui.Sidebar` on its `<div>` root. No runtime or CSS consumer today (styling keys off `.ui-sidebar` classes); emit-only structural marker. |
| `data-fui-sticky="<edge>"` | Emitted by `framework/ui.Sticky` (`layout.go`) with the pinned edge (`top`/`bottom`). No runtime or CSS consumer today (styling keys off `.ui-sticky--*` classes); emit-only structural marker. |
| `data-fui-z-tier="<tier>"` | Emitted by `framework/ui.Sticky` with the layering tier from `StickyConfig.ZIndexTier` (`sticky` default, or `dropdown`/`modal`/`popover`/`toast` matching the theme's `ZIndexSet` tokens). CSS-only consumer: the `ui-sticky` stylesheet keys `z-index: var(--z-<tier>)` off this attribute so a sticky toolbar can layer above/below other surfaces without bespoke CSS. |
| `data-fui-viewport="desktop\|mobile"` | Emitted by `framework/ui.Responsive` on each variant wrapper. No runtime or CSS consumer today — the per-breakpoint stylesheet toggles `display` via the `.ui-responsive__desktop` / `.ui-responsive__mobile` classes; emit-only structural marker. |


For the authoritative list, grep `data-fui-` in
`core-ui/runtime/runtime.js`. Adding a new attribute requires updating
`core-ui/ARCHITECTURE.md`, this extract, and a runtime test.

**Component-action attributes** (the compiled `data-action` family, distinct
from the `data-fui-*` runtime primitives above): `data-action="<name>"` on an
element inside a `[data-component]` binds the named compiled action to that
element's click (and `data-action-<event>` / `data-action-type` to
input/change/submit). `data-action-mount="<name>"` fires the named action
*once on hydration* (and again after each SPA nav) — the hook a server-rendered
island uses to populate itself on load, since the other triggers are
user-event-driven. Any `data-param-*` on the element flows into the handler's
`params`. Covered by `core-ui/runtime/action_mount_e2e_test.go`.

**Response headers the runtime understands:**

| Header | Effect |
|---|---|
| `X-Gofastr-Push-State: <path>` | Apply via `history.pushState` after the RPC succeeds (URL update without re-fetch) |
| `X-Gofastr-Partial: true` | Body is a screen-partial (used by the cross-page nav path) |
| `X-Gofastr-Title: <text>` | Percent-encoded title — `decodeURIComponent` it, then set `document.title` after the partial swap. (It's encoded because HTTP header values are Latin-1; a raw UTF-8 title like `Docs — GoFastr` would otherwise arrive mojibaked as `Docs â GoFastr`.) |

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

## Forms

**Default: forms behave like standard HTML forms.** The runtime only
intercepts a form when you explicitly ask it to, so auth flows,
file uploads, password-manager UX, and Location-follow redirects
work the moment you drop a `<form>` into the page.

### When the runtime intercepts

| Trigger                                | Body sent by runtime                              | Server reads via                    |
|----------------------------------------|---------------------------------------------------|--------------------------------------|
| `enctype="application/json"`           | `application/json` of every form input            | `json.NewDecoder(r.Body)`            |
| `data-fui-spa` (no/urlencoded enctype) | `application/x-www-form-urlencoded`               | `r.ParseForm()` + `r.PostFormValue`  |
| `data-fui-rpc="/some/endpoint"`        | Per the RPC contract (see Widgets section)        | RPC handler                          |

Every other form — no special attribute, default or `urlencoded` or
`multipart/form-data` enctype — is **NOT intercepted**. The browser
submits it the standard way: POST body matches enctype, Set-Cookie
headers apply, 303→Location is followed, file inputs upload natively.

### Server redirects (after intercept)

When the interceptor IS active and the handler responds with a `30x`
+ `Location` header, the runtime navigates to the location via the
SPA navigator (no hard refresh between same-app pages), falling back
to `window.location.assign`. For the non-intercepted default path,
redirect-following is the browser's own job — same result.

```go
http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
```

### Cookie-set + redirect: the canonical auth flow

```go
// /auth/login form handler — battery/auth ships this out of the box.
http.SetCookie(w, sessionCookie)
http.Redirect(w, r, successRedirect(r, "/"), http.StatusSeeOther)
```

The runtime preserves the cookie and follows the redirect, so a form
POST to `/auth/login` from an SSR login page lands the user on the
next screen without a hard refresh — and without the host needing
client-side glue.

## Heavy-JS plugin markers

Pages that mount a sandboxed heavy-JS plugin (see
[plugin-platform](plugin-platform.md)) carry a mount marker emitted by
`framework/pluginhost.MountMarker`: `data-fui-plugin="<name>"` with
`data-fui-plugin-docid`, `data-fui-plugin-doc` (server-rendered initial
JSON), `data-fui-plugin-minheight`, and `data-fui-plugin-capabilities`
(the grant set, `resource:verb` grammar). These are scanned by the plugin
host broker — a separate script served at its own route, NOT part of
`runtime.js` or its budgets. Plugins may add namespaced extras (the
wysiwyg editor adds `data-fui-plugin-for` naming its hidden form fields);
those are documented by the owning plugin.

---

## Common mistakes

These are the misreadings of the model that have already cost rework
(they are cataloged as "failure modes" in `core-ui/ARCHITECTURE.md`):

- **Intercepting all link clicks for SPA.** State-change interactions
  like pagination should not be `<a href="?p=2">` links at all — they
  are island RPCs. Cross-page navigation (`/a` → `/b`) IS intercepted
  by design; don't disable that to "fix" an in-page interaction that
  should never have been a route.
- **Making every interaction a full navigation.** Clicking a sort
  header must not reload the page — that's a hard refresh, which the
  model rules out for in-page state. Use an island RPC.
- **Making every interaction client-only after hydration.** Client-
  managed pagination state only works for datasets that fit in one
  render, and it duplicates server logic. Islands are server-driven;
  let the server do the math and return HTML.
- **Shipping CSS from the app or a generator to make a surface look
  right.** All styling lives in the design system (component CSS via
  `registry.RegisterStyle`, layouts via `core-ui/app`, tokens via
  `core-ui/style`). A surface that needs CSS the components don't
  provide is a design-system gap to fix upstream — see
  [theming](theming.md).
- **Delivering a user action's response over SSE.** SSE is push-only
  (background events). The result of a user action arrives in the RPC
  response itself.
