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
| `data-fui-copy-status` | Marks an element (typically a visually-hidden `role="status"` span) that receives a textual announcement when its sibling/ancestor `data-fui-copy-text-from` button succeeds. Pairs with `data-fui-copy-announce` (default "Copied"). |
| `data-fui-copy-announce="<msg>"` | Overrides the announcement text written into the matching `data-fui-copy-status` element on copy success. |
| `data-fui-copy-toast="<json>"` | When set on a `data-fui-copy-text-from` button, the runtime dispatches a toast via `window.__gofastr.toast(<json>)` on copy success. Use for "Copied to clipboard" notifications without per-button JS. |
| `data-fui-os` *(on `<html>`)* | Set by the runtime at boot to `"mac"` or `"other"` based on best-effort platform detection. Used by `framework/ui.ShortcutHint` to display platform-correct mod-key glyphs purely in CSS (no per-component JS). Functional shortcut matching does not depend on this attribute. |
| `data-fui-infinite-scroll="<rpc-path>"` | Marks an infinite-scroll wrapper. The runtime POSTs to `<rpc-path>` (form-encoded body with `cursor=<token>`) when the contained `data-fui-infinite-sentinel` enters the viewport, then appends the HTML response into the items container. Pair with `data-fui-infinite-cursor`, `data-fui-infinite-items` (optional, CSS selector — default the wrapper itself), and `data-fui-infinite-root-margin` (default `200px`). Response carries `X-Gofastr-Infinite-Cursor: <next>` for the next call; empty/missing → end of feed, sentinel removed, observer disconnected. `aria-busy` toggles during fetch. |
| `data-fui-infinite-sentinel` | Marks the IntersectionObserver target inside an infinite-scroll wrapper. The sentinel is removed when end-of-feed is reached. |
| `data-fui-infinite-cursor="<token>"` | Initial cursor token on the infinite-scroll wrapper. Updated in-place after every fetch. |
| `data-fui-infinite-items="<selector>"` | Optional CSS selector identifying the child container into which new items are appended. Defaults to the wrapper itself. |
| `data-fui-infinite-root-margin="<px>"` | Optional IntersectionObserver `rootMargin` value. Default `200px`. |
| `data-fui-tree-toggle` | Marks the expand/collapse button inside a `core-ui/patterns/tree` treeitem row. The keyboard nav (ArrowRight to expand, ArrowLeft to collapse, Enter/Space to toggle) clicks this button so any `data-fui-rpc` on it fires lazy-load. |
| `data-fui-fill-input="<selector>"` / `data-fui-fill-text="<selector>"` | A button that fills the target input or text node with this element's `data-value` (or text content). |
| `data-fui-disable-when-invalid` | On a submit button: disabled while any field in the surrounding `<form>` reports `:invalid`. |
| `data-fui-persist-storage="<key>"` | The element's value persists across reloads in `localStorage` under the given key. |
| `data-fui-flash-on-update` / `data-fui-flash-duration-ms="<ms>"` | A signal-bound element flashes (CSS class `is-fui-flash`) for `<ms>` after each update. |
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
| `data-fui-match-prefix` | On a `<nav> <a>` link: opts the link into prefix-matching for active-route highlighting. The runtime tags it `aria-current="page"` + `.active` whenever the current URL starts with the link's href (only when href ends in `/` — `/components/` lights up on `/components/accordion`). Without this attribute the runtime does exact-href matching only, so breadcrumbs and sidebars (where multiple links share prefixes) keep the server-rendered single active item. Root `/` is never a prefix match. |
| `data-fui-fileupload` | Marks the drag-drop zone surrounding a `framework/ui.FileUpload` `<input type="file">`. The runtime wires dragover/dragleave/drop handlers that forward dropped File objects into the input's `files` property and dispatch a `change` event so form RPC pipelines fire uniformly whether the user clicked-to-pick or dragged-to-drop. |
| `data-fui-popover-anchor` | On a `data-fui-open` trigger button: opt the opened widget into trigger-anchored positioning. The value is the preferred side — `"top"`, `"bottom"`, `"left"`, `"right"`, or empty / `"auto"` (= bottom-first, then top, right, left). The runtime measures both rects after open and applies inline `position: fixed; top; left` so the popover sits next to the trigger; if the preferred side would overflow the viewport (8px margin), it auto-flips to the opposite. Re-runs on `window.resize` AND `window.scroll` (capture, rAF-throttled) so the popover tracks the trigger when the page scrolls. Distinct from `preset.Modal`'s deep-link affordances — popovers are click-driven and don't deep-link. |
| `data-fui-banner-dismiss` | On the X button inside a `framework/ui.Banner`: clicking sets `hidden` on the nearest `[data-fui-comp="ui-banner"]` ancestor. The runtime delegates the click globally so dismissal survives partial-island swaps. |
| `data-fui-banner-dismiss-id="<id>"` | Optional companion to `data-fui-banner-dismiss`. When set, the dismissal is recorded in `localStorage` under `gofastr.banner-dismiss.<id>` and the same banner is auto-hidden on every subsequent page load until the key is cleared. Use for "deprecation notice — got it" banners. |
| `data-fui-slider-mirror` | On an `<input type="range">` inside a `framework/ui.Slider`: opt the slider into runtime value-mirroring. The slider module listens for `input` events on these elements and writes the current value into the associated `<output for="<id>">` so the displayed number tracks the thumb as the user drags. Auto-emitted when `SliderConfig.ShowValue` is true. |
| `data-fui-number-step="<delta>"` | On a button inside a `framework/ui.NumberInput`: clicking the button increments the linked `<input type="number">` by `<delta>` (signed). Honors the input's `min` / `max` / `step` and dispatches an `input` + `change` event after writing the new value so form-RPC pipelines see the change. Pair with `data-fui-number-for`. |
| `data-fui-number-for="<input-id>"` | On a `data-fui-number-step` button: the id of the `<input type="number">` it controls. |
| `data-fui-multiselect-chips` | On the chips strip inside a `core-ui/patterns/multiselect`: the runtime rebuilds the chip list inside this element after every `change` event on a descendant `.ui-multiselect__check` checkbox. `aria-live="polite"` ships on the same element so SR users hear updates. |
| `data-fui-multiselect-placeholder="<text>"` | Empty-state placeholder shown via `::before` when no chips are rendered. |
| `data-fui-multiselect-remove="<input-id>"` | On a chip's × button: clicking unchecks the linked checkbox (which fires `change` and re-renders chips). |
| `data-fui-dropzone-preview` | On a `<input type="file">` inside a `framework/ui.FileDropzone`: opt the input into image-preview rendering. After each `change`, the dropzone runtime FileReader-reads each selected image and renders `<img>` tags into the sibling `[data-fui-dropzone-preview-for="<input-id>"]` container. |
| `data-fui-dropzone-preview-for="<input-id>"` | On the previews strip element: links it to the input it should display previews for. |
| `data-fui-range-slider="<id>"` | On each `<input type="range">` of a `framework/ui.RangeSlider` pair: marks the pair so the runtime cross-clamps min ≤ max on every input event. Both inputs in the pair share the same id. |
| `data-fui-range-slider-value="<id>"` | On the `<output>` element of a RangeSlider with `ShowValue=true`: the runtime mirrors `min – max` into this element as the user drags. |
| `data-fui-tag-input="<form-field-name>"` | On the text `<input>` of a `framework/ui.TagInput`: the runtime listens for Enter / comma to commit a new chip (creates a sibling `<input type=hidden>` with this name + the typed value), and Backspace-on-empty to remove the last chip. |
| `data-fui-tag-input-zone` | Wrapper containing the chips + the text input. Used by the runtime to scope chip insertion / removal. |
| `data-fui-tag-input-id="<input-id>"` | On the TagInput text input: the input's id (used by the runtime to associate the labeled `<output>` if present). |
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
| `data-fui-popover-side` | Written by the runtime onto the anchored popover's widget root after placement — value is the final chosen side (`"top"`, `"bottom"`, `"left"`, `"right"`, post auto-flip). CSS uses it to position the directional arrow (`::before`) and to apply the anchored chrome (border, shadow, max-inline/block-size). Cleared on dismiss. |
| `data-fui-popover-trigger` | Written by the runtime onto the originating trigger button while its anchored popover is open. The runtime also adds the `.is-popover-trigger-active` class so the trigger can be highlighted while its popover is the currently-active surface. Both are stripped on dismiss or when the popover re-anchors to a different trigger. |
| `data-fui-prefetch="<module>"` | On any element: opt the page into hover/focus-prefetch of a split runtime module (e.g. `data-fui-prefetch="fileupload"`). On the first `pointerover` or `focusin` (capture phase, once per element) the runtime fires `__gofastr.loadModule(<module>)` so the module is ready by the time the user clicks. Multiple modules can be listed space-separated. Used to keep typical pages on `core.js` only while still feeling instant on interaction. See `docs/runtime-code-split-plan.md`. |

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

### Patterns use the same contract

Every package under `core-ui/patterns/*` (accordion, breadcrumbs,
combobox, disclosure, infinitescroll, multiselect, nestedlist,
pagination, progress, skeleton, sortablelist, tabs, tree) uses
`registry.RegisterStyle` and wraps its top-level rendered element
with `Style.WrapHTML(...)`. Class selectors stay class-based
(`.accordion`, `.tabs`, `.nested-list`) — the marker only signals
to the auto-loader "fetch this stylesheet". No host setup required.

**Legacy `BaseCSS() string` exports are forbidden** — host apps used
to import each pattern and concatenate `BaseCSS()` into their custom
CSS via `WithCustomCSS`, but a single forgotten concat shipped a
component without any styling on the page (the 2026-05-19 nestedlist
incident). The contract is enforced by
`core-ui/check.LintNoPatternBaseCSS`, run as a test in CI: any new
pattern package exporting a `BaseCSS` function fails the build.

The canonical shape for a new pattern package:

```go
// core-ui/patterns/foo/foo.go
package foo

import (
    "github.com/DonaldMurillo/gofastr/core-ui/registry"
    "github.com/DonaldMurillo/gofastr/core-ui/style"
    "github.com/DonaldMurillo/gofastr/core/render"
)

var Style = registry.RegisterStyle("foo", styleFn)

func styleFn(_ style.Theme) string { return baseCSS }

func Render(cfg Config) render.HTML {
    return Style.WrapHTML(render.Tag("div", attrs(cfg), ...))
}

const baseCSS = `.foo { ... }`
```

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
                 accordion, breadcrumbs, combobox, disclosure,
                 infinitescroll, multiselect, pagination, progress,
                 skeleton, sortablelist, tabs, tree
  component/   — Component / InteractiveComponent interfaces (the contract
                 every renderable satisfies)
  widget/      — island/widget builder + registration
  widget/preset/ — opinionated mounting shortcuts:
                 Modal, Drawer, Popover, ToastStack, Toast, Banner,
                 FloatingPanel, BottomSheet
  widget/theme/ — page-level theme tokens + utility classes
  signal/      — reactive state + SSE push
  island/      — runtime-side island manager
  runtime/     — runtime.js (client) + Go embed wrapper
  runtime/src/ — code-split runtime modules (loaded on demand):
                 animatedcounter, banner, carousel, combobox,
                 dropzone, fileupload, infinitescroll, lightbox,
                 menu, multiselect, numberinput, popover, rangeslider,
                 shortcut, slider, sortablelist, sse, taginput,
                 textarea, toasts, toc, tree, widgets
  runtime/colorscheme.js — dark/light mode bootstrap (runs sync in <head>
                 before CSS parses, reads localStorage + OS hint,
                 sets data-color-scheme on <html>)
  style/       — theme structs, stylesheet builder, token resolution
  check/       — .ui.go linter

framework/
  uihost/      — wires core-ui app onto framework.App router; serves
                 runtime.js, theme.css, styles.css; handles SSE; client-side
                 navigation partial-fetch endpoint
  static/      — SSG builder (renders every screen at build time)
  ui/          — opinionated semantic components on top of core-ui
                 (see full list in the cheat sheet below)
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
8. **Modals + drawers can deep-link.** Toasts and dropdowns intentionally cannot. If you find yourself wanting a `?toast=…` URL, stop — toasts are ephemeral by definition.
9. **Animation durations and easings live on the theme** (`Theme.Durations`, `Theme.Easings`). Never hard-code `transition: transform 0.3s ease` in component CSS — read `var(--duration-…)` / `var(--easing-…)` so a single theme tweak retunes every surface.

---

## UI primitive cheat sheet

The framework ships base surfaces (`core-ui/widget/preset.*`) and
opinionated facilities (`framework/ui.*`). Pick the layer that matches
your need:

### Surfaces (widget presets)

| You want | Use | Notes |
| --- | --- | --- |
| Confirm a destructive action | `preset.Modal` + `framework/ui.ConfirmAction` | Or skip the modal entirely and put `data-fui-confirm="…"` on the button. |
| Edit/show entity detail | `preset.Modal` with `DeepLink("modal", "<name>").DeepLinkParam("id")` | URL stays consistent across refresh/share/back. Buttons opening it carry `data-fui-deeplink="id=<row-id>"`. |
| Confirm + act in one shot | `framework/ui.ConfirmAction` | Returns a trigger button + hidden `preset.Modal` alertdialog. Eliminates per-button confirm boilerplate. |
| Secondary nav / filters | `preset.Drawer` | Edge-anchored, backdrop'd. Same deep-link wiring as modals. |
| Click-triggered help / share / inline expander | `preset.Popover` | Anchored floating surface, no backdrop dim, no focus trap. Escape and click-outside dismiss. |
| Floating panel (non-modal, persistent) | `preset.FloatingPanel` | Bottom-right mounted panel. No backdrop, no focus trap. Use for chat widgets, debug panels, AI assistants. |
| Mobile-friendly bottom sheet | `preset.BottomSheet` | Bottom-mounted dialog with backdrop, Esc + click-outside close. Use for mobile action sheets, filter panels. |
| Background events ("Saved", "Build failed") | `preset.ToastStack` + `framework/ui.ToastBus` | Server pushes via SSE; the runtime stacks + auto-dismisses with hover-pause. No URL state. |
| Dismissible announcement banner | `preset.Banner` + `framework/ui.Banner` | Full-width banner with optional localStorage-persisted dismiss. |

### Data & visualization

| You want | Use | Notes |
| --- | --- | --- |
| Table with sort / filter / pagination | `framework/ui.DataTable` | Island-driven, server-side sort/filter/pagination. No client-side re-implementation. |
| Categorical bar chart | `framework/ui.BarChart` | Pure-SVG, no JS. Per-bar color overrides, theme-primary default. |
| Multi-series line / area chart | `framework/ui.LineChart` | Pure-SVG time-series, no JS. Multiple series, area fill mode, palette cycling. |
| Ratio chart (pie / donut) | `framework/ui.PieChart` | Pure-SVG, no JS. Slice colors from theme palette, configurable inner radius for donut. |
| Inline trend sparkline | `framework/ui.Sparkline` | Pure-SVG, no JS. Line or area shape. Pairs with Card for metric-overview surfaces. |
| Unified diff viewer | `framework/ui.DiffViewer` | Line-by-line, unified or split layout. Parses standard `diff -u` format. |
| Collapsible JSON tree | `framework/ui.JSONViewer` | Nested `<details>`/`<summary>` collapse. No JS. Configurable open depth. |
| Image gallery | `framework/ui.Gallery` | Grid / strip / masonry layouts. Optional Lightbox or per-item Href click behaviour. |
| Image lightbox | `framework/ui.Lightbox` | Overlay viewer with Prev/Next nav, gallery groups, arrow-key keyboard nav. |
| Image carousel / slider | `framework/ui.Carousel` | Runtime-driven Prev/Next, pagination dots, optional auto-rotate and loop. |

### Navigation & structure

| You want | Use | Notes |
| --- | --- | --- |
| Primary navigation | `framework/ui.Sidebar` | Inline column ≥ md, hamburger + `preset.Drawer` < md, same content tree, active-route highlighting from the current URL. |
| Action menu on a row | `framework/ui.Menu` | Built on `<details data-fui-disclosure>` so Esc / SPA-nav close come free. Keyboard nav handled by the runtime. |
| Command palette (Cmd+K) | `framework/ui.CommandPalette` | Modal + `core-ui/patterns/combobox`. Debounced server search, keyboard nav, listbox selection. Returns trigger + preset pair. |
| Global search input | `framework/ui.GlobalSearch` | Combobox-based search field with `data-fui-shortcut-focus` for keyboard shortcut opening. |
| Page-width wrapper | `framework/ui.Container` | Max-width page wrapper with breakpoint-aware padding. Narrow / default / wide / full variants. |
| Vertical/horizontal spacing | `framework/ui.Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box` | Six spatial primitives sharing one stylesheet (all in `layout.go`). Replace hand-rolled `display:flex` divs. |
| Labelled content surface | `framework/ui.Card` | Header / body / footer regions, elevated / outlined / flat variants, optional interactive (whole surface is an `<a>`) form. |
| Toolbar of action buttons | `framework/ui.Toolbar` | `role="toolbar"` wrapper with optional named groups. Horizontal strip of buttons/links with visual separators. |
| Step progress indicator | `framework/ui.ProgressSteps` | Linear step indicator (upcoming / current / complete). Horizontal or vertical. `<ol>` with `aria-current="step"`. |
| Table of contents | `framework/ui.TableOfContents` | Auto-generated from `<h2>`/`<h3>` headings. IntersectionObserver active-section tracking. |
| Timeline / event history | `framework/ui.Timeline` | Vertical event list on a rail. Colored dot variants (neutral / success / warn / danger / info). Semantic `<ol>`. |

### Forms & inputs

| You want | Use | Notes |
| --- | --- | --- |
| Form layout / field grid | `framework/ui.Form` | Label + input + error wiring. `FieldErrors`-aware validation display. |
| Labelled text input | `framework/ui.FormField` | Wraps any input with label, help text, and error display. |
| Multi-line text input | `framework/ui.TextArea` | Labelled `<textarea>` with optional autogrow (`data-fui-autogrow`). |
| Form toggles (boolean / single-select / setting) | `framework/ui.Checkbox` / `Radio` / `Switch` | Labelled native inputs, `FieldErrors`-aware, focus ring + touch target token-driven. |
| Segmented toggle bar | `framework/ui.SegmentedControl` | Native `<input type="radio">` group styled as sliding pill bar. CSS-only indicator. Optional RPC-on-change. |
| Star / heart / thumb rating | `framework/ui.RatingInput` | Hidden radio group, keyboard-accessible. CSS-only hover preview via `:has()`. Multiple glyph shapes. |
| Numeric stepper | `framework/ui.NumberInput` | `<input type="number">` with +/− buttons. Honors min/max/step. |
| Range slider (single) | `framework/ui.Slider` | `<input type="range">` with optional value mirror output. |
| Range slider (dual min/max) | `framework/ui.RangeSlider` | Two `<input type="range">` cross-clamped so min ≤ max. |
| Color picker | `framework/ui.ColorPicker` | Styled `<input type="color">`. Browser native UI, framework owns label + swatch. |
| Time picker | `framework/ui.TimePicker` | Styled `<input type="time">`. Browser native UI, framework owns label + touch target. |
| Tag / chip input | `framework/ui.TagInput` | Enter/comma to commit chips, Backspace-on-empty to remove. Creates hidden inputs for form POST. |
| Multi-select with chips | `framework/ui.Multiselect` (pattern) | Checkbox list with chip strip. Runtime rebuilds chips on change. `aria-live="polite"`. |
| Drag-drop file picker | `framework/ui.FileUpload` | Native `<input type="file">` is the source of truth; `data-fui-fileupload` adds drag-zone enhancement. |
| Drag-drop with image preview | `framework/ui.FileDropzone` | File input + drop zone + FileReader image previews. |
| Combobox (autocomplete search) | `core-ui/patterns/combobox` | Input + listbox, debounced RPC search, signal-driven list swap. Used by CommandPalette and GlobalSearch. |

### Feedback & status

| You want | Use | Notes |
| --- | --- | --- |
| Inline loading indicator | `framework/ui.Spinner` | `role="status"` + `aria-busy`, ring / dots variants. Pairs with `aria-busy` on the containing form during RPC. |
| Notification bell + dropdown | `framework/ui.NotificationBell` | Bell button + unread badge + `preset.Popover` item list. SSE-driven via signal bindings. |
| Ephemeral notification toast | `framework/ui.Notification` | Styled toast content (title + body + variant). Drop inside `preset.Toast` or `preset.ToastStack`. |
| Section break | `framework/ui.Divider` | Native `<hr>` for plain horizontal dividers; `role="separator"` for vertical or labelled (e.g. "OR" between auth options). |
| Hover/focus hint on a control | `framework/ui.Tooltip` | CSS-only reveal, `aria-describedby` auto-wired. No JavaScript. |
| Dismissible banner | `framework/ui.Banner` | Full-width banner with optional `localStorage`-persisted dismiss (`data-fui-banner-dismiss-id`). |
| Pill — filter chip / applied filter / selection | `framework/ui.Tag` | Status variants, optional `Href` (filter link) or `Dismiss` (× RPC). Distinct from `StatusBadge` (read-only). |
| Active filter chip toolbar | `framework/ui.FilterChipBar` | Toolbar of removable filter chips. Optional "Clear all" action. Island-driven, signal-swapped. |
| Copy-to-clipboard button | `framework/ui.CopyButton` | Targets any element by CSS selector. Label swap on success. SR announcement via `data-fui-copy-status`. |

### Visual primitives

| You want | Use | Notes |
| --- | --- | --- |
| Responsive lazy-loaded imagery | `framework/ui.OptimizedImage` | `<picture>` + `srcset`, lazy by default, mandatory `Width`+`Height` to eliminate CLS. |
| Overlapping avatar stack | `framework/ui.AvatarGroup` | Negative-margin stack with "+N" overflow indicator. Size propagates to children. |
| Animated number counter | `framework/ui.AnimatedCounter` | Ticks from → to on scroll-into-view. Respects `prefers-reduced-motion`. |
| Text link (inline / action / muted) | `framework/ui.Link` | Three variants: inline prose link, 44px action link, subdued muted link. |
| Markdown rendered as HTML | `framework/ui.Markdown` | Themed prose wrapper over `core/markdown`. Headings, lists, code blocks get theme tokens. |
| Keyboard shortcut hint | `framework/ui.ShortcutHint` | Platform-aware mod-key glyphs (⌘ vs Ctrl) via `data-fui-os`. |
| Theme override for a subtree | `framework/ui.Themed` | Wraps any content in a `.fui-theme-<hash>` div for section-level theming. |
| Infinite scroll feed | `core-ui/patterns/infinitescroll` | IntersectionObserver sentinel, cursor-based pagination, appends HTML on scroll. |
| Sortable drag-and-drop list | `core-ui/patterns/sortablelist` | Drag reorder + keyboard reorder. POSTs new order to RPC. Reverts on non-2xx. |
| Expandable tree view | `core-ui/patterns/tree` | Lazy-load children via RPC on expand. Arrow-key nav, `data-fui-tree-toggle`. |

### Deep-linking modals + drawers

Wire a deep link on a widget:

```go
preset.Modal("user-edit").
    Hidden().
    DeepLink("modal", "user-edit").
    DeepLinkParam("user_id").
    Signal("user_id", widget.SignalFunc(func() (any, error) { return "", nil })).
    Slot("body", &UserEditForm{}).
    Build()
```

Open from a row click that carries per-row data:

```html
<button data-fui-open="user-edit" data-fui-deeplink="user_id=42">Edit</button>
```

Result: clicking the button opens the modal AND pushes
`?modal=user-edit&user_id=42` onto the URL. Refresh, share, and the
browser back button all keep the modal open / closed correctly. The
signal seed runs before the modal becomes visible so the form is
already filled in.
