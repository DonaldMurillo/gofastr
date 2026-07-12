# Pane-host — primary pane + openable side panes

`framework/ui.PaneHost` is a layout shell: one always-visible
**primary** pane and one or two openable side panes — `secondary` and
`tertiary`. It owns the *pane lifecycle* (show/hide, focus handoff on
open, focus restore on close, and a responsive collapse). It does NOT
own content loading.

Use it for master/detail surfaces, inspector panels, a list + a live
preview — anywhere a primary region stays put while a side region opens
and closes around it.

```go
import "github.com/DonaldMurillo/gofastr/framework/ui"

body := ui.PaneHost(ui.PaneHostConfig{
    Primary:        customerList,
    Secondary:      detailRegion,    // optional
    Tertiary:       inspectorRegion, // optional
    SecondaryOpen:  false,           // SSR first-paint state (Hard Rule 6)
    SecondaryLabel: "Customer details",
    ID:             "customers-host",
})
```

`Primary` is required (`PaneHost` panics when empty, mirroring
`DataTable`/`DocLayout`). Each side pane is a labelled `role="region"`;
`SecondaryLabel`/`TertiaryLabel` default to `"Secondary"`/`"Tertiary"`.

## What it renders

A root `<div data-fui-comp="ui-pane-host" data-fui-pane-host>` with
three slot children, each carrying `data-fui-pane="primary|secondary|
tertiary"`. The grid is `display:grid`; the column count is derived
purely from open-state modifier classes on the root
(`ui-pane-host--secondary-open`, `--tertiary-open`), so **no inline
`style` is ever emitted** (strict CSP, Hard Rule 9b). A side pane closed
at first paint carries `hidden`; the runtime reveals it on open.

## Driving panes

Three attribute-driven triggers (delegated, so they survive island
swaps):

| Attribute | Effect |
|---|---|
| `data-fui-pane-open="secondary\|tertiary"` | Open that pane. Focus moves to the pane's first focusable (or the region); the trigger is remembered for restore. |
| `data-fui-pane-close="secondary\|tertiary"` | Hide it and restore focus to the opener. Bare attribute (no value) closes the topmost open pane. |
| `data-fui-pane-swap="secondary\|tertiary"` | Open the named pane and close the other side sibling — the "this link fills the third pane instead of navigating" flow. |

```html
<button data-fui-pane-open="secondary">Show details</button>
<!-- inside the secondary pane -->
<button data-fui-pane-close>Close</button>
```

A trigger resolves its host via the nearest `[data-fui-pane-host]`
ancestor. For a trigger that lives OUTSIDE the host (e.g. in a global
toolbar), set `data-fui-pane-host-target="<host-id>"` to the host's
`id`.

App code can also drive panes programmatically — the API mirrors
`openWidget`/`closeWidget`:

```js
__gofastr.openPane("customers-host", "secondary");
__gofastr.closePane("customers-host", "secondary");
__gofastr.swapPane("customers-host", "tertiary");
```

The host dispatches `pane-host:open` and `pane-host:close` events
(`{ bubbles: true, detail: { pane } }`) for observability — no
`data-fui-*` attribute is needed to receive them.

## Filling a pane from the server

`PaneHost` does NOT fetch pane content. To load a link's response into a
pane, use the existing RPC + signal rail: the trigger carries
`data-fui-rpc` + `data-fui-rpc-signal`, and the pane contains a region
bound to that signal in HTML mode:

```html
<div data-fui-pane="secondary" role="region" aria-label="Details">
  <div data-fui-signal="customer-detail" data-fui-signal-mode="html">
    <!-- RPC response HTML lands here -->
  </div>
</div>
```

A row trigger then both opens the pane and fires the fetch:

```html
<a href="/api/customers/42/detail"
   data-fui-rpc data-fui-rpc-method="GET"
   data-fui-rpc-signal="customer-detail"
   data-fui-pane-open="secondary">View</a>
```

Open/close is in-page state — never a URL route (Hard Rule 1). Optional
URL round-tripping (sharing a deep link that re-opens a pane) is a
future extension, out of scope for v1.

## Responsive collapse

Below `768px` (the breakpoint the CSS and the runtime module share —
they MUST stay in sync), an open side pane stops being an inline column
and becomes a fixed overlay drawer. When `matchMedia('(max-width:
768px)')` matches AND a pane is open, the runtime sets
`data-fui-pane-mode="overlay"` on the host; CSS repositions the open
pane to the right edge with a backdrop scrim (`::before`), and the
module applies:

- a **focus trap** — Tab cycles within the pane (reuses the shared
  `NS._focusSel` selector; it does not touch the widgets module's
  private `_modalStack`),
- a refcounted **scroll lock** — `__gofastr.doc.lockScroll('panehost:<id>')`,
  released on close / widen / navigate,
- **ESC** and **backdrop-click** to close.

Widening the viewport or closing the pane clears overlay mode and
releases the lock. Pane widths and the drawer width are themeable via
the `--ui-pane-host-secondary-w` / `--ui-pane-host-tertiary-w` /
`--ui-pane-host-drawer-w` custom properties (defaults 360px / 300px /
420px).

## Common mistakes

- **Treating pane state as a route.** Open/close/swap is in-page state
  (Hard Rule 1). Wiring it into the URL needs the future deep-link
  extension; for v1, render detail screens through the RPC→signal rail
  or SSR the pane open (`SecondaryOpen: true`) on a detail route.
- **Fetching pane content with a bespoke mechanism.** `PaneHost` is a
  layout shell, not a content loader. Use `data-fui-rpc` +
  `data-fui-rpc-signal` into a `data-fui-signal-mode="html"` region
  inside the pane — don't build a new fetch path.
- **Forgetting the `hidden` first-paint contract.** A closed side pane
  must ship `hidden` from SSR so first paint matches state (Hard Rule 6)
  and there is no flash. `PaneHostConfig.SecondaryOpen`/`TertiaryOpen`
  control this; don't try to open a pane by removing `hidden` yourself.
- **Editing the 768px breakpoint in only one place.** The CSS
  `@media (max-width: 768px)` and the module's
  `matchMedia('(max-width: 768px)')` MUST match, or the drawer and the
  grid collapse disagree.
- **Adding inline `style` to fix a column.** Column state comes from the
  root's open modifier classes; CSP forbids inline style. Theme widths
  via the `--ui-pane-host-*-w` custom properties instead.
