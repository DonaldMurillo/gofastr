# Widget overlay overhaul

Working doc for the `feat/widget-overlay-overhaul` branch. Captures the
plan agreed on 2026-05-16. Delete when the work is merged.

## Goal

Make modals, drawers, dropdowns, toasts, and sidebars first-class
primitives with a single overlay model, animation tokens, a11y
contracts, and (for modals + drawers) URL deeplinking.

## Layer split

| Primitive | `core-ui` (base, generic chrome)                                                       | `framework/ui` (opinionated facility)                                                |
| --------- | -------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| Modal     | `widget/preset.Modal` — center surface, backdrop, focus trap, ESC, ARIA, animation     | `ui.ConfirmModal`, `ui.FormModal` — wired confirm/cancel + validation                |
| Dropdown  | `widget/preset.Dropdown` — anchored, `role=menu`, keyboard nav (arrows, type-ahead)    | `ui.Menu(items)` — typed `MenuItem` with icon/label/href/danger/separator            |
| Drawer    | `widget/preset.Drawer` — edge surface, backdrop, focus trap, animation                 | `ui.DetailDrawer`, `ui.FilterDrawer` — entity-detail / filter-form patterns          |
| Toast     | `widget/preset.ToastStack` — stack + slide animation. **No deeplinking**                | `app.Toast(ctx, ...)` — server API pushing into the stack via SSE                    |
| Sidebar   | *(none — not an overlay)*                                                              | `ui.Sidebar(SidebarConfig)` — responsive; uses `preset.Drawer` under the hood < md   |

Rule: surface + chrome + focus + animation + a11y → `core-ui/widget/preset`. Pattern that
composes a surface with content/handlers → `framework/ui`.

## Deeplinking (Modal + Drawer)

New on `widget.Definition`:

```go
DeepLink     string            // e.g. "modal" — query key
DeepLinkValue string           // e.g. "user-edit" — value that opens THIS widget
DeepLinkData []string          // additional query keys → mirrored into signals
```

Builder methods:

```go
preset.Modal("user-edit").
    Hidden().
    DeepLink("modal", "user-edit").
    DeepLinkParam("user_id").             // ?user_id=42 → signal "user_id"="42"
    Slot("body", &UserEditForm{}).
    Build()
```

Flow:
1. **Initial SSR** — `uihost` scans `r.URL.Query()` for any registered DeepLink. Match → render the widget surface **open** with signals seeded from `DeepLinkData` keys.
2. **Click `data-fui-open`** — runtime opens widget AND `pushState`s the deep-link params onto the URL.
3. **Close** — runtime strips the deep-link params, `pushState`. No fetch.
4. **popstate** — existing screen-partial fetch path; SSR returns same or different open-state.
5. **Refresh / share / bookmark** — works because step 1.

## Toast — stacking + animation only

- `preset.ToastStack(name)` hidden widget, top-right by default, slot bound to signal `__toasts`.
- `app.Toast(ctx, ui.NotificationConfig{...})` appends to per-session list, pushes via SSE.
- Max 3 visible, rest collapse to "+N more".
- Per-toast `data-fui-toast-ttl-ms`. Hover/focus pauses.
- No URL state.

## Animation contract

- New `theme.Durations.{OverlayEnter, OverlayExit, ToastEnter, ToastExit, DropdownEnter}`.
- New `theme.Easings.{EaseOut, EaseIn, EaseInOut, Spring}`.
- Widget chrome CSS reads `var(--duration-…)` / `var(--easing-…)`.
- One global `@media (prefers-reduced-motion: reduce){[data-fui-widget]{transition:none}}`.
- **Kill** `_injectOverlayCSS` + `openOverlay` in `runtime.js` — drawer/dialog/sheet become registered hidden widgets opened via `data-fui-open`.

## Execution order

1. Branch + plan doc *(this)*
2. Theme: add `Durations.OverlayEnter/Exit/Toast*/DropdownEnter` + new `Easings` struct
3. Widget definition: add `DeepLink`, `DeepLinkValue`, `DeepLinkData` + builder methods
4. SSR: `uihost` honours deep links (open at first paint, seed signals)
5. Runtime: pushState/popstate handling for deep-linked widgets
6. Animation tokens applied to default modal/drawer chrome CSS
7. Retire `openOverlay` / `_injectOverlayCSS` block; migrate any callers
   — done. `examples/core-ui-demo` now registers `confirm-dialog`,
   `demo-drawer`, and `demo-sheet` as hidden widgets with custom
   skeletons that emit the legacy `dialog` / `drawer` / `sheet` /
   `data-overlay` classes for chromedp selector compatibility. The
   `openOverlay`/`closeOverlay`/`closeAllOverlays`/`_injectOverlayCSS`/
   `overlayCache`/`overlayStack` block in `runtime.js` is deleted; in
   its place the widget runtime gained body-scroll lock, modal stack,
   Tab focus trap, return-focus-on-close, and centralized LIFO
   Escape closing. `framework/uihost` now delegates the
   `/__gofastr/widgets` stub to `widget.ServeWidgetList`. Net runtime
   size after the migration: 82.5 KB (cap dropped to 85 KB).
8. ToastStack preset + `app.Toast` server API + SSE wiring
9. Dropdown preset (anchored positioning + keyboard nav + ARIA)
10. `framework/ui.Sidebar` responsive lifecycle (uses `preset.Drawer` < md)
11. Tests: widget_test, runtime_test, chromedp for each preset
12. Docs: update `core-ui/ARCHITECTURE.md` (data-fui-* table, hard rules), add brief recipe section in widget package docs
13. Dogfood: kiln modals adopt deeplinks where useful

## Done when

- All `go test ./...` green
- `./scripts/test-all.sh` green
- A new screen in `examples/website` exercises every primitive
- `_injectOverlayCSS` and `openOverlay` are gone from `runtime.js`
- `core-ui/ARCHITECTURE.md` reflects the new attributes and the unified overlay model
