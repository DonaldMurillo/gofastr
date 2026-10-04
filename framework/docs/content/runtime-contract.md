# Runtime contract: SSR, hydration, islands, and the `data-cui-*` reference

<!--
  SYNC NOTE: this doc is an embedded extract of core-ui/ARCHITECTURE.md,
  which is the repo's source of truth for the UI/runtime contract. The
  two must be kept in sync: when the model or the attribute table
  changes there, update this file in the same commit.
  framework/docs/doc_sync_test.go fails when an attribute documented in
  core-ui/ARCHITECTURE.md is missing here.
-->

This page carries the runtime contract for readers of the embedded
docs (`gofastr docs`, the `framework_docs_*` MCP tools from
`framework/docs/mcptools`): the
SSR/hydration/island/SSE model and the full `data-cui-*` attribute
reference. If you are working inside the framework repo, read
`core-ui/ARCHITECTURE.md` instead; it is the authoritative version
and adds the recipes, the styling contract, and the component
cheat sheet.

## The model in one paragraph

Every page is **server-rendered (SSR)** on first request: full HTML, full
data, no skeleton, no client-side data fetch on initial load. The browser
receives the rendered page and `runtime.js` **hydrates** the existing DOM:
attaches event listeners, signal bindings, SSE streams. After hydration,
**page-to-page navigation is client-side** (Angular-router style: pushState,
partial fetch, swap content, with cache so back is instant, no hard
refreshes). **Interactions that change state inside the current page are
handled by islands**: a click triggers an RPC to the server-side island
handler, which returns the updated island HTML; the runtime swaps just
that island's content. The rest of the page stays put. **Passive freshness
is polled**: a region with `data-cui-poll` (or a widget with
`Builder.Poll`) re-fetches server-rendered HTML on an interval, no held
connection, no cross-replica infrastructure. **Server-pushed updates**
(e.g. another user changed something) flow through signals + SSE to update
bound DOM nodes without any client action, reserved for semantics that
need the connection itself (presence, collaboration, sub-second updates).
The full escalation ladder, client signals → RPC → poll → SSE push, is
[reactivity](reactivity.md); the interactive layer is stateless (sessions
are signed tokens, state lives in the DB or the client signal store), so
any replica serves any request.

---

## The five scenarios

| Scenario | What runs | What's on the wire |
|---|---|---|
| **Initial load** of any URL | Full SSR via `framework/uihost` → `app.RenderPage` → `Screen.Load(ctx)` → `Screen.Render()` | One HTML response with everything inline |
| **Page → page navigation** (`/a` → `/b`) | Client-side router intercepts `<a>` click, fetches partial via `X-Gofastr-Navigate: 1` + `X-Gofastr-From: /a`, swaps the content cell of the deepest layout layer the two routes share (`X-Gofastr-Swap` names it), caches the previous page for instant back. No shared root → full fetch + shell swap | One small partial HTML response: only the screen content plus whatever layout layers actually differ; shared chrome never re-sent |
| **In-page state change** (sort, paginate, expand a row, open a tab) | Click on an island element → RPC to the island's handler → server returns new island HTML → runtime swaps just the island's slot | One small RPC response with the changed island HTML |
| **Passive freshness** (a counter, a status, a dashboard that should stay roughly current) | `data-cui-poll` region (or `Builder.Poll` widget) → interval GET of a server-rendered fragment → runtime swaps the region | One small GET per interval; no connection held, any replica answers |
| **Server-pushed update** (background event, another user's action) | Server renders fresh island HTML and calls `Manager.PushUpdate` → `island` SSE frame → runtime swaps the matching `[data-island="…"]` region | SSE frames over a single long-lived connection |

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
heading, a piece of marketing copy, a footer) is **not** an island; it's
just rendered HTML.

Islands are built with the `core-ui/widget` builder (see
[widgets](widgets.md)) or wired by hand with the attributes below (see
[interactive-patterns](interactive-patterns.md) → "Writing a
hand-written island, end to end").

---

## Runtime primitives (the wiring)

The runtime understands a small set of `data-cui-*` attributes on the
hydrated DOM. **You don't write JavaScript**; you compose these on the
server side and the runtime does the work.

### Who owns which prefix

Every class and `data-*` attribute the framework emits carries the
prefix of the tree that DEFINES it, and a lower layer never names an
upper layer's vocabulary. The gate is `core-ui/check.LintLayerPrefixJS`
/ `LintLayerPrefixGo`, run over the kernel and headless trees by their
own test suites.

| Prefix | Defined by | Read or written by | Examples |
|---|---|---|---|
| `data-cui-*`, `cui-*`, `#cui-*` | `core-ui` (the kernel: `runtime.js`, `core-ui/app`, `core-ui/widget`, `core-ui/interactive`) | anyone: the kernel reads them, every layer above emits them | `data-cui-rpc`, `data-cui-open`, `data-cui-comp`, `cui-widget`, `cui-visually-hidden`, `#cui-toast-fallback` |
| `data-hui-*` | `framework/headless` (structure + behaviour hooks) | headless modules, the kit's sheets, and the two kernel modules that place content into headless markup (`formerrors.js` reads `data-hui-field`, `data-hui-field-error`, `data-hui-choice`; `feedback.js` clones `data-hui-toast-template`) | `data-hui-field-error`, `data-hui-copy-state`, `data-hui-toast-template` |
| `fui-*`, `data-fui-*`, `--fui-*` | the rest of `framework/`: `framework/ui` (classes, its own modules), `framework/uihost`, `framework/pluginhost` | framework code and app sheets; never the kernel or headless | `fui-notification`, `data-fui-lightbox`, `data-fui-plugin`, `data-fui-page-loading`, `--fui-button-bg` |
| a plugin's own prefix | the plugin repository | the plugin | `gofastr-plugins` names its attributes after itself, not `data-fui-plugin*` |

What this rules out, each a shape that shipped before v0.87.0: a
kernel module that names a kit class (`fui-notification__title`,
`fui-field__error`, `fui-copied`), a headless module that mounts a kit
container (`fui-toast-stack-auto`), and a kit class an app's owned
sheet overrides. The fix shapes are a hook the lower layer declares
(`data-hui-field-error`, `data-hui-copy-state`), a template the kit
registers for the lower layer to clone (`registry.RegisterTemplate`,
`preset.ToastTemplate`), or a module the kit ships itself
(`framework/ui/searchinput.js`, `filedropzone.js`, `lightbox.js`).

The `data-fui-*` keys that remain are the framework's own modules'
and hosts': `data-fui-lightbox*` and `data-fui-zoomed` (the kit's
lightbox module), `data-fui-dropzone-preview*` (its dropzone module),
`data-fui-pane*` (its pane host), `data-fui-z-tier` (`ui.Sticky`'s
sheet), `data-fui-network-retry-*`, `data-fui-plugin*`
(`framework/pluginhost`) and `data-fui-page-loading`
(`framework/uihost`). Every other attribute the runtime reads is
`data-cui-*`, in the table below.

| Attribute | Purpose |
|---|---|
| `data-cui-rpc="<path>"` | Click / form-submit fires a request to `<path>`. A non-2xx answer to a form submission is never silent: the server's validation envelope (`{error, fields: {name: [messages]}}`) marks each named field's control (`aria-invalid`, `aria-describedby`) and places a `role="alert"` message by the headless hooks, never by a kit class (a reserved or live `[data-hui-field-error]` node inside the `[data-hui-field]` group, or the next sibling of a bare `[data-hui-choice]` label), and when no field matched, the `error` text is toasted. |
| `data-cui-rpc-method="GET\|POST\|…"` | HTTP method (default POST) |
| `data-cui-rpc-signal="<name>"` | The response body is treated as a signal value and broadcast to bound nodes |
| `data-cui-rpc-close` | Containing widget closes on 2xx |
| `data-cui-rpc-reset` | Containing form resets on 2xx |
| `data-cui-rpc-open="<widget-name>"` | A registered widget opens on 2xx (e.g. "save in drawer → open results sheet") |
| `data-cui-rpc-navigate="<path>"` | Client-side SPA navigation to `<path>` on 2xx. Bypasses the screen cache and re-renders even when `<path>` is the current page; the RPC mutated server state, so the destination must be fetched fresh |
| `data-cui-rpc-refresh="<widget-name>"` | On 2xx, triggers an immediate `/state` re-fetch (`pollNow`) on the NAMED polling widget instead of the one the button lives in. For a mutation whose result a *different* widget renders, e.g. a Reset button inside a confirm modal refreshing the chat panel. |
| `data-cui-signal="<name>"` | This node's content/attribute updates when the named signal changes |
| `data-cui-signal-mode="text\|html\|attr"` | How to apply the signal value (default `text`) |
| `data-cui-signal-attr="<attr>"` | Attribute name when mode is `attr` |
| `data-cui-signal-set="<name>[:<value>]"` | Click sets the named signal to `<value>` purely client-side (no RPC). Omit `:<value>` to set the empty string. Used by `framework/ui.Tabs` buttons (`<name>:<index>`). |
| `data-cui-signal-inc="<name>[:<delta>]"` | Click increments the named signal by `<delta>` (default `1`; negative decrements) client-side. Used by `framework/ui.Counter`. |
| `data-cui-signal-toggle="<name>"` | Click flips the named boolean signal client-side. Used by `framework/ui.SignalToggle` and `interactive.ToggleLocal`. |
| `data-cui-tab-index="<n>"` | Set on `framework/ui.Tabs` buttons and panels to associate each with its zero-based index. CSS keys the active-button highlight and visible panel off the wrapper's `data-active` matching this index. When the wrapper's `data-active` attribute is updated through a signal (`data-cui-signal-mode="attr"`), the core runtime also mirrors the new index into `aria-selected` on every `[role="tab"][data-cui-tab-index]` descendant so assistive tech tracks the selection, not just the CSS highlight. |
| `data-hui-tabs-state` | On a `framework/ui.Tabs` wrapper (`TabsConfig.StateAttrs`): the `headless-tabs` module mirrors `data-active` into `data-state="active"/"inactive"` on each `[role=tab]` button, the contract Radix-style ports pin test locators to. |
| `data-hui-tabs-vacate` | On a `framework/ui.Tabs` wrapper (`TabsConfig.VacateHidden`): hidden panels ship empty (content in `data-hui-tabs-stash`); the `headless-tabs` module restores content on first show and moves live nodes out/in on later switches, so swapped island content survives re-show. Vacated panels are detached: document-scoped updates targeting them are dropped permanently (nothing is queued; re-show resurrects the pre-vacate nodes, and only updates arriving after re-show land). |
| `data-hui-tabs-stash` | On the JSON `<script>` beside the panels of a `VacateHidden` strip: map of tab index → panel HTML for the panels that shipped empty. Escaped so embedded `</script>` cannot terminate it. |
| `data-cui-computed="<reducer>"` | Marks a `core-ui/store` computed slice. The `computed` runtime module subscribes the node to its dependency signals and, on any change, runs the host-registered JS reducer `window.__gofastr._reducers[<reducer>]` over the current dep values and broadcasts the result to this node's `data-cui-signal`. CSP-safe; the reducer is a real function the host registers (no `eval`). |
| `data-cui-computed-deps="<a,b>"` | Comma-separated dependency signal names a `data-cui-computed` node recomputes from. |
| `data-cui-open="<widget-name>"` | Click opens a registered widget surface |
| `data-cui-ctx="<opaque>"` | On a `data-cui-open` trigger: per-trigger context for the opened widget's chrome. The runtime forwards it as `?ctx=` on the chrome fetch and keys the client chrome cache by `(name, ctx)` (LRU, capped at 32 entries, cleared on SPA navigation). Opaque to the framework — never parsed. `serveChrome` is the validation boundary: it bounds a URL-carried `ctx` at 256 bytes and rejects invalid UTF-8 and control runes before rendering. `widget.ChromeContext(ctx)` only reads the accepted value, and the slot MUST authorise the entity it names against the request context. Use for per-entity dialogs: `data-cui-open="layout-remove" data-cui-ctx="inv-42"`. |
| `data-cui-push-state="<path>"` | After the RPC succeeds, apply this URL via `history.pushState` (no re-fetch). Useful when the button knows the canonical URL ahead of time (e.g. pagination button "page 3" → `data-cui-push-state="?p=3"`). Server-supplied `X-Gofastr-Push-State` header takes precedence. The runtime also refuses a destination that is not same-origin: the SPA navigator applies its own origin check, and the pre-boot `location.href` fallback applies the same one inline, so a `javascript:` or cross-origin value in this attribute navigates nowhere. |
| `data-cui-confirm="<message>"` | Pre-flight `window.confirm(<message>)` gate, honored on every form submit the runtime sees — native POST, `data-cui-spa`, and `data-cui-rpc` forms alike — and on non-form `data-cui-rpc` clicks. On a form, an attribute on the submit button takes precedence over one on the form element. Cancel aborts: the submit is prevented (a native form never navigates), the RPC never fires. Use for destructive actions (delete, revoke). |
| `data-cui-rpc-trigger="input"` | On a `data-cui-rpc` carrier (a `<form>` or any element — the combobox uses a `<div>` so a host form survives HTML parsing), dispatch the RPC on every `input` event from any control inside, after a debounce window. A non-form carrier's named, enabled controls are serialized into the body exactly like a form's. |
| `data-cui-rpc-debounce-ms="<ms>"` | Debounce window for `data-cui-rpc-trigger="input"`. Default 250. |
| `data-cui-rpc-after-text="<text>"` | On 2xx RPC, replace the trigger's text content with `<text>`. One-shot; idempotent on re-click via `data-cui-rpc-after-done`. |
| `data-cui-rpc-after-disable` | On 2xx RPC, mark the trigger as `aria-disabled="true"` and (for `<button>`/`<input>`) set `disabled=true` permanently. Use with `after-text` for "Saved ✓" / "Revealed ✓" feedback. |
| `data-cui-rpc-scroll-to="<selector>"` | On 2xx RPC, smooth-scroll the matching element into view. Use to direct the user's eye at newly-inserted content. A malformed selector is a no-op; it can never corrupt the RPC response signal. |
| `data-cui-comp="<name>"` | Marks an instance of a registered styled component. The runtime scans for it on every DOM insertion and lazily loads `/<__gofastr/comp/<name>.css>` once per session via a `<link data-cui-style="<name>">` (dedup'd, never re-fetched). See "Component CSS" below. |
| `data-cui-scope="<name>"` | Marks the root of an owned style: a layout root (`LayoutSpec.Style`), a screen's wrapper (`Screen.WithStyle`: its `<article>`, or one plain `<div>`, never the primary cell), or a component root (`Style.Scope`). The server writes it; the runtime never does. It is two things at once: the root of the style's compiled `@scope`, whose lower bound stops at the children of any nested owner, and a loader marker, read exactly like `data-cui-comp`: the SSR head scan (`registry.Scan`) links `/__gofastr/comp/<name>.css` for every name on the page, and the runtime's `scanAndLoadCSS` loads it on insertion (a cross-layout `swapShell` scans the new shell's parent, because the shell root carries its layout's scope). An element may carry both markers (`Style.Scope(ui.Card(...))`) and loads both sheets. Owned rules are scoped, so they beat an equal-specificity kit rule by scope proximity whatever order the sheets load in. App markup cannot set it: `html.SafeExtraAttrs` drops every `data-cui-*` key. |
| `data-cui-internal` | Marks kit component markup that holds none of the caller's content: a label or title built from a string field, a control's input, a dismiss button, an icon the component draws. Every compiled owned style's `@scope` lower bound stops at it, so an owner styles the content it passes into a component and never the component's insides. Components set it through `headless.Internal`, and a component that builds markup and hands it to another as slot content marks it with `headless.Own`, which also marks the element holding that slot. Never on a component's root (an owner may place the root), never on an element that holds a slot or on an ancestor of one; a mark inside a marked subtree is allowed and inert. `TestKitMarksInternalSubtrees` (framework/ui) renders every kit component with every slot filled and every slot empty and fails on unmarked internal markup, a marked root, or caller content under a mark. The server writes it; the runtime never reads it. App markup cannot set it: `html.SafeExtraAttrs` drops every `data-cui-*` key. |
| `data-cui-bundle="<a,b,c>"` | Set on the SSR-emitted bundle `<link>` to list the components it covers. The runtime reads it at boot and seeds `_pendingLinks` so the per-component scan never double-loads anything already in the bundle. |
| `data-cui-layout="<name>"` | Set by EVERY layout layer on its wrapper `<div>` with the layout's name (e.g. `app`, `marketing`). Emit-only since the layout-chain rewrite: it is the CSS/debug contract (`.layout-<name>` pairing); the runtime's swap decisions read `data-cui-layout-key` instead. |
| `data-cui-layout-key="<key>"` | The layer's comparable identity, on the same wrapper `<div>`: `l:<name>` for a plain layout (the app default root, a direct screen's layout), `g:<prefix>:<name>` for a screen-group layer (`g:<prefix>` when the level is marker-only because its layout already renders at an outer level). The route manifest carries each route's chain as the `layouts` array of these keys, outermost → innermost; document order of the marked elements is the chain order. On SPA navigation the runtime compares the DOM's key spine against the destination's chain positionally: it swaps at the deepest shared layer, and when no root is shared it fetches the full page and replaces the whole shell. A group layer's key embeds the layout name so a per-screen layout override inside a group compares as a different layer than its siblings. |
| `data-cui-layout-slot="<key>"` | On the layer's content cell: the `<main id="main-content">` for layer 0, the `.layout-content` div (tabindex="-1") for nested layers, the group wrapper itself for marker-only levels. This is the runtime's swap target: a partial response's `X-Gofastr-Swap: <key>` (or a cache entry's recorded layer) selects the cell whose slot key matches. After the swap the runtime focuses the cell so screen readers announce the new content — with `focusVisible: false` when the navigation began from a pointer click, so the focus ring stays keyboard signal (a keyboard-activated navigation, a history move, or a programmatic `navigate()` leaves the ring to the browser's `:focus-visible` heuristic). |
| `data-cui-outlet="<layer key>#<name>"` | On a tree-layout outlet cell (`app.NewLayout`): a non-primary outlet of a layout layer, addressed by its layer key plus outlet name. The SPA navigator resolves envelope fills by this address (loop + string compare, like `findSlot`) and swaps the cell's innerHTML when the fill's hash differs. |
| `data-cui-area="<layer key>~<name>"` | On a tree-layout route-area cell: a layout area re-rendered by the server on every navigation its layer survives. Same addressing and swap rules as `data-cui-outlet`; the area's fn runs on every render, kept layers included (collect mode on partials). |
| `data-cui-fill="<addr>"` | On the `<template>` elements of a fills-envelope partial body (`X-Gofastr-Envelope: 2`): the primary payload is addressed by the bare swap key (no hash, always applied), every non-primary fill by its `data-cui-outlet`/`data-cui-area` address. Parsed inert in a detached template. |
| `data-cui-fill-hash="<hex>"` | The FNV-64a hash of a fill's HTML: stamped on outlet/area elements by full pages and by the runtime after each applied fill, carried on the envelope's fill templates. The navigator skips a fill whose hash equals the target's stamp (unchanged content), otherwise applies it and re-stamps. |
| `data-cui-vt="<name>"` | On a tree-layout placed cell (primary, outlet, or route area) whose Go placement names a view transition (`app.PrimaryConfig.Transition` on the LayoutSpec primary / `app.OutletOptions.Transition` (the typed `app.Outlet` handle's options) / `app.AreaSpec.Transition`, typed `app.Transition` values). The server renders the name (the author's raw `Transition.Name`, or a generated `vt-<layout>-<slot>`); the transition demand module (runtime `src/transition.js`, loaded when the document holds a `data-cui-vt` cell or a `data-cui-vt-kinds` vocabulary, at boot or after any apply) mirrors every `data-cui-vt` cell onto the CSSOM `view-transition-name` before a client navigation's view-transition snapshots (a `style` attribute is refused by the framework's default CSP, a CSSOM write is not), and wraps the swap in `document.startViewTransition({update, types})` with types `forward` / `back` / `reload`. Before that module loads — and on every page that declares no transition — the swap applies directly, with no view transition at all. `Layout.TransitionCSS()` generates the animation rules (enter/exit, back-direction variants, the name assignment), which the host collects into app.css for every registered layout; a raw `Transition.Name` with zero anims generates only the assignment and leaves the animation to author CSS (the platform's full power: shared-element morphs, geometry, custom keyframes); root-wide presets ship as `app.ViewTransitionPresetCSS` (fade / slide / none). Under `prefers-reduced-motion: reduce` the runtime starts no transition at all. A cancelable `gofastr:transition` event (detail `{from, to, types}`) fires on `document` before each transition; `preventDefault()` commits the swap with no transition. A streamed envelope's first unit (seed + primary + ready fills) commits through the same wrapper, so loading content being replaced by the real fill rides the same transition; late units apply directly ( merge). |
| `data-cui-vt-when="<media condition>"` | Beside `data-cui-vt` on a placed cell, or on the region the layout build marks via `app.LayoutTree.VTRegion()`, when the transition declares `app.Transition.Narrow` ("920px"): the name is breakpoint-conditional — the placed cell owns it at `(width >= Narrow)`, the region below it. The master-detail collapse: below the breakpoint list and detail are one pane and the whole pane must transition; a detail-only snapshot there would morph its group geometry across the list. The runtime mirror writes the CSSOM `view-transition-name` only while the condition matches and CLEARS it otherwise, so a viewport resize across the breakpoint moves the name instead of duplicating it (two live names of one spelling make the browser skip the whole transition); `Layout.TransitionCSS()` wraps the two assignment rules in the same `@media` conditions, keyed on this attribute. |
| `data-cui-loading="<addr>"` | On the inert `<template>` the server renders BESIDE an outlet cell, an area cell, or the primary slot's cell (addressed by the bare layer key) when that region declares `Loading` (`app.OutletOptions.Loading` / `app.AreaSpec.Loading` / `app.PrimaryConfig.Loading`): the browser already holds the loading content before any navigation fetch starts. On a navigation that will change the outlet, after `data-cui-after` ms of in-flight wait the runtime moves the outlet's old nodes into an in-document hidden park and clones the template's content in; on apply the response replaces it (honoring `data-cui-min`), and on failure, abort or a superseded navigation the parked nodes come back exactly — same nodes, so input values, listeners and island state survive. Inert with JavaScript off: SSR pages never show loading content, the real content is in the outlet. |
| `data-cui-after="<ms>"` / `data-cui-min="<ms>"` | On the loading template: `after` is how long the navigation must be in flight before the loading content shows (default 120 ms, the dim's delay — a faster response paints nothing extra); `min` keeps it, once shown, at least this long before the apply replaces it (no skeleton flash). Emitted by the server from `app.Loading.After` / `.Min`. |
| `data-cui-loadstate="shown\|exit"` | Runtime-written, on the outlet/slot cell across the loading content's lifecycle: `shown` while it holds the cloned content (the enter animation runs now — author CSS keys richer enter/exit effects off the same states; the framework ships a default fade in `frameworkDimCSS`), `exit` while the apply waits for the region's own `animationend` (capped at 400 ms, child animations ignored) before replacing it, absent otherwise. The framework CSS also exempts the region from the aria-busy dim (the loading content replaces the old content; dimming it would double the signal). Removed when the content is replaced or restored. |
| `data-fui-page-loading` | On the body-level `<div>` a host's `uihost.WithPageLoading(component)` renders into every full page: the host's own page-wide loading indicator, REPLACING the default `html[aria-busy]::after` progress strip (both never show at once). Pure CSS state — visibility keys off the same `html[aria-busy]` carrier, so it transitions in and out with no runtime involvement. The component is presentational config, rendered once per page with no Load/DI. |
| `data-cui-lang="<tag>"` | On the outermost layer the server renders (layer 0 of a full page, the first re-rendered layer of a subtree partial, the bare `<main>` of a layout-less page): the page's resolved document language (`App.LangForPath`, layered with the screen's `ScreenLang`). `<html lang>` lives outside the shell the runtime swaps, so the value must travel with the swap payload; after every SPA swap the runtime copies it onto `document.documentElement.lang` via `doc.setHtmlAttr` (in the DOC_MANIFEST). A payload without the marker leaves the document alone. A site whose language varies per route keys its outer layout per language (`Layout.WithKey`), otherwise no carrier arrives for the other language. |
| `data-cui-skip-label="<text>"` | Same carrier and same rule as `data-cui-lang`, for the app shell's skip-link text (`App.SkipLabelForPath` / `WithSkipLabelFunc`): after every SPA swap the runtime writes it into the `[data-skip-link]` link, the first string a keyboard user tabs to, so it speaks the destination page's language (#411). |
| `data-hui-disclosure-trap` | Opt-in modifier on a `data-hui-disclosure` `<details>` element (`headless.Disclosure`'s `Trap` prop): while open, the `headless-disclosure` module confines Tab inside the disclosure body. Use for mobile drawer / full-sheet popover patterns that need modal-style focus containment (vs. the default non-trapping inline disclosure). |
| `data-cui-action="close"` | On any element inside a mounted widget: clicking it dismisses the widget (the widget's scoped click handler in `widgets.js`), with no request. `close` is the only value the runtime reads; `headless.ButtonProps.Action` (and so `ui.Button` `ExtraAttrs`) admits exactly that value and refuses any other at render. |
| `data-cui-widget="<name>"` | Marks a registered widget instance; the runtime mounts behavior on it after first paint. |
| `data-cui-backdrop` | Marks an element as a click-to-dismiss overlay backdrop. Pairs with `data-cui-open` to make the floating surface dismissible. |
| `data-cui-style="<name>"` | Set on the runtime-injected `<link rel="stylesheet">` so duplicates are dedup'd by component name. |
| `data-hui-shortcut-click="<chord>"` / `data-hui-shortcut-focus="<chord>"` | Global keyboard shortcut: e.g. `Meta+K` or `/` focuses or clicks the target element. Rendered by `ui.GlobalSearch`, `ui.CommandPalette`, `ui.ShortcutHint` (BindTarget) and host chrome; bound by the registered `headless-navigation` module, which owns one document keydown with an isComposing guard and resolves the first connected target. |
| `data-cui-submit-on-enter` | On a `<form>`, Enter inside any child textarea submits the form. |
| `data-cui-clear-on-esc` | On an `<input>`/`<textarea>`, Escape clears the value. |
| `data-cui-autogrow` | On a `<textarea>`, height auto-grows with content. |
| `data-cui-charcount-source="<id>"` | An element that displays the live character count of the referenced input. |
| `data-hui-copy` | On the wrapper `framework/ui.CopyButton` renders (the button and its status span live inside it): marks a copy control. The `headless-feedback` module's delegated click reader performs the clipboard write (`navigator.clipboard.writeText` on the target's text) — no clipboard mutation is promised without script, the target stays readable and selectable. |
| `data-hui-copy-target="<id>"` | On the same wrapper: the ELEMENT ID of the copy source (not a selector — the module resolves it with `getElementById`; a leading `#` is stripped at render). |
| `data-hui-copy-copied` / `data-hui-copy-back` | The copied and idle label texts: on success the module swaps `data-hui-copy-copied` into a `[data-hui-copy-label]` span inside the wrapper, when one exists, and back after ~1.2s — the same window `data-hui-copy-state="done"` sits on the wrapper. `framework/ui.CopyButton` renders no `data-hui-copy-label` span: it draws both labels and its sheet shows one at a time from `data-hui-copy-state`. The span is for a host's own copy button. |
| `data-hui-copy-sentence` / `data-hui-copy-name` / `data-hui-copy-status` | The announcement contract: on success the module writes the sentence (with `{name}` substituted from `data-hui-copy-name`) into the visually-hidden `role="status"` span carrying `data-hui-copy-status`, clear then frame, so screen-reader users hear "Copied" without focus loss. |
| `data-hui-copy-toast="<json>"` | On the button (or any element) inside a `data-hui-copy` wrapper: the module reads the first `[data-hui-copy-toast]` in the wrapper and dispatches its own `NS.toast(<json>)` on copy success. Use for "Copied to clipboard" notifications without per-button JS. |
| `data-cui-os` *(on `<html>`)* | Set by the runtime at boot to `"mac"` or `"other"` based on best-effort platform detection. Used by `framework/ui.ShortcutHint` to display platform-correct mod-key glyphs purely in CSS (no per-component JS). Functional shortcut matching does not depend on this attribute. |
| `data-cui-static` *(on `<html>`)* | Injected **only** by the static exporter (`framework/static.Builder`) onto `<html>`. When present, the runtime enters static mode: it fetches the dumped catalog file (`/__gofastr/widgets.json`) instead of the live session-gated endpoint, and a `data-cui-rpc` click/submit surfaces a "Needs the Go server" notice (via the CSP-clean `#cui-nav-toast` mini toast) instead of firing a dead request, so a visitor who tries a server-backed demo learns why it's inert and how to run it locally. `data-cui-open` is **not** gated; overlays resolve against the widget catalog + chrome HTML the exporter dumps as query-free files, so navigation surfaces (command palette, section-menu drawers) work. Client-only features (theme toggle, copy, signal mutations) are unaffected. Live pages never carry it, so every static-mode guard is a no-op in the normal server-backed app. |
| `data-hui-tree` / `data-hui-tree-toggle` | On a `framework/ui.Tree` (headless.Tree anatomy): the registered `headless-tree` module (framework/headless, `Requires("rpc")` for the lazy branches) owns the WAI-ARIA keyboard contract — the roving tabindex, arrows, Home/End, type-ahead, and expand/collapse that drives the same toggle button a click drives, so any lazy-load `data-cui-rpc` on the toggle fires either way. |
| `data-cui-fill-input="<selector>"` / `data-cui-fill-text="<selector>"` | A button that fills the target input or text node with this element's `data-value` (or text content). |
| `data-cui-disable-when-invalid` | On a submit button: disabled while any field in the surrounding `<form>` reports `:invalid`. |
| `data-cui-persist-storage="<key>"` | The element's value persists across reloads in `localStorage`, stored namespaced as `gofastr.persist.` + `encodeURIComponent(<key>)` so an attribute-borne key can only ever touch that namespace. A value stored under the pre-namespace raw `<key>` is not read. |
| `data-cui-flash-on-update` / `data-cui-flash-duration-ms="<ms>"` | A signal-bound element flashes (CSS class `cui-flash`) for `<ms>` after each update. |
| `data-cui-scroll-bottom-on-update` | A signal-bound scroll container auto-scrolls to the bottom on each update (chat / log views). |
| `data-cui-tick-elapsed="<unix-ms>"` | Element's text updates once per second with the elapsed human-readable interval since the given epoch. |
| `data-cui-rpc-body="<json>"` | Static JSON body for `data-cui-rpc` requests that don't come from a `<form>`. |
| `data-cui-rpc-after-done` | Internal marker: set by the runtime after a one-shot `after-text` / `after-disable` fires so re-clicks are idempotent. |
| `data-cui-deeplink="<k1=v1&k2=v2>"` | On a `data-cui-open` button: per-click overrides for the opened widget's declared `DeepLinkParams`. The runtime mirrors the pairs into the widget's signals on open AND pushes them onto the URL (alongside the widget's `DeepLinkKey=DeepLinkValue`) so refresh / share / back-button preserve the open modal AND its data. Used for row-level "Edit user 42" flows. |
| `data-hui-toast-id="<id>"` | Marks one item inside a toast stack. Items the module builds carry the id `NS.toast()` assigned (`t<n>`); a server-rendered row is given one on sight (`s<n>`) — the id is what arms its TTL timer and keys the timer registry (a Map, so an attribute-borne `__proto__` re-parents nothing). |
| `data-cui-toast-stack="<name>"` | Marks the container into which `__gofastr.toast()` appends items. The name matches the widget name passed to `preset.ToastStack`; `headless-feedback` scans both this and the `data-hui-toast-stack` spelling. `framework/uihost` mounts one named `uihost.DefaultToastStack` (`gofastr-toasts`) at boot when the app mounted none, so a header toast always has a region; the module mounts no container of its own and returns `null` without one, which hands the toast to the kernel's `data-cui-toast-fallback` region. |
| `data-hui-toast-template` | On the inert `<template>` `preset.ToastSlotHTML` renders inside the stack: the row `headless-feedback` clones for a runtime toast. The kit registers it under `preset.ToastTemplate` (`registry.RegisterTemplate`), rendered by `headless.ToastTemplate`; the template carries `data-hui-toast-glyph-<tone>` (the icon text), `data-hui-toast-variant-<tone>` (the class the root gains), and the stack's words as fallbacks (`data-hui-toast-dismiss-label`, `data-hui-toast-tone-<tone>`). The tones are info, success, warning and danger; a toast with `variant: 'error'` (the kernel's own failure toasts) is drawn and announced as danger. Variants beyond the tones (`ToastTemplateProps.Variants`: `framework/ui` lists neutral and every `RegisterStatusVariant` name) ride `data-hui-toast-variants`, a JSON map of `{"<name>": {"class": "…", "glyph": "…"}}`: a row of that variant wears its class and glyph, is polite, and says no tone word. A variant the template does not list gets no variant class, no glyph and no tone word. Inside, the row is hooks: `data-hui-toast-item` (the stack row), `data-hui-toast` (the root), `data-hui-toast-tone`, `-icon`, `-title`, `-body` (filled, or removed when empty) and `data-hui-toast-dismiss`. No template on the page: the module builds the same row bare. |
| `data-hui-toast-leaving` | Written by `headless-feedback` on a toast item the moment it is dismissed; the kit's `ui-toast-stack` sheet animates the row out on it and the module removes the row when the animation ends. |
| `data-hui-toast-ttl-ms="<n>"` | On a toast item: auto-dismiss after `n` milliseconds, armed by `headless-feedback` for module-built and server-rendered rows alike. Hovering or focusing the item pauses the timer; leaving resumes from where it stopped. Omit (or 0) for persistent toasts that require explicit dismissal. |
| `data-hui-toast-dismiss` | Click target inside a module-built toast item that triggers dismiss. A server row's dismiss is a link with its Island — the kernel owns that click — and pairs with the module's CSS-driven fade-out animation. |
| `data-cui-embed-state` *(on the embed root)* | Lifecycle of an embedded surface. The server writes `loading` into the shell HTML; the `boot-embed` fragment writes every later value, and ships **only** in the `embed` bundle served at `/__gofastr/embed-runtime.js`. `ready` once the handshake completed and the surface's server-rendered content was injected. `error` when there is no parent to hand over a nonce, no token arrived within 15s, the exchange was refused, or the content fetch failed. `expired` when the grant's absolute lifetime ran out and refresh could not renew it. Nothing in the runtime branches on it; it exists so tests can see the frame's state. The host page is cross-origin and can neither read nor style inside the frame. See `framework/docs/content/embed.md`. |
| `data-cui-toast-fallback` | Marks the degraded inline container core injects when the `headless-feedback` module fails to load (transient 5xx, network hiccup). Used by `__gofastr._fallbackToast(cfg)` so an X-Gofastr-Toast payload still reaches the user even when the full module is unavailable. Unstyled-but-visible; no TTL, no animation. |
| `data-hui-menu="<id>"` | Marks a `<details data-hui-disclosure>` as a `framework/ui.Menu` dropdown (the headless menu anatomy, bound by the registered `headless-menu` module) — at the top level and on nested submenu rows alike (`MenuItem.Children` renders a nested `<details data-hui-menu>` whose `<summary role="menuitem">` carries `aria-haspopup="menu"`). The module focuses the first `[role=menuitem]`/`[role=menuitemradio]` when the disclosure opens; arrow keys / Home / End / type-ahead navigate within the item's own panel (a submenu's rows never leak into the parent's rotation); Escape closes one level and returns focus to its controller. The base disclosure behaviours (aria-expanded mirror, close-on-navigate) are `headless-disclosure`'s, which `headless-menu` requires. |
| `data-hui-menu-radio="<group>"` | Emitted by `framework/ui.Menu` on a `role="menuitemradio"` row: `MenuItem.Radio` names the group, `MenuItem.Checked` the initial `aria-checked` state. On activation (click / Enter / Space) the menu module arbitrates the group client-side — the activated row goes `aria-checked="true"`, every same-group sibling anywhere in the same menu — submenus included; a group split across a submenu boundary stays one group — goes `false` (the same mutex pattern the headless package's tabs use). |
| `data-hui-menu-panel` | Emitted by `framework/ui.Menu` on the `role="menu"` panel `<div>`. No runtime or CSS consumer today (the menu module scopes by `data-hui-menu` + the panel part); emit-only structural marker. |
| `data-hui-menu-lazy` | Emitted by `framework/ui.Menu` when `MenuConfig.LazyPanel` is set: the panel's rows ship inside this inert `<template>` as the panel div's only child, so closed-menu row text, labels, and roles are invisible to live-DOM queries (host Playwright `getByText`/`getByLabel` contracts) until first open; the rows are still in the HTML source, so nothing is hidden from a crawler that parses the response. The panel `<div>` itself always renders, so `aria-controls` still resolves while closed. |
| `data-hui-menu-trigger="<menu-id>"` | Emitted by `framework/ui.Menu` when `MenuConfig.TriggerElement` is set: the presentation wrapper (`role="presentation"`, `display: contents`) holding the caller's own button/anchor, beside the summary-less `<details data-hui-menu="<menu-id>" data-hui-disclosure>` that carries the panel. An interactive element inside `<summary>` is axe `nested-interactive` (SERIOUS), so a caller-owned trigger must not route through `TriggerHTML`. The value pairs the wrapper with the details it names so the module can wire `aria-expanded`/`aria-controls` onto the caller's trigger. |
| `data-cui-match-prefix` | On a `<nav> <a>` link: opts the link into prefix-matching for active-route highlighting AND hands the link's current-state to the `activelink` module. The runtime tags it `aria-current="page"` + `.active` when the current path equals the link's href or continues it at a segment boundary: `/docs` and `/docs/` both light up on `/docs` and `/docs/getting-started`, and neither matches `/docs-old`. A server-rendered first-paint mark on such a link is activelink-owned too: the sweep clears it when the route moves elsewhere (without the handover a stale SSR `aria-current` survived beside the new mark — two lit entries). Links with neither the handover attribute nor the module's own `.active` class (pagination, server breadcrumbs, hand-set state) keep owning their attributes. Without this attribute the runtime does exact-href matching only, and sets/clears only what it stamped. Root `/` is never a prefix match. |
| `data-cui-activelink` | On a `<nav> <a>` link: hands the link's current-state to the `activelink` module without changing how it matches (exact href unless `data-cui-match-prefix` is present too). `headless.Sidebar` marks every leaf with it, so the `aria-current="page"` the server settled for first paint is the module's to clear after a client navigation. The module loads idle, so a navigation can land before it ever stamped `.active` on the old link; without the handover the stale first-paint mark survived beside the fresh one, two lit entries. Links with neither handover attribute nor the module's `.active` class keep owning their attributes. |
| `data-cui-activelink-skip` | On a `<nav> <a>` link: opts OUT of active-route highlighting entirely. The `activelink` runtime module neither sets nor clears `aria-current` or `.active` on it, at load or after SPA navigation. The escape hatch for a link whose current-state is owned by something else: a hand-set attribute (`aria-current="location"` on an in-page anchor), app JS, a signal binding. Same hands-off treatment as href-less links. |
| `data-cui-popover-anchor` | On a `data-cui-open` trigger button: opt the opened widget into trigger-anchored positioning. The value is the preferred side: `"top"`, `"bottom"`, `"left"`, `"right"`, or empty / `"auto"` (= bottom-first, then top, right, left). The runtime measures both rects after open and applies inline `position: fixed; top; left` so the popover sits next to the trigger; if the preferred side would overflow the viewport (8px margin), it auto-flips to the opposite. Re-runs on `window.resize` AND `window.scroll` (capture, rAF-throttled) so the popover tracks the trigger when the page scrolls. Distinct from `preset.Modal`'s deep-link affordances; popovers are click-driven and don't deep-link. |
| `data-hui-system-dismiss` | On the × button inside a `framework/ui.Banner` (the `headless.SystemBanner` contract): the headless module's delegated click sets `hidden` on the nearest `[data-hui-system]` ancestor, so dismissal survives partial-island swaps. The offline banner carries none — its ending is the reconnect. |
| `data-hui-rail` | Marks a scroll-spy rail nav (`framework/ui.AnchoredRail` renders `headless.Rail`'s anatomy; bound by the registered `headless-rail` module, which declares `Requires("headless-toc")` and arms its navs through the shared observer). The module IntersectionObserves the anchored targets inside the configured region and tags the link whose target is in the active band `aria-current="true"` + `.is-active`. The `activelink` module leaves these links alone (the rail owns their current-state; its links carry `.is-active`, never activelink's `.active`). |
| `data-hui-rail-observe="<selector>"` / `data-hui-rail-target="<selector>"` | On the rail nav: the selector for the observed content region (whose scroll position picks the active link) and, optionally, ADDITIONAL target elements when sections aren't headings (default `h2[id], h3[id]`, e.g. `section[id]`). Only anchors whose `href="#id"` resolves to an element inside the observed region participate. |
| `data-hui-action` / `data-hui-action-endpoint` / `data-hui-action-method` | On an action button (the headless action contract, bound through the kernel's `action` primitive): the click flips the button between idle and done, dispatches a fetch to endpoint+method (default POST), rolls back on non-2xx, and mirrors `data-state` (`idle`/`pending`/`committed`/`error`) plus `aria-pressed` for toggles. Used by `framework/ui.OptimisticAction` for "Save / Saved!" patterns without per-button JS. |
| `data-hui-action-untoggle` / `data-hui-action-group` | The ToggleAction halves of the same contract (`framework/ui.ToggleAction`, `framework/ui/toggleaction.go`): `untoggle` is present only when the button may revert — its value is the endpoint a second click hits (same method), empty for a local flip with no request of its own; `group` joins buttons into a client-side mutex — committing any button with the same key optimistically reverts the previously-committed sibling (no extra RPC; the server stays the source of truth and a later navigation refreshes from server state), with a three-state re-entry guard so rapid clicks can't race. |
| `data-hui-action-idle` / `data-hui-action-done` | Markers on the two label spans inside an action button. The runtime shows/hides them as the button transitions between idle and committed states. SSR ships the initial visible state. |
| `data-hui-action-failed` / `data-hui-action-status` | The rollback's voice: the failure sentence travels on the root (`data-hui-action-failed`, from the component's Strings) and the polite `role="status"` span carrying `data-hui-action-status` inside the button is where the module announces it — clear then frame, so a repeated sentence is heard again. |
| `data-hui-system` / `data-hui-system-offline` | On a NetworkRetryBanner — the offline SystemBanner: the banner ships hidden and the headless module shows it when the framework reports the connection lost with a retry scheduled (reading `window.__gofastr.sseStatus`; the failure-count and SSE-silence triggers retired with the old module) and hides it on reconnect. A banner arriving during an outage reads the mirrored state on arm instead of waiting for the next event. |
| `data-hui-network-retry` | On the banner's retry anchor (a real link to the health endpoint — no script reloads through it): with script, `headless-feedback` fetches the endpoint in place; a 2xx reports recovery and hides every mounted offline banner, a failed probe leaves it shown. `window.__gofastr.networkStatus.reportFailure()/reportRecovery()` drive the same banners from app-level connection signals. |
| `data-hui-system-id="<id>"` | The message's identity on a SystemBanner (from `BannerConfig.DismissID`): a dismissal is remembered for the session under it — `sessionStorage` (`hui.system.dismissed`) plus the cookie `gofastr.banner-dismiss.<encodeURIComponent(id)>` the module writes, component-encoded at the storage boundary so a DOM-sourced id can never contribute cookie delimiters. `framework/ui.Banner` reads the same cookie to skip rendering a dismissed banner server-side; for the usual letters-and-digits ids the cookie name is the id verbatim. Use for "deprecation notice — got it" banners. The dismissed set never applies to the offline banner. |
| `data-hui-slider-output` | On the `<output>` of a `framework/ui.Slider` with `ShowValue`: the `headless-controls` module writes the input's live value into it as the thumb moves (a real form output, `for`-associated, whose SSR text is the true value). Auto-emitted when `SliderConfig.ShowValue` is true; a slider without an output renders no hook at all. |
| `data-hui-number-input-decrement` / `data-hui-number-input-increment` | On the −/+ buttons of a `framework/ui.NumberInput`: clicking steps the linked `<input type="number">` inside the bounds its own `min`/`max`/`step` declare — the declared bounds are the server's, never repaired client-side — then dispatches an `input` + `change` event after writing the new value so form-RPC pipelines see the change. |
| `data-hui-number-input-for="<input-id>"` | On a `data-hui-number-input-decrement`/`-increment` button: the id of the `<input type="number">` it controls (the module resolves it with `getElementById`, so an island swap that replaces the row rebinds on arrival). |
| `data-hui-multiselect` / `data-hui-multiselect-chips` / `data-hui-multiselect-placeholder="<text>"` / `data-hui-multiselect-remove-label="<fmt>"` / `data-hui-multiselect-remove="<input-id>"` | On a `framework/ui.MultiSelect` (headless.MultiSelect anatomy; the disclosure itself is `data-hui-disclosure`'s): the registered `headless-multiselect` module (framework/headless, `Requires("headless-disclosure")`) rebuilds the chips strip from the checkboxes' own state after every change, names each chip's × from the remove-label format ({label} substituted), and closes the disclosure on click-outside. The placeholder is the sheet's `:empty::before` content. The submit contract is the plain form: every checkbox shares the field name, no script needed. |
| `data-cui-dropdown` | On a dropdown trigger button. The `dropdown` runtime module toggles `aria-expanded` and shows/hides the paired panel on click, closes on outside-click / Escape / SPA navigation, and is the singleton-by-default (opening one closes the others). |
| `data-cui-dropdown-wrap` | On the wrapper around a `data-cui-dropdown` trigger + `data-cui-dropdown-panel`. Scopes open/close to one dropdown instance; the runtime sets/clears `data-cui-dropdown-open` on it to track state. |
| `data-cui-dropdown-panel` | On the floating panel sibling of a `data-cui-dropdown` trigger. The runtime toggles its `hidden` attribute as the dropdown opens/closes. |
| `data-cui-dropdown-open` | Runtime-written marker on a `data-cui-dropdown-wrap` while its dropdown is open. CSS keys the open state off it; the runtime uses it to find and close open dropdowns. |
| `data-cui-animate-signal="<name>"` | On an element wired by the `animate` runtime module: names the signal to watch. When the signal becomes truthy the runtime adds `data-cui-animate-class`; falsy removes it. Initial state is applied on wire. |
| `data-cui-animate-class="<class>"` | The CSS class the `animate` module toggles on the element as its `data-cui-animate-signal` value flips between truthy and falsy. |
| `data-cui-reveal="<type>"` | Marks an element for the `reveal` runtime module's scroll-into-view animation. The element gets `cui-hidden` immediately; when it enters the viewport the runtime swaps in `cui-revealed` + `cui-reveal-<type>` (e.g. `data-cui-reveal="fade-up"` → `cui-reveal-fade-up`). One-shot. |
| `data-fui-dropzone-preview` | On a `<input type="file">` inside a `framework/ui.FileDropzone`: opt the input into image-preview rendering. After each `change`, the `filedropzone` module (framework/ui's own) FileReader-reads each selected image and renders `<img>` tags into the sibling `[data-fui-dropzone-preview-for="<input-id>"]` container; the drop itself, the chosen-names list and the pick announcement are the headless module's `data-hui-drop` hooks. |
| `data-fui-dropzone-preview-for="<input-id>"` | On the previews strip element: links it to the input it should display previews for. |
| `data-hui-range-slider-low` / `data-hui-range-slider-high` | On the two `<input type="range">` thumbs of a `framework/ui.RangeSlider` pair: the `headless-controls` module cross-clamps a drag that would cross the pair (the moved thumb yields) on every input event. The pair submits as `Name-min` / `Name-max`, each thumb named from the group label. |
| `data-hui-range-slider-output` | On the `<output>` of a RangeSlider with `ShowValue=true`: the sentence SHAPE travels in the attribute (`%s` for `%s`), and the module re-formats the live `low – high` through it as the user drags — so a translated page keeps its own words live. The SSR text is the true pair. |
| `data-hui-tag-input="<form-field-name>"` | On the root of a `framework/ui.TagInput`: carries the form-field name the module gives each chip's hidden `<input>` (the standard repeated-key pattern — what a reader sees removed is what a submit stops carrying). Enter or comma commits the draft, blur commits a half-typed tag, Backspace-on-empty removes the last chip, and `data-hui-tag-input-maxlength` caps a committed tag's length. |
| `data-hui-tag-input-field` / `data-hui-tag-input-list` / `data-hui-tag-input-add` | The text `<input>` (where the module listens and where it returns focus after a removal, so keyboard / screen-reader users stay in the field instead of dropping to `<body>`), the chip list it appends into, and the Add button that commits the draft. |
| `data-hui-tag-input-remove` / `data-hui-tag-input-remove-label` / `data-hui-tag-input-added` / `data-hui-tag-input-removed` / `data-hui-tag-input-status` | The removal and announcement contract: each chip's × carries `data-hui-tag-input-remove` with its accessible name from `-remove-label` (`%s` the tag); the module says "<tag> added"/"<tag> removed" through the sentences on the visually-hidden live region carrying `data-hui-tag-input-status`. |
| `data-hui-counter-animate` | On a `framework/ui.AnimatedCounter` (the signal-bound counter's root): the `headless-controls` module ticks the signal-written value from `data-hui-counter-from` to the SSR text over `data-hui-counter-ms` on arrival. The final value is always the SSR text, so a reader without script sees the true count; `prefers-reduced-motion` leaves the number where the server put it. |
| `data-hui-counter-from="<n>"` / `data-hui-counter-ms="<n>"` | AnimatedCounter starting value and animation duration (ms; the module's default when unset). |
| `data-hui-theme-toggle` / `data-hui-theme-option` / `data-hui-theme-cycle` | On a `framework/ui.ThemeToggle`: the pill group's root carries `data-hui-theme-toggle` with each option button `data-hui-theme-option="light\|auto\|dark"` (a real radiogroup); the icon and label variants carry `data-hui-theme-cycle` on the button. The `headless-navigation` module applies the scheme — persisting under the same storage key the bootstrap reads, so a reload never flashes — checks the current option's `aria-checked`, and cycles light/dark/auto on click. |
| `data-hui-back-to-top` | On a `framework/ui.BackToTop` anchor: marks the real same-origin link (its href is the no-script jump). The `headless-navigation` module adds the threshold, the in-page scroll and the focus return. |
| `data-hui-back-to-top-threshold="<px>"` / `data-hui-back-to-top-target="<id>"` / `data-hui-back-to-top-smooth` | BackToTop tuning: the scroll threshold before the control becomes visible, the target ELEMENT ID to scroll to (never a selector — the module resolves it with `getElementById`; empty scrolls the document root), and the smooth-scroll opt-in (ignored under reduced motion). |
| `data-hui-back-to-top-visible` | Runtime-written BackToTop visibility marker (`headless-navigation` owns it; no component renders it). CSS keys off it to reveal the control once the threshold is crossed — one document-top sentinel and an IntersectionObserver, no scroll listener. |
| `data-hui-toc` | On a `framework/ui.TableOfContents` nav (the headless primitive's anatomy): the table itself ships SERVER-RENDERED — the entries are real links in the first response, not harvested after paint — and the registered `headless-toc` module tracks the active entry (IntersectionObserver over the rail) and tags its link `aria-current` / `.is-active`. |
| `data-hui-toc-target="<selector>"` | On the same nav: a CSS selector for ADDITIONAL target elements when sections aren't headings (default `h2[id], h3[id]`, e.g. `section[id]`). Only anchors whose `href="#id"` resolves to an element inside the observed region participate; the active link is still whichever anchored target is in view. |
| `data-hui-sortable` / `data-hui-sortable-rpc="<path>"` / `data-hui-sortable-item` / `data-hui-sort-key="<key>"` / `data-hui-sortable-group="<id>"` / `data-hui-sortable-container="<id>"` / `data-hui-sortable-version="<token>"` / `data-hui-sortable-conflict="<rpc>"` / `data-hui-sortable-s-*` | On a `framework/ui.SortableList` (headless.SortableList anatomy): the registered `headless-sortablelist` module (framework/headless) owns HTML5 drag reorder plus the keyboard model (Space grabs, Arrow Up/Down moves within a column, Arrow Left/Right crosses to an adjacent column of the same group, Space drops, Esc cancels), the polite per-move announcements (the `data-hui-sortable-s-*` attributes carry the Strings, `{label}`/`{list}`/`{position}` substituted at say-time), and the server-authoritative commit: same-container reorders POST `order=<keys>` plus `container=` when configured and `version=` when versioned; cross-container drops add `moved=<key>` and always carry `container=`; non-2xx reverts the DOM; a versioned 409 fires the conflict path (GET the conflict endpoint, replace the list's rows — an empty body reconciles the column to zero items), after reading the 409 body under hard bounds (JSON content-type, ~4 KB, `{"error":{"message":<string>}}`, capped ~300 chars). |
| `data-hui-shortcut-target="<selector>"` | Optional companion to a page-level `data-hui-shortcut-focus` on a non-focusable wrapper: when the chord fires, `headless-navigation` focuses the element matched by this selector instead of the wrapper itself. Used by `framework/ui.GlobalSearch` where the chord lives on the wrapper but the focus target is the inner `<input>`. |
| `data-fui-lightbox="<name>"` | On the slot wrapper of a `framework/ui.Lightbox`: identifies the open viewer for the lightbox module — a registered behaviour `framework/ui` ships, so the kernel's own tables name no lightbox; the module's cold-load interactions arrive through the behaviours block. Pair with optional `data-fui-lightbox-nav="true"` to enable Prev/Next + ArrowLeft/Right keyboard nav across siblings sharing `data-fui-lightbox-group`. |
| `data-fui-lightbox-nav="true"` | On the lightbox slot wrapper: opts into the lightbox module's arrow-key + Prev/Next button navigation. |
| `data-fui-lightbox-group="<id>"` | On a trigger anchor that opens a Lightbox: identifies the gallery group whose siblings the lightbox module walks during Prev/Next nav. |
| `data-fui-lightbox-prev` / `data-fui-lightbox-next` | On Prev/Next buttons inside the open Lightbox: clicking steps to the previous/next image in the gallery group. The module's registration declares these as its retained click selectors. |
| `data-fui-lightbox-image` | On the `<img>` inside a Lightbox viewer: the image the module's pinch-to-zoom owns, named by attribute rather than class (the module contract binds attributes only; an unwired `headless.LightboxViewer` publishes the same fact as `data-hui-lightbox-image` — one vocabulary per render, the two spellings never ride the same element). |
| `data-hui-carousel` | Marks a `framework/ui.Carousel` root (the headless primitive's anatomy). The registered `headless-carousel` module wires Prev/Next clicks, dot clicks, ArrowLeft/Right keyboard nav, and the live slide status. The deferred-slide virtual scroll went with the retired core-ui/runtime module. |
| `data-hui-carousel-track` | The inner scrolling `<ul>` of a carousel. The module reads its `scrollLeft` + slide offsets to compute the current index. |
| `data-hui-carousel-status` / `data-hui-carousel-status-fmt` | The live slide sentence ("Slide 2 of 5") and its `"{word}\|{n}"` format, resolved through the strings bridge; the module rewrites the status on every slide change and announces it politely. |
| `data-hui-carousel-prev` / `data-hui-carousel-next` | On Prev/Next controls: stepping by one slide. Auto-disabled at the ends when Loop is off. |
| `data-hui-carousel-goto="<i>"` | On a pagination dot: clicking (or focusing, for the anchor dialect) scrolls to slide `<i>`; the active dot carries `aria-current`. |
| `data-hui-carousel-rotate-ms="<ms>"` | Opt-in auto-advance interval on the root. The module pauses on hover, focus-within, prefers-reduced-motion, and Page Visibility hidden. |
| `data-hui-carousel-loop` | On the root: wrap-around — Next on the last slide goes to first, Prev on the first goes to last. |
| `data-cui-popover-side` | Written by the runtime onto the anchored popover's widget root after placement: value is the final chosen side (`"top"`, `"bottom"`, `"left"`, `"right"`, post auto-flip). CSS uses it to position the directional arrow (`::before`) and to apply the anchored chrome (border, shadow, max-inline/block-size). Cleared on dismiss. |
| `data-cui-popover-trigger` | Written by the runtime onto the originating trigger button while its anchored popover is open. The runtime also adds the `.is-popover-trigger-active` class so the trigger can be highlighted while its popover is the currently-active surface. Both are stripped on dismiss or when the popover re-anchors to a different trigger. |
| `data-cui-poll="<duration>"` | Marks a region for passive freshness. The runtime re-fetches `data-cui-poll-src` on the given Go-duration interval (`30s`, `5m`, `1h`; 5s floor) and replaces the region's `innerHTML` with the response body, the same swap path an RPC signal uses. Jittered so a fleet of tabs does not synchronize; pauses while the tab is hidden; doubles the interval on a failed fetch (HTTP errors included; capped at 5x the base, reset on the next success). Any replica can answer the fetch; no fanout, no held connection. Each successful applied tick, page-level and widget-level (`Builder.Poll`) alike, increments `window.__gofastr.pollStatus` (`{ ticks, lastTickAt }`), the poll analog of `sseStatus`, for health UI and tests. |
| `data-cui-poll-src="<url>"` | The URL the runtime fetches on each `data-cui-poll` tick. The response body replaces the host region's `innerHTML`. Pair with `data-cui-poll`. Use a read endpoint: the poller fires on a timer, so a write endpoint would write on every tab on every interval. |
| `data-cui-prefetch="<module>"` | On any element: opt the page into hover/focus-prefetch of a split runtime module (e.g. `data-cui-prefetch="popover"`). On the first `pointerover` or `focusin` (capture phase, once per element) the runtime fires `__gofastr.loadModule(<module>)` so the module is ready by the time the user clicks. Multiple modules can be listed space-separated. Used to keep typical pages on `core.js` only while still feeling instant on interaction. Module names are shape-checked (`^[\w-]+$`) before the URL is built: the value is DOM input, and a `../../../evil` token would otherwise normalize out of the runtime serve route onto an arbitrary same-origin script. See `runtime-minification.md` for the size story. |
| `data-cui-nav="off"` | On an anchor: decline SPA navigation for this link. The runtime lets the click through to the browser, so the destination gets a full document load. For hosts whose destination page binds behavior at script load — a soft swap never re-runs those initializers, so every handler on the destination dies. The screen-level equivalent is `Screen.NoSPA`, which excludes a route from the client manifest so *every* link to it loads fully. |
| `data-cui-doc` | On a `<script src>` emitted by `uihost.RegisterDocumentScript(src, scope)`: the script has a DOCUMENT lifetime, shipping only on routes the scope predicate accepts (the predicate sees the registered route pattern, `/session/:id`, at render time and in the manifest alike). The runtime reads the live document's `data-cui-doc` srcs and compares them against the destination route's manifest `docScripts` at every soft-nav entry point (click hijack before `preventDefault`, `navigate()`, `popstate`, `loadPage`'s redirect leg). A difference — entering OR leaving the scope — performs a real document load instead of a partial swap; equal sets stay partial, and Back/Forward across an edge loads the destination fresh. The reason is a browser fact: such a script installs capabilities INTO the document (WebMCP's `navigator.modelContext` tools are the driving case), and DOM removal is not capability revocation, nor does a partial swap ever run a body script. |
| `data-cui-screen-group="<prefix>"` | On the `.cui-screen-group` wrapper div around each group layer. Since the layout-chain rewrite the swap logic no longer keys on it (chains carry group identity in `data-cui-layout-key`); the runtime still reads it in one place, locating the outermost shell element for a cross-chain shell replacement, and it remains the CSS/introspection contract for group boundaries. The prefix matches the group's URL prefix (trailing slash). |
| `data-hui-panehost` | Emitted by `framework/ui.PaneHost` on its root `<div>` (alongside `data-cui-comp="ui-pane-host"`). Marker the registered `headless-panehost` module scans for to wire open/close/swap triggers and the responsive overlay-drawer collapse. |
| `data-hui-pane="primary\|secondary\|tertiary"` | Emitted by `PaneHost` on each of its three slot children. The module addresses a pane by this value when opening/closing it; CSS keys the overlay-drawer chrome off it under the mode attribute. |
| `data-hui-pane-open` | Written by the render (from `SecondaryOpen`/`TertiaryOpen`) and maintained by the module: the space-separated list of open side panes, `secondary`, `tertiary`, or `secondary tertiary`. The sheet's grid column rules match it token-wise (`[data-hui-pane-open~="secondary"]`), so a client-side open changes the columns, and the module's topmost/Escape/bare-close read it as the open stack. |
| `data-hui-pane-open-control="secondary\|tertiary"` | On a button: click opens the named side pane (reveals the column, hands focus to the pane's first focusable, remembers the trigger for restore). Rendered by `core-ui/interactive`'s `OpenPaneOnClick`. The trigger resolves its host via `data-hui-pane-host-target`, else the nearest `[data-hui-panehost]` ancestor, else the first host on the page. |
| `data-hui-pane-close="secondary\|tertiary"` | On a button: click hides the named pane and restores focus to the element that opened it. Bare attribute (empty value, matched by presence) closes the topmost open pane. Rendered by `ClosePaneOnClick`. |
| `data-hui-pane-swap="secondary\|tertiary"` | On a button: click opens the named pane AND closes the other secondary/tertiary sibling: the "opening a link fills the third pane instead of navigating" flow that swaps which side pane is shown. Rendered by `SwapPaneOnClick`. |
| `data-hui-pane-host-target="<id>"` | On an open/close/swap trigger that lives OUTSIDE its `[data-hui-panehost]` ancestor: the `id` of the host the trigger drives. Without it the runtime resolves the host by `closest('[data-hui-panehost]')`, falling back to the first host. |
| `data-hui-pane-mode="overlay"` | Written by the `headless-panehost` module onto the host when `matchMedia('(max-width: 768px)')` matches AND a side pane is open (removed otherwise, so a closed narrow page paints no scrim). CSS flips the open pane to a fixed overlay drawer (backdrop scrim via `::before`, right edge, full height) and the module applies a Tab focus trap + refcounted scroll lock + ESC/backdrop-to-close. Cleared when the viewport widens or the pane closes. |
| `data-hui-pane-deeplink="<param>"` | Emitted by `PaneHost` when `PaneHostConfig.DeepLinkParam` is set: opt-in URL round-tripping, naming the query parameter that records pane state. Opening through a keyed trigger writes `?<param>=<pane>:<key>`, closing strips it, and `popstate` replays the state by re-clicking the matching `[data-hui-pane-key]`. Pane state stays in-page state (Hard Rule 1); the parameter only records it so refresh/share/Back reproduce what is on screen, the same contract widget deep links give modals. The SERVER renders first paint from the same parameter via `ui.PaneDeepLink`; without that a shared link paints the pane closed and opens it after hydration. Absent on every host that does not opt in, so the popstate listener is inert for them. |
| `data-hui-pane-key="<key>"` | On a pane-open/swap trigger (`interactive.PaneKey`): the identity of what the pane will show: a record id or slug. Read only on hosts carrying `data-hui-pane-deeplink`; it supplies the `<key>` half of the query value and is the selector `popstate` uses to find the trigger to replay. An unkeyed trigger still opens its pane but leaves the URL alone. The value lands in the URL verbatim, so the server must look it up, never reflect it. |
| `data-cui-intercept-overlay` | Written by the demand-loaded `intercept` module on the container it appends to `<body>` for an intercepted route. Holds the screen render the server returned as an overlay variant; the scrim and docking come from `app.InterceptOverlayCSS()`, which the host injects only when some route declares an intercept. Removed on close. |
| `data-cui-intercept-as="drawer\|sheet"` | On the same container: which presentation the SERVER chose, mirrored from the `X-Gofastr-Overlay` response header (itself derived from the registered `app.InterceptFrom` ScreenType). CSS keys the docking edge off it. The client never picks its own chrome: a forged request can change the wrapper element and nothing else, since policy, params, Load, and content are identical on the canonical and overlay paths. |
| `data-cui-intercept-close` | On a button inside intercepted overlay content: click closes the overlay. Closing routes through `history.back()`, so the button, ESC, and the backdrop all resolve to the same history move and the page underneath is never refetched. |
| `data-wizard-steps="<n>"` | On the `<form>` wrapper of a `Wizard` component. The runtime uses this to know the total number of steps for navigation. |
| `data-cui-drag-dismiss="true"` | On a widget root whose Definition has `DragDismiss=true` (e.g. `preset.BottomSheet`). Driven by the demand-loaded `runtime/src/dragdismiss.js` module (the marker itself is the load trigger: present at boot for SSR-inlined sheets; dynamically-opened chrome is caught by the MutationObserver scan). Drag starts only from the `data-cui-drag-handle` bar; the module follows pointer Y movement with `transform: translateY` and closes the widget on `pointerup` when distance > 80px or downward velocity > 0.5 px/ms. Snaps back otherwise. While dragging, `data-cui-dragging` is set on the root (used by CSS to suppress conflicting animations). |
| `data-cui-drag-handle="true"` | On the visible drag-handle bar rendered at the top of a drag-dismiss-enabled widget. Marks the affordance for cursor styling; the actual pointer logic is delegated from the widget root. |
| `data-fui-zoomed` | Written by the lightbox module onto the viewer's `[data-fui-lightbox-image]` image when the user has pinch-zoomed past 1×. CSS uses it to flip the cursor from `zoom-in` to `grab` and to enable single-pointer panning. Cleared on snap-back and on lightbox close. |
| `data-cui-trusted` | Marks a server-emitted region as trusted to host the legacy `data-kiln-tool` click/submit delegators. Without this ancestor (or `<body class="kiln-app">`), the legacy delegator refuses to dispatch, preventing stored-XSS content from forging authenticated kiln-tool POSTs. Apply only to chrome you fully control. |
| `data-hui-sidebar` | On `headless.Sidebar`'s root div (rendered by `framework/ui.Sidebar`). The `headless-sidebar` module scopes collapse state and controls to this element (styling keys off `.fui-sidebar` classes). |
| `data-hui-sidebar-variant="<variant>"` | On the same root: the layout posture (`persistent`, `collapsible`, `off-canvas`, `auto-hide`). The module reads it to skip the collapse restore for off-canvas roots, which have no inline column to restore. |
| `data-hui-sidebar-collapse="<mode>"` | On the root of a collapsible sidebar: `auto` names the module-owned mode. Never a click target — the collapse BUTTON carries `data-hui-sidebar-toggle`, and a link click inside the nav is navigation, not a collapse. |
| `data-hui-sidebar-toggle` | On the collapse button. Toggling flips `data-collapsed` on the root, `aria-expanded` and the accessible name on the button, and persists under the storage key when the root carries one. |
| `data-hui-sidebar-storage="<key>"` | On a collapsible sidebar root with `SidebarCollapseAuto` (the zero value). Names the local-storage entry used to restore the collapsed state across navigation and reloads; the state is stored namespaced as `gofastr.sidebar-collapse.` + `encodeURIComponent(<key>)`; a value stored under the pre-namespace raw `<key>` is not read. A server-owned collapse state (`SidebarCollapseCollapsed`/`Expanded`, rendered as `data-collapsed` on the root) omits this attribute, which suppresses BOTH the hydration-time localStorage read and the post-toggle write: a per-user state restored from the database survives first paint on a device whose localStorage says otherwise, and no stale local value is written back. |
| `data-hui-sidebar-collapse-label="<text>"` / `data-hui-sidebar-expand-label="<text>"` | On the collapse button, always (the component resolved the words — `SidebarConfig.CollapseLabel`/`ExpandLabel`, else the i18nui defaults). The module uses the matching attribute when flipping the button's `aria-label` on a client-side toggle, so a host's custom wording survives interaction. |
| `data-hui-sidebar-group-toggle` | On a button-dialect group header (`SidebarConfig.GroupMarkup: SidebarGroupButton`). The sidebar module toggles `aria-expanded` on the button plus the `hidden` attribute on the element named by `aria-controls`. The default `<details>` dialect instead carries the disclosure hooks (`data-hui-disclosure` + `data-hui-disclosure-persist` with a per-group key). |
| `data-fui-z-tier="<tier>"` | Emitted by `framework/ui.Sticky` with the layering tier from `StickyConfig.ZIndexTier` (`sticky` default, or `dropdown`/`modal`/`popover`/`toast` matching the theme's `ZIndexSet` tokens). CSS-only consumer: the `ui-sticky` stylesheet keys `z-index: var(--z-<tier>)` off this attribute so a sticky toolbar can layer above/below other surfaces without bespoke CSS. |
| `data-cui-window-drag` | On any element inside a desktop-host window: marks the drag surface of a borderless (ChromeNone) window, the widget drag-handle pattern. The demand-loaded `desktop` module's delegated `mousedown` listener matches the click target (or an ancestor) against this attribute and calls `__gofastr.desktop.window.startDrag()`, which posts `{"type":"drag"}` through the WebView's `window.webkit.messageHandlers.gofastr` message channel rather than the HTTP bridge, because the native `performWindowDragWithEvent:` needs the still-current mouse-down event. In a plain browser (no message handler) the call is a no-op, so screens carrying the attribute render unchanged in `--serve` mode. |


For the authoritative list, grep `data-cui-` in
`core-ui/runtime/runtime.js`. Adding a new attribute requires updating
`core-ui/ARCHITECTURE.md`, this extract, and a runtime test.

**Component-action attributes** (the compiled `data-action` family, distinct
from the `data-cui-*` runtime primitives above): `data-action="<name>"` on an
element inside a `[data-component]` binds the named compiled action to that
element's click (and `data-action-<event>` / `data-action-type` to
input/change/submit). `data-action-mount="<name>"` fires the named action
*once on hydration* (and again after each SPA nav): the hook a server-rendered
island uses to populate itself on load, since the other triggers are
user-event-driven. Any `data-param-*` on the element flows into the handler's
`params`. Covered by `core-ui/runtime/action_mount_e2e_test.go`.

**Response headers the runtime understands:**

| Header | Effect |
|---|---|
| `X-Gofastr-Push-State: <path>` | Apply via `history.pushState` after the RPC succeeds (URL update without re-fetch) |
| `X-Gofastr-Partial: true` | Body is a screen-partial (used by the cross-page nav path) |
| `X-Gofastr-Swap: <layer key>` | Names the layout layer the partial body renders BELOW (see `data-cui-layout-key`). The runtime swaps the matching `data-cui-layout-slot` cell and records the key on the cache entry so a replay swaps the same cell. Emitted when the navigation request carried `X-Gofastr-From` and the two routes share an addressable chain prefix; a key the DOM doesn't have (deploy skew) makes the runtime recover with a full-page load. Absent → the body is bare screen content for the whole `<main>`. |
| `X-Gofastr-Envelope: 2` | Set when the partial body is a fills envelope: the seed island, then the primary `<template data-cui-fill>` (addressed by the bare swap key, no hash), then one hashed template per kept-layer fill. Emitted only when the request carried `X-Gofastr-Fills: 2` and the render produced fills; otherwise today's body. |
| `X-Gofastr-Title: <text>` | Percent-encoded title: `decodeURIComponent` it, then set `document.title` after the partial swap. (It's encoded because HTTP header values are Latin-1; a raw UTF-8 title like `Docs — GoFastr` would otherwise arrive mojibaked as `Docs â GoFastr`. The server strips invisible/bidi codepoints — `core/textsafe` — from the screen title before this header and the full page's `<title>` element, so a title computed from loaded data cannot reorder or salt the tab readout.) |
| `X-Gofastr-Invalidate: <JSON string array>` | Evict entries from the SPA screen cache on a 2xx response (read on every mutation or navigation dispatch: RPC, widget RPC, nav partials, full-shell fetches, intercepted nav, toggle/optimistic actions, sortable reorders; never on poll replies). `"/orders"` drops that pathname **and** every cached query variant (`/orders?page=2`, …); `"/orders?page=2"` drops exactly that entry; `"*"` clears the cache. No prefix matching: `"/orders"` never touches `/orders/42`. Applied before `X-Gofastr-Location`, so a mutated-and-redirected response evicts first and the redirect target is fetched fresh. Set from Go with `ui.InvalidateScreens(w, paths...)` (accumulates like `AddToast`). |

**Screen cache + invalidation.** The router keeps a 20-entry LRU of
rendered screens keyed by `pathname+search` (the initial page included)
so back/forward is instant. Eviction never re-renders the visible page;
an RPC that changed what the *current* screen shows should return island
HTML or use `data-cui-rpc-navigate`; the header exists for screens you
are **not** on (an admin action that stales `/pricing`, a create that
stales every page of a list). The JS mirrors are
`__gofastr.invalidate(...selectors)` (same selector rules as the header)
and `__gofastr.refresh()` (re-fetch and re-render the current screen,
bypassing the cache). Scope is per tab: the header only reaches the tab
whose request carried it, and an evicted entry costs nothing until that
tab actually navigates; surfaces that must stay fresh across tabs
belong on the polling rung (`data-cui-poll`), not on cache eviction.
Iframe embeds have no screen cache, so the header is a no-op there.

**Cancelling a navigation (`gofastr:beforenavigate`).** The router
dispatches this cancelable event on the anchor element (`bubbles`,
`cancelable`, so a `document`-level listener sees it and `target` is the
link) immediately before committing an intercepted click — after every
fall-through check, so it fires only for clicks the router would
actually handle. `detail` carries `href` (the raw attribute value),
`path` (the resolved path+search the router would navigate to), `hash`
(the `#fragment`, `''` when there is none), and `anchor` (the element).
A listener calling `preventDefault()` on it claims the click: the
router still suppresses the browser default (a cancelled click never
becomes a hard page load) and then does nothing — no `pushState`, no
partial fetch, no intercept overlay. This is the supported way for
userland to take over a link (e.g. smooth-scroll to an in-page section)
instead of racing the router with a capture-phase click listener. The
event does NOT fire for clicks the router ignores: modifier-key clicks,
external / `mailto:` / `tel:` links, `<a download>`, non-`_self`
targets, unknown routes, `data-cui-rpc` anchors, and links to the
current path — including a link whose only difference from the current
URL is the `#fragment`; that click falls through to native hash
behavior.

**Active-link highlighting.** After every navigation (and at module
load) the idle-loaded `activelink` module walks every `nav a` with an
`href` and tags the one matching the current path with
`aria-current="page"` and the `active` class — the value is `page`, the
ARIA-correct value for a page link, NOT `true`, so
`[aria-current="true"]` selectors do not match it. Clearing is
ownership-based: a link loses both when it carries the module's own
`.active` class, the `data-cui-activelink` handover or the
`data-cui-match-prefix` handover — those attributes are the contract
by which a server-rendered first-paint mark (`ui.Sidebar`'s
`CurrentPath`, an exact-match `aria-current`) becomes activelink's to
move, so a kept sidebar never shows two lit entries after a client
navigation. `headless.Sidebar` marks every leaf `data-cui-activelink`
because the module loads idle: a navigation can land before it ever
stamped the old link, and only the handover tells it the stale mark is
its own. A link with none of the three (pagination's
`aria-current="page"`, server breadcrumbs, hand-set state) keeps
whatever it carries: the sweep sets and clears only what it owns.
Marking a link current also opens its closest
`details[data-hui-sidebar-group]` ancestor, so a collapsed sidebar
group opens for its current child with no request. Left completely
untouched (neither set nor cleared): href-less links (server-managed),
links inside a rail nav (`data-hui-rail`, whose links carry `.is-active`,
never `activelink`'s `.active`, so the sweep's strip branch never
touches them), and links carrying `data-cui-activelink-skip` (the
opt-out for a current-state owned by app code or a hand-set attribute).
`data-cui-match-prefix` opts a link into segment-prefix matching: the
attribute's VALUE, when non-empty, names the prefix (a link can own a
section it does not live at — `framework/ui.Sidebar` emits its items'
`MatchPath` there, so a section stays lit across client navigations
into it); an empty value falls back to the href. `/docs` lights up on
`/docs` and `/docs/getting-started`, never on `/docs-old`.

**Deferred parts announce themselves (`gofastr:fill`).** When a
route's outlet defers (`app.OutletOptions.Deferred`, the manifest's
`deferred` list), its fill travels as its own part request
(`X-Gofastr-Part`) beside the page fetch, and the runtime dispatches
`gofastr:fill` on `window` once per applied part, right after its DOM
lands: `detail` carries `addr` (the fill's `data-cui-outlet` address),
`path` (the destination path), and `t` (`performance.now()` at apply
time). A part arrives well after `gofastr:navigate` (which fires with
the page commit), so this event is the supported hook for "the
activity panel landed 700 ms later" UI — no MutationObserver
heuristics. (Streaming was the old transport for this; it is gone —
Chrome keeps no body in DevTools for a fetch read through a stream
reader, and a navigation's requests must be inspectable.)

**The flow for an in-page update** (e.g. clicking "page 2" on a pagination island):

```
[click]
  → button has data-cui-rpc="/island/customers/page" data-cui-rpc-method="POST" data-cui-rpc-signal="customers-rows"
  → runtime POSTs {"page": 2}
  → server handler computes new rows, renders HTML, returns it
  → runtime treats the response body as the new value of signal "customers-rows"
  → every node with data-cui-signal="customers-rows" data-cui-signal-mode="html" gets innerHTML replaced
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
| `data-cui-spa` (no/urlencoded enctype) | `application/x-www-form-urlencoded`               | `r.ParseForm()` + `r.PostFormValue`  |
| `data-cui-rpc="/some/endpoint"`        | Per the RPC contract (see Widgets section)        | RPC handler                          |

Every other form, with no special attribute and the default or
`multipart/form-data` enctype, is **NOT intercepted**. The browser
submits it the standard way: POST body matches enctype, Set-Cookie
headers apply, 303→Location is followed, file inputs upload natively.

### Server redirects (after intercept)

When the interceptor IS active and the handler responds with a `30x`
+ `Location` header, the runtime navigates to the location via the
SPA navigator (no hard refresh between same-app pages), falling back
to `window.location.assign`. For the non-intercepted default path,
redirect-following is the browser's own job; same result.

<!-- gofastr:compile
import "net/http"
var w http.ResponseWriter
var r *http.Request
-->
```go
http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
```

### Cookie-set + redirect: the canonical auth flow

```go
// /auth/login form handler; battery/auth ships this out of the box.
http.SetCookie(w, sessionCookie)
http.Redirect(w, r, successRedirect(r, "/"), http.StatusSeeOther)
```

The runtime preserves the cookie and follows the redirect, so a form
POST to `/auth/login` from an SSR login page lands the user on the
next screen without a hard refresh, and without the host needing
client-side glue.

## Heavy-JS plugin markers

Pages that mount a sandboxed heavy-JS plugin (see
[plugin-platform](plugin-platform.md)) carry a mount marker emitted by
`framework/pluginhost.MountMarker`: `data-fui-plugin="<name>"` with
`data-fui-plugin-docid`, `data-fui-plugin-doc` (server-rendered initial
JSON), `data-fui-plugin-minheight`, and `data-fui-plugin-capabilities`
(the grant set, `resource:verb` grammar). An optional
`data-fui-plugin-fallback` wrapper (from `MountConfig.Fallback`) holds a
server-rendered node the broker shows while the frame loads and swaps
back to on `bootError`, so a dead frame degrades to static output. These
are scanned by the plugin host broker: a separate script served at its
own route, NOT part of `runtime.js` or its budgets. Plugins may add
namespaced extras (the wysiwyg editor adds `data-fui-plugin-for` naming
its hidden form fields); those are documented by the owning plugin.

---

## Registered behaviours

A package can register its own runtime module beside its stylesheet:
`registry.RegisterBehavior(name, js, registry.Markers("[data-x]"))`.
The host serves it at `/__gofastr/runtime/<name>.js?v=<hash>` and lists
it in the module manifest; its markers reach the kernel as
`window.__gofastr_behaviors` (from `/__gofastr/manifest.js`) on live
pages and as the inline `<script type="application/json"
id="gofastr-behaviors">` block in static exports and the embed frame.
The kernel scans those markers after its own table, so the module loads
once when a marker appears, at boot, on DOM insertion, or after a
client navigation, and `data-cui-prefetch="<name>"` warms it on hover.
A malformed block (a `window.__gofastr_behaviors` that is not a
descriptor map, an inline `#gofastr-behaviors` that is not JSON) is
ignored: the kernel registers no behaviours and boots on its own
module table. The catch is silent on purpose — a `console.warn` does
not clear the core gzip budget (measured: +17 bytes at the shortest
useful wording, against 8 bytes of clearance), and unlike a failed
fetch the console reports nothing on its own.

Registered markers use the package's own `data-` prefix; a
`data-cui-*` marker is admitted only when the attribute is in the table
above — and a `data-cui-*` attribute named by an interaction selector
(a click's `Selector`, a keydown's `Scope`) is held to the same rule by
`TestInteractionSelectorsInTheTreeUseDocumentedDataFuiAttrs`. The
module is held to the same source lints as the kernel's own
(`core-ui/check`: no `var`, no selector or storage key built from a raw
value, and the rest): the clean-tree tests find every registered
behaviour through its `//go:embed` directive. Contract and rules:
`core-ui/ARCHITECTURE.md` "Component behaviour".

A behaviour may declare dependencies:
`registry.Requires("action")` names the modules that must be loaded and
registered before it, embedded kernel modules or other registered
behaviours; the behaviours block carries them as `r`, and the loader
honours them on every load path, requirements first, then the module's
script. A name that is neither, or a cycle, panics where the block is
built — at the first render that builds the manifest, not at startup
(the registry is only complete once every package's init has run);
through the framework's recovery middleware the panic surfaces as a
500 with the cycle path in the log line. The interaction bridge is a
load path for registered behaviours too: a behaviour declares the
interactions it needs retained through its module's cold-cache fetch
with `registry.Interactions(...)` — one spec per interaction, in the
bridge's own shape (`Event`; a click's `Selector`, the node resolved
with `closest`; a keydown's `Keys` and `Scope`, a selector that must
match somewhere in the document before the key is retained) — the
behaviours block carries them as `x` beside `s`, `i` and `r`, and the
kernel installs one document-level listener per spec over the
registered descriptors exactly as over its own marker table,
preventing the default synchronously and replaying the event on the
original node once the module registers. A behaviour that declares no
interactions installs no listener and costs nothing; a malformed `x`
field is swallowed with the rest of a broken block. An interaction
selector is owned the way a marker is: one that names a `data-cui-*`
attribute names an attribute in the table above, and a selector
belongs to one behaviour — nothing refuses two descriptors that claim
the same event and selector, and each of them retains and replays, so
one user action arrives at both modules twice. The grammar is wider
than the marker grammar, and deliberately: a marker is parsed back out
of rendered HTML host-side for preload, while an interaction selector
only ever reaches the browser's own `querySelector` and `closest`, so
it may carry combinators, `:not([attr])` and comma lists. What the
browser throws on is refused at registration, because a throw inside
the bridge's document-level listener kills retention silently; class
and id selectors are refused too, not because they throw but because a
registered module binds by attribute. Readiness is
registration: the loader resolves a module's
promise only when `loadedModules[name]` is set after its script ran —
and the module sets its flag FIRST, before it installs anything (a
script that failed halfway with its flag unset has its load rejected
and fetched again, and the retry re-executes the file: a flag at the
end leaves every listener of the first pass installed twice) — and a
script that ran and never registered rejects with "module failed
to register", drops its cached promise, and a retry fetches again.
Preload and the static export list a needed behaviour's requirements
beside it. A module with no marker (a primitive) is reachable only
through `Requires` or an explicit `loadModule`; the kernel's `action`
module is one — `window.__gofastr.action.request(url, method)` performs
a same-origin mutation with the CSRF header and resolves to a boolean,
and `window.__gofastr.action.bind(el, spec)` attaches the whole
optimistic lifecycle (idle → pending → committed → error, the label
flip, `aria-busy` while pending, the `action:*` events) to
an element from a spec of `endpoint`, `method`, `idle`, `done`,
`group`, `untoggle` and `pressed`. A bound group converges on one
committed member: the revoke runs at click time and again when a
commit settles, so a failed commit restores the sibling it displaced
while a successful one displaces every other, and two members clicked
inside one round trip still end with exactly one committed. Group
members whose elements left the document are pruned on
`gofastr:navigate`.

The framework's own `headless` module is registered this way by
`framework/headless/behavior.go`, binding that package's `data-hui-*`
hooks with the markers `[data-hui-reveal]`, `[data-hui-color]`,
`[data-hui-when]`, `[data-hui-form-errors]`, `[data-hui-action]`,
`[data-hui-drop]` and `[data-hui-system]`.

## See also

- [UI capability map](ui-capability-map.md) maps product architectures to this runtime boundary and its scaling semantics.
- [UI composition recipes](ui-composition-recipes.md) turns that architecture into product-shaped page grammar.
- [Events and SSE](events.md) defines best-effort push and durable alternatives.

## Common mistakes

These are the misreadings of the model that have already cost rework
(they are cataloged as "failure modes" in `core-ui/ARCHITECTURE.md`):

- **Intercepting all link clicks for SPA.** State-change interactions
  like pagination should not be `<a href="?p=2">` links at all; they
  are island RPCs. Cross-page navigation (`/a` → `/b`) IS intercepted
  by design; don't disable that to "fix" an in-page interaction that
  should never have been a route.
- **Making every interaction a full navigation.** Clicking a sort
  header must not reload the page; that's a hard refresh, which the
  model rules out for in-page state. Use an island RPC.
- **Making every interaction client-only after hydration.** Client-
  managed pagination state only works for datasets that fit in one
  render, and it duplicates server logic. Islands are server-driven;
  let the server do the math and return HTML.
- **Shipping CSS from the app or a generator to make a surface look
  right.** All styling lives in the design system (component CSS via
  `registry.RegisterStyle`, layouts via `core-ui/app`, tokens via
  `core-ui/style`). A surface that needs CSS the components don't
  provide is a design-system gap to fix upstream; see
  [theming](theming.md).
- **Delivering a user action's response over SSE.** SSE is push-only
  (background events). The result of a user action arrives in the RPC
  response itself. See [Optimistic UI](optimistic-ui.md) for the full
  mutation lifecycle.
