# GoFastr UI architecture

The CSP source linter accepts comment directives in LF and CRLF files.
The directive must still occupy its whole comment line.
Disk-backed UI host static files use operation-scoped `os.Root` handles.
Each read verifies the original directory identity and retains kernel path
containment. An idle host does not hold directory handles open on Windows.
`UIHost.AssetURL` gives a static file a content-hash URL (`?v=<hash>`); the
host serves a request at the file's current hash with immutable caching.
RPC success toasts finish before navigation, including the feedback module's
first lazy load.

> **Read this before writing any UI, runtime, or `framework/uihost` code.**
> Misunderstanding the model has caused multiple round-trips of rework.
> This document is the contract.

---

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
`Builder.Poll`) re-fetches server-rendered HTML on an interval. No held
connection, no cross-replica infrastructure. **Server-pushed updates**
(e.g. another user changed something) flow through signals + SSE to update
bound DOM nodes without any client action, reserved for semantics that
need the connection itself (presence, collaboration, sub-second updates),
never for anything a poll or an RPC covers. The full escalation ladder
of client signals → RPC → poll → SSE push is
`framework/docs/content/reactivity.md`; the interactive layer is stateless
(sessions are signed tokens, state lives in the DB or the client signal
store), so any replica serves any request.

Recovered component and layout render panics retain their fallback behavior,
but report the component type, scrubbed panic message, and stack at error
level. This applies in development and production. In Go test binaries,
UI host page requests (full pages, navigation partials, fills envelopes,
and deferred parts) answer 500 after a recovered render panic, keeping
the existing fallback body. Rendering completes before the first response
write; the test-only writer selects the status before committing headers.
These finite responses do not stream. The separate SSE endpoint is unchanged.
`framework.TestHarness` also attaches a request-local observer that fails
its test. Generated end-to-end tests inspect captured server logs for the
same failures, including requests made by their browser. Production and
`gofastr dev` keep their existing recovery status.
Tests that deliberately exercise recovery can wrap their own handler with
`framework/testkit.AllowRenderPanics(t, handler)` to keep production status.
The exemption ends at `t.Cleanup`, affects no other handler, and is safe
for parallel tests with separate wrappers and servers. It does not silence
logs or TestHarness reporters and cannot be selected by an HTTP request.

The same test binaries (and `gofastr dev`) scan every markup response
the app router serves (pages, partials, island RPC answers, widget
chrome, and the HTML inside JSON signal values) for names the upgrade
registry retired — classes and `data-cui-*` attributes — and report
them through a separate observer; a retired name never changes a
response's status. Production never scans. See
`framework/docs/content/testkit.md` § Retired markup.

Document titles append ` — <app name>` only when the screen title does
not already end with that exact suffix. Full pages, not-found outlet
outcomes, recovery screens, and partial-navigation title headers follow
the same rule.

Custom not-found screens can implement
`uihost.NotFoundTitler.NotFoundTitle(context.Context, string)` to choose
the title for the current request path, without storing that path on the
shared screen. An empty path identifies a missing-outlet outcome. Empty
custom titles fall back to `app.ScreenTitler.ScreenTitle(ctx)`, then the
generic not-found title. Full loads and partial navigation use the same
effective title; a failed route's registered title cannot replace it.
An unmatched route keeps its request path even when the root layout has
a `FallbackNotFound` outlet. The empty-path outcome applies only when a
registered route cannot fill its required outlet.

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

## Screen render failures: 404 vs 500

The render pipeline folds every failure into one error return; the host
discriminates on it:

- **A panic in a screen's `Render` or `Load`** is a server bug. It is
  contained (`component.SafeRenderCtx` / `safeScreenLoad`), wrapped
  with `app.ErrScreenPanicked`, and answered **500**. The host logs one
  `slog` Error line naming the path and the scrubbed panic
  (`textsafe.Recovered`); a `Render` panic also gets the component
  recovery's own Error line with the stack. The panic text never reaches the response
  body. Same outcome on every serving path: full page with layout,
  layout-less page, partial navigation, overlay, and the embed content
  route.
- **A `Render` panic in a screen that implements
  `component.ErrorBoundary`** answers with the screen's own
  `RenderError` markup on every serving path, with the panic still
  logged with its stack. The screen took ownership of its failure. A
  `Load` panic is still a 500: the boundary covers rendering only.
- **A `Load` that returns an error** keeps the 404 it contracted (the
  screen chose "not found"; `uihost.ScreenStatusCode` renders a body
  with a chosen status instead, and `app.SetStatus` sets one from
  inside the render), logged at Warn with the path so it is no longer
  silent.
- **A path no route owns** stays a plain 404 and logs nothing.

The 500 is the same error page a failed route resolver gets
(`serveError`): RFC 9457 `application/problem+json` for machine
Accepts, otherwise the error body (`WithErrorScreen`, or the built-in
one) through the root layout, or a minimal document when there is no
root layout. `Vary: Accept` on every arm, and no arm echoes the path or
the panic. The embed content route answers the minimal document
directly, since a frame must not receive the site's root layout.

---

## What is an island?

An island is a **server-rendered, server-driven component** with its own
RPC endpoints and (optionally) signal bindings. It owns:

1. Its rendered HTML (SSR).
2. Its server-side state (in the DB: the interactive layer is stateless, so never in process RAM; any replica serves any request).
3. Its update logic (handlers that re-render and respond).

Pagination is an island. A sortable table is an island. A "favorite" toggle
on a card is an island. A page header that needs to react to user-scope
changes is an island. **Inline content that never changes** (a static
heading, a piece of marketing copy, a footer) is **not** an island. It's
just rendered HTML.

**Element ids in an island.** A component that derives an id from its
content (a section slugged from its heading) gets it from the render
context's id scope, which suffixes repeats on the page (`features`,
`features-2`). An island renders twice: inside the page, where earlier
sections have taken ids, and alone in its RPC handler, where nothing has.
So an island renders under its own id namespace in both places:
`island.NewIsland(id, c).RenderCtx(ctx)` renders `c` under
`render.WithIDNamespace(ctx, id)`, and a section titled "Features" in
island `cart` is `cart--features` on the page and in the RPC answer
alike, with repeats suffixed inside the namespace only. A slug never
holds `--`, so the namespace never meets a page id, and the page scope
reserves every id the island hands out. The namespace is spelled in
id-safe bytes: letters, digits and single inner hyphens stay, any other
byte becomes `_` and two hex digits (island `cart.v2` gives
`cart_2ev2--features`). The spelling holds no `--`, so the first `--`
of a namespaced id is always the boundary and two namespaces never give
one id, and no island id puts markup into an attribute. A deferred layout
outlet is the same shape (it renders inline on a first load and alone as
a part request) and gets the namespace of its address, of a kind of its
own (`render.WithIDNamespaceKind`): `l:shell#aside` reads
`_l-shell-aside--features`, and an address whose layout or outlet name
holds anything but letters and digits is escaped whole
(`l:shell#aside-a` is `_l-l_3ashell_23aside-a--features`), so outlets
`aside-a` and `aside_a` stay apart. An island's escaped `_` is always
followed by two hex digits, so no island id, `l-shell-aside` included,
reaches an outlet's namespace. An id that starts with a region's
namespace and `--` belongs to that region: an explicit
`cart--features` written outside island `cart` can repeat one of its
derived ids, because the island renders the same ids on the page and
alone and cannot move them for the page. Each island and deferred outlet
renders as its own id region inside its namespace, so an explicit id in
it wins over a derived one wherever the two sit, as on the page. A
region reads the id attributes its HTML writes (`RegionBatch.ReserveRendered`)
before it settles, so an explicit id wins whether or not its component
called `render.ReserveID`. Explicit ids are never namespaced or changed.

The framework primitives live in:

- `core-ui/widget`: the builder API (`widget.New(name).Mount(...).Slot(...).Signal(...).RPC(...).RPCWithSignal(...)`)
- `core-ui/node`: the JSON-clean UI element tree (`Node`, `Action`, tree
  helpers). A dependency-free, serializable description of a screen. Both the
  app spec codegen (`cmd/gofastr`) and Kiln's World IR (`kiln/world`, which
  type-aliases `node.Node`/`node.Action`) compose it; neither owns it. The IR
  used to live under `kiln/`, which forced first-party callers to import the
  Kiln namespace. It was moved down into `core-ui` so the dependency points the
  right way (Kiln consumes core-ui, like any other caller).
- `core-ui/noderender`: walks a node tree and emits HTML via
  `core-ui/html`. Two entry points, split by trust. `RenderNode` (and
  `RenderKind`, which renders one element from pre-rendered children)
  treats the IR as UNTRUSTED and strips `data-action`, `data-action-*`,
  and `data-param-*`, the attributes `frag/boot.js` resolves into a
  `__gofastr.trigger()` call at hydration and on every `gofastr:navigate`,
  so an untrusted IR naming them picks a compiled server action and its
  arguments. The same path strips the attribute of every registered
  behaviour marker (`registry.IsMarkerAttr`): a module binds whatever
  carries its marker, so `data-passkey-delete` on an agent-authored
  button would delete the viewer's passkey on click. `data-island` is
  refused outright: it is the SSE swap target,
  so naming it lets an element impersonate a registered island.
  `RenderTrustedNode` and `RenderTrustedKind` are the first-party pair.
  They keep the action attributes and are what the app generator
  emits. The attribute name alone cannot tell a trusted tree from an
  untrusted one, so the caller picks the entry point; picking `RenderNode`
  for first-party IR silently drops every action attribute.
- `core-ui/island`: the runtime-side island manager (registration, SSE push, slot lookup)
- `core-ui/interactive`: declarative interactivity primitives (`OnClick/OnSubmit` wrapping for in-page RPC, signal binding, widget chaining)
- `core-ui/runtime/runtime.js`: the client-side hydration runtime
  (minified at first read in production; see
  [`runtime-minification.md`](../framework/docs/content/runtime-minification.md))
- `core-ui/runtime/minify`: token-aware Go JS minifier that
  shrinks every embedded runtime source on first read

---

## Runtime primitives (the wiring)

The runtime understands a small set of `data-cui-*` attributes on the
hydrated DOM. **You don't write JavaScript**. You compose these on the
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
| `data-search*` | `framework/ui`: the hooks `CommandPalette` (with `Search`) and `SearchResults` declare | `battery/sitesearch`'s runtime modules, which fill and update that markup | `data-search`, `data-search-all`, `data-search-page`, `data-search-ignore` |
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
`data-fui-pane*` (its pane host), `data-fui-local-*` (`framework/localentity`'s `localentity` and `localentity-form` behaviours: the list, form and count carriers, their row/empty templates, the `-text`/`-delete`/`-edit` hooks inside a row, and the `-item`/`-key`/`-editing`/`-state` marks the module writes), `data-fui-z-tier` (`ui.Sticky`'s
sheet), `data-fui-network-retry-*`, `data-fui-plugin*`
(`framework/pluginhost`) and `data-fui-page-loading`
(`framework/uihost`). Every other attribute the runtime reads is
`data-cui-*`, in the table below.

| Attribute | Purpose |
|---|---|
| `data-cui-rpc="<path>"` | Click on the element (or submit of a `<form data-cui-rpc>`) fires a request to `<path>`. Body precedence: an explicit `data-cui-rpc-body` JSON wins; otherwise a `<form>` node serializes itself, and any other form control (radio/select/input/textarea) serializes its ENCLOSING form via `node.form` so the control's own `name=value` round-trips (`framework/ui.SegmentedControl` `RPCPath` relies on this, so place the control inside a `<form>`); a control with no enclosing form and no explicit body posts an empty body. GET folds the serialized form into the query string; a multipart form (or one with a file input) posts `FormData`, everything else posts JSON. A non-2xx answer to a form submission is never silent: the server's validation envelope (`{error, fields: {name: [messages]}}`) marks each named field's control (`aria-invalid`, and the error node's id added to its `aria-describedby` beside any hint already there; the retry that removes a live node drops only that id) and places a `role="alert"` paragraph by the headless hooks, never by a kit class: it fills a reserved `[data-hui-field-error]` node inside the `[data-hui-field]` group (`FormFieldConfig.ReserveError`) or appends a live one, and after a bare `[data-hui-choice]` label it inserts the paragraph as the next sibling; when no field matched, the `error` text is toasted (module or fallback). The envelope is read in either spelling: CRUD's flat `{error: "…", fields}` or `core/handler.WriteError`'s nested `{success: false, error: {code, message, fields}}`, which every typed handler (`handler.ValidationError`, `HandlerAdapter`) answers with; the toast then carries `error.message`. The form's next answer, refused or successful, first clears every error the form shows, the messages the server rendered with the page included: `aria-invalid` goes, live nodes are removed and every other error node empties back to reserved. In the JSON body a repeated name becomes an array (checkbox group, multi-select) with one exception, the HTML checkbox idiom: a hidden input followed by a checkbox of the same name (hidden `false`, checkbox `true`) serializes as one scalar, the last value, so a bool field submits `"true"` or `"false"` and never `["false","true"]`. A trigger that carries no value and no `data-cui-rpc-signal` (a button, span or link outside a form, holding no named control) ignores a click while its request is in flight, so a double click sends one request even before the rpc module has loaded; a form, a control or a signal-targeted trigger supersedes instead, the newest dispatch winning. |
| `data-cui-rpc-method="GET\|POST\|…"` | HTTP method (default POST) |
| `data-cui-rpc-signal="<name>"` | The response body is treated as a signal value and broadcast to bound nodes |
| `data-cui-rpc-close` | Containing widget closes on 2xx |
| `data-cui-rpc-reset` | Containing form resets on 2xx |
| `data-cui-rpc-open="<widget-name>"` | A registered widget opens on 2xx (e.g. "save in drawer → open results sheet") |
| `data-cui-rpc-navigate="<path>"` | Client-side SPA navigation to `<path>` on 2xx. Bypasses the screen cache and re-renders even when `<path>` is the current page: the RPC mutated server state, so the destination must be fetched fresh. When the RPC method is not `GET`, the page it ran on is evicted (its exact URL, or every query variant when it has no query, on every locale) from the screen cache first, so Back after a delete fetches the record again instead of replaying it |
| `data-cui-rpc-refresh="<widget-name>"` | On 2xx, triggers an immediate `/state` re-fetch (`pollNow`) on the NAMED polling widget instead of the one the button lives in. For a mutation whose result a *different* widget renders, e.g. a Reset button inside a confirm modal refreshing the chat panel. |
| `data-cui-signal="<name>"` | This node's content/attribute updates when the named signal changes |
| `data-cui-signal-mode="text\|number\|html\|attr"` | How to apply the signal value (default `text`). `number` is `text` to the kernel; the marker loads the `headless-numbers` module, which rewrites a numeric value the way the page's `<html lang>` writes numbers (`Intl.NumberFormat`) in a microtask after each write, matching the server's `i18n.Number` text. The core runtime carries no bytes for it. `html` is the trusted-HTML escape hatch: on a string value the runtime replaces `innerHTML`, on a non-string value (e.g. the dispatchRPC error object `{ok:false,status,text}` broadcast on non-2xx) it leaves the DOM **unchanged** so a failed RPC cannot corrupt the trusted region. `text` always renders (a human-readable "Error: …" string for error objects). `attr` updates the attribute named by `data-cui-signal-attr`. |
| `data-cui-signal-attr="<attr>"` | Attribute name when mode is `attr`. For the boolean attributes `disabled`, `hidden`, `checked`, `open` and `selected`, a false value (`false`, `null`, `""`, `0`, `"false"`) removes the attribute and any other value sets it, since a boolean attribute is on whenever it is present; `checked` and `selected` also write the live property (`disabled`, `hidden` and `open` reflect the attribute), so a checkbox the reader already clicked still follows the signal. The server render (`store.Slice.BindAttr`) follows the same rule: a false value omits the attribute and a true one writes it with an empty value, so the page never arrives with `disabled="false"`. `value` writes the live `el.value` as well as the attribute. Other attributes, `aria-*` included, get the value as text. |
| `data-cui-signal-set="<name>[:<value>]"` | Click sets the named signal to `<value>` purely client-side (no RPC). Omit `:<value>` to set the empty string. Used by `framework/ui.Tabs` buttons (`<name>:<index>`). |
| `data-cui-signal-inc="<name>[:<delta>]"` | Click increments the named signal by `<delta>` (default `1`; negative decrements) client-side. Used by `framework/ui.Counter`. |
| `data-cui-signal-toggle="<name>"` | Click flips the named boolean signal client-side. Used by `framework/ui.SignalToggle` and `interactive.ToggleLocal`. The three signal mutations work inside a widget root too (the document delegate applies them; the widget's scoped listener leaves them alone), and on a node that also carries `data-cui-rpc` the mutation wins and no request is sent, inside a widget or out. |
| `data-cui-tab-index="<n>"` | Set on `framework/ui.Tabs` buttons and panels to associate each with its zero-based index. CSS keys the active-button highlight and visible panel off the wrapper's `data-active` matching this index. When the wrapper's `data-active` attribute is updated through a signal (`data-cui-signal-mode="attr"`), the core runtime also mirrors the new index into `aria-selected` on every `[role="tab"][data-cui-tab-index]` descendant so assistive tech tracks the selection, not just the CSS highlight. |
| `data-cui-computed="<reducer>"` | Marks a `core-ui/store` computed slice. The `computed` runtime module subscribes the node to its dependency signals and, on any change, runs the host-registered JS reducer `window.__gofastr._reducers[<reducer>]` over the current dep values and broadcasts the result to this node's `data-cui-signal`. CSP-safe: the reducer is a real function the host registers (no `eval`). |
| `data-cui-computed-deps="<a,b>"` | Comma-separated dependency signal names a `data-cui-computed` node recomputes from. A node never recomputes inside its own recompute, so a dependency cycle stops after one pass; `store.Computed` refuses one at declaration. |
| `data-cui-compute` | Loads the `compute` demand module, which exposes `window.__gofastr.compute`. It is a trigger marker only; worker name, function, and payload stay in the imperative `compute.task(...)` call. |
| `data-cui-open="<widget-name>"` | Click opens a registered widget surface |
| `data-cui-ctx="<opaque>"` | On a `data-cui-open` trigger: per-trigger context for the opened widget's chrome (#321). The runtime forwards it as `?ctx=` on the chrome fetch and keys the client chrome cache by `(name, ctx)` (LRU, capped at 32 entries, cleared on SPA navigation). Opaque to the framework — never parsed. `serveChrome` is the validation boundary: it bounds a URL-carried `ctx` at 256 bytes and rejects invalid UTF-8 and control runes before rendering. The slot reads the accepted value with `widget.ChromeContext(ctx)`, which only reads, decides what it means, and MUST authorise the entity it names against the request context. `WithChromeContext` takes an unrestricted string: an in-process caller is trusted, the URL is not. Use for per-entity dialogs: `data-cui-open="layout-remove" data-cui-ctx="inv-42"`. |
| `data-cui-push-state="<path>"` | After the RPC succeeds, apply this URL via `history.pushState` (no re-fetch). Useful when the button knows the canonical URL ahead of time (e.g. pagination button "page 3" → `data-cui-push-state="?p=3"`). Server-supplied `X-Gofastr-Push-State` header takes precedence. The runtime also refuses a destination that is not same-origin: the SPA navigator applies its own origin check, and the pre-boot `location.href` fallback applies the same one inline, so a `javascript:` or cross-origin value in this attribute navigates nowhere. |
| `data-cui-confirm="<message>"` | Pre-flight confirmation gate, honored on every form submit the runtime sees — native POST, `data-cui-spa`, and `data-cui-rpc` forms alike — and on non-form `data-cui-rpc` clicks. On a form, an attribute on the submit button takes precedence over one on the form element. The ask is the `confirm` module's themed dialog: the submit is cancelled while it is open and submitted again on an accept (a module that cannot load fails closed: nothing is sent). A page with no confirm template, or a browser without `showModal`, asks with `window.confirm(<message>)`. Cancel aborts: the submit stays prevented (a native form never navigates), the RPC never fires. Use for destructive actions (delete, revoke). |
| `data-cui-confirm-title="<text>"` / `data-cui-confirm-accept="<label>"` / `data-cui-confirm-tone="danger"` | Beside `data-cui-confirm`, on the element that carries it: the dialog's heading, its accept button's label, and the danger tone, which shows the template's danger accept button in place of the primary one. Absent, the template's own defaults stand. `interactive.Action.WithConfirmDialog` writes all four. |
| `<template data-cui-confirm-dialog>` / `data-cui-confirm-part="title\|message\|accept\|accept-danger\|cancel\|label"` | The kit's confirm dialog, rendered once per page by the host from the template a kit registers (`preset.ConfirmTemplate`; `framework/ui` registers one). The `confirm` module clones the template's `<dialog>`, writes the gated element's text into the `title` and `message` parts (and `label`, the accept button's text node, when one is marked), drops the accept button the tone does not pick, loads the sheets the clone names (`data-cui-comp` and `data-cui-scope` on its root and inside it) and waits for those alone, not for unrelated prewarms (the host prefetched them at first paint, so this is normally a cache hit; each wait is bounded at 3 seconds), opens it with `showModal()`, focuses `cancel`, and resolves true on the accept button and false on cancel, Escape or a backdrop click. The clone is removed on close and focus returns to the trigger. Asked from inside a modal widget, the dialog owns the keyboard while it is open: the widget's Tab trap and Escape handler stand down for a modal `<dialog>` that is not inside the top widget, so Tab reaches the accept button and Escape closes only the dialog. One question at a time: an ask made while another waits on its sheets or stands open resolves false at once, so a double click opens one dialog and sends at most one request. The module names no class. |
| `data-cui-rpc-trigger="input"` | On a `<form data-cui-rpc=…>`, dispatch the RPC on every `input` event from any control inside, after a debounce window. |
| `data-cui-rpc-debounce-ms="<ms>"` | Debounce window for `data-cui-rpc-trigger="input"`. Default 250. Any other dispatch of the same node inside the window (a submit) cancels the pending request, so the form is sent once, and a node that left the page before its window ends sends nothing. |
| `data-cui-rpc-after-text="<text>"` | On 2xx RPC, replace the trigger's text content with `<text>`. One-shot, idempotent on re-click via `data-cui-rpc-after-done`. |
| `data-cui-rpc-after-disable` | On 2xx RPC, mark the trigger as `aria-disabled="true"` and (for `<button>`/`<input>`) set `disabled=true` permanently. Use with `after-text` for "Saved ✓" / "Revealed ✓" feedback. |
| `data-cui-rpc-scroll-to="<selector>"` | On 2xx RPC, smooth-scroll the matching element into view. Use to direct the user's eye at newly-inserted content. |
| `data-cui-rpc-error-toast="<title>"` | On a non-form RPC trigger: a non-2xx answer shows an error toast (`_toastOrFallback`, so the kernel fallback region catches it when the feedback module cannot load) titled `<title>`, with the response's JSON `error` string, clipped to 300 characters, as the body; a network failure toasts the same title with a network-error body. An empty value titles it `Request failed (<status>)`. Without it a refused button RPC stays silent; a form source ignores it, since `formerrors` already reports a refusal. Set from Go with `interactive.Action.OnErrorToast`. |
| `data-cui-rpc-success-toast="<title>"` | On a 2xx RPC answer (button or form), a success toast titled `<title>` fires through `_toastOrFallback` BEFORE the trigger's `data-cui-rpc-navigate` runs: the toast stack lives outside the swapped region, so the toast survives the SPA navigation its own RPC triggered — a save that lands the reader back on the record page still says "Saved". An empty value titles it `Done`. Set from Go with `interactive.Action.OnSuccessToast`. |
| `data-cui-rpc-success-action='{"label":"<label>","attrs":{…}}'` | Puts one button on the toast `data-cui-rpc-success-toast` shows on a 2xx answer; without that toast it shows nothing. The button is labelled `<label>` and carries `attrs`, so pressing it fires that RPC through the toast stack's own widget dispatch, and closes the toast. `headless-feedback` copies only `data-cui-rpc` and `data-cui-rpc-*` keys onto the button and leaves the button out when the label or `data-cui-rpc` is missing; JSON that does not parse shows the toast without it. `rpc.js` passes the JSON through as the toast's `action`, which `__gofastr.toast(cfg)` also takes as an object; a toast that keeps its button stays up at least 10 s, paused while hovered or focused. The kernel's fallback region draws no button. Set from Go with `interactive.Action.OnSuccessToastAction`; `headless.Button` and `headless.Form` refuse one that is not a label and an RPC, whose wiring fails a button's own checks, or that has no success toast to ride. |
| `data-cui-comp="<name>"` | Marks an instance of a registered styled component. The runtime scans for it on every DOM insertion and lazily loads `/<__gofastr/comp/<name>.css>` once per session via a `<link data-cui-style="<name>">` (dedup'd, never re-fetched). See "Component CSS" below. |
| `data-cui-scope="<name>"` | Marks the root of an owned style: a layout root (`LayoutSpec.Style`), a screen's wrapper (`Screen.WithStyle`: its `<article>`, or one plain `<div>`, never the primary cell), or a component root (`Style.Scope`). The server writes it; the runtime never does. It is two things at once: the root of the style's compiled `@scope`, whose lower bound stops at the children of any nested owner, and a loader marker, read exactly like `data-cui-comp`: the SSR head scan (`registry.Scan`) links `/__gofastr/comp/<name>.css` for every name on the page, and the runtime's `scanAndLoadCSS` loads it on insertion (a cross-layout `swapShell` scans the new shell's parent, because the shell root carries its layout's scope). An element may carry both markers (`Style.Scope(ui.Card(...))`) and loads both sheets. Owned rules are scoped, so they beat an equal-specificity kit rule by scope proximity whatever order the sheets load in. App markup cannot set it: `html.SafeExtraAttrs` drops every `data-cui-*` key. |
| `data-cui-internal` | Marks kit component markup that holds none of the caller's content: a label or title built from a string field, a control's input, a dismiss button, an icon the component draws. Every compiled owned style's `@scope` lower bound stops at it, so an owner styles the content it passes into a component and never the component's insides. Components set it through `headless.Internal`, and a component that builds markup and hands it to another as slot content marks it with `headless.Own`, which also marks the element holding that slot. Never on a component's root (an owner may place the root), never on an element that holds a slot or on an ancestor of one; a mark inside a marked subtree is allowed and inert. `TestKitMarksInternalSubtrees` (framework/ui) renders every kit component with every slot filled and every slot empty and fails on unmarked internal markup, a marked root, or caller content under a mark. The server writes it; the runtime never reads it. App markup cannot set it: `html.SafeExtraAttrs` drops every `data-cui-*` key. |
| `data-cui-bundle="<a,b,c>"` | Set on the SSR-emitted bundle, a `<link>` or, when the page inlines its first-paint head, a `<style>`, to list the components it covers. The runtime reads it from either element at boot and seeds `_pendingLinks` so the per-component scan never double-loads anything already in the bundle. |
| `data-cui-bundle-href="<url>"` | Set beside `data-cui-bundle` on an inline `<style>` that carries only the rules the page's markup can match (see "First-paint inlining"). The value is the URL a linked head would name for the same sheets. The `fullbundle` behaviour, a registered module `framework/uihost` ships, links that URL right after the `<style>` at boot and removes the attribute. It reads the attribute in `<head>` only, links nothing for a URL other than a component-sheet route (`/__gofastr/comp-bundle.css?…`, `/__gofastr/comp/<name>.css?…`), and joins the link to the kernel's stylesheet wait. |
| `data-cui-layout="<name>"` | Set by EVERY layout layer on its wrapper `<div>` with the layout's name (e.g. `app`, `marketing`). Emit-only since the layout-chain rewrite: it is the CSS/debug contract (`.layout-<name>` pairing), and the runtime's swap decisions read `data-cui-layout-key` instead. |
| `data-cui-layout-key="<key>"` | The layer's comparable identity, on the same wrapper `<div>`: `l:<name>` for a plain layout (the app default root, a direct screen's layout) (`l:<key>` in both forms when the layout declares `Layout.WithKey`, so a shell's identity can vary per language while its name keeps the CSS contract), `g:<prefix>:<name>` for a screen-group layer (`g:<prefix>` when the level is marker-only because its layout already renders at an outer level). The route manifest carries each route's chain as the `layouts` array of these keys, outermost → innermost; document order of the marked elements is the chain order. On SPA navigation the runtime compares the DOM's key spine against the destination's chain positionally: it swaps at the deepest shared layer, and when no root is shared it fetches the full page and replaces the whole shell. A group layer's key embeds the layout name so a per-screen layout override inside a group compares as a different layer than its siblings. |
| `data-cui-layout-slot="<key>"` | On the layer's content cell: the `<main id="main-content">` for layer 0, the `.layout-content` div (tabindex="-1") for nested layers, the group wrapper itself for marker-only levels. This is the runtime's swap target: a partial response's `X-Gofastr-Swap: <key>` (or a cache entry's recorded layer) selects the cell whose slot key matches, replacing the old `.layout-content ?? [role=main] ?? main` structural guess. After the swap the runtime focuses the cell (screen-reader announcement); a pointer-initiated navigation passes `focusVisible: false` so the focus ring stays keyboard signal, and uihost's base CSS suppresses `outline` on the swap targets when they are not `:focus-visible`. |
| `data-cui-outlet="<layer key>#<name>"` | On a tree-layout outlet cell (`app.NewLayout`): a non-primary outlet of a layout layer, addressed by its layer key plus outlet name. The SPA navigator resolves envelope fills by this address (loop + string compare, like `findSlot`) and replaces the cell's innerHTML with the fill on every navigation its layer survives (no hash, no skip). |
| `data-cui-area="<layer key>~<name>"` | On a tree-layout route-area cell (a `div`, or a `span` for an `AreaSpec.Inline` area that sits inside phrasing content): a layout area re-rendered by the server on every navigation its layer survives. Same addressing and swap rules as `data-cui-outlet`; the area's fn runs on every render, kept layers included (collect mode on partials). |
| `data-cui-fill="<addr>"` | On the `<template>` elements of a fills-envelope partial body (`X-Gofastr-Envelope: 2`): the primary payload is addressed by the bare swap key (always applied), every non-primary fill by its `data-cui-outlet`/`data-cui-area` address. Parsed inert in a detached template. No fill-hash attribute exists — every kept-layer fill is re-applied on every navigation; outlet DOM that must persist lives in a nested layout layer the chain keeps. |
| `data-cui-vt="<name>"` | On a tree-layout placed cell (primary, outlet, or route area) whose Go placement names a view transition (`app.PrimaryConfig.Transition` on the LayoutSpec primary / `app.OutletSpec.Transition` / `app.AreaSpec.Transition`, typed `app.Transition` values). The server renders the name (the author's raw `Transition.Name`, or a generated `vt-<layout>-<slot>`); the transition demand module (runtime `src/transition.js`, loaded when the document holds a `data-cui-vt` cell or a `data-cui-vt-kinds` vocabulary, at boot or after any apply) mirrors every `data-cui-vt` cell onto the CSSOM `view-transition-name` before a client navigation's view-transition snapshots (a `style` attribute is refused by the framework's default CSP, a CSSOM write is not), and wraps the swap in `document.startViewTransition({update, types})` with types `forward` / `back` / `reload`. Before that module loads — and on every page that declares no transition — the swap applies directly, with no view transition at all. `Layout.TransitionCSS()` generates the animation rules (enter/exit, back-direction variants, the name assignment), which the host collects into app.css for every registered layout; a raw `Transition.Name` with zero anims generates only the assignment and leaves the animation to author CSS (the platform's full power: shared-element morphs, geometry, custom keyframes); root-wide presets ship as `app.ViewTransitionPresetCSS` (fade / slide / none). Under `prefers-reduced-motion: reduce` the runtime starts no transition at all. A cancelable `gofastr:transition` event (detail `{from, to, types}`) fires on `document` before each transition; `preventDefault()` commits the swap with no transition. A streamed envelope's first unit (seed + primary + ready fills) commits through the same wrapper, so loading content being replaced by the real fill rides the same transition; late units apply directly. |
| `data-cui-vt-when="<media condition>"` | Beside `data-cui-vt` on a placed cell, or on the region the layout build marks via `app.LayoutTree.VTRegion()`, when the transition declares `app.Transition.Narrow` ("920px"): the name is breakpoint-conditional — the placed cell owns it at `(width >= Narrow)`, the region below it. The master-detail collapse: below the breakpoint list and detail are one pane and the whole pane must transition; a detail-only snapshot there would morph its group geometry across the list. The runtime mirror writes the CSSOM `view-transition-name` only while the condition matches and CLEARS it otherwise, so a viewport resize across the breakpoint moves the name instead of duplicating it (two live names of one spelling make the browser skip the whole transition); `Layout.TransitionCSS()` wraps the two assignment rules in the same `@media` conditions, keyed on this attribute. |
| `data-cui-loading="<addr>"` | On the inert `<template>` the server renders BESIDE an outlet cell, an area cell, or the primary slot's cell (addressed by the bare layer key) when that region declares `Loading` (`app.OutletSpec.Loading` / `app.AreaSpec.Loading` / `app.PrimaryConfig.Loading`): the browser already holds the loading content before any navigation fetch starts. On a navigation that will change the outlet, after `data-cui-after` ms of in-flight wait the runtime moves the outlet's old nodes into an in-document hidden park and clones the template's content in; on apply the response replaces it (honoring `data-cui-min`), and on failure, abort or a superseded navigation the parked nodes come back exactly — same nodes, so input values, listeners and island state survive. Inert with JavaScript off: SSR pages never show loading content, the real content is in the outlet. |
| `data-cui-after="<ms>"` / `data-cui-min="<ms>"` | On the loading template: `after` is how long the navigation must be in flight before the loading content shows (default 120 ms, the dim's delay — a faster response paints nothing extra); `min` keeps it, once shown, at least this long before the apply replaces it (no skeleton flash). Emitted by the server from `app.Loading.After` / `.Min`. |
| `data-cui-loadstate="shown\|exit\|error"` | Runtime-written, on the outlet/slot cell across the loading content's lifecycle: `shown` while it holds the cloned content (the enter animation runs now — author CSS keys richer enter/exit effects off the same states; the framework ships a default fade in `frameworkDimCSS`), `exit` while the apply waits for the region's own `animationend` (capped at 400 ms, child animations ignored) before replacing it, `error` on a deferred outlet whose part request failed twice (the request and its one retry): the runtime's `runtime.part_failed` phrase replaces the loading content, and the mark stays while that phrase does (a later navigation that refills the region drops it), absent otherwise. The framework CSS also exempts the region from the aria-busy dim (the loading content replaces the old content; dimming it would double the signal). Removed when the content is replaced or restored. |
| `data-cui-vt-kinds="<names>"` | On `<html>` at first paint and on the doc shell every swapped payload's root layer carries (copied onto the documentElement after a swap): the document's declared keyed-transition vocabulary, the sorted union of the chain's `Transitions` map keys (`app.PrimaryConfig` / `app.OutletSpec` `.Transitions`). The runtime gates the page answer's `X-Gofastr-Transition` pick against it — a name outside the vocabulary is ignored and the navigation keeps the direction type only. After a swap the transition demand module copies the vocabulary off the payload's doc shell with a direct `setAttribute`; the attribute is module-owned, so the `__gofastr.doc` manifest does not list it. |
| `data-fui-page-loading` | On the body-level `<div>` a host's `uihost.WithPageLoading(component)` renders into every full page: the host's own page-wide loading indicator, REPLACING the default `html[aria-busy]::after` progress strip (both never show at once). Pure CSS state — visibility keys off the same `html[aria-busy]` carrier, so it transitions in and out with no runtime involvement. The component is presentational config, rendered once per page with no Load/DI. |
| `data-cui-dir="rtl\|ltr"` | Same carrier as `data-cui-lang`, always beside it: the direction of that language (`i18n.Dir`: the language's script, its own subtag or CLDR's likely one). The server also writes it as `<html dir>` on every full page. The core runtime does not copy it; the `doc-dir` registered behaviour (`core-ui/app`), which the first `[data-cui-dir="rtl"]` marker loads, sets `<html dir>` after every SPA swap from the carrier whose `data-cui-lang` matches `<html lang>`, and from the language's likely script (`Intl.Locale(lang).maximize().script`) when no carrier names one (the cached first page's). It writes `<html dir>` with a direct `setAttribute`; the attribute is module-owned, so the `__gofastr.doc` manifest does not list it. A left-to-right site never fetches it. |
| `data-cui-lang="<tag>"` | On the outermost layer the server renders (layer 0 of a full page, the first re-rendered layer of a subtree partial, the bare `<main>` of a layout-less page): the page's resolved document language (`App.LangFor`: the route's `LangFunc`, then the request's negotiated locale, then the app's `Lang`, layered with the screen's `ScreenLang`). `<html lang>` lives outside the shell the runtime swaps, so the value must travel with the swap payload; after every SPA swap the runtime copies it onto `document.documentElement.lang` via `doc.setHtmlAttr` (in the DOC_MANIFEST). A payload without the marker leaves the document alone. The inert page-head template also carries the destination values when navigation retains the outer layout. |
| `data-cui-page-head` | Escaped page-owned meta/link HTML on an inert template in the navigation payload. The runtime replaces marked head nodes on navigation and cache replay; it never replays scripts. |
| `data-cui-page-meta` | Per-screen meta/link ownership marker. The navigator preserves unmarked site-wide head metadata. |
| `data-cui-skip-label="<text>"` | Same carrier and same rule as `data-cui-lang`, for the app shell's skip-link text (`App.SkipLabelFor` / `WithSkipLabelFunc`; the default is the `app.skip_to_main` phrase in the request's language): after every SPA swap the runtime writes it into the `[data-skip-link]` link, the first string a keyboard user tabs to, so it speaks the destination page's language (#411). |
| `data-cui-strings="<json>"` | On the inert `template[data-cui-page-head]` a SPA partial carries (uihost), and on the carrier the runtime builds from a fully fetched page (from its `#gofastr-strings` block): the runtime strings in the destination reader's locale (`runtime.StringsJSON`). A soft navigation keeps the document, so after every swap whose payload has the page-head template the runtime replaces `say()`'s table with this map; an absent attribute means English and clears the previous page's words. |
| `data-hui-disclosure` / `data-hui-disclosure-trap` / `data-hui-disclosure-dismiss` / `data-hui-disclosure-persist="<key>"` | On a `headless.Disclosure` / `framework/ui.Collapsible` `<details>` (and on menu dropdowns, which compose the same anatomy): `headless-disclosure` (a registered behaviour of `framework/headless`) mirrors `aria-expanded` onto the controller, closes the deepest open disclosure containing focus on Escape with focus returned to its controller, closes non-persistent disclosures on `gofastr:navigate` and when a link inside one is clicked (not a `data-cui-open` link, which opens a widget over the page, so the menu stays and the widget returns focus into it), restores/writes the open state of persistent ones under a namespaced, component-encoded session key, closes an open dismiss disclosure (every menu root, and `Disclosure.Dismiss`) on a click outside it or a touch or pen release on plain content outside it (iOS Safari sends no click for a tap on content with no click handler of its own; a tap on a control closes it through the control's click, so an in-flow panel never collapses before the browser aims that click), shifts an opened dismiss disclosure's panel back inside the viewport when it would cross an inline edge (writing `--hui-panel-shift`, which the kit's menu and dropdown panels read as a `translate`), floats an opened dismiss panel that a scrolling ancestor would clip (a table's last-row menu) as `position: fixed` at its trigger's edge, flipped above the trigger when the viewport has no room below and moved with it while anything scrolls, its inline styles cleared on close, and confines Tab inside the topmost open trap disclosure (the widget runtime's own containment technique over the kernel's focus selector). |
| `X-Gofastr-Defer: 1` | On a client navigation's PAGE request (with `X-Gofastr-Navigate`/`X-Gofastr-From`) whose destination route has deferred outlets (`app.OutletSpec.Deferred`, listed per route in the manifest's `deferred` array): the server skips those outlets' loaders and ships their `Loading` content in place — kept layers as envelope fills, rendered layers inline in the cells. Without the header (a first load, a whole document) every fill renders inline: nothing depends on JavaScript. Deferral belongs to the OUTLET, not a fill (a candidate can decline at request time, so a per-fill flag cannot tell the manifest in advance).. |
| `X-Gofastr-Part: <addr>` | One deferred outlet's fill, as its own request beside the page fetch: the same URL plus this header (plus `X-Gofastr-Navigate: 1`), read with `text()` so DevTools shows the request with its body, status and timing (streaming was removed for exactly this: Chrome keeps no body for a fetch read through a stream reader). The server runs the policy phase and ONLY the winning fill's loader — no screen `Load`, no other fills, no area builds — and answers one `<template data-cui-fill="<addr>">` plus an optional seed delta (never a session mint; `Cache-Control: no-store`). Parts fetch with priority `low` beside the page's `high`, at most three in flight, the rest queued; parts landing before the page commit wait in a buffer, the commit applies the page then the buffer, later parts apply on land through the region's `data-cui-loadstate` exit (never a second view transition). A whole-page disagreement — the route not resolving, the address not a deferred outlet of it, a policy Redirect/Block, a dead session — answers 409 with `X-Gofastr-Part-Reset: 1` and no body the client applies; the runtime reloads the URL as a whole document, at most once per navigation. A part that fails in transport (a non-2xx answer such as a proxy's 502, 503 or 429, or a dropped connection) is requested once more after a short pause; a second failure puts the region in `data-cui-loadstate="error"`. |
| `X-Gofastr-Markup: 2` | On every navigation request the runtime makes (the click's partial fetch, the envelope navigator, hover/visible prefetch, intercepted overlays): the markup generation the live kernel reads, from `window.__gofastr._markup` (`runtime.MarkupVersion` in Go; 2 is the `data-cui-*` spelling, 1 the `data-fui-*` one through v0.86). Modules read the kernel's value, so a module loaded into an older kernel sends none. A tab opened before a deploy keeps its runtime, and swapping in markup that kernel cannot read leaves every interactive marker dead (a `data-cui-rpc` form submits as a native GET, its fields in the URL). So when a browser script fetch (`Sec-Fetch-Mode` `cors`, `same-origin` or `no-cors`) names another generation or none, uihost answers 409 with an HTML partial at the swap key the live DOM holds, carrying `<meta http-equiv="refresh">` to the destination and a reload link: every runtime applies an HTML partial the same way, inserting the meta starts the browser's own navigation, and the destination loads whole with the current runtime. A request without Fetch Metadata (curl, a Go test, a browser older than Safari 16.4) gets the ordinary partial. Not covered: an old runtime's cross-chain navigation, which fetches the whole document with no navigate header and cannot be told apart from app code fetching a page; bump the generation in `frag/kernel.js` and `core-ui/runtime/markup.go` together when the attribute vocabulary changes again. |
| `data-cui-action="close"` | On any element inside a mounted widget: clicking it dismisses the widget (the widget's scoped click handler in `widgets.js`), with no request. `close` is the only value the runtime reads; `headless.ButtonProps.Action` (and so `ui.Button` `ExtraAttrs`) admits exactly that value and refuses any other at render. |
| `data-cui-widget="<name>"` | Marks a registered widget instance: the runtime mounts behavior on it after first paint. |
| `data-cui-backdrop` | Marks an element as a click-to-dismiss overlay backdrop. Pairs with `data-cui-open` to make the floating surface dismissible. |
| `data-cui-style="<name>"` | The dedup key for a component's stylesheet `<link>`. `loadComponentCSS` appends a link only when no `link[data-cui-style="<name>"]` exists, so every per-component link carries it, whether the runtime injected it or the host SSR-emitted it (a single-component page, and every page of a static export, where the bundle is not used). The host's link also carries `id="cui-css-<name>"`, the id the runtime would have given it. An SSR link without the marker is invisible to the dedup and gets loaded a second time, after `app.css`, which reverses the cascade against the host's overrides. |
| `data-cui-submit-on-enter` | On a `<form>`, Enter inside any child textarea submits the form. |
| `data-cui-clear-on-esc` | On an `<input>`/`<textarea>`, Escape clears the value. |
| `data-cui-autogrow` | On a `<textarea>`, height auto-grows with content. |
| `data-cui-charcount-source="<id>"` | An element that displays the live character count of the referenced input. |
| `data-cui-os` *(on `<html>`)* | Set by the runtime at boot to `"mac"` or `"other"` based on best-effort platform detection. Used by `framework/ui.ShortcutHint` to display platform-correct mod-key glyphs purely in CSS (no per-component JS). Functional shortcut matching does not depend on this attribute. |
| `data-cui-static` *(on `<html>`)* | Injected **only** by the static exporter (`framework/static.Builder`) onto `<html>`. When present, the runtime enters static mode: it fetches the dumped catalog file (`/__gofastr/widgets.json`) instead of the live session-gated endpoint, at boot and after every client-side navigation (which mounts and deep-links from that one catalog rather than asking `/__gofastr/widgets?page=`), and a `data-cui-rpc` click/submit surfaces a "Needs the Go server" notice (via the CSP-clean `#cui-nav-toast` mini toast) instead of firing a dead request, so a visitor who tries a server-backed demo learns why it's inert and how to run it locally. `data-cui-open` is **not** gated: overlays resolve against the widget catalog + chrome HTML the exporter dumps as query-free files, so navigation surfaces (command palette, section-menu drawers) work. Client-only features (theme toggle, copy, signal mutations) are unaffected. Live pages never carry it, so every static-mode guard is a no-op in the normal server-backed app. |
| `data-hui-tree` / `data-hui-tree-toggle` | On a `framework/ui.Tree` (headless.Tree anatomy): the registered `headless-tree` module (framework/headless, `Requires("rpc")` for the lazy branches) owns the WAI-ARIA keyboard contract — the roving tabindex, arrows, Home/End, type-ahead, and expand/collapse that drives the same toggle button a click drives, so any lazy-load `data-cui-rpc` on the toggle fires either way. |
| `data-cui-fill-input="<selector>"` / `data-cui-fill-text="<selector>"` | A button that fills the target input or text node with this element's `data-value` (or text content). |
| `data-cui-disable-when-invalid` | On a submit button: disabled while any field in the surrounding `<form>` reports `:invalid`. |
| `data-cui-persist-storage="<key>"` | The element's value persists across reloads in `localStorage`, stored namespaced as `gofastr.persist.` + `encodeURIComponent(<key>)` so an attribute-borne key can only ever touch that namespace. A value stored under the pre-namespace raw `<key>` is not read. |
| `data-cui-flash-on-update` / `data-cui-flash-duration-ms="<ms>"` | A signal-bound element flashes (CSS class `cui-flash`) for `<ms>` after each update (default 600). The built-in app.css styles the class: a `--color-primary` tint fading to the element's own background over `--duration-slow`, only under `prefers-reduced-motion: no-preference` (the runtime skips the class under `reduce`). |
| `data-cui-scroll-bottom-on-update` | A signal-bound scroll container auto-scrolls to the bottom on each update (chat / log views). |
| `data-cui-tick-elapsed="<unix-ms>"` | Element's text updates once per second with the elapsed human-readable interval since the given epoch. |
| `data-cui-rpc-body="<json>"` | Static JSON body for `data-cui-rpc` requests that don't come from a `<form>`. |
| `data-cui-rpc-after-done` | Internal marker: set by the runtime after a one-shot `after-text` / `after-disable` fires so re-clicks are idempotent. |
| `data-cui-deeplink="<k1=v1&k2=v2>"` | On a `data-cui-open` button: per-click overrides for the opened widget's declared `DeepLinkParams`. The runtime mirrors the pairs into the widget's signals on open AND pushes them onto the URL (alongside the widget's `DeepLinkKey=DeepLinkValue`) so refresh / share / back-button preserve the open modal AND its data. Used for row-level "Edit user 42" flows. |
| `data-cui-toast="<json>"` | On a clickable element: clicking fires a toast with the given config (variant/title/body/ttl/stack). The runtime's global click delegator parses + dispatches via `__gofastr.toast()`. |
| `data-hui-toast-id="<id>"` | Marks one item inside a toast stack. Items the module builds carry the id `NS.toast()` assigned (`t<n>`); a server-rendered row is given one on sight (`s<n>`), which is what arms its TTL and click-to-dismiss. Owned by `headless-feedback`. |
| `data-cui-toast-stack="<name>"` | Marks the container into which `__gofastr.toast()` appends items. The name matches the widget name passed to `preset.ToastStack`. `framework/uihost` mounts one named `uihost.DefaultToastStack` (`gofastr-toasts`) at boot when the app mounted none, so a header toast always has a region; the module mounts no container of its own and returns `null` without one, which hands the toast to the kernel's `data-cui-toast-fallback` region. |
| `data-hui-toast-template` | On the inert `<template>` `preset.ToastSlotHTML` renders inside the stack: the row `headless-feedback` clones for a runtime toast. The kit registers it under `preset.ToastTemplate` (`registry.RegisterTemplate`), rendered by `headless.ToastTemplate`; the template carries `data-hui-toast-glyph-<tone>` (the icon text), `data-hui-toast-variant-<tone>` (the class the root gains), and the stack's words as fallbacks (`data-hui-toast-dismiss-label`, `data-hui-toast-tone-<tone>`). Inside, the row is hooks: `data-hui-toast-item` (the stack row), `data-hui-toast` (the root), `data-hui-toast-tone`, `-icon`, `-title`, `-body` (filled, or removed when empty), `data-hui-toast-action` (a button, `hidden` until a toast's `action: {label, attrs}` fills it, removed otherwise) and `data-hui-toast-dismiss`. No template on the page: the module builds the same row bare. |
| `data-hui-toast-leaving` | Written by `headless-feedback` on a toast item the moment it is dismissed; the kit's `ui-toast-stack` sheet animates the row out on it and the module removes the row when the animation ends. |
| `data-cui-embed-state` *(on the embed root)* | Lifecycle of an embedded surface. The server writes `loading` into the shell HTML; the `boot-embed` fragment writes every later value, and ships **only** in the `embed` bundle served at `/__gofastr/embed-runtime.js`. `ready` once the handshake completed and the surface's server-rendered content was injected. `error` when there is no parent to hand over a nonce, no token arrived within 15s, the exchange was refused, or the content fetch failed. `expired` when the grant's absolute lifetime ran out and refresh could not renew it. Nothing in the runtime branches on it; it exists so tests can see the frame's state. The host page is cross-origin and can neither read nor style inside the frame. See `framework/docs/content/embed.md`. |
| `data-cui-toast-fallback` | Marks the degraded inline container core injects when the `headless-feedback` module fails to load (transient 5xx, network hiccup). Used by `__gofastr._fallbackToast(cfg)` so an X-Gofastr-Toast payload still reaches the user even when the full module is unavailable. Unstyled-but-visible; no TTL, no animation. |
| `data-hui-menu` / `data-hui-menu-trigger="<id>"` / `data-hui-menu-panel` / `data-hui-menu-radio="<group>"` / `data-hui-menu-lazy` | On a `framework/ui.Menu` (headless.Menu anatomy): the dropdown and its submenus are disclosures (`data-hui-disclosure` beside `data-hui-menu`), `headless-menu` (a registered behaviour of `framework/headless`, `Requires("headless-disclosure", "widgets")`) owns the keyboard contract — roving focus scoped to the item's own panel, wrapping arrows, Home/End, bounded type-ahead, RTL-aware submenu open/close, Tab closing the chain, radio arbitration across the whole menu, focus-on-open, and the lazy template's inflation. The retired menu-module attribute family is gone with the `menu` module. |
| `data-cui-match-prefix="<path>"` | On an `<a>` link, inside a `<nav>` landmark or not: opts the link into prefix-matching for active-route highlighting. The attribute's VALUE, when non-empty, names the section prefix — a link can own a section it does not live at (`framework/ui.Sidebar` emits its items' `MatchPath` there, which is how a section stays lit across client navigations into it); an empty value falls back to the href. The runtime tags it `aria-current="page"` + `.active` when the current path equals the prefix or continues it at a segment boundary: `/docs` and `/docs/` both light up on `/docs` and `/docs/getting-started`, and neither matches `/docs-old`. Without this attribute the runtime does exact matching only (a query-less href matches the URL's path, a trailing slash aside, since a screen's sort, page and filter live in its query, unless its nav also links that path with a query, a tab strip whose current tab the server picks; an href with a query must equal the URL, again a trailing slash on its path aside), so breadcrumbs and sidebars (where multiple links share prefixes) keep the server-rendered single active item. Root `/` is never a prefix match. |
| `data-cui-activelink` | On an `<a>` link, inside a `<nav>` landmark or not: hands the link's current-state to the `activelink` module without changing how it matches (exact, as above, unless `data-cui-match-prefix` is present too). `headless.Sidebar` marks every leaf with it, so the `aria-current="page"` the server settled for first paint is the module's to clear after a client navigation. The module loads idle, so a navigation can land before it ever stamped `.active` on the old link; without the handover the stale first-paint mark survived beside the fresh one, two lit entries. The handover covers navigations only: on a document that has not navigated client-side, the load-time sweep stamps matches and leaves these marks alone, so `Active` on `/orders` served at `/orders?page=2` or on a detail page keeps its mark. Links with neither handover attribute nor the module's `.active` class keep owning their attributes. |
| `data-cui-activelink-skip` | On a `<nav> <a>` link: opts OUT of active-route highlighting entirely. The `activelink` runtime module neither sets nor clears `aria-current` or `.active` on it, at load or after SPA navigation. The escape hatch for a link whose current-state is owned by something else: a hand-set attribute (`aria-current="location"` on an in-page anchor), app JS, a signal binding. Same hands-off treatment as href-less links. |
| `data-cui-popover-anchor` | On a `data-cui-open` trigger button: opt the opened widget into trigger-anchored positioning. The value is the preferred side: `"top"`, `"bottom"`, `"left"`, `"right"`, or empty / `"auto"` (= bottom-first, then top, right, left). The runtime measures both rects after open and applies inline `position: fixed; top; left` so the popover sits next to the trigger; if the preferred side would overflow the viewport (8px margin), it auto-flips to the opposite. Re-runs on `window.resize` AND `window.scroll` (capture, rAF-throttled) so the popover tracks the trigger when the page scrolls. Distinct from `preset.Modal`'s deep-link affordances: popovers are click-driven and don't deep-link. |
| `data-hui-multiselect` / `data-hui-multiselect-chips` / `data-hui-multiselect-placeholder="<text>"` / `data-hui-multiselect-remove-label="<fmt>"` / `data-hui-multiselect-remove="<input-id>"` | On a `framework/ui.MultiSelect` (headless.MultiSelect anatomy; the disclosure itself is `data-hui-disclosure`'s): the registered `headless-multiselect` module (framework/headless, `Requires("headless-disclosure")`) rebuilds the chips strip from the checkboxes' own state after every change, names each chip's × from the remove-label format ({label} substituted), unchecks the checkbox a × names inside that ×'s own multiselect (option ids derive from the instance id, which defaults to the field name, so two multiselects for one field share them), and closes the disclosure on click-outside. The placeholder is the sheet's `:empty::before` content. The submit contract is the plain form: every checkbox shares the field name, no script needed. |
| `data-cui-dropdown` | On a dropdown trigger button. The `dropdown` runtime module toggles `aria-expanded` and shows/hides the paired panel on click. Outside-click and SPA navigation close all dropdowns without moving focus. Escape defers to the modal stack, then closes only the focused (or topmost) dropdown layer; when focus was inside its panel, focus returns to that dropdown's trigger. Opening one dropdown still closes the others by default. |
| `data-cui-dropdown-wrap` | On the wrapper around a `data-cui-dropdown` trigger + `data-cui-dropdown-panel`. Scopes open/close to one dropdown instance; the runtime sets/clears `data-cui-dropdown-open` on it to track state. |
| `data-cui-dropdown-panel` | On the floating panel sibling of a `data-cui-dropdown` trigger. The runtime toggles its `hidden` attribute as the dropdown opens/closes. |
| `data-cui-dropdown-open` | Runtime-written marker on a `data-cui-dropdown-wrap` while its dropdown is open. CSS keys the open state off it; the runtime uses it to find and close open dropdowns. |
| `data-cui-animate-signal="<name>"` | On an element wired by the `animate` runtime module: names the signal to watch. When the signal becomes truthy the runtime adds `data-cui-animate-class`; falsy removes it. Initial state is applied on wire. |
| `data-cui-animate-class="<class>"` | The CSS class the `animate` module toggles on the element as its `data-cui-animate-signal` value flips between truthy and falsy. |
| `data-cui-reveal="<type>"` | Marks an element for the `reveal` runtime module's scroll-into-view animation. The element gets `cui-hidden` immediately; when it enters the viewport the runtime swaps in `cui-revealed` + `cui-reveal-<type>` (e.g. `data-cui-reveal="fade-up"` → `cui-reveal-fade-up`). One-shot. The module loads after first paint, so an element already on screen at its first scan has been painted visible: it is marked `cui-revealed` and never hidden (hiding it to fade it back was a blink on every load). Content scanned later (a navigation, an island swap) is hidden before it paints and keeps the entrance. |
| `data-fui-dropzone-preview` | On a `<input type="file">` inside a `framework/ui.FileDropzone`: opt the input into image-preview rendering. After each `change`, the `filedropzone` module (framework/ui's own) FileReader-reads each selected image and renders `<img>` tags into the sibling `[data-fui-dropzone-preview-for="<input-id>"]` container; the drop itself, the chosen-names list and the pick announcement are the headless module's `data-hui-drop` hooks. |
| `data-fui-dropzone-preview-for="<input-id>"` | On the previews strip element: links it to the input it should display previews for. |
| `data-hui-back-to-top-visible` | Runtime-written BackToTop visibility marker (`headless-navigation` owns it; no component renders it). CSS uses it to reveal the control once the threshold is crossed. |
| `data-hui-toc` / `data-hui-toc-target="<selector>"` | On a `framework/ui.TableOfContents` nav (headless.TableOfContents anatomy): the server renders the entry list from explicit `Items`, and `headless-toc` (a registered behaviour of `framework/headless`, loaded on the `data-hui-toc` marker) marks the active entry — `aria-current="true"` plus a class, never a style — through the IntersectionObserver `headless-rail` owns (`Requires("headless-rail")`, one observer shared with AnchoredRail). The retired runtime-filled toc contract is gone with the `toc` module. |
| `data-hui-rail` / `data-hui-rail-observe="<selector>"` / `data-hui-rail-target="<selector>"` | On a `framework/ui.AnchoredRail` aside (headless.Rail anatomy): `headless-rail` (a registered behaviour of `framework/headless`) watches the observed region and sets `aria-current="true"` + `.is-active` on the link whose target section holds the top of the view. The entries are real fragment links, so the rail works with no script. The retired scrollspy wrapper contract is gone with the `scrollspy` module. |
| `data-hui-shortcut-focus` / `data-hui-shortcut-click` / `data-hui-shortcut-target` / `data-hui-shortcut-hint` | The page's keyboard chords, rendered by `ui.GlobalSearch`, `ui.CommandPalette`, `ui.ShortcutHint` (BindTarget) and host chrome; bound by the registered `headless-navigation` module (framework/headless), which owns one document keydown with an isComposing guard and resolves the first connected target outside an `inert` subtree (a chord without Mod or Alt never fires while focus is in an input, a contenteditable editor or a `role="textbox"` widget, and a modifier the chord does not name makes it a different chord, except Shift on a non-letter such as `?`), so a chord under an open drawer's lower layers or the page beneath reaches the top layer's control. The retired shortcut-module spellings are gone with the `shortcut` module. |
| `data-hui-combobox*` (input, listbox, static, loader, loading, status, count, no-results, inline) | On a `headless.Combobox` / `ui.GlobalSearch` / `ui.CommandPalette`: the registered `headless-combobox` module (framework/headless) owns the keyboard contract and the pick (Escape on an open listbox closes only the list, in the capture phase, so a surrounding modal, intercept overlay or disclosure stays open; `-inline` on the input, set by `Inline` and by the command palette, marks a list that is the dialog's own content, whose Escape closes the dialog); the RPC debounce and the signal swap stay the kernel's `data-cui-rpc` contract; the no-script path is the same-origin GET form. The retired combobox-module static-list marker went with the `combobox` module. |
| `data-hui-tabs` / `data-hui-tabs-state` / `data-hui-tabs-vacate` / `data-hui-tabs-stash` | On a `headless.Tabs` strip: the registered `headless-tabs` module (framework/headless) owns the roving-tabindex keyboard contract (RTL-aware arrows, Home/End), the data-state mirror and the vacate stash restore. Selection stays the kernel's signal contract (`data-cui-signal-set` beside each tab's fragment href). |
| `data-hui-panehost` / `data-hui-pane` / `data-hui-pane-open` / `data-hui-pane-deeplink` / `data-hui-pane-open-control` / `-close` / `-swap` / `-key` | The pane host anatomy and its trigger controls: headless.PaneHost renders the shell (bound by the registered `headless-panehost` module, framework/headless, `Requires("widgets")` for the drawer trap), and core-ui/interactive's OpenPaneOnClick/ClosePaneOnClick/SwapPaneOnClick/PaneKey render the triggers. The retired panehost-module spellings are gone with the module. |
| `data-hui-sidebar` / `-variant` / `-collapse` / `-storage` / `-toggle` / `-group` / `-group-toggle` / `-collapse-label` / `-expand-label` | The sidebar anatomy and its collapse contract: headless.Sidebar renders the shell (bound by the registered `headless-sidebar` module, framework/headless, `Requires("widgets")` for the mobile drawer); the storage hook rides only when the caller names a key — a keyless sidebar is server-owned and the module never writes. The retired sidebar-module spellings are gone with the module. |
| `data-hui-sortable` / `data-hui-sortable-rpc="<path>"` / `data-hui-sortable-item` / `data-hui-sort-key="<key>"` / `data-hui-sortable-group="<id>"` / `data-hui-sortable-container="<id>"` / `data-hui-sortable-version="<token>"` / `data-hui-sortable-conflict="<rpc>"` / `data-hui-sortable-s-*` | On a `framework/ui.SortableList` (headless.SortableList anatomy): the registered `headless-sortablelist` module (framework/headless) owns HTML5 drag reorder plus the keyboard model (Space grabs, Arrow Up/Down moves within a column, Arrow Left/Right crosses to an adjacent column of the same group, Space drops, Esc cancels, and so does the grabbed row losing focus; a drag that ends with no drop on a list, Esc mid-drag or a release elsewhere, puts the row back uncommitted), the polite per-move announcements (the `data-hui-sortable-s-*` attributes carry the Strings, `{label}`/`{list}`/`{position}` substituted at say-time), and the server-authoritative commit: same-container reorders POST `order=<keys>` plus `container=` when configured and `version=` when versioned; cross-container drops add `moved=<key>` and always carry `container=`; non-2xx reverts the DOM; a versioned 409 fires the conflict path (GET the conflict endpoint, replace the list's rows — an empty body reconciles the column to zero items), after reading the 409 body under hard bounds (JSON content-type, ~4 KB, `{"error":{"message":<string>}}`, capped ~300 chars). |
| `data-fui-lightbox="<name>"` | On the slot wrapper of a `framework/ui.Lightbox`: identifies the open viewer for the lightbox module (a registered behaviour `framework/ui` ships; the kernel's own tables name no lightbox). Pair with optional `data-fui-lightbox-nav="true"` to enable Prev/Next + ArrowLeft/Right keyboard nav across siblings sharing `data-fui-lightbox-group`. |
| `data-fui-lightbox-nav="true"` | On the lightbox slot wrapper: opts into the lightbox module's arrow-key + Prev/Next button navigation. |
| `data-fui-lightbox-image` | On the `<img>` inside a Lightbox viewer: the image the module's pinch-to-zoom owns. Present so the zoom targets an attribute, never a class (a class map may rename every class); an unwired `headless.LightboxViewer` publishes the same fact as `data-hui-lightbox-image` — the two spellings are alternatives, one vocabulary per render. |
| `data-fui-lightbox-group="<id>"` | On a trigger anchor that opens a Lightbox: identifies the gallery group whose siblings the lightbox module walks during Prev/Next nav. |
| `data-fui-lightbox-prev` / `data-fui-lightbox-next` | On Prev/Next buttons inside the open Lightbox: clicking steps to the previous/next image in the gallery group. |
| `data-cui-popover-side` | Written by the runtime onto the anchored popover's widget root after placement: value is the final chosen side (`"top"`, `"bottom"`, `"left"`, `"right"`, post auto-flip). CSS uses it to position the directional arrow (`::before`) and to apply the anchored chrome (border, shadow, max-inline/block-size). Cleared on dismiss. |
| `data-cui-popover-trigger` | Written by the runtime onto the originating trigger button while its anchored popover is open. The runtime also adds the `.is-popover-trigger-active` class so the trigger can be highlighted while its popover is the currently-active surface. Both are stripped on dismiss or when the popover re-anchors to a different trigger. |
| `data-cui-prefetch="<module>"` | On any element: opt the page into hover/focus-prefetch of a split runtime module (e.g. `data-cui-prefetch="popover"`). On the first `pointerover` or `focusin` (capture phase, once per element) the runtime fires `__gofastr.loadModule(<module>)` so the module is ready by the time the user clicks. Multiple modules can be listed space-separated. Used to keep typical pages on `core.js` only while still feeling instant on interaction. Module names are shape-checked (`^[\w-]+$`) before the URL is built: the value is DOM input, and a `../../../evil` token would otherwise normalize out of the runtime serve route onto an arbitrary same-origin script. See `framework/docs/content/runtime-minification.md` for the size story. |
| `data-cui-nav="off"` | On an anchor: decline SPA navigation for this link. The runtime lets the click through to the browser, so the destination gets a full document load. For hosts whose destination page binds behavior at script load — a soft swap never re-runs those initializers, so every handler on the destination dies. The screen-level equivalent is `Screen.NoSPA`, which excludes a route from the client manifest so *every* link to it loads fully. |
| `data-cui-doc` | On a `<script src>` emitted by `uihost.RegisterDocumentScript(src, scope)`: the script has a DOCUMENT lifetime, shipping only on routes the scope predicate accepts (the predicate sees the registered route pattern, `/session/:id`, at render time and in the manifest alike). The runtime reads the live document's `data-cui-doc` srcs and compares them against the destination route's manifest `docScripts` at every soft-nav entry point (click hijack before `preventDefault`, `navigate()`, `popstate`, `loadPage`'s redirect leg). A difference — entering OR leaving the scope — performs a real document load instead of a partial swap; equal sets stay partial, and Back/Forward across an edge loads the destination fresh. The reason is a browser fact: such a script installs capabilities INTO the document (WebMCP's `navigator.modelContext` tools are the driving case), and DOM removal is not capability revocation, nor does a partial swap ever run a body script. |
| `data-cui-screen-group="<prefix>"` | On the `.cui-screen-group` wrapper div around each group layer. Since the layout-chain rewrite the swap logic no longer keys on it (chains carry group identity in `data-cui-layout-key`); the runtime still reads it in one place, locating the outermost shell element for a cross-chain shell replacement, and it remains the CSS/introspection contract for group boundaries. The prefix matches the group's URL prefix (trailing slash). |
| `data-hui-pane="primary\|secondary\|tertiary"` | Emitted by `PaneHost` (headless.PaneHost) on each of its three slot children. The runtime addresses a pane by this value when opening/closing it; CSS keys the open-state grid columns and the overlay-drawer chrome off the combination of the root's open modifier classes and this slot marker. |
| `data-hui-pane-mode="overlay"` | Written by the registered `headless-panehost` module onto the host when `matchMedia('(max-width: 768px)')` matches AND a pane is open. CSS flips the open pane to a fixed overlay drawer (backdrop scrim via `::before`, right edge, full height) and the module applies a focus trap + scroll lock + ESC/backdrop-to-close. Cleared when the viewport widens or the pane closes. |
| `data-hui-pane-deeplink="<param>"` | Emitted by `PaneHost` when `PaneHostConfig.DeepLinkParam` is set: opt-in URL round-tripping, naming the query parameter that records pane state. Opening through a keyed trigger writes `?<param>=<pane>:<key>`, closing strips it, and `popstate` and a page load replay the state by re-clicking the matching trigger: the `[data-hui-pane-key]` that opens or swaps to the named pane (one key may label a trigger for each pane). A key no trigger on the page carries opens the pane empty. Pane state stays in-page state (Hard Rule 1); the parameter only records it so refresh/share/Back reproduce what is on screen, the same contract widget deep links give modals. The SERVER renders first paint from the same parameter via `ui.PaneDeepLink`. Without that a shared link paints the pane closed and opens and fills it after hydration. Absent on every host that does not opt in, so the popstate listener is inert for them. |
| `data-cui-intercept-overlay` | Written by the demand-loaded `intercept` module on the container it appends to `<body>` for an intercepted route. Holds the screen render the server returned as an overlay variant; the scrim and docking come from `app.InterceptOverlayCSS()`, which the host injects only when some route declares an intercept. The container's children are the LAYERS of an open stack, deepest first. Removed when the last layer closes. |
| `data-cui-intercept-as="drawer\|sheet"` | On each layer (a child of the overlay container): which presentation the SERVER chose for it, mirrored from the `X-Gofastr-Overlay` response header (itself derived from the registered `app.InterceptFrom` ScreenType). CSS keys each layer's docking edge off it, so one stack can hold a sheet over a drawer. The client never picks its own chrome: a forged request can change the wrapper element and nothing else, since policy, params, Load, and content are identical on the canonical and overlay paths. |
| `data-cui-intercept-close` | On a button inside intercepted overlay content: click closes the TOP layer (through `history.back()`'s path, so the button, ESC, and the backdrop all resolve to the same history move and the page underneath is never refetched). A layer whose form carries `data-hui-leave-guard` and unsaved edits asks first. |
| `data-cui-intercept-page` | On a link inside the TOP intercepted layer: open the link's target as the page instead of in the pane. The stack closes, history walks back over every entry the layers wrote (each layer's open and its pane's own query moves) to the page under the stack, and the page is pushed there and loaded by the router, so Back returns to the page under the stack, never to a layer that is gone or a pane's earlier URL. The leave guard asks about the whole document first. `ui.DrawerBar`'s `PageURL` draws one. |
| `data-cui-intercept-swap` | On a link inside the TOP intercepted layer: render the link's target in that layer instead of stacking a new one or navigating, the way a query-only link to the layer's own path re-renders it. The fetch names the layer's own origin (`X-Gofastr-From`), so the server answers only when the target opens over the page under the layer; each swap is one history entry of the layer, so Back steps back inside it and closing the layer consumes them all. The leave guard asks about the layer's content first. `ui.DrawerBar`'s `Prev` and `Next` draw them: a record drawer steps through the list under it. |
| `data-cui-intercept-panel="<path>"` | On a link on a page with no intercept stack open: the link's href is the page a record opens over (a list) and the attribute names the record's root-relative path. The page under the link takes the current history entry, the router loads it, then the record mounts as the first layer over it, so Back from the layer lands on that page. The reverse of `data-cui-intercept-page`. When the record's route does not open over the href's route (its `app.InterceptFrom` origin), or the path is not same-origin, it is a plain navigation to the href. `ui.LinkButton` carries it through `ExtraAttrs`. |
| `data-hui-leave-guard` / `data-hui-leave-guard-message="<text>"` / `data-hui-leave-guard-title` / `data-hui-leave-guard-accept` | On a `<form>` whose unsaved edits must ask before the user leaves them: the registered `headless-leaveguard` module (framework/headless) keeps one shared "changed" state per guarded form — dirty on `input`/`change`, clean after a successful submit (the rpc module dispatches `gofastr:formresult` on the form, and a refused answer with `ok:false` puts the dirt back) or a `reset`. The guard asks only when a move would discard a dirty form, scoped to what that move discards: a router-taken link asks through `gofastr:beforenavigate` (the intercept module's `_interceptScope` says what the click discards: nothing when it stacks a layer over the edits or the cap refuses it, the top pane for a query move inside it, everything when the router replaces the page); Back and Forward on a page with no open stack ask through the router's first popstate hook; the intercept module's close paths (Esc, the close control, a popstate that closes layers, refetches a pane, or leaves for a page other than the one under the stack) ask through `__gofastr._leaveGuard.ok(scope)`; and `beforeunload` is armed so a reload or tab close gets the browser's own prompt. On a page carrying the kit's `<template data-cui-confirm-dialog>` the guard asks in that dialog: the hooks it answers are synchronous, so it declines the move at once, opens the dialog (the confirm module's `askWords`, worded by `data-hui-leave-guard-message`, `-title` and `-accept`, with the danger accept), and on Discard cleans the dirty forms the move drops and makes the move again (the link clicked again, the history move gone again by the same distance or, with no known distance, a navigation to where it was headed, the close path run again); `__gofastr._leaveGuard.ok(scope, retry)` takes that retry. Cancel leaves everything where the decline put it, and a move made while the dialog is open is declined without a second ask. A page without the template, or a caller that passes no retry, asks with `window.confirm` (the message attribute's text, defaulting to "You have unsaved changes."). A declined Back is repaired with a re-push, so the URL, the layers and history.length all stay where they were. While a form is dirty the module sets `data-hui-dirty` on it and on every control outside it that names it by `form=` (a header Save), and removes both when it cleans; `ui.ButtonConfig.QuietUntilDirty` draws from it. Without the runtime the hook is inert markup. |
| `data-wizard-steps="<n>"` | On the `<form>` wrapper of a `Wizard` component. The runtime uses this to know the total number of steps for navigation. |
| `data-cui-drag-dismiss="true"` | On a widget root whose Definition has `DragDismiss=true` (e.g. `preset.BottomSheet`). Driven by the demand-loaded `runtime/src/dragdismiss.js` module (the marker itself is the load trigger: present at boot for SSR-inlined sheets; dynamically-opened chrome is caught by the MutationObserver scan). Drag starts only from the `data-cui-drag-handle` bar; the module follows pointer Y movement with `transform: translateY` and closes the widget on `pointerup` when distance > 80px or downward velocity > 0.5 px/ms. Snaps back otherwise. While dragging, `data-cui-dragging` is set on the root (used by CSS to suppress conflicting animations). |
| `data-fui-plugin="<name>"` | Mount marker emitted by `framework/pluginhost.MountMarker` for a heavy-JS plugin. The host broker (`framework/pluginhost/host/pluginhost.js`, served at its own route, NOT part of runtime.js) scans for it and mounts the plugin's sandboxed opaque-origin iframe in place. |
| `data-fui-plugin-docid="<id>"` | On the plugin mount marker: the persistence key the plugin instance edits; adapters echo it in save RPCs. |
| `data-fui-plugin-doc` | On the plugin mount marker: server-rendered initial document JSON (HTML-escaped) handed to the plugin in the `init` protocol event. |
| `data-fui-plugin-minheight="<css>"` | On the plugin mount marker: initial iframe height before the plugin's first `resize` event. |
| `data-fui-plugin-capabilities="<a,b>"` | On the plugin mount marker: comma-separated capability grant set advertised to the plugin in `init` (same `resource:verb` grammar as battery/auth token scopes). |
| `data-fui-plugin-for="<json,md>"` | Plugin-defined extension attribute (wysiwyg): names the hidden form fields the host adapter mirrors `docChanged` content into. Plugins may add namespaced `data-fui-plugin-*` extras via `MountConfig.Attributes`; document them in the owning plugin. |
| `data-fui-plugin-fallback` | Wraps the server-rendered pre-hydration node inside the plugin mount marker (`MountConfig.Fallback`). The broker shows it while the frame loads, hides it — never removes it — on the frame's `ready`, and swaps back to it on `bootError` (a dead frame degrades to the static node, not an empty box). |
| `data-search` / `data-search-loading` / `data-search-empty` / `data-search-all="<path>"` / `data-search-all-label` | On the listbox of a `ui.CommandPalette` with `Search`. The marker loads `battery/sitesearch`'s `sitesearch` module, which adds result rows (`data-search-result`) as the reader types, a disabled row in the loading or empty words while there are none, and, with `data-search-all` (`CommandPaletteConfig.SearchPage`), a last row opening that search page for the query, worded by `data-search-all-label` with `{q}` filled. Tests: `battery/sitesearch/parity_chromium_test.go`. |
| `data-search-page="<path>"` / `-query` / `-count` / `-one` / `-none` / `-more` / `-limit` / `-input` / `-status` / `-list` / `-pager` | On the `<section>` `ui.SearchResults` renders and its parts. The root names the page's path and the query the server rendered, the status words (`{n}`, `{q}`) and the page size; the parts mark the input, the status line, the result list and the pager. The marker loads `battery/sitesearch`'s `sitesearch-page` module. As the reader types it fetches the page's own render for the new query with the router's partial headers and swaps in the status, list and pager, never the input; the address follows with `_pushURL(url, {replace: true})`, and a submit runs at once instead of reloading. On a static export (`data-cui-static`) it ranks with `sitesearch-static`, draws the rows with DOM calls, runs a `?q=` from the address on load, and pages with a "more" button worded by `-more`. Without the runtime the form GETs the page and the pager links page it. Tests: `battery/sitesearch/page_chromium_test.go`. |
| `data-cui-drag-handle="true"` | On the visible drag-handle bar rendered at the top of a drag-dismiss-enabled widget. Marks the affordance for cursor styling; the actual pointer logic is delegated from the widget root. |
| `data-fui-zoomed` | Written by the lightbox module onto the viewer's `[data-fui-lightbox-image]` image when the user has pinch-zoomed past 1×. CSS uses it to flip the cursor from `zoom-in` to `grab` and to enable single-pointer panning. Cleared on snap-back, on Prev/Next and on lightbox close. |
| `data-behavior="/__gofastr/widget/<id>.js"` | On a `[data-widget]` / `[data-component]` root: the behaviour script the runtime appends as `<script src>` on first hydration. **The runtime's most privileged attribute**: it is a script-loading sink, so the value is matched against exactly the shape `core-ui/component` emits and anything else is refused with a console warning. Never hand-write it. |
| `data-widget="<id>"` | Widget root marker. Names the widget for hydration, chrome lookup, and `X-FUI-Widget` on scoped RPCs. |
| `data-component="<id>"` | Component island root marker. The hydration counterpart of `data-widget` for non-widget islands; `data-action` handlers resolve their component id by walking up to it. |
| `data-bind="<key>"` | Two-way input binding: the runtime mirrors the element's value into the named state key on `input`. |
| `data-cui-trusted` | Marks a server-emitted region as trusted to host the legacy `data-kiln-tool` click/submit delegators. Without this ancestor (or `<body class="kiln-app">`), the legacy delegator refuses to dispatch, which prevents stored-XSS content from forging authenticated kiln-tool POSTs. Apply only to chrome you fully control. |
| `data-fui-z-tier="<tier>"` | Emitted by `framework/ui.Sticky` with the layering tier from `StickyConfig.ZIndexTier` (`sticky` default, or `dropdown`/`modal`/`popover`/`toast` matching the theme's `ZIndexSet` tokens). CSS-only consumer: the `ui-sticky` stylesheet keys `z-index: var(--z-<tier>)` off this attribute so a sticky toolbar can layer above/below other surfaces without bespoke CSS. |
| `data-cui-poll="<duration>"` | Marks an element for the demand-loaded `poll` runtime module. On the interval (Go-duration syntax: `"5s"`, `"30s"`, `"1m"`, compound `"1m30s"`) the module GETs `data-cui-poll-src` and swaps the response HTML into the element's `innerHTML` through the same `innerHTML + scanAndLoadCSS` path `html`-mode signal regions use: one region-swap pipeline, not a second one. Clamp: intervals below 5s are raised to 5s so a typo can't DoS the server. ±10% jitter per tick desynchronises a page full of polls; pauses while `document.hidden` and fetches immediately on regain; doubles the interval (capped at 5× base) on fetch failure and resets to base on the next success. The marker is idempotent (`__fuiPollWired` guard); timers self-teardown when the element leaves the DOM and are reclaimed on SPA navigation via the `_moduleScanners.poll` hook. Terminal state (#192): a handler that wants the poll to stop after this tick sets the `X-Gofastr-Poll-Stop: 1` response header. The runtime applies the response body (so the terminal state renders) and then tears down the timer, so no further fetches land. A swapped-in replacement that omits `data-cui-poll` (or carries `data-cui-poll="off"`/`"0"`) is not (re)wired, since those values parse to NaN, so an island swap that replaces the whole region element also ends the poll. Pair with `data-cui-poll-src`. |
| `data-cui-poll-src="<url>"` | The GET endpoint the `poll` runtime module fetches on each `data-cui-poll` tick. The response body replaces the parent element's `innerHTML`. Same-origin by default (`credentials: 'same-origin'`); the endpoint should return an HTML fragment, not a full document. Every successful applied tick (page-level here, widget-level `Builder.Poll` alike) increments the shared liveness observable `window.__gofastr.pollStatus` (`{ ticks, lastTickAt }`, one object mutated in place, the poll analog of `sseStatus`); an HTTP-error response counts as a failure and triggers the back-off. Terminal state (#192): the response may carry `X-Gofastr-Poll-Stop` (truthy: `1`/`true`/`yes`/`on`) to end the poll after applying this tick. The runtime honors it on both the page-level and widget (`Builder.Poll`) paths; for widgets the `/state` handler emits it when `Builder.PollTerminal` reports terminal. |
| `data-cui-window-drag` | On any element inside a desktop-host window: marks the drag surface of a borderless (ChromeNone) or unified-title window. The demand-loaded `desktop` module's delegated `mousedown` listener matches the click target (or an ancestor) against this attribute and calls `__gofastr.desktop.window.startDrag()`, which posts `{"type":"drag"}` through the native WebView message channel (`window.webkit.messageHandlers.gofastr` on WKWebView; `window.chrome.webview` on WebView2), not the HTTP bridge. The native host then starts the window drag. In a plain browser (no message channel) the call is a no-op, so screens carrying the attribute render unchanged in `--serve` mode. Only a primary-button press drags, and never one on a control inside the region (`button`, `input`, `select`, `textarea`, `a[href]`, an editable `[contenteditable]`) or inside a `data-cui-window-no-drag` subtree: the native host takes the mouse once a drag starts, so the control would never receive its click. |
| `data-cui-window-no-drag` | On an element inside a `data-cui-window-drag` region: a press on it or inside it keeps its click instead of starting a window drag. For a clickable element the built-in control list does not cover (a menu trigger, a `role="button"` div). Read by the same `desktop` module listener. |


For the authoritative list, grep `data-cui-` in `core-ui/runtime/runtime.js`.
Adding a new attribute requires updating this table, adding a runtime
test, AND assigning it an owning fragment in
`core-ui/runtime/fragments.go` (see [§ Fragments](#fragments-runtime-composition)
below). The build fails on an unassigned attribute, so the map cannot
drift ahead of the source.

**Component-action attributes** (the compiled `data-action` family, distinct
from the `data-cui-*` runtime primitives above): `data-action="<name>"` on an
element inside a `[data-component]` binds the named compiled action to that
element's click (and `data-action-<event>` / `data-action-type` to
input/change/submit). `data-action-mount="<name>"` fires the named action
*once on hydration* (and again after each SPA nav), the hook a server-rendered
island uses to populate itself on load, since the other triggers are
user-event-driven. Any `data-param-*` on the element flows into the handler's
`params`. Covered by `core-ui/runtime/action_mount_e2e_test.go`. Markup inserted outside a navigation (an intercepted
route's overlay, an island swap, a widget's chrome) fires its mounts too,
each element once, through the `actionloader` module a page with compiled
actions loads (boot marks what its own pass fired). The module also
fetches the compiled action script of a component such markup brings (retrying one that
failed to load), then fires that component's mounts once it registers
(`core-ui/runtime/action_mount_inserted_e2e_test.go`,
`actionloader_inserted_e2e_test.go`). The host
compiles a screen's actions at `Mount` and again in `ValidateBoot`, which
`App.Start` calls after every plugin `Init`, so a screen a plugin registers
there gets them; a screen registered while the app serves does not.

**Response headers the runtime understands:**

| Header | Effect |
|---|---|
| `X-Gofastr-Push-State: <path>` | Apply via `history.pushState` after the RPC succeeds (URL update without re-fetch) |
| `X-Gofastr-Partial: true` | Body is a screen-partial (used by the cross-page nav path) |
| `X-Gofastr-Swap: <layer key>` | Names the layout layer the partial body renders BELOW (see `data-cui-layout-key`). The runtime swaps the matching `data-cui-layout-slot` cell and records the key on the cache entry so a replay swaps the same cell. Emitted when the navigation request carried `X-Gofastr-From` and the two routes share an addressable chain prefix; a key the DOM doesn't have (deploy skew) makes the runtime recover with a full-page load. Absent → the body is bare screen content for the whole `<main>`. |
| `X-Gofastr-Envelope: 2` | Set when the partial body is a fills envelope: the seed island, then the primary `<template data-cui-fill>` (addressed by the bare swap key, no hash), then one hashed template per kept-layer fill. Emitted only when the request carried `X-Gofastr-Fills: 2` and the render produced fills; otherwise today's body. |
| `X-Gofastr-Title: <text>` | Percent-encoded title: `decodeURIComponent` it, then set `document.title` after the partial swap. (It's encoded because HTTP header values are Latin-1; a raw UTF-8 title like `Docs — GoFastr` would otherwise arrive mojibaked as `Docs â GoFastr`.) An untitled screen's header carries the app name, the same title its full document gets (uihost; a partial that omitted it left the previous page's title and announcement in place). With no header at all (another server) the runtime keeps the current `document.title` verbatim: only the header is decoded, and a header that fails to decode is used as sent. |
| `X-Gofastr-Invalidate: <JSON string array>` | Evict entries from the SPA screen cache on a 2xx response (read on every mutation or navigation dispatch: RPC, widget RPC, nav partials, full-shell fetches, intercepted nav, toggle/optimistic actions, sortable reorders, never on poll replies). `"/orders"` drops that pathname **and** every cached query variant (`/orders?page=2`, …); `"/orders?page=2"` drops exactly that entry; `"*"` clears the cache. No prefix matching: `"/orders"` never touches `/orders/42`. Applied before `X-Gofastr-Location`, so a mutated-and-redirected response evicts first and the redirect target is fetched fresh. Set from Go with `ui.InvalidateScreens(w, paths...)` (accumulates like `AddToast`). |
| `X-Gofastr-Document: <URL>` | Load the URL as a page (`location.replace`) and swap nothing. uihost sends it with a 200 and no body when a screen policy redirects a runtime fetch to an absolute http(s) URL: a nav partial, or the whole-page fetch of a cross-chain navigation (told apart from a document load by `Sec-Fetch-Mode`). fetch follows a 303 itself, and a site that refuses CORS, an identity provider's login for one, fails it. Anything but an `http://` or `https://` URL fails the navigation instead. A relative target keeps `X-Gofastr-Location`. |

**Screen cache + invalidation.** The router keeps a 20-entry LRU of
rendered screens keyed by `pathname+search` (the initial page included)
so back/forward is instant. Eviction never re-renders the visible page.
An RPC that changed what the *current* screen shows should return island
HTML or use `data-cui-rpc-navigate`; the header exists for screens you
are **not** on (an admin action that stales `/pricing`, a create that
stales every page of a list). The JS mirrors are
`__gofastr.invalidate(...selectors)` (same selector rules as the header)
and `__gofastr.refresh()` (re-fetch and re-render the current screen,
bypassing the cache). A forced fetch (`refresh()`,
`navigate(path, {force: true})`, `data-cui-rpc-navigate`) joins a
navigation already fetching the same path, never one replaying a cached
or prefetched copy: that copy is what the force exists to replace. Scope is per tab: the header only reaches the tab
whose request carried it, and an evicted entry costs nothing until that
tab actually navigates. Surfaces that must stay fresh across tabs
belong on the polling rung (`data-cui-poll`), not on cache eviction.
The embed composition ships no nav fragment, hence no cache, so the
header is a no-op there by construction.

**Cache entry layering and whole-document navigation answers.** A cache
entry records the layer key its HTML renders below, and a replay swaps
exactly that content cell. Entries keyed shallower than a later
navigation's swap boundary would destroy kept-layer DOM on replay, so the
runtime **re-captures the leaving page's entry at each navigation's swap
boundary** before the first DOM write (the boot capture starts keyed at
layer 0; a fetch re-keys it deeper as needed). The capture is dropped when
the DOM does not show the named page (a superseded navigation's origin
mismatch), so a mid-flight drop cannot poison the dropped destination's
entry; the route seed a replay restores comes from the tracked live route
state, not the first-paint head island (which is stale after the first
SPA navigation). That leave capture is the envelope module's. A module
that loads only after the plain navigator has swapped the DOM cannot tell
which page the DOM shows, so it takes no boot capture and keys nothing
until its own first navigation answers OK. The plain
navigator, which runs until a page with an outlet or area loads that
module, narrows the
replay instead: an entry keyed above the navigation's deepest shared
layer applies only that layer's cell of the entry, with the entry's
page-head carrier and seed island, so Back from `/group/b` to the
visit's first page `/group/a` keeps the group layout's DOM.

When a navigation response is a **full HTML document** — a static host
that serves whole pages and ignores the request headers — the runtime
reads it as an envelope instead of taking only `<main>`: the swap
boundary is the deepest layer key the live DOM and the fetched document
both carry (the same rule the server applies to `X-Gofastr-Swap`), the
document's cell for it is the primary, and every `data-cui-outlet` /
`data-cui-area` outside it in the document is a fill applied through the
same envelope path. Any miss falls back to the whole-`<main>` swap.

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
targets, unknown routes, `data-cui-rpc` anchors, clicks a listener
that ran first already `preventDefault()`ed (a listener on the anchor
or any element below `document` runs before the router's delegated
listener, and the router then leaves the click alone), and links to
the current path. A link to the current path with a `#fragment` falls
through to native hash behavior; one without a fragment, including a
repeat click on a link whose navigation is still loading, is
suppressed and does nothing, never a whole-document reload.

**Widget lifecycle events (`fui:widget-open` / `fui:widget-close`).**
The widgets module dispatches `fui:widget-open` on `document` at the
end of `mountWidget`, once the root is in the DOM and wired: once per
mount on both chrome paths (lazily fetched chrome appended to
`<body>`, SSR-inlined chrome hydrated in place), again on every
re-open after a close, and again when a swap re-inserts a root (see
the widget DOM lifetime below). `detail` carries `{ name, root,
hydrated, reinserted }`: `root` is the `[data-cui-widget]` element
itself, `hydrated` mirrors the mount path, and `reinserted` is true
only on the post-swap re-insertion. `fui:widget-close` fires with
`{ name, root }` from the dismiss path, the single funnel for
`closeWidget`, the chrome's own close button, backdrop clicks, and
deep-link stripping, before the root is hidden or removed, so a
listener can match `detail.root` against what it bound. The `fui:`
prefix is the widget-UI family on `document`; the `gofastr:` prefix
stays reserved for the window-level navigation/SSE events. This is
the supported way to bind into widget chrome when it appears
(per-language strings, section selects) instead of a whole-document
MutationObserver.

**Active-link highlighting.** After every navigation (and at module
load) the idle-loaded `activelink` module walks every `nav a`, plus any
`<a>` carrying `data-cui-match-prefix` or `data-cui-activelink` outside a
nav, with an `href` and tags the one matching the current path with
`aria-current="page"` and the `active` class — the value is `page`, the
ARIA-correct value for a page link, NOT `true`, so
`[aria-current="true"]` selectors do not match it. A widget root
announced by `fui:widget-open` gets the same sweep, scoped to that
root: the sidebar's phone drawer is fetched after load and rendered
with no current path, and no navigation follows its mount. Clearing is
ownership-based: a link loses both when it carries the module's own
`.active` class, the `data-cui-activelink` handover (`headless.Sidebar`
marks every leaf with it) or the `data-cui-match-prefix` handover. The
handover is what lets a server-rendered first-paint mark be moved by a
module that loads idle: a navigation can land before the module ever
stamped the old link, and without the attribute the stale mark
survived beside the fresh one. The `data-cui-activelink` handover
applies to navigations only: boot's navigate listener records one in
`__gofastr._navigated`, and a load-time sweep on a document with none
stamps matches without clearing those leaves, because the server's
mark is still the truth there (`Active` on a detail page or on a URL
with a query). A link with none of the three
(pagination's `aria-current="page"`, server breadcrumbs, hand-set
state) keeps whatever it carries. Left completely untouched
(neither set nor cleared): href-less links (server-managed), links
inside a rail nav (`data-hui-rail`, whose links carry `.is-active`,
set by the registered `headless-rail` module, never `activelink`'s
`.active`, so the sweep's strip branch never touches them), and links
carrying `data-cui-activelink-skip` (the opt-out for a current-state
owned by app code or a hand-set attribute). `data-cui-match-prefix`
opts a link into segment-prefix matching, and its VALUE (when
non-empty) names the prefix — the sidebar emits its `MatchPath` there
(see the attribute table): `/docs` lights up on `/docs` and
`/docs/getting-started`, never on `/docs-old`.

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

On a non-2xx response, `dispatchRPC` broadcasts the auto-built error
object `{ok:false,status,text}` into the named signal. `html`-mode
regions **skip the DOM write** for that non-string value (their content
is trusted HTML by contract; overwriting it with a JSON blob would
corrupt the region on every failed RPC). `text`-mode regions render a
human-readable "Error: <status> — <text>" line. This is the
optimistic-UI invariant: a failed mutation leaves the bound region
exactly where it was.

### Fragments (runtime composition)

Every `data-cui-*` attribute is owned by exactly one **fragment**: the
unit the runtime composer (spec: `SPEC-runtime-composer.md`; declaration:
`core-ui/runtime/fragments.go`) includes or omits per composition. The goal
is the same as the component-CSS pipeline: ship only the JavaScript a page
needs. `fragment_map_test.go` fails the build if any attribute in the runtime
sources lacks an owner, or any declared owner has drifted out of the source.

Two kinds of fragment, and the distinction is a safety rule, not an
optimization knob:

- **Marker** (`rpc`, `signals`, `compute`, and every `src/*.js` demand
  module). Behavior is triggered by a `data-cui-*` marker in the DOM, so a
  composition that omits the fragment self-heals: the kernel's
  `_scanForModules` sees the marker and demand-loads it. Safe to omit by
  default; a resolution miss cannot strand a marker.
- **Boot** (`kernel`, `nav`, `widgets-boot`, `sse`, `boot-embed`). These
  install listeners or fetch at boot with no marker to recover from:
  `nav`'s `<a>` hijack, `widgets-boot`'s `/__gofastr/widgets` catalog fetch,
  `sse`'s `EventSource`. Omitting one is a deliberate, manifest-driven SSR
  decision (exactly as `intercept` already is at `_scanForModules`). A boot
  fragment can NEVER be made marker-driven: that is the silent-failure
  case, a button that does nothing with no error. `kernel` is always
  present (the substrate: module loader, the `data-cui-comp` CSS scanner,
  doc state, `window.__gofastr`).

Ownership, not reference, decides the map: an attribute belongs to the
fragment or module whose code implements its handler. The ~55 attributes
that appear in BOTH `runtime.js` and a `src/*.js` module are core's
load/dispatch glue (`_scanForModules`, `dispatchRPC`'s widget-scoping
reads), not ownership. Registered behaviours (`registry.RegisterBehavior`,
"Component behaviour" below) are outside this map on purpose: they own
their own prefix or — for the ones that bind documented `data-cui-*`
wiring, like the optimistic/toggle action adapters and the lightbox —
attributes a registered source reads, not anything in this package;
the scanner learns their markers from the behaviours block rather than
from the table. An attribute owned by a `src/<name>.js` module maps
to that module. `data-cui-compute` is the one overlap to note: it is owned
by the `compute` core fragment (which step 2 extracts from today's
`src/compute.js`), not by the module of the same name.

### Global document state (`__gofastr.doc`)

Persistent state on `<html>`/`<body>`, meaning attributes, classes, and
runtime-created body children, goes through ONE module: `doc`, defined
at the top of `runtime.js` and exposed as `window.__gofastr.doc`. Its
frozen `MANIFEST` enumerates every allowed name; the table below mirrors
it and `core-ui/runtime/doc_manifest_test.go` fails the build when they
drift (hard rule 5 applied to document-level state). Writes outside the
manifest still land (the guard never breaks the page) but emit a
`console.warn` naming the offender.

The API: `setHtmlAttr(name, v)` / `removeHtmlAttr(name)` for `<html>`
attributes; `bodyClass(name, on)` for `<body>` classes;
`lockScroll(owner)` / `unlockScroll(owner)` / `scrollLocked()`: an
owner-refcounted viewport lock (a `Set` of owners; the
`documentElement.style.overflow` lock releases only when the LAST owner
unlocks, so a lightbox over a modal, or any second locker, can't release
the lock early); `singleton(id, factory)`, which returns the existing body
child with that id (SSR-provided elements are adopted without invoking
the factory) or creates and appends it once; `appendBody(el)` for
non-singleton body children (widget chrome, backdrops); and `reattach()`,
which re-appends any runtime-created singleton that lost its parent, called
by the SPA cross-chain swap after it replaces the layout shell.

| Surface | Name | Writer | Consumer |
|---|---|---|---|
| `<html>` attr | `aria-busy` | core runtime during an in-flight SPA-nav fetch (`doc.setHtmlAttr`), removed when the nav settles. The same window also marks every outlet/area of the kept layers plus the swap slot with `aria-busy="true"` (`loadPage`'s busyMarks), cleared on apply/failure | CSS can show a progress strip via `[aria-busy="true"]`; assistive tech hears "busy" per region; `frameworkBuiltinCSS` dims the marked regions (with a transition delay so fast responses never flicker) |
| `<html>` attr | `data-color-scheme` | `colorscheme.js`, the separate SYNCHRONOUS `<head>` bootstrap (plus the theme toggle via `window.__gofastr_colorScheme.set`). It must stay a separate sync script so dark tokens apply before first paint (FOUC); it runs before `runtime.js` exists, so it writes directly. Enumerated in the manifest as documentation | every `--color-*` token block; `<meta name="color-scheme">` mirrors it for UA controls |
| `<html>` attr | `data-cui-os` | core runtime at boot (`doc.setHtmlAttr`) | `framework/ui.ShortcutHint` CSS picks ⌘ vs Ctrl glyphs |
| `<html>` attr | `data-cui-static` | the static exporter (`framework/static.Builder`), server-side only. The runtime never writes it. Enumerated as documentation | runtime static-mode guards read it at boot |
| `<html>` attr | `lang` | core runtime after an SPA swap (`doc.setHtmlAttr`), copying `data-cui-lang` off the swapped payload's outermost layer; the initial value is server-rendered by the app on `<html lang>` | `documentElement.lang`: screen readers pick pronunciation rules from it (WCAG 3.1.1), indexers pick the language index |
| `<body>` class | `cui-sse-down` | NOT written by core-ui (the per-widget SSE block that owned it is gone). Kiln's dev-mode reload client (`kiln/live/reload.go`) toggles it on its EventSource `error`; app surfaces should read `window.__gofastr.sseStatus` / `pollStatus` instead | CSS connection-state styling (kiln panel dot) |
| `<body>` class | `cui-sse-up` | same as `cui-sse-down`: kiln dev-mode reload client only | CSS connection-state styling |
| `<body>` singleton | `cui-backtotop-sentinel` | backtotop module (`doc.singleton`): one shared scroll sentinel for every BackToTop button | its own IntersectionObserver |
| `<body>` singleton | `cui-nav-toast` | core `_showNavToast` (`doc.singleton`): nav-failure / static-mode notices | user-visible mini toast; styled by `.cui-nav-toast` in `frameworkBuiltinCSS`; e2e tests target `#cui-nav-toast` |
| `<body>` singleton | `cui-toast-fallback` | core `_fallbackToast` (`doc.singleton`): the degraded, unstyled toast region used when the toasts module fails to load. Distinct from the styled toast stack by design | user-visible fallback notices; carries `data-cui-toast-fallback` for tests/CSS |

The viewport scroll lock (`documentElement.style.overflow`) is also
owned by `doc` but is keyed by owner, not name: the widgets module locks
with `widget:<name>` per mounted modal; the `panehost` module locks
with `panehost:<host-id>` while a side pane is open in overlay-drawer
mode. Any new surface that needs a scroll lock (lightbox zoom, drawer)
calls `lockScroll`/`unlockScroll` with its own owner token instead of
touching the style property.

Deliberately NOT wrapped: transient DOM that exists only within one
synchronous operation (the copy module's clipboard `<textarea>`), and
pure reads (`#cui-route-announce` is SSR-provided; the runtime only
writes its text). Per-widget body children (chrome, backdrops) are
transient per-widget elements, not singletons. They go through
`doc.appendBody` and are removed on dismiss.

### Pane-host layout (`framework/ui.PaneHost`)

`PaneHost` is a layout shell: an always-visible **primary** pane plus
one or two openable side panes (`secondary`, `tertiary`). It owns the
PANE LIFECYCLE (show/hide, focus handoff on open, focus restore on
close, and a responsive collapse), not content loading. SSR emits the
primary plus any side panes; a side pane closed at first paint carries
`hidden` and the host carries an open modifier class per open pane, so
first paint matches server state (Hard Rule 6) and the CSS grid columns
derive purely from those classes, no inline style.

Open/close/swap is in-page state, never a URL route (Hard Rule 1).
Triggers are attribute-driven (the pane-open-control / -close /
`-swap`); app code can also drive panes through `__gofastr.openPane` /
`closePane` / `swapPane`. To fill a pane from a link, use the EXISTING
`data-cui-rpc` + `data-cui-rpc-signal` rail broadcasting into a
`data-cui-signal` + `data-cui-signal-mode="html"` region inside the
pane. `PaneHost` does not fetch.

URL round-tripping is opt-in per host: set `PaneHostConfig.DeepLinkParam`
and label each trigger with `interactive.PaneKey`. Opening writes
`?<param>=<pane>:<key>`, closing strips it, and Back, a refresh and a
shared link replay the state by re-clicking the trigger with that key
for that pane, so the RPC that fills the pane runs again and the content
comes back with it, not just the open column. This
does not make a pane a route: the parameter records in-page state so
refresh and share reproduce it, which is the same thing widget deep
links do for modals. Because the server owns first paint, read the
parameter with `ui.PaneDeepLink` and set `SecondaryOpen`/`TertiaryOpen`
plus the pane's content from it; skip that and a shared link paints the
pane closed, then opens and fills it after hydration.

### Intercepting routes (`app.InterceptFrom`)

One detail screen, two presentations. `/products/42` is an ordinary
page registration and stays the canonical render: hard load, refresh,
external link, and any soft navigation from elsewhere all render it in
full. A soft navigation that started on the declared origin route
presents the same render as a drawer or sheet over that page, which
stays mounted underneath.

The split is decided on the SERVER. The runtime sends
`X-Gofastr-Intercept: 1` plus `X-Gofastr-From: <current location>`;
`Router.InterceptFor` re-resolves that origin against the route table
and agrees only when the target screen declared it. Agreement is
signalled by `X-Gofastr-Overlay: drawer|sheet`, and no header means the
client falls back to the ordinary `<main>` swap. `X-Gofastr-From` is
client-controlled and never trusted as an instruction: a forged value
can change the wrapper element and nothing else, because routing,
policy, params, `Load`, and content are identical on both paths.

The screen can tell which render it is in: `app.OverlayFromContext(ctx)`
reports the presentation (`ScreenDrawer` or `ScreenSheet`) inside an
overlay render and nothing in the canonical one, so a screen draws a
layer's chrome (`ui.DrawerBar`: close, path, copy link) only where it
is a layer. A drawer is `--ui-intercept-drawer-w` wide, by default half
the viewport between 480px and 720px; each layer's inset is
`--cui-intercept-pad`, which chrome reads to bleed to the layer's edges.

Costs stay off pages that don't use it. The route manifest carries
`intercept: {from, also, as}` per route; core loads the `intercept` demand
module only when some entry has one, and the UI host injects
`app.InterceptOverlayCSS()` under the same condition.

Intercepted HTML never enters the screen cache. The cache is keyed by
path and holds canonical page renders, so storing an overlay variant
would poison a later direct visit. Closing routes through
`history.back()`, so Back, Escape, the backdrop, and any
`data-cui-intercept-close` button are one code path and the page
underneath is never refetched.

**Stacking.** An intercepted link clicked INSIDE an open pane opens the
target as a NEW layer over the current one — the same route check as
the first open, with the layer the click happened in as the origin
(the target's `InterceptFrom` names the pattern of the screen it
stacks over). Each open is one `pushState`, inside the tab's history;
the router's `currentPath` deliberately stays on the page under the
stack, so the router's own popstate diff never refetches the list
underneath (the module claims those history moves first). At most
four layers (a record and three related records over it): a fifth
intercepted open is REFUSED with a warning toast, fetching nothing and
leaving the stack, the URL and every layer's content as they were, so
no unsaved edit is discarded to make room; the cap keeps the DOM, the
history and the drawer chrome bounded while the deep link of every
layer stays a full page. `app.InterceptOverlayCSS()` overlaps stacked
drawers at the inline end, each a step (`--ui-intercept-stack-step`)
narrower than the layer under it, and has every layer cast the scrim
over the layers under it, so the lower layers show as strips that dim
one step per layer above them. Lower layers stay in the DOM untouched (their form inputs
keep unsaved edits) and are `inert` + `aria-hidden="true"`; only the
top layer takes focus and the Tab trap (a modal widget opened inside a
layer keeps its own trap; an open dropdown or overlay-mode pane host
consumes Escape first). Back closes exactly the top layer and restores
focus to the control that opened it; Esc, the backdrop and the close
control close the top layer only; a refresh or cold load of the top
URL renders it full page as ever; Forward does what it does for a
single layer today — no layer re-opens, the URL loads as a plain page.
A link inside a pane that changes only the pane's own query
(`?sort=…&page=…`) re-renders INSIDE the pane — the pane's URL gains
one history entry per click, the same contract a list at top level
follows — and Back/Forward walk those query states within the pane. A
form in a layer carrying `data-hui-leave-guard` asks before any of
these moves discard its edits, and only then: opening a related record
over a changed one discards nothing and does not ask; a declined Back
is repaired with a re-push that restores the URL and history.length.

**More than one origin.** `Intercept.AlsoFrom` names more patterns the
overlay opens over, beside `From`: a create screen opens over its list
and over each related record whose Related tab adds to it. The manifest
carries them as `also`, and the server and the runtime accept any of
them. The route check matches the link's path alone, so a target with a
query (`/invoices/create?prefill_customer_id=7`) intercepts like one
without.

**Returning.** An overlay render knows the page it opened over:
`app.OverlayOriginFromContext(ctx)` returns that location's path (the
query and fragment cut off; anything but a rooted local path returns
""), and "" in a canonical render. A form in the overlay names it as
its success navigation, and the rpc module asks the intercept module
first (`__gofastr._interceptReturn(path, node)`). When the saving node
sits in the top layer and the destination's path is a layer's own (the
top one or one under it) or the page under the stack, the stack RETURNS
there instead of leaving: the layers above it close, their history
entries consumed in one `history.go`, and it re-renders in place at its
current URL (a layer refetches its overlay variant, the page under the
stack refreshes through `__gofastr.refresh()`), so the saved row shows
and Back walks the stack as it was before the open. A save in a record
pane that names the record keeps the pane and refreshes it. The leave
guard asks about what the move discards: the layer re-rendered and
every layer above it, or the whole document when the page under the
stack refreshes. Any other destination is a real navigation, as before.

Choosing between the three overlay tools: an intercept is for a detail
with its own canonical page; a widget deep link
(`?modal=…`) is for an overlay that is the only view of that content;
pane-host `DeepLinkParam` is for a region of the current page that
should survive a refresh.

Responsive collapse: when `matchMedia('(max-width: 768px)')` matches
AND a pane is open, the registered `headless-panehost` module sets
`data-hui-pane-mode="overlay"` on the host. CSS repositions the open
pane as a fixed right-edge drawer with a backdrop scrim (`::before`);
the module applies a focus trap (Tab cycles within the pane, reusing
`NS._focusSel`), a refcounted scroll lock (`doc.lockScroll('panehost:<id>')`),
and ESC / backdrop-click-to-close. Widening the viewport or closing the
pane clears overlay mode and releases the lock. The 768px breakpoint
literal MUST stay in sync between this module and the `ui-pane-host`
CSS.

### Shared state: the signal store (`core-ui/store`)

The signal bus is stringly-typed and starts empty. `core-ui/store` layers a
typed, server-declared API on top and **seeds initial values
into the client store at SSR**, so `getSignal` returns the server value on
first paint instead of `undefined`. A `Slice[T]` is a *renderer*: declaring
it registers a seed, and its `Bind`/`BindAttr`/`BindHTML` helpers emit both
the `data-cui-signal` attribute and the resolved value from one source (no
SSR/store drift). The model is **producer → signal → consumers**: an
island/widget owns a value and `Publish`es updates through the existing
`data-cui-rpc-signal` path; presentational consumers `Bind` to it and update
client-side with no per-consumer round-trip.

- **Seeding.** The host scans the rendered page for referenced signal names
  (plus all app-global slices) and emits one inert
  `<script type="application/json" id="gofastr-signals">{name:value}</script>`
  (same CSP-safe data-island pattern as `gofastr-routes`/`gofastr-catalog`).
  `runtime.js` reads it on boot and seeds `_signals` **before hydration**.
- **Scope.** Page-scoped slices seed only on pages that reference them;
  app-global slices (`.Global()`) seed on every page and survive SPA nav.
- **SPA-nav merge.** Partial responses carry a scope-split
  `#gofastr-signals-partial` island; the runtime applies page-scoped values
  unconditionally but **never clobbers an app-global the user already
  mutated** (it only seeds a global the first time it is seen).
- **Whole documents.** A full page's flat seed mixes scopes, so the host
  also names its page-scoped keys in an inert
  `<script type="application/json" id="gofastr-signals-page">[name,…]</script>`.
  The navigator turns those keys into the same partial-shaped island and
  puts it in the boot cache entry and in any fetched document it swaps
  in, so Back to the first page of a visit and a cross-chain or
  full-answer navigation restore page state as a partial does. A host
  that sends no list restores none. The envelope navigator follows the
  same rule, and a page it re-captures on leave keeps the seed it
  arrived with, so a cached Back or Forward on either navigator
  restores the values that page arrived with.
- **Computed.** `store.Computed` derives a value client-side from dependency
  slices via a host-registered JS reducer (CSP-safe, no `eval`). Register
  reducers on `window.__gofastr._reducers` from a script loaded **after**
  `runtime.js` (e.g. via `WithExtraScripts`). The runtime is now composed
  from fragments (`core-ui/runtime/frag/*.js`, see [§ Fragments](#fragments-runtime-composition)):
  the `kernel` fragment assigns `window.__gofastr` as a fresh object during
  boot, and `rpc`/`signals`/`nav` extend it via `Object.assign`. A
  `_reducers` property set before the IIFE runs is therefore replaced.
  Register after `runtime.js` so it survives. (The contract is unchanged
  from the pre-split "assigns wholesale" wording; the reason is the same:
  the kernel fragment's `window.__gofastr = {…}` line is the fresh-object
  assignment that overwrites anything set earlier.)

See `framework/docs/content/signal-store.md` for the full guide.

### Runtime strings (`#gofastr-strings`, `__gofastr.say`)

The words the runtime says itself (a failed request's toast, a
navigation that could not load, the embed frame's error, a local
entity's validation messages) are `i18n.Phrase`s registered with
`runtime.RegisterStrings`. The host writes the ones the request's locale
translates, and only those, into an inert
`<script type="application/json" id="gofastr-strings">{key:text}</script>`
on full pages and in the embed shell; an English page carries none. The
id is reserved: `registry.DataBlock` refuses it.

The kernel's `say(key, english, params)`, exported as
`window.__gofastr.say`, reads the block once, falls back to the English
argument on a miss, and fills `{{name}}` placeholders from `params`.
Fragments call `say(...)`; modules and behaviours call `NS.say(...)`.
Never write prose straight into a toast, `textContent`, `aria-label` or
`title`, nor a unit glued to a value (`ms + 'ms'`):
`core-ui/check.LintRuntimeProse` refuses it, and
`TestRuntimeSayCallsMatchPhrases` holds every `say('key', 'English'`
call to a registered phrase with the same English. A client-side
navigation keeps the block it booted with, so a reader who switches
locale mid-session sees the old runtime words until the next full load.

### Locale prefixes (`#gofastr-locales`, `__gofastr_locales`)

An app built with `framework.WithLocalePrefixes` serves `/es/pricing` as
the `/pricing` screen in Spanish. The router strips the prefix before
matching, so the server's route table, the manifest
(`window.__gofastr_routes`) and the screen cache all hold unprefixed
paths. The host writes the locale set into an inert
`<script type="application/json" id="gofastr-locales">{"default":"en","prefixes":["es"],"base":""}</script>`
on full pages (`base` is a static export's mount path, which sits in
front of the prefix); the kernel reads it once into
`window.__gofastr_locales`. The id is reserved: `registry.DataBlock`
refuses it.

Three kernel helpers, exported on `__gofastr` for modules, carry the
rules:

- `_unlocalize(path)` strips a prefix. Route lookups (`routeEntry`, the
  preload and intercept route matchers, the envelope's route info) and
  cache invalidation compare unprefixed paths, so a `/es/` link resolves
  to the same route and the same cache entry family as its English
  twin.
- `_localeOf(path)` names a path's locale (`''` for the default). A
  navigation whose destination locale differs from the current page's
  crosses a document boundary (`crossesDocBoundary`): the whole
  document loads, so `<html lang>`, the shell and the runtime strings
  change with it.
- `_lp(url)` puts the current page's prefix on a root-relative URL that
  has none. Every runtime fetch goes through it: the action endpoint,
  widget discovery and chrome, poll and RPC URLs, the action module and
  eager preloads. The server then answers in the reader's locale, and
  the `page` the action endpoint receives is the unprefixed path.

The server side of the same contract rewrites root-relative URLs in a
prefixed HTML response (`core-ui/urlprefix`: `href`, form actions and
the URL-valued `data-cui-*` attributes listed in `urlprefix.URLAttrs`)
and the `Location`, `X-Gofastr-Location`, `X-Gofastr-Push-State` and
`X-Gofastr-Document` headers. Markup never spells a
prefix itself. A new URL-valued `data-cui-*` attribute joins
`urlprefix.URLAttrs`, or `TestURLAttrsAllCovered` fails.

The rewriter leaves an anchor with `hreflang` alone: it already names
its locale. An `<a rel="alternate" hreflang>` means "this page in that
language" (a language switcher, `ui.LanguageMenu`), so the active-link
module (`src/activelink.js`) re-points every one whose tag is in the
locale set at the current page after each navigation, at idle load and
when widget chrome opens: the default locale at the plain path, the
others under `/<tag>` after `base`. Chrome that outlives a swap then
never links the page the reader left. An alternate naming a tag outside
the set (another site's translation) and an `hreflang` link without
`rel="alternate"` keep their `href`. No attribute marks the switcher:
`rel` and `hreflang` are the HTML spelling of the contract.

### First-load order

A live page paints from its document and stylesheets alone. The one
script that blocks is the color-scheme bootstrap at the top of `<head>`
(dark tokens must apply before CSS parses). Every other script the host
emits (`manifest.js`, `runtime.js`, the per-screen action scripts, the
extra-script rail) carries `defer` and closes `<head>`, after the
stylesheets: none blocks the parser or first paint, the preload scanner
finds them in the first bytes, and they execute in document order once
parsing ends, so `manifest.js` globals are set when the kernel boots,
rail scripts see `runtime.js`, and a deferred script in the page's
`<body>` runs after all of them. A layout's own deferred script in
`<head>` sits before the host's and runs first, so it cannot read
runtime globals at execution time. Because `runtime.js` is deferred, its
initial pass (component CSS, demand modules, `data-action-mount`) waits
for `DOMContentLoaded`, after the action scripts that register the
mount handlers. Runtime-module preload hints carry
`fetchpriority="low"` so they warm the cache without competing with
the document and its stylesheets. The route graph rides in
`manifest.js` (`window.__gofastr_routes`, cached once per deploy) rather
than in every document; exported pages keep the inline
`#gofastr-routes` block because they must be self-contained. The graph
carries each route's loading content, which speaks the reader's
language, so an app with a translator builds one `manifest.js` body
and one route graph per catalog locale (keyed by
`i18n.CatalogLocale`, never by a raw request tag, so a forged
`X-Locale` cannot grow the cache). A page links its locale's body by
hash, and the handler serves whichever locale's body the `?v=` names,
since the script request itself carries no locale prefix. An app
without a translator keeps the one body, rendered under a bare context.
The PWA worker precaches every locale's `manifest.js`.
`framework/uihost/first_load_contract_test.go` pins the script and
preload rules, and `examples/site` holds its landing pages under one
round trip of compressed HTML.

### Extra scripts (the script rail)

`uihost.WithExtraScripts` adds external `<script defer src="…">` tags
at the end of `<head>`, after `runtime.js` and the per-screen action
scripts.
They are same-origin resources under the default CSP (`default-src
'self'`), so every URL on the rail must be a path the app itself serves;
the dev livereload client is the canonical example. The tags ship only on
full shell renders — SPA partial responses never carry them.

Options freeze at `New`, but plugins and batteries wire themselves in
`Init`, which runs after `Mount`. `(*uihost.UIHost).RegisterExternalScript`
exists for that window: it appends to the same rail, in
first-registration order, and is idempotent. `src` must be a same-origin
absolute path (`/x.js`), optionally with a query (`/x.js?v=abc`);
schemes, protocol-relative URLs, backslashes, control characters, and
`.`/`..` segments (raw or percent-encoded) are rejected. Registration
returns an error once the host has rendered its first full shell: a late
registration would ship on some pages and not others.

`uihost.ScriptHandler(js)` and `uihost.ScriptURL(path, js)` are the
serving half for host apps: mount the handler at `path`, register the
returned `path?v=<hash>` URL, and the response follows the same
strong-ETag + immutable-on-match policy as every `/__gofastr` script,
with the hash changing whenever the bytes do.

### Background compute (`core-ui/compute`)

Applications register self-contained worker JavaScript and WebAssembly bytes
at process startup with `compute.RegisterWorker` and `compute.RegisterWASM`.
Names use the same URL-safe grammar as runtime modules (1–64 lowercase
letters, digits, `-`, `_`). Registration copies the bytes and calculates a
full SHA-256 hash. Identical re-registration is a no-op; conflicting content
under the same name panics.

The UI host serves registered assets at content-addressed same-origin URLs:

```
/__gofastr/compute/<name>.js?v=<sha256>
/__gofastr/compute/<name>.wasm?v=<sha256>
```

Both responses are immutable for one year. SSR emits the inert
`#gofastr-compute-assets` JSON manifest beside
`#gofastr-runtime-modules`; the `compute` demand module uses it to build
versioned URLs. A page opts in with the single `data-cui-compute` marker.
That marker only loads the module. It does not declare a task or construct
DOM. Island/widget code calls `window.__gofastr.compute.task(worker, fn,
payload)`, `wasmURL(name)`, and `dispose(worker)`.

One classic Worker instance is cached per registered name. Requests use
`{id, fn, payload}` and responses use `{id, ok, result}` or
`{id, ok, error}`. Worker errors reject every pending request and discard
the failed instance. A request that receives no response for 30 seconds
rejects, terminates that worker, and lets the next call create a fresh one.
`dispose` applies the same termination/rejection behavior explicitly.

The document CSP still permits only same-origin worker scripts (no `blob:`).
Dedicated workers enforce the CSP on their own script response, so worker
assets receive the narrow `script-src 'self' 'wasm-unsafe-eval'` permission
required by current browsers to compile WebAssembly; JavaScript `eval` stays
disabled. Shared memory and WebAssembly threads require cross-origin
isolation (COOP + COEP). GoFastr does not enable COEP, so
`SharedArrayBuffer`/threads are outside this surface.

See `framework/docs/content/compute.md` for registration, protocol, and
WebAssembly authoring examples.

### SSE connection state (`__gofastr.sseStatus`)

The island-stream module (`runtime/src/sse.js`) is demand-loaded when
the document holds a **push target**: any `[data-island]` region (the
server can `PushUpdate` any island id, so every island counts —
presence rosters included) or the offline connection banner
(`[data-hui-system-offline]`, which reads the state this module
mirrors). The `<meta name="gofastr-sse">` tag every session-bearing
page carries means "SSE is available", never "open it": the stream
holds one of the browser's six HTTP/1.1 connections per host, so it
opens only while a push target is live and closes when the last one
leaves. The module re-evaluates the document at load, after every
navigation apply (cached replays included, through the per-module
scanner hook), when a deferred part lands (`gofastr:fill`), and on DOM
insertion; a reopen re-reads the meta, so the session parameter is the
current one (the navigator's `X-Gofastr-Session` rollover rewrites it).

The module mirrors its transport state onto ONE live object, mutated
in place so every reference (the NetworkRetryBanner, app code) sees
updates without re-reading:

| Field | Updated when |
| --- | --- |
| `window.__gofastr.sseStatus.connected` | `true` on EventSource `open`, `false` on `error`, when the last push target leaves the document, and on `pagehide` (the transport closes when the page hides, bfcache entry or unload, and reconnects on `pageshow.persisted`, so a navigated-away page never hoards one of the tab's ~6 per-host connections) |
| `window.__gofastr.sseStatus.lastEventAt` | every received `island` frame and on `open` (a `Date.now()` ms timestamp) |
| `window.__gofastr.sseStatus.retryCount` | incremented on each transport error, reset to 0 on `open` and on a deliberate close |

Connect/disconnect transitions also dispatch
`document.dispatchEvent(new CustomEvent('gofastr:sse-status', { detail: sseStatus }))`.
Per-frame updates only touch `lastEventAt` silently. No event is
fired per message, to avoid a notification storm. The `gofastr:`
prefix matches the existing `gofastr:navigate` convention. The
NetworkRetryBanner reads `lastEventAt` for its silence trigger and
listens for the event to re-probe its health endpoint on reconnect.

### Sequenced WebSocket client (`__gofastr.connectWebSocket`)

The `ws` demand module (`runtime/src/ws.js`) is the browser half of the
`core/stream.StateChannel` contract (see `framework/docs/content/`
→ reactivity and core-packages). Unlike every other module it has no
DOM marker: an application loads it explicitly with
`__gofastr.loadModule('ws')` and calls two functions.

`__gofastr.createSequencedReducer(initial, apply)` returns
`reduce(envelope, generation)`; it applies an envelope only when
`envelope.sequence` is strictly greater than the last applied one, so a
reconnect snapshot captured before a live mutation cannot resurrect the
state that mutation replaced. Pass the generation `onMessage` carries:
the first `type: 'snapshot'` envelope of a generation the reducer has
not hydrated from applies whatever its sequence (a restarted server or
another replica numbers from its own start), and every later envelope
of that generation is held to the strict rule. Without a generation
argument the strict rule applies to everything. Results carry
`{ applied, state, appliedSequence }`.

`__gofastr.connectWebSocket(url, opts)` manages one WebSocket with
reconnect and a distinct generation per reconnect:

| Hook | Fires when |
| --- | --- |
| `opts.onGenerationStart({generation, resumedAfterSequence})` | socket open, once per generation |
| `opts.onHydrated({generation, snapshotSequence})` | the application calls `handle.hydrated(seq)`, once per generation |
| `opts.onGenerationEnd({generation, reasonClass})` | the socket is gone, once per generation |
| `opts.onMessage({data, raw, generation})` | every message (`data` is parsed JSON or null) |

`handle.status` is one live object (same shape as `sseStatus`):
`{ generation, phase, reasonClass, attempts, lastSequence }`, with
phase `connecting → open → hydrated → resynced` (via
`handle.resyncComplete()`) and `closed`/`stopped` at the ends.
`reasonClass` is one of `closed`, `error`, `refused`, `stop` — the
module never logs and never surfaces raw close reasons, payloads, or
credentials. `refused` is a server that accepted the handshake only to
close with a code in 4400–4499 (4000 + a 4xx status, kept on
`status.refused`); it is not retried, because a browser cannot read
the status of a failed handshake and a refusal is not a blip. A
4500–4599 close (4000 + a 5xx) is `error` and retried. Retries back
off from 1 s, doubling to a 30 s cap; `attempts` returns to 0 only
after a socket has stayed open 10 s, so a server that accepts and
closes at once is not redialed every second.

A new generation invalidates only generation-bound work; which state
survives a reconnect is the application's decision. WebSocket recovery
proves nothing about media protocols layered on top (WebRTC and
friends); those resynchronize through these hooks.

### Browser-local databases (`__gofastr.localdb`)

The `localdb` module (`core-ui/localdb/localdb.js`) is the browser half
of `core-ui/localdb`: IndexedDB databases declared in Go, stored in the
visitor's browser, never on the server. It is not part of
`core-ui/runtime`: the package registers it as an on-request behaviour
(`registry.OnRequest()`), so only a binary that imports
`core-ui/localdb` carries it, and only a page that loads it with
`__gofastr.loadModule('localdb')` downloads it
(`framework/localentity`'s two behaviours declare `Requires("localdb")`).

The schema rides an inert `<script type="application/json"
id="gofastr-localdb">` data block (`registry.RegisterDataBlock`) that
the host puts in every page head (live and export mode) when any
database is declared. Only
declared databases, stores and indexes are reachable; anything else
rejects with a coded error. The browser-side IndexedDB name is
`gofastr.<name>`.

| Call | Does |
| --- | --- |
| `localdb.open(name)` | Resolves the database handle. The first open reconciles the stored schema with the declaration (below). |
| `db.get/put/add/delete/clear/count/list(store, …)` | One transaction each, resolved when it COMMITS. `put` on an `AutoKey` store mints a UUIDv7 key when the record has none. |
| `db.list(store, {index, only \| lower/upper, direction, offset, limit, keys})` | Reads in key or index order through a cursor. Ordering is IndexedDB's, never page script sorting an array. |
| `db.tx(stores, mode, fn)` | One transaction over several stores, all-or-nothing: a throw inside `fn` aborts it and nothing lands. |
| `db.watch(store, fn)` | `fn({db, store, origin, changes})` after every committed write, from this tab (`origin: "local"`) or another tab of the origin (`"remote"`). Returns the unsubscribe. |
| `localdb.persist()` / `persisted()` / `estimate()` | The `navigator.storage` eviction request and usage figures. `false` and `null` are answers, not errors. |
| `localdb.newID()` | The UUIDv7 minter `AutoKey` uses: monotonic within a tab. |

Schema changes are **additive and automatic**. A page whose declaration
names a store or index the stored database lacks bumps the IndexedDB
version itself and creates it. Nothing is ever deleted or rebuilt: a
tab still running the previous deploy may need what the new one
dropped, and an older page opening a newer database finds a superset
and works. A store whose primary key path changed, or an index whose
definition changed, rejects `schema`: rebuilding it in place would undo
the other deploy's version whenever tabs on two deploys meet, so the
new shape takes a new name. When another tab upgrades, this
tab's connection closes on `versionchange` and the next operation
reopens it; `document` sees a `gofastr:localdb` event
(`detail: {type, db}`, type `versionchange`, `close` or `blocked`).

Cross-tab notification rides one `BroadcastChannel` per database
carrying store names, ops and keys, never record values; the receiver
re-reads what it needs. Any same-origin script can post on that
channel, so a message naming an undeclared store or an unknown op is
dropped whole. Errors carry a stable `code` (`unsupported`,
`unknown-db`, `unknown-store`, `unknown-index`, `invalid`,
`constraint`, `quota`, `schema`, `version`, `closed`, `aborted`,
`failed`) and drop the browser's message; the module never logs.

### WebRTC rooms (`__gofastr.connectRoom`)

The `rtc` demand module (`runtime/src/rtc.js`) is the WebRTC half of a
`core/stream.StateChannel` signaling server (`battery/rtc`,
see `framework/docs/content/rtc.md`). Like the `ws` module it has no DOM
marker: an application loads it with `__gofastr.loadModule('rtc')` (it
loads `ws` itself) and calls `__gofastr.connectRoom(url, opts)`, which
owns one `RTCPeerConnection` per remote peer with perfect negotiation,
trickle ICE, and negotiated data channels. The hooks are `onPeer`,
`onPeerLeave`, `onTrack`, `onChannel`, `onMessage`, `onPeerState`,
`onStatus`, `onSnapshot`, and `onPhase` (the `ws` generation phases
collapsed to one bounded string; `closed:refused` is final and carries
the status on `room.status.refused`). `room.replaceTrack` swaps a
sender's track without renegotiation and keeps the list new
connections are built from current; `room.send` passes binary through. The newer peer of any pair is the
polite one and waits for the older peer's first offer, so the initial
negotiation rarely collides; when two first offers still cross, the
polite side rebuilds its never-negotiated connection instead of rolling
back, and rollback is reserved for mid-call collisions. Offers are
burst-limited per connection. The generation rule on reconnect: peers
absent from the new snapshot close; peers still `connected`/`connecting`
keep their peer connection (no renegotiation storm), and the negotiated
data channels the remote's rebuild closes are recreated on it; anything
else is rebuilt with tracks and channels re-added, and the remembered
status is resent. A reconnect that lands under a new self id (the
battery mints one per socket when `Join.PeerID` is empty) rebuilds
every peer connection: each remote saw the old id leave. A status carried by a snapshot or join fires
`onStatus` like a live one. The ICE list a snapshot or the server's
half-TTL `iceServers` push carries is applied to every live connection
(`setConfiguration`), so a kept connection can restart ICE after its
first TURN credential expired.
Signals are addressed and transient, so they bypass
`createSequencedReducer`; snapshot, join, leave, status, and
iceServers go through it. Like `ws`, the module never logs: no SDP, candidate, credential, or
close reason reaches the console or a status object.
### Desktop bridge (`__gofastr.desktop`)

The `desktop` demand module (`runtime/src/desktop.js`) is the browser
half of `battery/desktop`'s typed bridge. Like `ws` it has no DOM
marker and no new `data-cui-*` attribute: the desktop host injects
`window.__gofastr_desktop` at document start, and the host-served
generated `bridge.js` layer loads the module explicitly and installs
one bracket-keyed namespace per registered capability
(`__gofastr.desktop.clipboard.writeText({text})`). The transport is a
single POST to the `/__gofastr/desktop/call/{cap}/{method}` chokepoint
owned by the battery (never the SSE bus, never a bespoke
EventSource); `sse.js` only ever carries island frames. Native events
flow the other way through `__gofastr.desktop._dispatch(name,
payload)` (the battery evaluates that call in the window), with
`on`/`off` listeners on top; island refreshes after a native event
stay server-driven through `island.Manager.PushUpdate` as today. In a
plain browser `__gofastr_desktop` is absent, `available` is false, and
every `call` rejects `{code:"unsupported"}` without a network request.
See `framework/docs/content/desktop.md`.

The `formerrors` demand module (`runtime/src/formerrors.js`) is the
failure half of a `data-cui-rpc` form submission. `rpc.js` loads it the
first time a form's request answers non-2xx and hands it the form, the
status, and the body; the module renders the server's validation
envelope (`{error, fields: {name: [messages]}}`, or the nested
`{error: {message, fields}}` that `core/handler.WriteError` writes for
every typed handler) beside each named
field's control, by the headless hooks `framework/ui` renders for a
server-side error (a `data-hui-field-error` node inside the
`data-hui-field` group, filled or reserved; the next sibling of a bare
`data-hui-choice` label) and never by a kit class, and toasts the
`error` text when no field matched. It has no DOM marker and owns no
attribute of its own; the happy path never loads it.

### Cross-replica presence (`gofastr.presence` fanout lane)

Presence rosters aggregate across `serve` replicas over the existing
`core/fanout` transport, on a dedicated `gofastr.presence` topic parallel
to the `gofastr.islands` invalidation lane: same transport, disjoint
topics, disjoint payload shapes. Invalidations stay event-style and
lossy; presence is lossy *self-healing state*: each replica broadcasts
its full local roster per topic on join/leave and on a 15 s heartbeat,
and receivers keep a TTL'd (45 s) remote table, so a dropped message
heals on the next beat and a crashed replica's members vanish within
TTL. `Manager.stop()` publishes an empty roster synchronously first, so
rolling restarts converge promptly without waiting out the TTL.

The contract points that must not drift:

- `Manager.SetFanout` wires BOTH lanes in one call; the framework's
  existing duck-typed `SetFanout` plumbing reaches it with no new glue.
- `PresenceRoster(topic)` returns the merged local ∪ live-remote roster,
  deduplicated by identity; with no fanout configured it is
  byte-identical to the single-replica result.
- Announcements carry ONLY what `PresenceRoster` already exposes
  (`UserID`, `DisplayName`), never session ids. There is still no
  ungated HTTP roster endpoint.
- Remote replicas are capped per topic (512) so a forged-replica-id
  flood cannot grow the table unboundedly.

### ui.node.v1 render path (third-party process modules)

A process module (framework #37) never emits HTML/CSS/JS or `data-cui-*`.
It returns a `ui.node.v1` JSON tree; `core-ui/uinodev1.Validate` produces a
typed `*Tree`: closed component enum, typed scalar props (no
`map[string]any`), host-relative URL-guarded, depth/size-capped. Then
`framework/uihost/uinoderender.Renderer` maps that tree to `framework/ui` +
`core-ui/html` primitives and assigns **every** id/class/ARIA/variant and
every `data-cui-rpc` URL itself. The module supplies none. ActionRefs
resolve host-side through an injected resolver (actionRef → installed route
id → namespaced URL); an unknown ref fails the whole render closed rather
than emitting a guessed URL.

This is deliberately NOT `core-ui/noderender` (a *denylist* for first-party
IR, through which a third party could forge the trusted `data-cui-*`
attributes `runtime.js` acts on via its `extraAttrs` passthrough). The
closed wire type makes that forgery **unrepresentable**, not merely denied:
`Bindings`, `Actions`, and free prop bags have no place to live in the
typed tree. A validated tree may never panic the renderer: the two named
hazards (`html.Section` requiring a label, `ui.DataTable` panicking on empty
columns) are guarded, not `recover`'d, so a real component bug surfaces
rather than hides.

### PWA surface (`uihost.WithPWA`)

Opt-in installable-app support lives in `framework/uihost` (see
`framework/docs/content/pwa.md` for the user guide). The contract with
the rest of this document:

- **Chrome injection.** `WithPWA` adds `<link rel="manifest">` (+ a
  `theme-color` meta when configured) to `<head>` and an external
  `<script defer src="/__gofastr/pwa/register.js">` on the script rail:
  no inline JS, same CSP posture as `runtime.js`. Routes mounted:
  `/manifest.webmanifest`, `/service-worker.js` (root-scoped),
  `/__gofastr/pwa/register.js`, `/__gofastr/pwa/offline`. With locale
  prefixes, a locale page links its own manifest (`/es/manifest.webmanifest`)
  whose `start_url` carries the prefix; `id` and `scope` stay the same,
  so every locale installs one app.
- **The service worker never interferes with the runtime's rails.**
  Its fetch handler has a baked deny list (`/__gofastr/sse`,
  `/__gofastr/session`, `/__gofastr/action`,
  `/__gofastr/widgets`, `/api/*`, `/auth/*`, plus `PWAConfig.DenyPaths`
  for custom mounts) and ignores non-GET, so island RPCs, SSE streams,
  sessions, and auth always hit the network untouched. Documents are
  network-first and never cached (SSR HTML can be personalized); only
  the versioned app-shell precache lives in Cache Storage, and nothing
  is added at runtime. The precache holds the runtime, split modules
  under their `?v=<hash>` URLs, app.css, the offline page + its
  per-component stylesheets, icons, and declared extras, never the
  comp-bundle, whose names-set is per-page. Matching is exact: `?v=` content-addressed URLs are
  cache-first (immutable), everything else is network-first with the
  cache as offline fallback, so post-deploy HTML never pairs with the
  old deployment's runtime/CSS.
- **The offline screen renders through the document shell but NOT the
  app layout**: it is precached at install time, so nothing
  personalized may render into it.
- **Updates never force a reload.** No `skipWaiting`; a waiting worker
  dispatches `gofastr:pwa-update` on `window` (via register.js) and
  activates when the old version's tabs close.
- **Static export** (`framework/static.Builder`) emits the same four
  assets with `BasePath` baked into the manifest URLs, precache/deny
  lists, and registration target.

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
| `data-cui-spa` with `method="GET"` | Successful controls become the URL query; the client navigator fetches and swaps the screen | URL query, e.g. `app.QueryFromContext` |
| `data-cui-spa` with no method or another method (no/urlencoded enctype) | `application/x-www-form-urlencoded`, sent with the form's method or POST | `r.ParseForm()` + `r.PostFormValue`  |
| `data-cui-rpc="/some/endpoint"`        | Per the RPC contract (see Widgets section)        | RPC handler                          |

Every other form, with no special attribute and a default or `urlencoded`
or `multipart/form-data` enctype, is **NOT intercepted**. The browser
submits it the standard way: POST body matches enctype, Set-Cookie
headers apply, 303→Location is followed, file inputs upload natively.

`ui.FilterToolbar` with `ClientNavigation: true` emits `data-cui-spa` on its GET form. With the runtime it
uses the existing client navigator, preserving the document and shared layout.
Without JavaScript it submits as an ordinary GET form. The form's successful
controls replace any query in the action, preserving repeated names, the locale
prefix, and the action's fragment. GET forms without the marker stay native. Only an explicit `method="get"`
(or a submitter's `formmethod="get"`) navigates; a marked form with no
method still POSTs, so its fields never land in the URL. A JSON form whose
submitter picks GET navigates the same way, since fetch cannot send a GET body.
Native GET is the toolbar's default: use it when a shared layout also renders
query-dependent content, because client navigation keeps that layout.
The plain-form bridge carries the activated submitter's name/value and its
`formaction`, `formmethod`, `formenctype`, and `formtarget` overrides. Alternate
browsing targets, dialog and image submissions, and plain-text POSTs stay native.
GET client navigation asks the existing leave guard at document scope before
discarding edits; Cancel stays put and Discard retries the complete URL. A
plain form whose RPC module cannot load replays native submission with the
original submitter. JSON/RPC-only forms retain their failure behavior instead
of being sent with an unsupported native encoding.

### Server redirects (after intercept)

When the interceptor IS active and the handler responds with a `30x`
+ `Location` header, the runtime loads the location as a new document
(`window.location.assign`): the target may not be in the SPA route
table, and the load resets the SSE connection the way a native form
submit would. For the non-intercepted default path, redirect-following
is the browser's own job, same result.

An answer that is not a redirect is handled as on a `data-cui-rpc`
form. A refusal (non-2xx) renders the server's validation envelope
(`{error, fields}`, or `core/handler`'s nested `{error: {message,
fields}}`) into the fields through the `formerrors` module, or
toasts the status when no field matches. A 2xx answer delivers its
`X-Gofastr-Toast` and `X-Gofastr-Invalidate` headers. Every intercepted
submit carries the page's `<meta name="csrf-token">` as `X-CSRF-Token`,
since a JSON body has no form field for `middleware.CSRF` to read.

```go
http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
```

The redirect rule above is the intercepted plain form's (`enctype="application/json"`,
`data-cui-spa`), whose redirect is its post/redirect/get answer. Every
other runtime fetch reads a followed redirect as **no answer**. fetch
follows a 3xx itself, so a request whose answer was a redirect (an
expired session sends it to the sign-in page) resolves with a 200 for
another URL; `response.redirected` is the only sign. The runtime never
takes that body as the request's own (#577):

- **`data-cui-rpc` and optimistic actions** fail. No success effect
  runs, no success toast, no `data-cui-rpc-navigate`, and the response
  signal reads `{ok: false, status: 0}`. An optimistic action rolls
  back. A **form** stays where it is with what the reader typed
  (`gofastr:formresult` reports `ok: false`, so a leave guard keeps the
  form dirty) and toasts `runtime.submit_failed`: navigating away would
  throw the input away. **Anything else** (a button, an optimistic
  toggle) hands the redirect target to the client navigator, the way
  the intercept overlay does, so a same-origin sign-in page opens with
  no reload and another origin loads as a document. No server handler
  answers a successful RPC with a 3xx. An element that should move the
  reader after its RPC names the destination in `data-cui-rpc-navigate`;
  `X-Gofastr-Location` moves the reader on navigation fetches only.
- **Passive fetches** swap, parse and cache nothing: region and widget
  poll (a failed poll, so it backs off), widget chrome (a failed open)
  and state, widget discovery, `fetchPage` and server actions (empty
  and `null`), deferred parts (the slot is restored), the SSE session
  re-mint, and the desktop bridge (the call rejects). The embed
  handshake's exchange and content fetches refuse redirects outright.
- The navigators, prefetch and the intercept overlay already handled
  it (`_followed` in `frag/nav.js`). Headless behaviours follow the
  same rule: a sortable reorder whose commit redirects reverts, its
  conflict refresh mounts nothing, and a selection Copy fails.

### Cookie-set + redirect: the canonical auth flow

```go
// /auth/login form handler. battery/auth ships this out of the box.
http.SetCookie(w, sessionCookie)
http.Redirect(w, r, successRedirect(r, "/"), http.StatusSeeOther)
```

The runtime preserves the cookie and follows the redirect, so a form
POST to `/auth/login` from an SSR login page lands the user on the
next screen without a hard refresh, and without the host needing
client-side glue.

## The three failure modes (do not repeat)

These are the misreadings of the model that have already cost rework.
Future agents: **if you find yourself doing one of these, stop and re-read this doc.**

### Failure 1: "Just intercept all link clicks for SPA"
> Symptom: every `<a href>` is hijacked by the runtime to do partial fetches.

That breaks the model for two reasons:
- **State-change interactions** like pagination should NOT be `<a href="?p=2">` links at all. They are island RPCs. The fact that I built pagination as a route-changing link is the root cause; "fixing" the link interception only papered over it.
- **Cross-page navigation** (a real `/a` → `/b`) IS supposed to be intercepted (Angular-router style with cache). Don't disable that to "fix" pagination. Pagination wasn't a route in the first place.

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

### Failure 4: "Ship CSS from the app / generator to make it look right"
> Symptom: an `AppSpecBaseCSS` string (or an app `theme.go`, or a page)
> accreting `.mrd-hero`, `.gofastr-entity-form`, `.layout-marketing`
> rules; hand-rolled `<div class="…">` structure; overriding a component's
> internals from outside via a CSS var or descendant selector.

Caught 2026-06 building the Meridian flagship: ~70 bespoke rules accreted
in the generator before anyone noticed, half of them re-implementing or
working around components that already existed (`ui.Grid`, `ui.Stack`,
`ui.ThemeToggle`) and a token that already existed (`--font-heading`).

Why it's seductive and why it's wrong: a generator's (or page's) local
success metric is "does this surface look right?", and inline CSS
satisfies it **instantly**. The cost stays invisible until someone asks
"why does the generator ship CSS?": CSS duplicated from the design
system, only *this* surface gets it, divergence from every other surface. Each
addition is individually defensible ("I need a container → add container
CSS"); the **sum** is drift. There is no single wrong step, so a per-step
instinct won't catch it. The tripwire is the *cumulative* shape: a
`*BaseCSS` string that keeps growing, an invented `.mrd-`/`.gofastr-`
class, a raw property where a `var(--*)` token belongs.

The fix is **One styling surface**, described below. Treat a surface that needs
CSS the components don't provide as a *design-system gap to fill upstream*,
not a patch. The app generator composing the design system is the system's
completeness test: when it can't, you found the gap.

---

## Why this model

- **Stability**: islands are stateless per-request HTTP. RPC is easy to
  retry, easy to log, easy to observe. SSE handles only push (no
  user-action-via-SSE), so connection-pool concerns stay manageable.
- **Scalability**: HTTP/2 multiplexes RPCs; a paginating user fires ~1
  request per click. SSE per session is one long-lived connection only
  for the genuine push channel. It is NOT how user actions reach the
  server. Mixing the two is fine; do not collapse them.
- **Server is source of truth**: pagination math, sort comparators,
  filtering rules, and validation all live in Go. The client never
  re-implements them. JS code shipped to the browser stays small and
  generic, not by being tiny in source (the runtime is ~7,400 lines of
  vanilla JS: a core `runtime.js` plus per-feature split modules under
  `core-ui/runtime/src/`), but by being carved and budgeted: a page
  loads `core.js` (≤13,695 B gzipped, enforced by
  `core-ui/runtime/budget_test.go`) plus only the demand modules its
  components actually use (≤3 KB gzipped each; `widgets` carries a
  tracked 5 KB override). A second gate pins the *typical-page* payload
  (core + widgets, since any page mounting a widget loads both) at
  ≤20 KB so features can't migrate out of core into widgets and bloat
  the real download while the core number stays pure. None of it
  re-implements server logic.

  The budget policy: the core should sit under TCP's initial congestion
  window (~14 KB ≈ 10 packets), so it arrives in the first round trip on
  a cold connection. Since 2026-10 the level-1 size is 15,779 B, 1,443 B
  past that line, and each byte over it is a measured, named entry in
  `budget_test.go`. That cliff is what the number protects; smaller
  buys nothing, bigger costs an RTT. When a budget trips, **carve a
  feature into a demand module, never raise the line.** Carving
  candidates are features only some pages use (SSE status chrome,
  flash-on-update, tabs aria mirroring); nav and island RPC must stay
  in core, because a demand module costs one extra request at first
  use: fine for drag-dismiss, fatal for the click path.
- **Hydration on SSR**: the first paint is a fully-rendered, accessible,
  scrape-able HTML document. Clients without JS get the same content,
  just without the interactivity layer. SEO + accessibility come for free.
- **No hard refreshes**: cross-page nav swaps content; in-page nav swaps
  islands. The runtime stays loaded; signals/SSE/cache survive.

---

## Recipes

### How to build a page

1. Implement `component.Component` (`Render() render.HTML`). Optionally also:
   - `app.ScreenTitler` / `app.ScreenDescriber` / `app.ScreenTyper`: individual optional interfaces so `app.Register(path, comp, layout)` reads just the metadata the component declares. Implement one, two, or all three (the combined `app.ScreenSpec` embeds all three for convenience). `ScreenTyper` defaults to `ScreenPage` when not implemented.
   - `app.ScreenLoader`: `Load(ctx) error` runs once per request after DI injection, before render
   - `app.ParamSetter`: `SetParams(map[string]string)` receives route params from dynamic paths
   (`Screen` itself is a struct value the router holds, not the interface you implement on your component.)
2. Inside Render, compose `core-ui/html` (1:1 tag primitives) +
   `framework/ui` (semantic components like PageHeader, FormField,
   DataTable; unstyled structure via `framework/headless`).
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
3. The runtime sees `data-cui-rpc="/islands/customers/page"` on the
   pagination button and `data-cui-signal="customer-list-rows"` with
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
   page 2. The user can refresh and get exactly this, no JS required.

2. **Click "page 3" inside the island**:
   - The button is `data-cui-rpc="/islands/customers/page" data-cui-rpc-method="POST"`.
   - The RPC handler reads `{"page": 3}`, mutates server-side state,
     renders the new rows, and returns the HTML.
   - **The handler also returns an `X-Gofastr-Push-State: ?p=3` header.**
     The runtime applies it via `history.pushState`. The URL becomes
     `?p=3` but the runtime **does not** trigger a fetch. Just URL update + island swap.
   - Result: URL and DOM are now consistent. Bookmark works. Share works.

3. **Browser back**:
   - `popstate` fires with the previous URL (`?p=2`).
   - `p` is an identity param (not a widget/pane deep-link param), so
     the runtime fetches the screen partial for the new URL via
     `X-Gofastr-Navigate: 1` (the same flow as cross-page nav) and
     swaps the shared layer's content cell. The screen's `Load(ctx)`
     reads `?p=2` again, the island is re-rendered server-side at page
     2, partial response comes back, runtime swaps. The cache makes
     typical back-stack transitions feel instant, and the entry's
     stored scroll position is restored after the swap.
   - This means popstate triggers a full screen-partial re-fetch, not
     a per-island patch. It's coarser than the click path but it's
     simpler and correct, and it's still way cheaper than a hard
     reload.

4. **Refresh / share / bookmark**: trivially correct because step 1
   handles the case.

This is "URL as the canonical state, RPC for the fast path, partial
re-fetch for popstate". You do not need to re-implement state in JS.
You do not need to teach the runtime about pagination. The runtime
just knows: *RPC response can carry a `pushState` header; popstate
triggers a screen-partial fetch.*

---

### Guarding a dynamic screen (route match + `RenderScreen`)

Middleware that guards `/session/:sessionId` must not re-parse the
path: `host.RouteMatchMiddleware()` resolves every request against the
screen router and stores an immutable `app.Match` on the context, and
`app.MatchFromContext(ctx)` hands the guard the same parameter values
the screen's `SetParams` receives (trailing slash included). The host
also populates the match on the render context itself, so a screen
Policy sees it with no middleware. A guard that refuses answers with
`host.RenderScreen(w, r, screen, uihost.ScreenResponse{Status: 410})`:
the branded page through the normal chrome on a full load, the bare
body on a client-side navigation, the same status and
`Cache-Control: private, no-store` on both, no session minted. A path
no screen matches carries no match; the guard falls through and the
`WithNotFoundScreen` 404 stays truthful. See
`framework/docs/content/ui-wiring.md` → "Guards on dynamic screens".

---

### Error pages that show (404 through the root layout)

A request no route matches answers 404 with the not-found page rendered
through the app's root layout — the layer-0 shell, header and nav
included, outlets at their declared defaults, the error body in
`<main>`'s slot — instead of a bare document: the runtime can keep the
shell and swap only the slot. A navigation fetch (`X-Gofastr-Navigate`
plus a known `X-Gofastr-From`) gets the same answer shape any route
gives it (partial/envelope swapping at the shared layer, status 404).
On the client, a non-OK response whose Content-Type is `text/html`
flows through the same apply paths a 200 takes (envelope, partial,
whole document via the doc-envelope reader, then the `<main>` swap):
the URL stays the target so Back works, the title comes from the
response, and the entry is never cached. A body that is not
`text/html` cannot be applied, whatever its status: a 200 included
(a raw-markdown or JSON route the client router intercepted), it
toasts naming the status (`Could not load /x (HTTP 404)`), the URL
goes back to the page still shown and the page stays. A navigation
started while another was loading rolls back past that one too: its
destination never rendered. Only a failed fetch keeps the "check
your connection" wording.
Never a `location.href` fallback. A static export ships the same page
as `404.html` at the root (see `framework/docs/content/static-export.md`).

## Theme

The framework's design tokens live in `core-ui/style.Theme`, a
**typed Go struct** with a fixed canonical field set: `Colors`,
`Spacing`, `Radii`, `Strokes`, `Leading`, `Tracking`, `Opacities`,
`Fonts`, `Breakpoints`, `Shadows`, `ZIndex`, `Durations`, `Easings`,
`Typography`, `FontWeights`, `Layout`, `Code`, plus the `Knobs` map.
`Strokes`, `Leading`, `Tracking`, `Opacities` and `Code` are optional:
an unset slot emits the default theme's value. Every token carries a `Name` (the CSS
custom property identifier) and a `Value` (the concrete value):

```go
t := style.DefaultTheme()
t.Colors.Primary // → style.Color{Name: "primary", Value: "#18181B"}
t.Colors.Primary.CSS()   // → "var(--color-primary)"
t.Colors.Primary.Value   // → "#18181B"
```

### The var-only contract

**Components always emit `var(--*)` references.** Build-time
resolution of `{tokens.text}` to literal hex values has been
removed; every reference is a CSS variable indirection. This is
required for section-level theme overrides via the CSS cascade:
a parent `.cui-theme-<hash> { --color-text: #f4f4f5 }` overrides
every descendant's `var(--color-text)` automatically. The hash is
content-derived from the overridden tokens (see `RegisterThemeOverride`),
so apps don't pick the class name. They pass an override struct and
get a stable hash back.

### Overriding tokens

Apps mutate fields directly. There's no MergeThemes helper:

```go
t := style.DefaultTheme()
t.Colors.Primary = style.Color{Name: "primary", Value: "#14B8A6"}
app.WithTheme(t)
```

`framework/ui/theme.Default(theme.Overrides{Primary: "#…", Dark: &theme.Overrides{Primary: "#…"}})`
wraps this pattern as a convenience for the most common cases, light
and dark.

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
// Register at package init: content-addressed, so re-registering
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

The framework emits one `.cui-theme-<hash> { --color-…: …; }` block
in `app.css` for every registered override. The wrapped `<div
class="cui-theme-<hash>">` scopes the override via CSS variable
cascade: no per-component changes, no inline `<style>`, no extra
HTTP requests beyond the always-present app.css.

### Component options (`Theme.Components` and the `--fui-*` variables)

Beside its tokens, a theme carries **component options**:
`Theme.Components`, a flattened map (`"density": "compact"`,
`"button.treatment": "outline"`) that answers questions a component
family asks about its own drawing. It is not a token — the reflection
token walk ignores it (it is a map, like `DarkColors`) — and
core-ui/style can only store, validate, hash and copy it. Turning it
into CSS is the job of the one **component-options compiler**
registered per process (`style.RegisterComponentOptionsCompiler`);
`framework/ui` registers its compiler from its package `init`, and a
registration after a theme was hashed or theme CSS was emitted panics,
because the host freezes `app.css`, the component catalog and the
manifest at first use and a late compiler would be missing from all
three. Registration never hashes — `ThemeRef` computes its hash on
first use — so the package-level `var Dark =
style.RegisterThemeOverride(…)` pattern is safe in a package that does
not import `framework/ui`; only a package-level `Class()`/`ThemeHash`
call can hash before the styled layer's init, and the panic names that
cause.

The cascade rule, which is the whole design:

- **Theme boundaries declare option variables; component rules consume
  them.** The root theme emits `:root { --fui-button-radius: …; }`, a
  scoped theme emits the same declarations inside
  `.cui-theme-<hash> { … }`, and a component stylesheet reads
  `border-radius: var(--fui-button-radius)` without ever redeclaring
  the variable on the component (a redeclaration would block
  inheritance and break nesting).
- **No descendant option rules.** `.cui-theme-a .fui-button` (0,2,0)
  outranks the component's own variant and state selectors, so an
  option that changes several properties together (a treatment: fill,
  text and border) emits SEVERAL variables, never one descendant
  rule. Nesting then resolves by inheritance: every theme built by
  `framework/ui/theme.Default` declares the complete option set, so an
  inner scope redeclares all of it and wins by proximity — no
  exception, no specificity ladder.
- **Token references resolve where declared.** A custom property's
  `var()` references compute at the element the declaration sits on,
  so `--fui-button-bg: var(--color-primary)` and the other `:root`-only
  option variables are re-emitted inside every scope block — light and
  dark — or a scope with its own palette would inherit the ROOT's
  resolved colours.
- **Scoped dark mode follows the document.** A theme override with a
  dark palette emits its dark tokens under
  `[data-color-scheme="dark"] .cui-theme-<hash>` plus the
  `prefers-color-scheme` fallback on
  `:root:not([data-color-scheme="light"]) .cui-theme-<hash>`, the same
  two selectors `darkSchemeCSS` uses at `:root` (`data-color-scheme`
  is written on `<html>` by the colour-scheme bootstrap). A scoped
  theme with NO dark palette stays light in dark mode: its light
  declarations block inheritance, by design.

The Button family consumes them now: the `ui-button` sheet's base
rule reads `min-height: var(--fui-density-control-h)`,
`gap: var(--fui-density-gap)` and
`border-radius: var(--fui-button-radius)`, and its `.fui-button--primary`
/ `.fui-button--danger` variant rules read the per-variant treatment
trios (`--fui-button-primary-bg/-fg/-border` and the `-danger` set).
The compiler declares one trio per treated variant rather than one
un-prefixed trio, so a variant's rule can never inherit another
variant's drawing.

**The :root floor.** A theme with no `Components` of its own — a bare
`style.DefaultTheme()`, the `gofastr theme init` scaffold, a host with
no `App.Theme` — still emits the styled layer's registered default
option set at `:root`: `RegisterComponentOptionsCompiler` takes the
complete default set as its second argument and the root emitter
compiles the theme's own options merged over it key by key, so every
host that links `framework/ui` ships the full `--fui-*` vocabulary and
the component rules above resolve, including a host whose theme sets
only some options (`{"density": "compact"}` keeps the default button
and field variables). Scope blocks compile the same merge: a
scoped theme with no options still declares the full default set,
because the inherited variables arrive already resolved against the
parent's palette and a palette-only `ui.Themed` band would otherwise
draw the page's primary. A key a scope leaves out therefore takes the
framework default, not the enclosing scope's value. A binary that
links no styled layer registers no compiler and emits none of the
variables. A theme's
identity is unchanged either way — `ThemeHash` fingerprints the
flattened options, and an optionless theme hashes as optionless in
every binary. There are no in-CSS fallbacks (`var(--x, fallback)`);
the floor sits at the theme boundaries, where one declaration covers
every rule beneath it.

The `fui-` prefix is reserved for `framework/ui`'s class names and
option variables. Classes belong to framework/ui; the `data-hui-*`
hooks belong to framework/headless. One prefix each: `fui-` is the framework's, `hui-`
is headless's. A caller who writes `fui-button` on their own markup
gets the framework's styling whenever that sheet is on the page.

Options ride the same plumbing as tokens: `ThemeToTokens` /
`ApplyTokens` carry them under the reserved `component.` prefix,
`Theme.Validate` enforces their grammar (lowercase dot-separated keys,
one lowercase word per value) and, when the compiler is registered,
their vocabulary too; `ThemeHash` separates themes that differ only in
options with or without a compiler registered (the fingerprint carries
the flattened options directly); the theme-edit writeback emits them,
and every registry that stores a theme clones the map before storing
and on every read.

### app.css: one asset, one request

The framework serves a single `/__gofastr/app.css` per app:
theme :root custom properties concatenated with `WithCustomCSS`
payload. Replaces the legacy `theme.css` + `styles.css` split.
SSG output emits the same single file. (`/__gofastr/theme.css`
and `/__gofastr/styles.css` are retained as `410 GONE` so stale
references surface clearly.)

Served CSS is minified outside dev: app.css, component sheets, the
bundle, widget sheets and the static export all pass through
`minify.ServedCSS`, gated like the runtime JS (`GOFASTR_DEV`,
`RUNTIME_NOMINIFY`; see `framework/docs/content/runtime-minification.md`).
`AppCSS()` and `Entry.CSSFor` stay the readable source and the version
hash is taken over it; `Entry.ServedCSSFor` is what goes on the wire.
A custom property reads back through `getComputedStyle` in the served
spelling (`minmax(0,1fr)`), so compare tokens, not spacing.

---

## Component CSS

Every component-owned stylesheet is registered, served and versioned
by the host, loaded lazily per-component, dedup'd globally, and
**always scoped** to `[data-cui-comp="<name>"]`. A first-load page
whose head is small carries its sheets in the document instead of
linking them (see "First-paint inlining" below); that is the host's
choice, never a component's or an app's. There is no
"unscoped component CSS"; global rules (resets, typography, theme
tokens) live in `theme.css` / `WithCustomCSS`.

### One styling surface (who ships CSS, and who must not)

There is exactly one place each kind of styling lives. Nothing else
ships CSS: no battery, no page, and no app or generator outside its
owned sheets.

| Styling | Lives in | Mechanism |
| --- | --- | --- |
| A component's look | its `framework/ui` file | `registry.RegisterStyle(name, fn)`, scoped to `[data-cui-comp]` |
| Page frames (the sidebar row, the centered container) | `framework/ui` | `ui.ContentRow`, `ui.Container`, composed in a `ui.Stack{Screen: true}` page column |
| An app's own pieces (its header, footer, docs page, a layout root) | the app package's `<name>.style.css` | owned style: `gofastr gen styles` writes typed class methods; the compiled sheet is `@scope`d to `data-cui-scope` and reads theme tokens only (`<name>.tokens.css` adds the app's own) |
| Global resets, base typography, tabular figures, landmark-focus | `framework/uihost` | `frameworkBuiltinCSS` |
| Colors / fonts / dark scheme | `core-ui/style` | theme tokens (`--color-*`, `--font-*`, `Theme.DarkColors`) |

**The app generator and every app ship ZERO unscoped CSS.** They
*compose* the design system and inherit all styling from it: `ui.Hero`,
`ui.Grid`, `ui.DetailList`, `ui.AuthCard`, `ui.Form`. A generated app's
`AppSpecBaseCSS()` returns `""`. What the kit does not ship, the
site's own chrome above all, is the app's own package with an owned
sheet: the framework has no site header, footer or docs-page component,
because a preset frame steers every site into one look.
`examples/acme-site/{siteheader,sitefooter,helpdocs}` are the
references, and the app generator writes the same header and footer
packages into a generated app as its own code. An owned sheet never
reaches into a kit component (`[data-cui-*]` and `.fui-*` selectors
are refused, and `data-cui-internal` bounds its `@scope`), and every
dimension in it is a token.

A docs page releases its TOC column when the slot is an empty
`data-cui-outlet` cell: the page's sheet observes the cell with
`:has(> :empty)`, so navigation can clear or refill it without
rebuilding the surrounding layout. Documentation navigation uses
`SidebarConfig.Compact`, including in the mobile drawer and no-script
fallback. It retains normal link and group semantics.

Dense workspaces compose `CardRow`, `DetailListConfig.Inline`,
`PageHeaderConfig.Compact`, and `ToolbarConfig.Plain`. Their opt-in styling
does not change the default components. An app bar hosts the
sidebar's own phone trigger (`ui.SidebarDrawerTrigger`) instead of
drawing a second phone menu. `PageHeaderConfig.Badge` renders status beside the heading without adding
it to the heading's accessible name. The title row wraps when space runs out;
headers without a badge keep their existing structure.
Screens compose their own block rhythm from `Stack` gaps,
`ui.Section`, and `ui.PageHeader`. `SectionConfig.Compact` leaves section
separation to its parent stack. `BannerConfig.Strip` is the square-edged,
full-width announcement treatment. These options add no runtime attributes
or navigation state.
`FilterToolbarConfig.Compact` fits the search and submit controls in a
narrow list without moving filter logic into JavaScript.
`StackConfig.TrimMargins` lets the stack's gap own paragraph spacing
without also adding the browser's paragraph margins.

**Sticky offsets are a layout fact, never a component flag.** The page
layout declares a sticky header once: `StackConfig.StickyHeader` pins
the stack's `<header>` child and publishes `--size-header-height` as
`--ui-sticky-top`. Every top-sticky part in the kit (ContentRow's
`Sticky` frame, StepRail, SectionMenu's rail, TableOfContents,
AnchoredRail, GlobalSearch, NetworkRetryBanner, `ui.Sticky`, FormFrame's
side panel, an anchored Section's scroll margin) sticks at
`calc(var(--ui-sticky-top, 0px) + <its own gap>)`, so it stops under
the header at any layer depth with no `BelowHeader` option. A part that
reaches the bottom of its containing block can move above that offset.
The page header paints at `calc(var(--z-sticky) + 1)`, above content
stickies at `var(--z-sticky)`, so they pass behind the banner at that
boundary. Modal, popover and toast tiers retain their higher layers. A part that
pins something above its children re-publishes the variable on a child
it does not read it from (ContentRow's workspace adds the toolbar's
height); redefining it on the element that reads it makes the property
refer to itself. A region that scrolls on its own (a bounded Box,
ContentRow's pinned nav column and Viewport columns, ListDetail's list,
the PaneHost drawer, the Workbench rail) resets it to `0px`, because its
sticky parts pin to its top, not the window's. A new top-sticky part
reads the variable; a new self-scrolling region resets it.

`ui.ContentRow` owns an optional labelled aside and toolbar placement.
Its aside observes an empty outlet by the same CSS rule. `ui.ListDetail`
owns a list scroll container beside a detail region, with detail first on phones.
The group layout, not the component, determines whether that list stays
mounted across detail navigation; putting it in an outlet replaces it.
`ContentRowConfig.Viewport` confines desktop scrolling to the row's regions
without forcing a fixed-height phone layout. `SidebarConfig.NativeMobile`
adds a no-script disclosure selected by the CSS `scripting` media feature;
the mounted drawer remains the scripted path. Apps need no detection script.
The list/detail region accepts `LayoutTree.VTRegion()`'s existing two
transition attributes; it adds no runtime vocabulary.
`ListDetailConfig.MobileSinglePane` is an opt-in phone mode. Its index
screen uses `ListDetailPlaceholder`; CSS `:has` selects the list or detail
as the primary content changes. `BackHref` renders a back link at the top
of the detail pane that the same CSS shows only on a phone detail; it
survives detail navigation because it sits outside the primary slot. The
default remains a stacked layout.
`DetailList` also responds to its container width, so an aside can show
complete values on desktop without relying on a phone viewport query.

When a surface needs styling the design system doesn't provide, the fix is
**upstream**: add or extend a component / layout / token, then compose it.
Never inline CSS, never a `*BaseCSS` string of rules, never override a
component's internals from outside. Give the component a config/variant
instead (`FormConfig.ExtraAttrs`, a new theme token). See
"Failure 4" above.

### The model in one paragraph

A component declares its CSS by calling
`registry.RegisterStyle(name, fn)` in a package var; the handle's
`.Render(c)` wraps the component's output and injects
`data-cui-comp="<name>"` onto its outermost tag (no extra DOM
node). The SSR host string-scans the final rendered HTML for those
markers and emits **one** `<link rel="stylesheet">` in `<head>` for
the page's exact set of components (or the same bytes as one `<style>`,
under the first-paint budget). Markers inside an inert
`<template>` (the confirm dialog, a toast item, a menu row, a loading
skeleton) do not count: the browser renders none of it at first paint.
Each sheet only template content needs gets a
`<link rel="prefetch" href="/__gofastr/comp/<name>.css?v=<hash>">`
instead, the exact URL the runtime builds from the catalog, so the
clone the runtime makes later finds its sheet in the HTTP cache. A
declarative shadow root (`<template shadowrootmode>`) renders, so its
markers count. After hydration, the runtime
scans newly inserted DOM (cross-page swap, island response, widget
mount, intercepted pane) and lazy-loads any new component's CSS as a `<link>` once
per session, dedup'd by `data-cui-style="<name>"`. The kernel does
not watch every insertion, so code that moves template content into
the page (the confirm module, the toast stack, a lazy menu panel, a
loading skeleton) calls `__gofastr.scanAndLoadCSS(container)` on what
it inserted. The scan reads descendants only: pass the element that
holds the new nodes, or load the root's own `data-cui-comp` /
`data-cui-scope` by name. The browser
caches the stylesheet by URL (`/__gofastr/comp/<name>.css?v=<hash>`)
under `immutable` headers in prod, content-addressed by a hash of
the sheet as served (minified outside dev), so a deploy that changes
the bytes, a source edit or a minifier change, busts the URL
automatically.

A client navigation does not paint the new page before its sheets
arrive. Each page links only the sheets it renders, so nearly every
navigation brings markup whose component sheet the current page never
loaded. Before it commits, the navigator scans the incoming markup (the
fetched partial, the whole document, a cached or prefetched screen
with its fills and held parts, and an intercept pane) with
`__gofastr._cssReady`, which links the new sheets while the old page is
still on screen and resolves once each sheet that markup renders has
loaded: its own link, or the full bundle link while a first-paint
subset is completing (before the `fullbundle` module has linked it, the
wait loads that module first, once and bounded at 3 s like a sheet). The title, the route announcement and
the loading regions' `data-cui-min` hold and exit move only after that
wait, so a loading region its `data-cui-after` timer showed during the
wait still holds and leaves as declared. Sheets the incoming markup does not render
(idle prewarms, another page's prefetch) are not waited on. Each
sheet's wait is bounded at 3 s, and a sheet that never answers is not
waited on again, so a stalled fetch delays one navigation and never
freezes it. A hover prefetch starts its page's sheets when it lands,
so the click that takes the entry usually finds them loaded; a visible
or eager prefetch leaves them for the click. A deferred part lands
only once the sheets it renders have loaded, so it enters styled
whether it answers before the commit, during the page's own wait, or
after. An intercept pane that an ordinary navigation overtakes during
its wait is dropped, never mounted over that navigation's URL; one that comes
back declined or failed is dropped too, so its fallback never navigates over
the newer page. Hash and history scroll
writes wait on the current page's sheets the same way. Swapping first and linking after
showed a phone on a slow link every new page unstyled for a moment.

### Three load modes

| Mode         | First-paint cost           | Behavior                                                                              |
| ------------ | -------------------------- | ------------------------------------------------------------------------------------- |
| `LoadAlways` | 1 request, render-blocking | SSR emits `<link>` in `<head>` on every page, regardless of whether the page renders the component. Only for a sheet the scan cannot see is needed: `ui-button`'s classes are worn by ToggleAction and OptimisticAction under their own markers. |
| `LoadAuto` (default) | 0 (deferred)               | SSR collector emits `<link>` only on pages that actually render the component. After hydration, the on-demand scanner picks up newly-inserted markers from partial responses. |
| `LoadPrewarm`| 0 (deferred)               | `LoadAuto` plus a throttled `requestIdleCallback` prefetch (serialized, one in flight). Use for components that are likely soon: a hotkey-opened palette, or app chrome a client navigation may swap in. `ui-sidebar` and `ui-page-header` prewarm: a page that renders one links it through the scan, and a page without one (a marketing page, an article) does not block on it. |

All three converge on `loadComponentCSS(name)`. The function is
**synchronous**: no `await` between the existence check and
inserting the link, plus a `_pendingLinks` guard, so promoting a
component across modes or having two scans race never produces a
duplicate request.

The runtime inserts each sheet it loads **before** `app.css`, the
element carrying `id="cui-app-css"` (the host puts that id on the
linked and the inlined `app.css` alike, and on the embed shell's link),
so a component whose sheet arrives after first paint keeps the order
the server head gives every other one and the host's override still
wins a tie. A sheet whose fetch fails (a 503 mid-deploy, a dropped
connection) gives its name back: the link is removed and, after a 3 s
cooldown (so a poll or push region swapping the same markup cannot
re-request a sheet that keeps failing on every swap), the next scan of
that marker requests it again. The boot seeding from
`data-cui-bundle` skips a bundle link that failed (its sheet is empty),
so its components fall to the scanner instead of staying unstyled.
Boot's mutation observer runs the same scan over every inserted subtree
(its root included), so markup written by a path that does not scan
for itself (a compiled action's `updateHTML` or `fetchPage`, a module's
own `innerHTML`) still loads its components' sheets.

The server keeps only registered names in a page's sheet list. A
`data-cui-comp` marker that names nothing (host markup, a typo, a
component this replica lacks mid-deploy) never reaches the bundle URL or
its `data-cui-bundle` list, and the bundle endpoint skips a name it does
not know instead of refusing the whole bundle; such an answer cannot
match its `?v=`, so it is served `no-cache`.

An embed frame's shell links no component sheet. The frame starts the
fetched content's sheets and waits for them (bounded like every sheet
wait) before it writes the markup, so the content paints styled.

Navigation's hash and history scroll writes wait for newly requested
component stylesheets to load or fail before measuring the page — and
each stylesheet wait is bounded (3 seconds): a stalled fetch that fires
neither `load` nor `error` must not block the scroll write, or one hung
request would freeze scrolling for the rest of the session. A newer
navigation or USER scroll intent (wheel, touch, key, or pointer input)
cancels the delayed write; browser scroll anchoring does not — the
stylesheets the wait exists for can grow content above the viewport,
and anchoring then moves `scrollY` with no user input, which is exactly
the drift the write is there to correct. This applies to both plain
partials and layout envelopes; cold component CSS must not move the
destination after the runtime has scrolled to it.
A forward navigation scrolls to its fragment, or to the top when it
has none, except one that changes only the query on the same path: a
list's sort, page or filter link keeps the reader where they were,
because the control they used is on screen and the top of the page is
not what they asked for. Core's navigator and the envelope module's
both apply the rule (`TestQueryNavKeepsScroll` runs each).
A row scrolled above a list pane can cross the window edge geometrically
without being visible there; it must not become the window's anchor.

The existence check is `link[data-cui-style="<name>"]`, so an
SSR-emitted per-component link must carry the same marker (and the
`cui-css-<name>` id) as the link the runtime would have written.
The host emits per-component links on a single-component page and
on every page of a static export, where the bundle endpoint does
not exist; without the marker the boot scan appended a second copy
of each after `app.css` and the host's overrides lost the cascade.
`core-ui/runtime/static_comp_css_e2e_test.go` pins both halves: a
marked SSR link is loaded once, an unmarked one twice.

### The bundle endpoint

When a single SSR page references ≥2 components, the host emits
one bundled `<link rel="stylesheet" href="/__gofastr/comp-bundle.css?names=a,b,c&v=<combinedHash>">`
instead of N individual links. The bundle handler concatenates the
per-component scoped CSS in the sorted order embedded in the URL.
Content-addressed via the SHA of the concatenated component
versions, served `immutable` in prod. After hydration, the on-
demand path uses single-component `<link>`s; the bundle is just a
first-paint optimization.

### First-paint inlining

A live page cannot paint until it holds its document and every
render-blocking resource its `<head>` names: the color-scheme bootstrap,
the component bundle and `app.css`. As links they cost a round trip
after the document arrives, which on a slow link is most of the time to
first paint. When the three together fit the budget, 16 KiB compressed,
the host writes them into the document:

```html
<head><script>/* color-scheme bootstrap */</script>
<style data-cui-bundle="a,b,c">/* the comp-bundle.css bytes */</style>
<style id="cui-app-css">/* the app.css bytes */</style>
```

The bytes are the ones the endpoints serve, minified the same way, in
the same order. A heavier head keeps its links, and so does a sheet
holding a relative URL (in `url()`, an `@import` string or
`image-set()`), which would resolve against the page's address once
inlined instead of the sheet's. The budget counts gzipped bytes, the
way they cross the wire once the app (`AppConfig.Compression`) or a
proxy in front of it compresses. The budget weighs a first
visit against a return visit: linked sheets come from the HTTP cache on
the next full load, inlined ones arrive again inside every document, and
16 KiB is about one round trip on a slow mobile link (400 kbps, 300 ms),
so a return visit never pays more than the first visit saved. Client
navigation is unaffected: the runtime seeds its dedup from the inline
bundle's `data-cui-bundle` and loads only the sheets a later page adds.

When the whole bundle misses the budget, the page may still inline the
part of it its markup can match. A component sheet styles every variant
and state of its component and a page renders a few: the site's home
page uses five of the data table's thirteen classes. The host drops each
rule on a `fui-` class that appears nowhere in the document: not in a
class attribute, a `<template>`, an attribute value or the head, nor in
the widget chrome the response inlines, nor in a registered behaviour's
source. Classes inside `:not()`, `:is()`, `:has()` and attribute
selectors never drop a rule, and neither does a selector with an
escape. Other rules, at-rules and owned sheets ship whole, and a group
rule (`@media`, `@supports`, `@container`, `@scope`) is dropped only
when nothing inside it is left. An emptied `@layer` block stays as
`@layer name{}`, since its first appearance fixes the layer order. If the subset fits, the page
inlines it marked with the full sheet's URL:

```html
<style data-cui-bundle="a,b,c" data-cui-bundle-href="/__gofastr/comp-bundle.css?names=a,b,c&amp;v=…">/* the rules the page can match */</style>
```

The `fullbundle` behaviour links that URL at boot, right after the
`<style>` and so before `app.css`, so the states a click or a client
navigation adds are styled once it arrives, in the cascade position the
full bundle would have had. A client navigation that renders a
component the subset covers waits on that link (the kernel finds it by
`data-cui-bundle`, and while the marker is unspent it first waits, once
and for at most 3 s, for the `fullbundle` module), so it commits and settles scroll only after
it loads. Until then a class the page did not render
at first paint has no rules. If the subset misses the budget too, the
head links as before.

Every block is allowed by its `'sha256-…'` hash in the response's own
`Content-Security-Policy`, never by `'unsafe-inline'`. The host patches
each policy on the response: the hash joins the governing
`style-src-elem`/`style-src` (`script-src-elem`/`script-src`) list, or a
new `style-src`/`script-src` copying `default-src` when that governs. A
policy that refuses the kind outright (`'none'`) keeps the page linked,
untouched; a list that already allows `'unsafe-inline'` without a nonce
or hash is left alone, since adding a hash would switch it off.
Report-Only policies get the hashes too. A body that could close its
element early (`</style`, `</script`, `<!--`) always links.

The patch only reaches a policy set on the response before the page
renders, the framework's own middleware or the host's. A policy written
elsewhere (a proxy, a CDN rule, a `<meta>` tag in a custom layout) never
learns the hashes; such an app sets `uihost.WithNoInlineFirstPaint()` and
keeps every head resource a link. Static exports and the PWA shell always
link: their files are served by hosts whose policy the framework does
not write.

### Runtime catalog

The host SSR-embeds an inert JSON block in `<head>`:

```html
<script type="application/json" id="gofastr-catalog">
{ "<name>": { "stylePath": "/__gofastr/comp/<name>.css", "version": "…", "loadMode": "auto" } }
</script>
```

The runtime reads `JSON.parse(document.getElementById('gofastr-catalog').textContent)`
at boot, so `loadComponentCSS` can resolve a marker name to a URL.
This is strict-CSP-clean (no inline JS, no separate script src).
The legacy `/__gofastr/catalog.js` endpoint now returns 410 GONE.

### The headless layer (`framework/headless`)

A second way to author a component separates what this section joins:
structure in one function, classes in a Classes value, behaviour bound by a
`data-hui-*` hook rather than a class. A headless component renders the
same markup at a nil Classes with no `class` attribute at all, and the
harness pins that render in a golden, so restyling cannot move a role,
a label or a hook. The layer satisfies every hard rule above the same
way `framework/ui` does: an in-page state change is an `Island` (the
`data-cui-rpc` contract on the element that keeps its href for
no-script), a request is a typed `Action`, and a signal is a typed
`Bind`; `ExtraAttrs` cannot carry a `data-cui-*` key. The behaviour
module now exists: `framework/headless/behavior.go` registers its
JavaScript under the name `headless` through the same seam a
stylesheet uses, and binds the `data-hui-*` hooks (see "Component
behaviour: the same seam" below for the mechanism). `framework/ui` is
the class map that dresses the parts: its buttons, form family and
Lightbox render through this package (the Lightbox section below is
the worked example); the components not yet moved are listed in the
stack's plan. Contract and invariants: `gofastr docs ui-headless`.

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

Register in a package var, never on first render. The host builds its
component catalog once (`registry.Freeze`), and the runtime loads sheets
only through that catalog, so a style registered later is missing on
every page reached by client-side navigation. `RegisterStyle` after the
freeze panics with the style's name; re-registering an identical entry
stays a no-op. Tests that register throwaway styles call
`registry.IsolateForTest`, which starts an unfrozen registry.

### The pattern packages are gone

`core-ui/patterns/` no longer exists: every pattern either moved to
`framework/ui` on its `framework/headless` primitive (breadcrumbs,
multiselect, progress, sortablelist, tree — and combobox, disclosure,
pagination, skeleton and tabs before them) or was deleted with no
replacement (accordion and nestedlist are `ui.Collapsible`;
infinitescroll had no user). A component that needs styling the design
system does not provide is a MISSING component: add it upstream, in
`framework/ui`, on the headless primitive that owns its structure. Its stylesheet registers through
`RegisterStyle` like every other sheet; there is no `BaseCSS()` export
for a host to concatenate.

### Component behaviour: the same seam

A component's behaviour registers the way its stylesheet does. The
package that renders the markup embeds its module beside the Go and
registers it with its markers:

```go
// framework/headless/behavior.go
//go:embed runtime.js
var runtimeJS string

var Behavior = registry.RegisterBehavior("headless", runtimeJS,
    registry.Markers("[data-hui-reveal]", "[data-hui-when]"))
```

From the host down a registered behaviour is a runtime module like the
embedded ones: served at `/__gofastr/runtime/<name>.js?v=<hash>`,
minified under the same gate, listed in the module manifest, preloaded
at low priority when its marker is in the rendered page. The kernel learns registered
markers from one block beside the manifest (`window.__gofastr_behaviors`
from `manifest.js` on live pages, the inline `#gofastr-behaviors` block
in exports and the embed frame) and scans them after its own table.
The marker is the one trigger; when it appears, at boot, on DOM
insertion or after a client navigation, the module loads once and
attaches. `registry.LoadIdle()` defers the load to idle time.

An optional API module that page code calls, rather than one that
binds markup, registers with `registry.OnRequest()` instead of
markers. It is served, versioned and listed in the module manifest like
any module, but it is left out of the marker block (the kernel's scan
never sees it), and it loads only through
`__gofastr.loadModule(<name>)` or another behaviour's `Requires`. It
takes no markers, `LoadIdle`, interactions or requirements. This is
where an optional API belongs instead of `runtime/src/`: a binary that
never imports the registering package never contains it, and a page
that never asks for it never downloads it. `core-ui/localdb` is the
first one.

Server-declared data such a module reads rides a registered data block:
`registry.RegisterDataBlock("gofastr-<name>", fn)` makes the host emit
`<script type="application/json" id="gofastr-<name>">` with `fn()`'s
JSON in every page head (live and export mode, sorted by id, `</`
escaped, nothing when `fn` returns nil). The host emits it without
importing the package that registered it. Ids are `gofastr-` plus
lowercase letters, digits and `-`, and the kernel's own block ids are
refused.

The rules, each a panic at registration: the name is a URL segment
(`^[a-z][a-z0-9-]{0,63}$`) and not an embedded module's; every marker
is an attribute selector on a `data-` attribute (`[data-x]` or
`[data-x="v"]`); at least one marker unless `OnRequest`, and none
with it; an identical re-registration is
a no-op and a different one panics. A `data-cui-*` marker is admitted
only when the attribute is already in the table above (hard rule 5
through the seam, `TestRegisteredBehaviorDataFuiMarkersAreDocumented`).

The module is held to the source lints every `src/*.js` module is held
to (`core-ui/check`: no `var`, no selector or storage key built from a
raw value, and the rest): the clean-tree tests reach it through the
`//go:embed` directive beside the `RegisterBehavior` call
(`check.RegisteredBehaviorSources`), so a module that lives beside its
Go package is not a module outside the rules.

The module keeps the contract every `src/*.js` module keeps: an IIFE
that binds only its own markers by attribute, sets
`window.__gofastr.loadedModules[<name>] = true` FIRST — before it
installs anything, right after its own early-return guard (a script
that failed halfway with its flag unset has its load rejected and
fetched again, and the retry re-executes the file: a flag at the end
leaves every listener of the first pass installed twice) — and
registers `window.__gofastr._moduleScanners[<name>] = fn(root)`,
idempotent, so the kernel can hand it inserted DOM and the document
after a navigation.

**Dependencies and readiness.** A behaviour may declare the modules
that must be registered before it: `registry.Requires(names...)`,
carried in the behaviours block as `r`. The names are embedded kernel
modules or registered behaviours; a name that is neither, or a cycle,
panics where the block is built — at the first render that builds the
manifest, not at startup (the registry is only complete once every
package's init has run), surfacing as a 500 through the framework's
recovery middleware. The loader is the one place every load goes
through (marker scan, idle queue, hover prefetch, interaction
bridge), so it is where dependencies live: `loadModule` loads a
module's requirements first, in parallel, and only then appends its
script. The interaction bridge reads registered descriptors too:
`registry.Interactions(...)` declares the interactions a behaviour
needs retained through its module's cold-cache fetch — the bridge's
own spec shape (event, and for a click the node's selector, for a
keydown the keys and the scope selector that arms the retention; a
key held with Alt, Ctrl, Meta or Shift is never retained) —
the behaviours block carries them as `x`, and the kernel installs its
retention listeners over the registered descriptors exactly as over
its own table; a behaviour that declares none pays nothing.
The lightbox is the
first module to declare interactions: prev/next clicks and the arrow
keys over an open viewer, retained through the module's cold-cache
fetch exactly as they were when the kernel's table carried them.
The headless module's own table behaviour is the second client of
that seam: its registration retains a click on
`[data-hui-table-sort]` (an island table's sort anchor) and on
`[data-hui-page]` (its pager's page anchor) so the first sort or
page turn on a cold module is recorded and the swap it triggers
still restores focus, through the same bridge, with no table name in
the kernel.

The lightbox's layer split is the seam's working example: the viewer's anatomy
is `framework/headless.LightboxViewer` (structure, roles, and
`data-hui-lightbox*` hooks that render exactly when `LightboxWiring`
is zero — the path a host's own viewer module binds), the
styled `framework/ui.Lightbox` renders through it dressed with the
fui-lightbox class map and the `data-fui-lightbox*` wiring instead of
the hui hooks (a viewer that rendered both would invite a host module
to double-bind the gallery the framework module steps), and the
module — registered in the same file — binds `data-cui-*` only (the
filedropzone rule: a ui-owned module never reads a `data-hui-*` hook),
pinch-zoom included, which targets the image by
`data-fui-lightbox-image` and not by the class it wore before the
move. The kernel's table, `preload.go` and this file's ownership map
lost their lightbox rows with it.

Readiness is registration, not transport:
the loader resolves a module's promise when `loadedModules[name]` is an
own truthy property after the script ran, and a script that ran and
never set its flag rejects with "module failed to register" and drops
its cached promise, so a retry fetches again. Preload follows
requirements (`NeededModules` lists them with their dependents) and
the static exporter dumps them, so the primitive arrives with its
adapters. A module with no marker of its own (a primitive, like the
kernel's `action`) is reachable only through `Requires` or an explicit
`loadModule`, and is served, hashed, budgeted and linted like the
rest; `window.__gofastr.action` (request, bind) is the primitive the
two action adapters and `framework/headless` bind their buttons
through: a bound group converges on one committed member (the revoke
runs at click time and again on settlement, so a failed commit
restores the sibling it displaced while a successful one displaces
every other), and group members that leave the document are pruned on
`gofastr:navigate` so the registry is not the last reference to a page
that navigated away.

### What about widgets?

The `core-ui/widget` registry continues to drive widgets (their
position chrome, slot composition, RPC endpoints). Widgets that
host styled components benefit from the same on-demand loader: the
mounted chrome HTML is scanned in `mountWidget` and any new
`data-cui-comp` triggers a load. Widget chrome CSS itself still
serves from the widget's `StylePath` (default
`/core-ui/widget/<name>/style.css`) under the prefix of the router
group `Mount` was given, the URL the widget catalog publishes; future
work may collapse the two paths.

Both registries coexist safely: they share the
`data-cui-style="<name>"` link dedup key on the client, so a widget
and a registered styled component can never double-load CSS even
if a future change merges them. Widgets surface through
`/__gofastr/widgets`; styled components surface through
the inline `<script id="gofastr-catalog">` JSON block and `/__gofastr/comp/<name>.css`.

**Widget DOM lifetime across SPA swaps.** A registered widget root the
runtime mounted (either chrome path) survives a full-shell swap. Roots
the runtime fetched are appended to `<body>`, outside the shell, so
the swap never touches them; the framework's own layouts inline SSR
chrome just before `</body>`, also outside the shell. A host layout
that wraps SSR-inlined chrome INSIDE the shell element used to lose
it: `swapShell` replaces that element wholesale, tearing the
registered root out of the document while `_widgets` still listed it,
which both stranded the widget and wedged the next open
(`_mountByName`'s "already mounted" early return). Since #409 the
widgets module rides `doc.reattach()` (nav.js calls it right after
every shell swap): a registered root that lost its parent is
re-appended to `<body>` and `fui:widget-open` re-fires with
`reinserted: true`. If the swapped-in shell SSR-inlines its own copy
of the widget (hosts that nest chrome in the shell inline it on every
page), the stale instance is dismissed instead (`fui:widget-close`,
root lifted out) and the navigate catalog pass hydrates the fresh
node, so two live roots for one registration never coexist.

Closing is unchanged: the widgets module's navigate listener closes
every open modal widget (backdrop'd drawers and modals ride
`_modalStack`) opened on the page being left, a fetched root is
removed from the DOM, an SSR-hydrated root is re-hidden, the chrome
cache is cleared, and the next open re-fetches and fires
`fui:widget-open` again. Consumers that bind into chrome rebind on
every `fui:widget-open` rather than assuming a root mounts once.

**An open belongs to the navigation it was issued under.** Both
navigators bump the shared `_navEpoch` synchronously at entry, and
every commit dispatches `gofastr:navigate` under its own live epoch.
An open is stale only when a navigation that BEGAN after it commits:
then a chrome or deep-link module that lands late mounts nothing and
writes no deep link, and a chrome failure raises no toast. An open
issued while a navigation was already in flight (the reader clicked a
link, then a trigger in chrome both pages share before the page
painted) lands on the destination: the commit neither drops it nor,
for a modal, closes it, and the catalog re-fetch's `_syncDeepLinks`
does not close it while its deep link is still being written under the
same epoch and URL. Comparing chrome-cache objects, the earlier test,
read every commit as a leave and dropped such opens without a word
(the `TestTrackerBellPopoverE2E` flake; pinned by
`TestOpenDuringNavigationSurvivesCommit` and, for the stale side,
`TestStaleChromeSkipsNextPage`).

---

## What lives where

```
core-ui/
  app/         : screen + router + layout + request-in-context helpers
                 (the URL → rendered page pipeline)
  di/          : dependency injection container (used by app)
  html/        : semantic HTML primitives, 1:1 with HTML tags
                 (Div, Button, Heading, Form, Table…)
  patterns/    : composed UI patterns (not 1:1 with HTML):
                 accordion, breadcrumbs, combobox, disclosure,
                 infinitescroll, multiselect, nestedlist, progress,
                 scrollspy, skeleton, sortablelist, tabs, tree
                 (the pagination pattern is retired; the pager is
                 framework/ui.Pagination over headless.Pagination)
  component/   : Component / InteractiveComponent interfaces (the contract
                 every renderable satisfies)
  interactive/ : declarative data-cui-* attribute builders (RPC, signal
                 bindings, widget chaining) that wrap render.HTML with no JS
  node/        : the JSON-clean serializable UI element tree (first-party IR;
                 dependency-free, composed by app spec codegen and Kiln)
  noderender/  : walks a node.Node tree and emits HTML via core-ui/html
                 (treats the IR as untrusted; see RenderTrustedNode)
  uinodev1/    : the closed ui.node.v1 wire type + validator for
                 process-isolated third-party modules (makes runtime-attribute
                 forgery unrepresentable, unlike the first-party node IR)
  compute/     : process-global Web Worker + WebAssembly asset registry
  widget/      : island/widget builder + registration
  widget/preset/ : opinionated mounting shortcuts:
                 Modal, Drawer, Popover, ToastStack, Toast, Banner,
                 FloatingPanel, BottomSheet
  widget/theme/ : page-level theme tokens + utility classes
  registry/    : process-global catalog of components whose CSS ships as real
                 stylesheets loaded on demand (RegisterStyle → *Style handle)
  store/       : typed shared client state (Slices) seeded into the signal bus
                 at SSR; the server-declared reactive-value primitive
  island/      : runtime-side island manager
  seo/         : typed Schema.org structs → JSON-LD blocks for rich results
  urlsafe/     : the single URL-scheme allow-list every URL sink runs through
  runtime/     : runtime.js (client) + Go embed wrapper
  runtime/src/ : code-split runtime modules (loaded on demand):
                 action, actionloader, activelink, animate, compute,
                 computed, desktop, dragdismiss, dropdown, envelope,
                 formerrors, intercept, loading, parts, poll, popover,
                 preload, reveal, rpc, rtc, sse, textarea, transition,
                 widgetfocus, widgethelpers, widgetlinks, widgets, ws.
                 Component behaviour that binds a kit class is not
                 here: framework/headless and framework/ui register
                 theirs (headless-*, filedropzone, searchinput,
                 lightbox) and the kernel serves them by name.
  runtime/colorscheme.js : dark/light mode bootstrap (runs sync in <head>
                 before CSS parses, reads localStorage + OS hint,
                 sets data-color-scheme on <html>)
  style/       : theme structs, stylesheet builder, token resolution
  check/       : .ui.go linter + the no-pattern-BaseCSS / no-inline-* guards

framework/
  uihost/      : wires core-ui app onto framework.App router; serves
                 runtime.js, app.css (the legacy theme.css / styles.css
                 paths return 410 Gone); handles SSE; client-side
                 navigation partial-fetch endpoint
  static/      : SSG builder (renders every screen at build time)
  headless/    : the structure half of a design system: components that
                 render tags, roles, labelling and data-hui-* hooks with
                 no classes; a Classes value maps parts to classes; a
                 caller's
                 Parts (Attrs, Slots, Binds), Strings and Island are
                 typed and checked;
                 a harness pins every component at the nil Classes. See
                 `gofastr docs ui-headless`.
  ui/          : opinionated semantic components on top of core-ui
                 (see full list in the cheat sheet below)
  ui/theme/    : canonical framework theme tokens
  ui/icons/    : vendored Feather and Lucide SVGs, constants and name aliases;
                 ui.Icon owns the shared SVG wrapper and accessibility.
                 App registrations override exact names before built-in lookup.
                 sources.json pins each upstream, per-file retrieval time and hashes;
                 LICENSE.feather, LICENSE.lucide and generated data retain notices.
                 Categories/CategoryFor expose complete curated grouping without changing SVGs.
                 gallery.RenderIconCatalog composes Sticky + FilterToolbar for URL-backed
                 q/category filtering on the server. The site's article uses overflow-x:clip
                 on phones so the toolbar remains sticky against the window.
                 Compact + Dropdown on FilterToolbar keeps its search, filter trigger and
                 tools on one row. The icon browser uses this without a sticky heading or
                 theme toggle. CopyButton{Tile:true} supplies each whole-tile copy
                 control, with one 24px glyph above the name; the Grid minimum is 5rem.
                 --ui-copy-btn-tile-min-height sets its minimum height (default 6rem).
```

---

## Hard rules

1. **In-page state stays on its route: query params or islands, never a new route.** A list screen's own sort, page and filter live in its query string, and its links are navigations the client router intercepts; a region embedded in a page (a pager inside a card, an expand) is an island, so no `<a href="?p=2">` for it. Neither gets a path of its own.
2. **Never** re-implement pagination/sort/filter logic in JS. Server-side, always.
3. **Never** make user-action-driven updates flow through SSE. SSE is for server-pushed updates only. RPC is for user-initiated updates.
4. **Never** introduce a hard refresh as a fix. If you find yourself doing `location.href = …`, stop.
5. **Never** add new `data-cui-*` attributes without updating this doc and the runtime test suite.
6. **Always** start with `Screen.Load(ctx)` reading initial state (route params, query) and SSR-ing the first paint correctly.
7. **Always** prefer composing existing widget/preset shortcuts over building a new island from scratch.
8. **Modals + drawers can deep-link.** Toasts and dropdowns intentionally cannot. If you find yourself wanting a `?toast=…` URL, stop: toasts are ephemeral by definition.
9. **Animation durations and easings live on the theme** (`Theme.Durations`, `Theme.Easings`). Never hard-code `transition: transform 0.3s ease` in component CSS; read `var(--duration-…)` / `var(--easing-…)` so a single theme tweak retunes every surface.
9b. **One styling surface: apps and generators ship ZERO CSS.** All styling lives in the design system (component CSS via `registry.RegisterStyle`, layouts via `core-ui/app`, tokens via `core-ui/style`). A surface needing styling the components don't provide is a gap to fix *upstream*, never an inline rule, a `*BaseCSS` string, an invented `.mrd-`/`.gofastr-` class, or a property where a `var(--*)` token belongs. See "Failure 4" + "One styling surface". Survey `framework/ui`/`core-ui` for an existing primitive before hand-rolling.
10. **State-changing fetches from runtime modules must forward the page's CSRF token.** Read `document.querySelector('meta[name="csrf-token"]')` once per fetch and set `X-CSRF-Token` on the request. `OptimisticAction`'s runtime is the canonical example. Apps verify the token server-side. The runtime doesn't enforce; it just makes the value reachable so each call site doesn't re-implement the lookup.
11. **Async runtime modules mark the trigger `aria-busy="true"` during in-flight RPCs and ignore re-entry.** Without it, keyboard Enter/Space fires duplicate submits and screen readers don't announce the state change. Clear it on commit / idle. Do not reach for `disabled` to get the re-entry guard: disabling a focused button drops keyboard focus to the body, and `disabled` has other owners (a hidden conditional region disables its controls) whose decision a settlement must not undo; the kernel's `action` primitive is the reference (a click while `pending` is a no-op, `disabled` untouched).
12. **Per-instance state lives in a `WeakMap` keyed by the wrapper element**: never module-globals. Multiple instances of the same widget on one page (or two banners, two scrollspies) is a normal scenario; assuming "one per page" is a bug that lands a code review later. Track active instances in a sibling `Set` so SPA-nav teardown can disconnect observers / clear timers without leaking.
13. **Runtime modules `disconnect()`/`clearInterval()` per-instance state on `gofastr:navigate`.** SPA navigation replaces the page DOM; the old wrapper becomes detached but the IO / interval keeps a strong ref to its targets until explicitly torn down. Walk the active-instance Set, clean up, then re-scan.

---

## UI primitive cheat sheet

The framework ships base surfaces (`core-ui/widget/preset.*`) and
opinionated facilities (`framework/ui.*`). Pick the layer that matches
your need:

### Surfaces (widget presets)

| You want | Use | Notes |
| --- | --- | --- |
| Confirm a destructive action | `preset.Modal` + `framework/ui.ConfirmAction` | Or skip the modal entirely and put `data-cui-confirm="…"` on the submit button (or its form — the button wins). Gates any submit, native or RPC. |
| Edit/show entity detail | `preset.Modal` with `DeepLink("modal", "<name>").DeepLinkParam("id")` | URL stays consistent across refresh/share/back. Buttons opening it carry `data-cui-deeplink="id=<row-id>"`. |
| Confirm + act in one shot | `framework/ui.ConfirmAction` | Returns a trigger button + hidden `preset.Modal` alertdialog. Eliminates per-button confirm boilerplate. |
| Secondary nav / filters | `preset.Drawer` | Edge-anchored, backdrop'd. Same deep-link wiring as modals. |
| Click-triggered help / share / inline expander | `preset.Popover` | Anchored floating surface, no backdrop dim, no focus trap. Escape and click-outside dismiss. While a modal that refuses Escape is open, Escape closes only a popover opened after that modal; one opened before it stays open under the scrim. |
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
| One reading against a range (CPU, latency budget, disk) | `framework/ui.Gauge` | Pure-SVG semicircle dial on an ARIA `meter`. `Bands` color the arc green / amber / red by the reading. |
| Two measures against each other (seats vs MRR) | `framework/ui.ScatterChart` | Pure-SVG points on two clean-stepped axes, one palette color per series, a `<title>` per point. |
| Values over two categories (sign-ups by weekday and hour) | `framework/ui.Heatmap` | Pure-SVG grid shaded in four strengths of one palette color; NaN cells draw empty. |
| Inline trend sparkline | `framework/ui.Sparkline` | Pure-SVG, no JS. Line or area shape. Pairs with Card for metric-overview surfaces. |
| Unified diff viewer | `framework/ui.DiffViewer` | Line-by-line, unified or split layout. Parses standard `diff -u` format. |
| Collapsible JSON tree | `framework/ui.JSONViewer` | Nested `<details>`/`<summary>` collapse. No JS. Configurable open depth. |
| Image gallery | `framework/ui.Gallery` | Grid / strip / masonry layouts. Optional Lightbox or per-item Href click behaviour. |
| Image lightbox | `framework/ui.Lightbox` | Overlay viewer with Prev/Next nav, gallery groups, arrow-key keyboard nav. |
| Image carousel / slider | `framework/ui.Carousel` | Renders headless.Carousel: a native scroll-snap track with fragment-anchor controls; the registered `headless-carousel` module binds the active slide, keyboard and rotation. |ev/Next, pagination dots, optional auto-rotate and loop. |

### Navigation & structure

| You want | Use | Notes |
| --- | --- | --- |
| Primary navigation | `framework/ui.Sidebar` | Inline column ≥ md, hamburger + `preset.Drawer` < md, same content tree, active-route highlighting from the current URL. |
| Site top bar, footer, docs page | the app's own package with an owned sheet | Copy `examples/acme-site/{siteheader,sitefooter,helpdocs}`: `html` elements, `headless.Disclosure` for the phone menu, tokens for every dimension. The framework ships no frame component. |
| Dominant record, incident, or operational summary | `framework/ui.RecordSummary` | Bounded status, next-decision, signal, compact support rail, ownership, and natural-width action slots. Actions stay in the lead region and move ahead of support context on phones. One page summary; do not duplicate it in a Banner. |
| Compact related signals without a card grid | `framework/ui.MetricBand` | Semantic description list; one flat row wide, two columns on phones, with an odd final signal spanning the row instead of leaving an empty quadrant. |
| Action menu on a row | `framework/ui.Menu` | Renders headless.Menu: a native `<details>` disclosure (Esc / SPA-nav close come free) whose keyboard contract the registered `headless-menu` module binds. |
| Command palette (Cmd+K) | `framework/ui.CommandPalette` | Modal + `headless.Combobox` (bound by the registered `headless-combobox` module). Debounced server search, keyboard nav, listbox selection. Returns trigger + preset pair. |
| Global search input | `framework/ui.GlobalSearch` | Renders headless.Combobox (island-backed search with a no-script GET form) with a `/` focus chord bound by the registered `headless-navigation` module. |
| Page-width wrapper | `framework/ui.Container` | Max-width page wrapper with breakpoint-aware padding. Narrow / default / wide / full variants. |
| Vertical/horizontal spacing | `framework/ui.Stack` / `Cluster` / `Grid` / `Center` / `Spacer` / `Box` | Six spatial primitives sharing one stylesheet (all in `layout.go`). `Cluster` wraps by default; `ClusterConfig.NoWrap` is the explicit compact-chrome opt-out. `BoxConfig.Bounded` caps a Box's height (`--ui-box-bounded-size`) and scrolls the rest. Replace hand-rolled `display:flex` divs. |
| Labelled content surface | `framework/ui.Card` | Header / body / footer regions, elevated / outlined / flat variants, optional interactive (whole surface is an `<a>`) form. |
| Toolbar of action buttons | `framework/ui.Toolbar` | `role="toolbar"` wrapper with optional named groups. Horizontal strip of buttons/links with visual separators. |
| Step progress indicator | `framework/ui.ProgressSteps` | Linear step indicator (upcoming / current / complete). Horizontal or vertical. `<ol>` with `aria-current="step"`. |
| Table of contents | `framework/ui.TableOfContents` | Server-rendered from explicit `Items` (required — the no-script contract is the rendered list). `headless-toc` marks the active entry through the observer `headless-rail` owns. |
| In-page rail | `framework/ui.AnchoredRail` | Sticky `<aside>` of fragment links with optional eyebrow/count chips; `headless-rail` marks the entry whose section is in view (`aria-current` + class). |
| Timeline / event history | `framework/ui.Timeline` | Vertical event list on a rail. Colored dot variants (neutral / success / warn / danger / info). Semantic `<ol>`. |

### Forms & inputs

| You want | Use | Notes |
| --- | --- | --- |
| Form layout / field grid | `framework/ui.Form` | Label + input + error wiring. `FieldErrors`-aware validation display. |
| Labelled text input | `framework/ui.FormField` | Wraps any input with label, help text, and error display. |
| Multi-line text input | `framework/ui.TextArea` | Labelled `<textarea>` with optional autogrow (`data-cui-autogrow`). |
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
| Drag-drop file picker | `framework/ui.FileUpload` | Native `<input type="file">` is the source of truth; the headless module's `data-hui-drop` hooks add drag-zone enhancement, list the chosen names and announce the pick. |
| Drag-drop with image preview | `framework/ui.FileDropzone` | File input + drop zone + FileReader image previews. |
| Combobox (autocomplete search) | `framework/ui.Combobox` | Typed combobox over the headless primitive: labelled input with `role=combobox` wiring, a GET-form no-script fallback to the same endpoint, an RPC-driven listbox whose options are real links. Used by CommandPalette and GlobalSearch. |

### Feedback & status

| You want | Use | Notes |
| --- | --- | --- |
| Inline loading indicator | `framework/ui.Spinner` | `role="status"` + `aria-busy`, ring / dots variants. Pairs with `aria-busy` on the containing form during RPC. |
| Notification bell + dropdown | `framework/ui.NotificationBell` | Bell button + unread badge + `preset.Popover` item list. SSE-driven via signal bindings. |
| Ephemeral notification toast | `framework/ui.Notification` | Styled toast content (title + body + variant). Drop inside `preset.Toast` or `preset.ToastStack`. |
| Section break | `framework/ui.Divider` | Native `<hr>` for plain horizontal dividers; `role="separator"` for vertical or labelled (e.g. "OR" between auth options). |
| Hover/focus hint on a control | `framework/ui.Tooltip` | CSS-only reveal, `aria-describedby` auto-wired. No JavaScript. |
| Dismissible banner | `framework/ui.Banner` | Full-width banner with optional session-persisted dismiss (`data-hui-system-dismiss`). |
| Pill: filter chip / applied filter / selection | `framework/ui.Tag` | Status variants, optional `Href` (filter link) or `Dismiss` (× RPC). Distinct from `StatusBadge` (read-only). |
| Active filter chip toolbar | `framework/ui.FilterChipBar` | Toolbar of removable filter chips. Optional "Clear all" action. Island-driven, signal-swapped. |
| Copy-to-clipboard button | `framework/ui.CopyButton` | Targets any element by id (`data-hui-copy-target`); no clipboard mutation without script. |

### Visual primitives

| You want | Use | Notes |
| --- | --- | --- |
| Named outline icon | `framework/ui.Icon` + `framework/ui/icons` constants | 291 canonical drawings and 13 name aliases on a 24 by 24 grid, two-unit rounded stroke, `currentColor`. Default size 20; `AriaLabel` opts into `role="img"`, otherwise decorative. Named framework icons and the site's header use this catalog. `RegisterIcon` accepts trusted app additions and exact-name overrides. |
| Responsive lazy-loaded imagery | `framework/ui.OptimizedImage` | `<picture>` + `srcset`, lazy by default, mandatory `Width`+`Height` to eliminate CLS. |
| Low-fidelity image placeholder | `Placeholder` on `framework/ui.OptimizedImage` / `PipelineImage` | Takes an inline raster `data:` URI (from `framework/image.BlurHashDataURL` or `Image.Placeholder`) and renders it as a stacked `<img>` behind the real one, positioned by static CSS. Zero JS: a per-instance value cannot go into CSS here (inline style attributes are blocked by the CSP and the `noinlinestyles` linter, and a data URI cannot be enumerated into a class), so an element is the only mechanism. Unusable values are dropped rather than fatal: a placeholder is data, not a caller bug. |
| Overlapping avatar stack | `framework/ui.AvatarGroup` | Readable 10% negative-margin stack with compact corner presence dots and an adaptive-surface "+N" overflow indicator. Size propagates to children. |
| Animated number counter | `framework/ui.AnimatedCounter` | Ticks from → to on scroll-into-view. Respects `prefers-reduced-motion`. |
| Text link (inline / action / muted) | `framework/ui.Link` | Three variants: inline prose link, 44px action link, subdued muted link. |
| Markdown rendered as HTML | `framework/ui.Markdown` | Themed prose wrapper over `core/markdown`. Headings, lists, code blocks get theme tokens. |
| Keyboard shortcut hint | `framework/ui.ShortcutHint` | Platform-aware mod-key glyphs (⌘ vs Ctrl) via `data-cui-os`. |
| Theme override for a subtree | `framework/ui.Themed` | Wraps any content in a `.cui-theme-<hash>` div for section-level theming. |
| Sortable drag-and-drop list | `framework/ui.SortableList` | Drag reorder + keyboard reorder on the headless primitive; the registered `headless-sortablelist` module POSTs the new order and reverts on non-2xx. |
| Expandable tree view | `framework/ui.Tree` | WAI-ARIA treeview on the headless primitive; lazy-load children via the toggle's `data-cui-rpc` on expand. Arrow-key nav, `data-hui-tree`. |

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
<button data-cui-open="user-edit" data-cui-deeplink="user_id=42">Edit</button>
```

Result: clicking the button opens the modal AND pushes
`?modal=user-edit&user_id=42` onto the URL. Refresh, share, and the
browser back button all keep the modal open / closed correctly. The
signal seed runs before the modal becomes visible so the form is
already filled in.

The layout envelope calls the core document-sync hook after navigation. Widget deep-link parameters are applied by the widgetlinks demand module.
