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

scriptTag := widget.Mount(router, &panel)
```

Drop `scriptTag` into any HTML page; the widget appears.

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

## Routing

`widget.Mount(router, &def)` registers the per-widget HTTP routes:

| Path | Purpose |
|---|---|
| `GET  <BootstrapPath>` (default `/core-ui/widget/<name>/bootstrap.js`) | Per-widget loader script |
| `GET  <StylePath>` (default `/core-ui/widget/<name>/style.css`) | Theme-resolved widget styles |
| `GET  <StatePath>` (default `/core-ui/widget/<name>/state`) | JSON snapshot of every named signal |
| `<RPC method> <RPC.Path>` | Each registered RPC handler |

The returned string is the `<script src="…"></script>` tag; embed it
in any page that should host the widget.

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

`examples/widgets-demo` is the canonical end-to-end exercise:

- `TestWidgetMountsAndHydrates` — bootstrap mounts chrome onto a
  vanilla page and hydrates `[data-fui-signal]` from `/state`.
- `TestWidgetRPCUpdatesSignal` — clicking a `data-fui-rpc` button
  POSTs to the server; the response flows into the bound signal
  with no page reload.
- `TestModalWidgetClosesOnAction` — a center-mounted modal renders
  with a backdrop and dismisses on `data-fui-action="close"`.

For backend-only verification (no chromedp), see
`core-ui/widget/widget_test.go` — covers the builder semantics,
mounted route surface, and JSON state encoding.
