# Widgets: `core-ui/widget`

Widgets are self-mounting overlay UIs (modals, drawers, popovers, floating panels, toast stacks, banners) that float above any page and survive SPA navigation. Components live inside a page's render tree; widgets mount globally onto the router and bootstrap automatically through the runtime.

## Presets & quickstart

Built-in presets in `core-ui/widget/preset`:
- **`FloatingPanel`** — corner-anchored panel (chat, devtools, agent drawer).
- **`Modal`** — centered native `<dialog>` opened with `showModal()`, backdrop scrim, Escape & outside-click dismiss.
- **`Drawer`** / **`BottomSheet`** — edge-sliding panel (`Edge` / `EdgeRight`, RTL-aware) or 75vh bottom sheet.
- **`Popover`** — click-triggered anchored panel with auto-flip, no backdrop dim.
- **`ToastStack`** — ephemeral notification stack (`uihost` mounts a default `uihost.DefaultToastStack` if your app hasn't mounted one, so `X-Gofastr-Toast` always works).
- **`Banner`** — top strip for status or build notices.

```go
import (
    "github.com/DonaldMurillo/gofastr/core-ui/widget"
    "github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
)

panel := preset.FloatingPanel("my-panel").
    Slot("body", myBodyComponent).
    Signal("counter", widget.SignalFunc(readCounter)).
    RPC("POST", "/api/inc", incHandler).
    Build()

widget.Mount(router, &panel)
// Or in one step: widget.MountBuilder(router, preset.FloatingPanel("my-panel")...)
```

Open any widget from HTML with `data-cui-open="<name>"` (and close from inside with `data-cui-action="close"`):

```html
<button data-cui-open="my-panel">Open panel</button>
```

Pages served via `framework/uihost` load the widget runtime automatically; bare-router hosts call `widget.MountRuntime(r)` once and embed `widget.RuntimeTag()`. When multiple routers in one process share a widget, call `widget.MountShared(r, def)`. The runtime dispatches `gofastr:widgets-synced` on `window` (`detail.path`) after each boot or navigation catalog pass settles.

## Slots, chrome & native `<dialog>`

- **Slots:** Canonical slots render in order: `header`, `body`, `footer` (additional custom slots follow).
- **Native `<dialog>`:** Widgets with `Backdrop` (`Modal`, `Drawer`, `BottomSheet`, `Center`) render `<dialog>` and open with `showModal()`, giving top-layer stacking, `::backdrop` scrim, inert background, Escape/back-gesture dismissal, and focus return automatically.
- **Surface contract:** Centered modals paint one `.cui-panel` card around all slots (don't add another card/padding inside `body`). Full-bleed bodies (like `ui.Lightbox` or `ui.CommandPalette`) opt out by placing `.cui-slot-bare` directly on the slot's root element. Corner/banner panels opt into a card surface with `.Surface()`.

## Signals, polling & RPCs

- **Signals:** `.Signal(name, widget.SignalFunc(fn))` serves values from `/core-ui/widget/<name>/state` into `[data-cui-signal="<name>"]` nodes (`textContent` by default, or `data-cui-signal-mode="html|attr"`). Cross-origin state endpoints are never fetched.
- **Polling:** `.Poll(15 * time.Second)` re-fetches `/state` on a jittered cadence (100ms browser floor, pauses when hidden, backs off on error). Needs no SSE or fanout. For sub-second server pushes, use `island.Manager.PushUpdate` (see [presence.md](presence.md) and [live-dashboards.md](live-dashboards.md)).
- **RPCs:** `.RPC(method, path, handler)` registers the route (upper-case `GET`/`POST`/`PUT`/`PATCH`/`DELETE`, default `POST`). Route the response to a signal using `data-cui-rpc-signal="<name>"` on the trigger (and optional `data-cui-rpc-body='{...}'` for static JSON payloads).

### Forms inside a modal

The page runtime skips events inside `[data-cui-widget]`; each widget owns its own `form[data-cui-rpc]` submit handler (sends JSON or multipart, forwards `X-CSRF-Token` and `X-FUI-Widget`, never reloads the page):

```go
form := render.HTML(`
  <form data-cui-rpc="/api/notes" data-cui-rpc-close data-cui-rpc-reset
        data-cui-rpc-signal="note-error">
    <label>Title <input name="title" required></label>
    <div data-cui-signal="note-error"></div>
    <button type="submit">Save</button>
    <button type="button" data-cui-action="close">Cancel</button>
  </form>`)
```

On 2xx: `data-cui-rpc-close` closes the widget, `data-cui-rpc-reset` resets the form, `data-cui-rpc-open="<other>"` opens another widget, `data-cui-rpc-navigate="/path"` triggers a cache-bypassing SPA navigation (refreshing even the current page), and `X-Gofastr-Toast` / `ui.InvalidateScreens` work automatically. On non-2xx, the modal stays open and writes `{ok:false, status, text}` to the target signal.

## Lifecycle events & DOM lifetime

Listen on `document` for `fui:widget-open` (`{ name, root, hydrated, reinserted }`) and `fui:widget-close` (`{ name, root }`). Non-modal roots survive SPA navigation; open backdrop modals close on navigation (and modals opened inside an intercepted pane close with that pane).

## Routing, gating & per-trigger context (`data-cui-ctx`)

`widget.Mount` registers `/core-ui/widget/<name>/{style.css,state,chrome}` (prefixed if mounted on a router group; read final URLs via `def.StyleURL()`, `StateURL()`, `ChromeURL()`—note that embedded screens require unprefixed widget mounts).

- **Gating:** Widget routes are public by default. Set `Definition.RequireSession` (any browser session, anti-recon) or `Definition.RequireAuthenticated` (signed-in user only; rejects anonymous page sessions, embed grants, and scoped API tokens). Both fail closed if no host predicate is installed and govern SSR-inlined chrome (`widget.GateSatisfied`). Use `.ListedFor(fn)` to hide a widget from `/__gofastr/widgets` for callers who shouldn't see it (always pair with a real route/session gate).
- **Per-row chrome (`data-cui-ctx`):** Pass context from a trigger to a shared modal without registering N widgets:

```html
<button data-cui-open="layout-remove" data-cui-ctx="inv-42">Remove…</button>
```

Read it inside a context-aware slot with `widget.ChromeContext(ctx)` (validated ≤ `widget.MaxChromeContext` 256 bytes, valid UTF-8, no control chars; cached per `(name, ctx)` up to 32 entries and cleared on SPA navigation). **Always authorize and escape `ctx` in `RenderCtx`**—it comes from the browser and is untrusted.

## Strict CSP

The widget runtime emits zero inline `style=` attributes, zero `onclick=` handlers, and never `eval`s strings.

## Common mistakes

- **Expecting `widget.Mount` to render a script tag.** It registers routes and adds the widget to the catalog; bare routers must call `widget.MountRuntime(r)` and embed `widget.RuntimeTag()`.
- **Forgetting `data-cui-rpc-signal` on the trigger.** RPC responses only update a signal when the trigger names it.
- **Putting a plain `<form action=...>` inside a modal.** Without `data-cui-rpc`, the browser performs a full-page submit instead of an in-modal RPC.
- **Trusting `widget.ChromeContext(ctx)` without checking permissions.** Always verify the current user can access the referenced ID and escape it in markup.
- **Building regular page content as a widget.** Widgets are floating overlays; in-page regions belong in components or islands.
