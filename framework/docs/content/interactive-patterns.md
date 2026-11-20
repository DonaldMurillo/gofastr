# Interactive patterns

GoFastr's runtime drives in-page interactivity through `data-cui-*` and `data-hui-*` attributes on regular server-rendered HTML—no hand-written JS required. Use the Go helpers in `core-ui/interactive` and `framework/ui`, or write attributes directly for custom islands.

For the wider reactivity ladder (signals → RPC → polling → SSE), see [Reactivity model](reactivity.md) and [Runtime contract](runtime-contract.md).

## Client-only patterns (no server round-trip)

- **Signals:** Named DOM strings mutated via `interactive.SetLocal`, `IncLocal`, `ToggleLocal` (`data-cui-signal-set="name:val"`, `data-cui-signal-inc="name[:delta]"`, `data-cui-signal-toggle="name"`). Elements with `data-cui-signal="name"` update automatically (add `data-cui-flash-on-update` for a reduced-motion-safe highlight).
- **Controls:**
  - `ui.Counter` (`Slice` or `SignalName`, configurable `Step`).
  - `ui.Tabs` (signal-driven tabs; opt-in `StateAttrs`, `ID`, and `VacateHidden` to keep hidden panels out of DOM—leave `VacateHidden` off if server pushes move tabs before user interaction).
  - `ui.SignalToggle` (`role="switch"` bound to `"true"`/`"false"`).
  - `ui.Collapsible` (native `<details>`, optional `Name` for native exclusive accordions).
  - `ui.CopyButton` (`Target` element ID, clipboard copy + screen-reader status).
  - `ui.PasswordInput` (show/hide toggle, `spellcheck="false"`, `autocapitalize="off"`), `ui.OneTimeCode` (6-digit numeric text input, `autocomplete="one-time-code"`), `ui.TextArea` (`Autogrow: true`).
  - `preset.ToastStack` (bottom-right auto-dismissing notifications), `ui.ThemeToggle` / `ui.ThemePicker`, `ui.AnchoredRail` / `ui.TableOfContents` (scroll spy + mobile `PhoneSheet`).
- **Dropdowns & panes:** `interactive.Dropdown(trigger, panel)` for click-toggle panels; `interactive.OpenPaneOnClick` / `ClosePaneOnClick` / `SwapPaneOnClick` / `PaneKey` for `ui.PaneHost` (see [pane-host.md](pane-host.md)); `interactive.OpenOnClick` / `ToastOnClick` for widgets and toasts:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core-ui/interactive"
import "github.com/DonaldMurillo/gofastr/framework/ui"
-->
```go
interactive.OpenOnClick(ui.Button(ui.ButtonConfig{Label: "Edit"}), "edit-drawer")
interactive.ToastOnClick(ui.Button(ui.ButtonConfig{Label: "Saved!"}),
    interactive.Toast{Variant: "success", Title: "Saved", TTLMs: 5000})
```

## RPC-backed patterns

Build an `interactive.Action` with `Get`, `Post`, `Put`, `Patch`, or `Delete`, then chain modifiers and attach via `OnClick`, `OnSubmit`, `LiveSearch`, or `.Attrs()` (preferred when passing `ExtraAttrs` to `ui.Button`):

<!-- gofastr:compile
stmt: _ = del
import "github.com/DonaldMurillo/gofastr/framework/ui"
import "github.com/DonaldMurillo/gofastr/core-ui/interactive"
-->
```go
del := ui.Button(ui.ButtonConfig{Label: "Delete", Variant: ui.ButtonDanger,
    ExtraAttrs: interactive.Delete("/api/items/42").
        WithConfirm("Delete this item?").
        OnSuccess(interactive.SetSignal("items")).Attrs()})
```

| Helper / modifier | What it does |
|---|---|
| `OnClick(el, action)` | Button click → `fetch()` with CSRF token forwarding |
| `OnSubmit(form, action)` | Intercepts `<form>` submit → JSON (or multipart) `fetch()` |
| `LiveSearch(form, action, debounceMs)` | Debounced `input` trigger (default 300ms) for search & filter forms |
| `.WithBody(json)` | Attaches a validated static JSON body to a non-form RPC |
| `.WithConfirm(msg)` / `.WithConfirmDialog(Confirm{Title, Message, Accept, Danger})` | Pre-flight themed confirmation dialog (Cancel focused by default); also `ui.MenuItem.Confirm` |
| `.OnSuccess(interactive.SetSignal(name))` | Writes response into `interactive.BindHTML` / `BindText` / `BindAttr` regions |
| `.OnSuccess(interactive.Navigate(path))` | Cache-bypassing SPA navigation; pair with server-side `ui.InvalidateScreens(w, "/orders")` to evict other cached routes |
| `.OnSuccess(interactive.AfterText(s), interactive.AfterDisable())` | One-shot button text swap + disable after 2xx |
| `.OnSuccess(interactive.ScrollTo(sel))` / `PushState(path)` | Smooth-scroll or `pushState` URL update after 2xx (server `X-Gofastr-Push-State` wins) |
| `.OnSuccessToast(title)` / `.OnSuccessToastAction(label, next)` | Success toast (survives navigation) with optional Undo action (server equivalent: `ui.AddToast` + `interactive.NewToastAction`) |
| `.OnErrorToast(title)` | Shows an error toast with the server's JSON `error` message on non-2xx |
| `interactive.EditToggle` / `CancelEdit` | Signal helpers for click-to-edit forms |
| `ui.OptimisticAction` / `ui.ToggleAction` | Immediate visual commit + background RPC + rollback on failure (see [optimistic-ui.md](optimistic-ui.md)) |

### Themed confirmation modal (`ui.ConfirmAction`)

When a destructive action needs a richer body than `WithConfirmDialog`, `ui.ConfirmAction` returns a `(trigger, modal)` pair—mount `modal` once at startup via `widget.Mount` and register your RPC handler:

```go
trigger, modal := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name: "delete-user-42", TriggerLabel: "Delete",
    Title: "Delete user?", Body: "This permanently removes the user.",
    RPCPath: "/users/42/delete", SuccessSignal: "users-list",
})
```

## Polling (passive freshness)

For counters, statuses, and dashboards that refresh on a cadence without holding an SSE connection:

```html
<div data-cui-poll="30s" data-cui-poll-src="/islands/orders/today">
  …initial SSR content…
</div>
```

- `data-cui-poll` accepts a Go duration (`5s` minimum), jitters automatically, pauses on hidden tabs, and backs off up to 5× on errors. Use read-only endpoints only.
- Widgets use `Builder.Poll(interval)` to refresh `/state` on a cadence (see [widgets.md](widgets.md)).

## Sortable list (single + kanban)

`ui.SortableList` renders an HTML5 drag-and-drop + keyboard-accessible (`Space` to grab/drop, Arrow keys to move, `Esc` to cancel) `<ol>` that POSTs `order=<keys>` (plus `container=<id>` and `moved=<key>` for Kanban columns sharing a `Group`):

```go
ui.SortableList(ui.SortableListConfig{
    Label:       col.Title,
    Group:       "board-1",        // omit for standalone single-list reorder
    Container:   col.ID,           // unique per column; empty columns still accept drops
    RPCPath:     "/api/board/move",
    Version:     boardVersion,     // optional: enables 409 conflict reconciliation
    ConflictRPC: "/api/board/conflict?container=" + col.ID,
    Items:       col.Items,
})
```

When `Version` is set:
- Call `ui.RenewSortableVersion(w, newVersion)` on 2xx moves and on `ConflictRPC` responses so subsequent moves carry the updated token.
- On `409 Conflict` with the standard JSON error envelope (`{"error":"..."}`), the runtime announces the message, fetches `ConflictRPC` (`ui.SortableListItems`), and reconciles the column instead of a blind rollback. See [optimistic-ui.md](optimistic-ui.md).

## Writing a hand-written island, end to end

Four gotchas when writing raw `data-cui-*` attributes yourself:

1. **Register the RPC route yourself.** Only `widget.Mount` auto-registers routes. A hand-written `data-cui-rpc="/islands/products/filter"` needs `app.Router().Post("/islands/products/filter", handler)` or clicks silently 404.
2. **JSON keys come from `name`, not `id`.** Non-multipart RPC forms serialize to JSON using each control's `name` attribute.
3. **`data-cui-rpc-trigger="input"` already covers `<select>`, checkboxes, and radios.** There is no `change` trigger—set `data-cui-rpc-debounce-ms="1"` on a `<select>` for instant firing.
4. **Route params:** `app.Router()` (Go `ServeMux`) requires `{id}`; screen routes (`app.Register` / `ScreenGroup`) accept either `:id` or `{id}`.

## Complex interactive components & presence dots

- **Built-in interactive components:** `ui.Carousel`, `ui.Combobox`, `ui.CommandPalette` (supports server `RPCPath` returning `ui.PaletteResults` or `Search: true` site search; render per-request trigger via `ui.CommandPaletteTrigger`), `ui.DatePicker`, `ui.FileDropzone`, `ui.Gallery` + `ui.Lightbox`, `ui.Menu`, `ui.MultiSelect`, `ui.NotificationBell`, `ui.Popover`, `ui.RangeSlider`, `ui.Slider`, `ui.SortableList`, `ui.TagInput`, `ui.Tree`, `ui.NetworkRetryBanner`.
- **Presence dots:** `ui.Avatar` / `ui.AvatarGroup` render semantic status dots (`AvatarOnline`, `AvatarAway`, `AvatarBusy`, `AvatarOffline`); pair with `core-ui/island` presence topics for live rosters (see [presence.md](presence.md)):

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/ui"
-->
```go
ui.AvatarGroup(ui.AvatarGroupConfig{
    Avatars: []ui.AvatarConfig{
        {Name: "Ada Lovelace", Status: ui.AvatarOnline},
        {Name: "Grace Hopper", Status: ui.AvatarAway},
        {Name: "Alan Turing",  Status: ui.AvatarBusy, StatusLabel: "In a review"},
    },
})
```

## Common mistakes

- **Forgetting to mount a hand-written `data-cui-rpc` endpoint.** Raw RPC URLs aren't auto-registered—check server logs for 404s.
- **Decoding RPC JSON by input `id` instead of `name`.** Browser form serialization uses `name`.
- **Modeling in-page state as routes or `location.href`.** Keep tabs, expands, and embedded table state on the current route (islands or query params).
- **Duplicating sort/filter/pagination math in JS.** Let the server compute and return HTML fragments.
- **Treating signals as typed booleans/numbers.** DOM signals are strings (`"true"`/`"false"`).
- **Delivering user-action responses over SSE.** Return the result directly in the HTTP RPC response; SSE is push-only for background updates.
