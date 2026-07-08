# Widgets — `core-ui/widget`

Widgets are self-mounting overlay UIs that run on top of any page. They
are distinct from components:

| Component | Widget |
|---|---|
| Part of a server-rendered page tree | Floats above any page |
| Rendered when its parent renders | Mounts itself via a script tag |
| Tied to a request's render pass | Long-lived, signal-driven |

Examples of widgets the framework already supports:

- **FloatingPanel** — corner-anchored chat / devtools / agent surface
- **Modal** — center dialog with backdrop, ESC + click-outside dismiss
- **Toast** — ephemeral bottom notifications
- **Drawer** — edge-mounted sliding panel
- **Banner** — top strip for build progress, version warnings, etc.
- **Popover** — click-triggered anchored surface, no backdrop dim, no focus trap. ESC + click-outside dismiss. Use for help panels, share menus, per-row expanders.

`kiln/chat/panel.go` is the canonical real-world consumer: the agent
chat panel is implemented as a `FloatingPanel` widget.

## Quickstart

```go
import (
    "github.com/DonaldMurillo/gofastr/core-ui/widget"
    "github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
)

panel := preset.FloatingPanel("my-panel").
    Slot("body", myBodyComponent).
    Signal("counter", widget.SignalFunc(readCounter)).
    SSE("/.events", "tick", "counter").
    RPCWithSignal("POST", "/api/inc", incHandler, "counter").
    Build()

widget.Mount(router, &panel)
// or in one step: widget.MountBuilder(router, preset.FloatingPanel("my-panel").…)
```

To open a widget from the page, wire any element with
`data-fui-open="<widget-name>"` — the runtime handles the click, shows
the widget, and (for modal surfaces) moves focus in:

```html
<button data-fui-open="my-panel">Open panel</button>
```

Widgets built with `.Hidden()` (and click-to-open presets like
`preset.Popover`) stay closed until such a trigger — or a matching
deep-link URL — opens them.

`Mount` registers the widget's HTTP routes and adds it to the process-wide
registry. Any page that carries the framework runtime auto-mounts every
registered widget — the runtime fetches `/__gofastr/widgets` on boot and
builds each one. Pages served through `framework/uihost` get the runtime
injected automatically; a bare-router host calls `widget.MountRuntime(r)`
once and embeds `widget.RuntimeTag()` in its page HTML.

## Anatomy

A widget is described by a `widget.Definition`:

```go
type Definition struct {
    Name      string                       // unique id; routes derive from it
    Position  Position                     // BottomRight, Center, Top, …
    Bootstrap BootstrapMode                // AutoScript (default) | Embedded
    Slots     []Slot                       // host-supplied content regions
    Signals   map[string]SignalSource      // server-side data → client signals
    SSE       []SSEBinding                 // event stream → signal updates
    RPCs      []RPCEndpoint                // client buttons/forms → server handlers
    Skeleton  func(slots) render.HTML      // optional custom chrome
    Backdrop  bool                         // dim the page behind
    CloseOnEscape, CloseOnClickOutside bool
}
```

Most fields have defaults; the fluent `widget.New(name).…` builder
fills them in idiomatically.

### Slots

Slots are named content regions. The framework renders the widget
chrome (positioning, focus management, backdrop) and embeds each
slot's component at the matching `<div class="fui-slot fui-slot-<name>">`
placeholder.

```go
panel := widget.New("notifications").
    Slot("header", headerComponent).
    Slot("body",   listComponent).
    Slot("footer", composeComponent).
    Build()
```

Canonical slot names are `header`, `body`, `footer` — they render in
that order. Other names render after the canonical three.

#### Slot surface contract — what the chrome paints vs what the body owns

The widget chrome owns the *panel* every surface sits on; the slot
component owns only its internal content and layout. Per position:

- **Drawers** (`Edge`, `EdgeRight`) — the position container paints
  the surface background, shadow, and scroll. Slot bodies add their
  own internal padding.
- **Bottom sheets** (`Bottom`) — the position container paints the
  surface background, shadow, and rounded top corners, caps height at
  75vh with internal scroll, and gives the slot default padding (the
  drag handle sits above it).
- **Anchored popovers** — the widget root paints the surface, border,
  radius, shadow, size caps, and the directional arrow.
- **Centered modals** (`Center`) — the chrome groups every slot inside
  a single `.fui-panel` element and paints the default panel on it:
  `var(--color-surface)` background, border, radius, padding, shadow,
  `min/max-inline-size` caps, and `overflow: auto` so tall content
  scrolls inside the dialog. A modal using `header`, `body`, and
  `footer` slots therefore reads as ONE dialog card, and a plain
  `preset.Modal` looks like a dialog out of the box — bodies must
  **not** re-paint background / padding / radius on their root (that
  double-pads the panel).

Full-bleed modal bodies (media viewers, custom chrome) opt out of the
centered panel so the body owns every pixel: the panel stays a
transparent container with no background, border, padding, or shadow.
`framework/ui.Lightbox` is the canonical bare body — it centers the
image directly on the backdrop, no card.

Two rules govern the opt-out (the selector is `.fui-pos-center >
.fui-panel:not(:has(> .fui-slot > …))`):

1. **The marker must be on the slot content's ROOT element** — the
   direct child of `.fui-slot`. `.fui-slot-bare`, `[data-fui-lightbox]`,
   and `[data-fui-comp="ui-cmd-palette"]` all qualify. A wrapper
   `<div>` between `.fui-slot` and the marker defeats it:
   `.fui-slot > <div> > .fui-slot-bare` does NOT match
   `:has(> .fui-slot > .fui-slot-bare)`, so the panel re-paints.
2. **One bare slot opts the WHOLE panel out.** The opt-out sits on the
   `.fui-panel`, which wraps every slot, so a single bare slot drops
   the panel chrome for the header and footer too. Bare means "this
   body owns all the chrome" — if you need a card around some slots
   but not others, paint that surface inside the bare slot rather
   than relying on the framework panel.

### Signals

A **signal** is a named server-side value the runtime keeps in sync
with `[data-fui-signal="<name>"]` DOM nodes. The widget framework
fetches initial values from `/<basePath>/state` and updates them via
SSE bindings.

```go
panel := widget.New("p").
    Signal("count", widget.SignalFunc(func() (any, error) {
        return atomic.LoadInt64(&counter), nil
    })).
    Build()
```

In your slot HTML:

```html
<span data-fui-signal="count">0</span>
```

The runtime updates `textContent` whenever the signal changes.
For HTML content, use `data-fui-signal-mode="html"`. For attribute
values, use `data-fui-signal-mode="attr"` plus
`data-fui-signal-attr="value"` (or whichever attr).

### SSE bindings

When an SSE event arrives, its payload becomes the new value of a
named signal. Hosts already serving an event stream just declare the
mapping:

```go
.SSE("/.kiln/events", "world_edit", "world_summary")
```

On every `world_edit` event from `/.kiln/events`, the bootstrap pushes
the event's `data` (JSON-decoded if possible) into `world_summary`,
and any `[data-fui-signal="world_summary"]` node re-renders.

### RPCs

A button or form click can invoke a server handler:

```go
.RPCWithSignal("POST", "/api/inc", incHandler, "count")
```

Slot HTML wires it via `data-fui-rpc`:

```html
<button data-fui-rpc="/api/inc" data-fui-rpc-signal="count">+1</button>
```

The runtime POSTs to the path; on success the response (parsed as
JSON if `content-type: application/json`, else as text) flows into the
named signal.

For forms, set `data-fui-rpc` on the `<form>` itself; the runtime
serializes inputs into a JSON body.

For RPCs that don't update a signal, drop the `…WithSignal` suffix:

```go
.RPC("POST", "/api/log-out", logoutHandler)
```

### Custom request body

Override the JSON body the runtime sends with `data-fui-rpc-body`:

```html
<button
  data-fui-rpc="/kiln/panel/approve_plan"
  data-fui-rpc-body='{"plan_id":"p1"}'
  data-fui-rpc-signal="chat_html"
>Approve</button>
```

### Close action

Any element with `data-fui-action="close"` dismisses the widget:

```html
<button data-fui-action="close">×</button>
```

### Recipe: a form inside a modal

Forms inside a widget are **owned by the widget runtime, not the page
runtime**. The core dispatcher deliberately skips any click or submit
inside `[data-fui-widget]` — each mounted widget installs its own
scoped handler that intercepts `form[data-fui-rpc]`, prevents the
native submit, and does the RPC round-trip (`fetch` with the form
serialized to JSON by input `name`, or multipart when a file input is
present). A widget form therefore never navigates the page; the modal
stays open across validation errors, and two attributes handle the
success path:

| Attribute | On | Effect after a 2xx response |
|---|---|---|
| `data-fui-rpc-close` | the form (or any RPC trigger) | Dismisses the widget |
| `data-fui-rpc-reset` | the `<form>` only | Calls `form.reset()`, clearing the fields for the next open |

Both are boolean attributes (presence, no value) and both only fire on
success — a non-2xx response leaves the modal open and untouched, and
writes `{ok: false, status, text}` into the form's
`data-fui-rpc-signal` so an error node can display it. A network
failure writes `{ok: false, status: 0, text: "Network error — please
try again"}` to the same signal.

End to end:

```go
form := render.HTML(`
  <form data-fui-rpc="/api/notes" data-fui-rpc-close data-fui-rpc-reset
        data-fui-rpc-signal="note-error">
    <label>Title <input name="title" required></label>
    <div data-fui-signal="note-error"></div>
    <button type="submit">Save</button>
    <button type="button" data-fui-action="close">Cancel</button>
  </form>`)

modal := preset.Modal("new-note").
    Hidden().
    Slot("body", app.NewStaticComponent(form)).
    RPC("POST", "/api/notes", http.HandlerFunc(createNote)). // route registered on Mount
    Build()
widget.Mount(router, &modal)
```

```html
<!-- anywhere on the page -->
<button data-fui-open="new-note">New note</button>
```

Details worth knowing:

- The centered modal chrome already paints the dialog panel (surface,
  border, padding on the `.fui-panel` that wraps the slots — see the
  slot surface contract above), so the form goes straight into the
  `body` slot with no wrapper card. Full-bleed bodies opt out with
  `fui-slot-bare`.
- While the RPC is in flight the form gets the `fui-loading` class and
  `aria-busy="true"`.
- The dispatch sends `X-FUI-Widget: <name>` and forwards the page's
  `<meta name="csrf-token">` as `X-CSRF-Token`, so `auth.CSRF`-guarded
  handlers work without a hidden `_csrf` field.
- On success the handler can additionally steer the UI:
  `data-fui-rpc-open="<widget>"` opens another widget (save in a
  drawer → open a results sheet), `data-fui-rpc-navigate="/path"` does
  an SPA navigation (cache-bypassing, and it re-renders even when the
  path is the page the widget floats over — so a quick-add modal can
  refresh the list it inserts into), and an `X-Gofastr-Toast` response
  header shows a toast.
- `data-fui-rpc-close` also works on a plain button RPC — "Confirm →
  do the thing → dismiss" needs no form at all (that's how
  `ui.ConfirmAction` is built).

## Routing

`widget.Mount(router, &def)` registers the per-widget HTTP routes:

| Path | Purpose |
|---|---|
| `GET  <StylePath>` (default `/core-ui/widget/<name>/style.css`) | Theme-resolved widget styles |
| `GET  <StatePath>` (default `/core-ui/widget/<name>/state`) | JSON snapshot of every named signal |
| `GET  /core-ui/widget/<name>/chrome` | Rendered chrome HTML, fetched lazily on first open |
| `<RPC method> <RPC.Path>` | Each registered RPC handler |

Default paths are filled in on `def` if unset, so the caller can read
them after `Mount` returns. Mounting is idempotent on `def.Name`. The
process-wide runtime routes (`/__gofastr/runtime.js`, `/__gofastr/widgets`)
come from `widget.MountRuntime(r)` — once per host, not per widget.

## Theming

Widgets resolve through `core-ui/style` and pick up the framework
default theme out of the box. Token overrides flow through:

1. `core-ui/widget/theme.PageTheme(overrides ...style.Theme)` returns
   a merged `style.Theme`. Use it to author custom widget chrome.
2. Or rely on the default — `widget.Mount` builds a stylesheet with
   `:root` CSS variables for every token.

Kiln's `set_theme` tool (see `kiln/protocol`) is the canonical example:
the agent (or a host) updates `world.App.Theme` and the next
`/kiln/theme.css` request reflects the new palette.

## Strict CSP

The framework runtime is **strict-CSP safe**. The bootstrap never:

- emits inline `style=` attributes
- attaches inline event handlers (`onclick=`, etc.)
- evaluates strings as code

`kiln/render` additionally drops dangerous attrs server-side
(`style`, `srcdoc`, `on*=`) so a bad agent turn can't poison the page.

Use class names that map to theme tokens. The page theme exposes
ready-made utility classes (`kiln-section`, `kiln-card`, `kiln-grid-3`,
`kiln-button`, `kiln-hero`, etc.) — `docs/widgets.md` is the canonical
reference for what's available; `core-ui/widget/theme/page.go` is the
source of truth.

## Testing

`examples/site` exercises every widget surface end-to-end —
Modal (`/components/modal`), Drawer (`/components/drawer`), Toast
(`/components/toast`), Menu (`/components/menu`), Sidebar
(`/components/sidebar`), and the trigger-anchored Popover preset
(`/components/popover`). The chromedp tests in
`examples/site/e2e_*_test.go` cover open + dismiss flows, focus
trap, scroll lock, deep-linking, anchored placement + auto-flip,
scroll-tracking, and the trigger-active highlight contract.

For backend-only verification (no chromedp), see
`core-ui/widget/widget_test.go` and
`core-ui/widget/preset/preset_test.go` — they cover the builder
semantics, mounted route surface, preset defaults, and JSON state
encoding.

## Common mistakes

- **Expecting `Mount` to return a script tag.** It returns nothing —
  it registers routes and adds the widget to the process registry. The
  widget appears only on pages that carry the framework runtime, which
  auto-mounts everything in `/__gofastr/widgets`. If your widget never
  shows up, the page is missing the runtime: `framework/uihost` pages
  get it injected; bare hosts must call `widget.MountRuntime(r)` and
  embed `widget.RuntimeTag()` themselves.
- **Forgetting `data-fui-rpc-signal`.** The RPC fires and succeeds,
  but the response goes nowhere — no DOM update. Name the target
  signal on the trigger (`data-fui-rpc-signal="count"`) or register
  the binding with `RPCWithSignal`.
- **Inline `style=` / `onclick=` in slot HTML.** The default CSP
  blocks both. Use theme-token class names for styling and the
  `data-fui-*` attributes (`data-fui-rpc`, `data-fui-action="close"`)
  for behavior — `kiln/render` strips the dangerous attrs server-side
  anyway.
- **Expecting the page runtime to handle a widget's form.** The core
  dispatcher skips everything inside `[data-fui-widget]`; the widget's
  own scoped handler owns `form[data-fui-rpc]`. A plain `<form
  action=…>` inside a modal (no `data-fui-rpc`) does a native
  full-page submit — put `data-fui-rpc` on the form and use
  `data-fui-rpc-close` / `data-fui-rpc-reset` for the success path
  (see the form-in-a-modal recipe above).
- **Building in-page content as a widget.** Widgets are overlays that
  float above any page. A sortable table, a form section, or anything
  that belongs to one page's render tree is a component (or an island)
  — see the component/widget table at the top of this doc.
