# Pane-host: primary pane + openable side panes

`framework/ui.PaneHost` is a layout shell with one always-visible **primary** pane and one or two openable side panes (`secondary` and `tertiary`). It owns pane visibility, focus handoff/restore, optional URL deep-linking, and mobile overlay collapse—not data fetching.

Use it for master/detail inspectors, list + preview workspaces, or any screen where side panels open alongside persistent content.

```go
import "github.com/DonaldMurillo/gofastr/framework/ui"

body := ui.PaneHost(ui.PaneHostConfig{
    Primary:        customerList,    // required (panics if empty)
    Secondary:      detailRegion,    // optional
    Tertiary:       inspectorRegion, // optional
    SecondaryOpen:  false,           // SSR first-paint state
    SecondaryLabel: "Customer details",
    ID:             "customers-host",
})
```

Closed side panes ship with `hidden` from SSR so first paint matches state with zero flash and zero inline `style=`. Customize widths via `--fui-pane-host-secondary-w` (360px), `--fui-pane-host-tertiary-w` (300px), and `--fui-pane-host-drawer-w` (420px).

## Driving panes & loading content

Use the Go helpers in `core-ui/interactive` (`OpenPaneOnClick`, `ClosePaneOnClick`, `SwapPaneOnClick`) or the underlying delegated attributes:

| Attribute | Effect |
|---|---|
| `data-hui-pane-open-control="secondary\|tertiary"` | Opens the pane, moves focus in, remembers opener |
| `data-hui-pane-close="[secondary\|tertiary]"` | Closes that pane (or topmost open pane when empty) and restores focus |
| `data-hui-pane-swap="secondary\|tertiary"` | Opens the named pane and closes its side sibling |
| `data-hui-pane-host-target="<host-id>"` | Targets a `PaneHost` outside the trigger's ancestor chain |

Programmatic JS API: `__gofastr.openPane(id, slot)`, `__gofastr.closePane(id, slot)`, `__gofastr.swapPane(id, slot)`. The host emits bubbling `pane-host:open` and `pane-host:close` events (`detail: { pane }`).

To populate a side pane from the server, compose with standard RPC + HTML signals—don't invent a separate fetch layer:

```html
<div data-hui-pane="secondary" role="region" aria-label="Details">
  <div data-cui-signal="customer-detail" data-cui-signal-mode="html"></div>
</div>

<a href="/api/customers/42/detail"
   data-cui-rpc data-cui-rpc-method="GET"
   data-cui-rpc-signal="customer-detail"
   data-hui-pane-open-control="secondary">View</a>
```

## Deep-linking a pane (`?pane=...`)

To make an open pane shareable and Back/Forward-aware without turning it into a separate route, set `DeepLinkParam` on `PaneHostConfig` and attach `interactive.PaneKey` to keyed triggers:

```go
// In the screen's Load/Render, parse the query param for SSR first paint:
slot, key, ok := ui.PaneDeepLink(r.URL.Query(), "pane")
detail, open := emptyState, false
if ok && slot == "secondary" {
    if t, found := ticketByID(key); found { // always look up; treat URL key as untrusted
        detail, open = renderTicket(t), true
    }
}

ui.PaneHost(ui.PaneHostConfig{
    Primary:       queue,
    Secondary:     detail,
    DeepLinkParam: "pane", // writes ?pane=secondary:<key>
    SecondaryOpen: open,
})
```

```go
interactive.PaneKey(interactive.OpenPaneOnClick(row, "secondary"), ticket.ID)
```

Opening a keyed trigger writes `?pane=secondary:<id>`, closing removes it, and Back/Forward replays the trigger's RPC so the pane returns filled. Triggers without `PaneKey` open normally without changing the URL.

## Responsive collapse

Below `768px`, an open side pane switches to a fixed right-edge overlay drawer (`data-hui-pane-mode="overlay"`) with a backdrop scrim, Tab focus trap (deferring to any open modal dialog), refcounted scroll lock, and Escape/backdrop dismiss. Inline desktop panes stay page content and ignore Escape.

## Common mistakes

- **Modeling pane open/close as routes.** Side panes are in-page state—use `DeepLinkParam` for shareable URLs, or a real screen if the detail deserves its own page and title.
- **Setting `DeepLinkParam` without reading `ui.PaneDeepLink` during SSR.** Without server-side initial rendering, shared links paint closed and pop open after hydration.
- **Manually removing `hidden` or adding inline `style=` widths.** Use `SecondaryOpen`/`TertiaryOpen` for SSR state and `--fui-pane-host-*-w` CSS variables for widths.
- **Changing the 768px breakpoint in only CSS or JS.** Both `@media (max-width: 768px)` and the module's `matchMedia` must stay in sync.
