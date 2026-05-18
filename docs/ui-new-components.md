# New UI components

A reference for the 11 components added together — accessibility,
keyboard contract, and RPC shape per component.

All examples assume:

```go
import (
    "github.com/DonaldMurillo/gofastr/core-ui/html"
    "github.com/DonaldMurillo/gofastr/core-ui/patterns/combobox"
    "github.com/DonaldMurillo/gofastr/core-ui/patterns/infinitescroll"
    "github.com/DonaldMurillo/gofastr/core-ui/patterns/tree"
    "github.com/DonaldMurillo/gofastr/core-ui/widget"
    "github.com/DonaldMurillo/gofastr/framework/ui"
)
```

See [`docs/proposals/ui-components-in-flight-spec.md`](proposals/ui-components-in-flight-spec.md)
for the full implementation contract.

---

## 1. `<kbd>` primitive — `core-ui/html.Kbd`

Semantic markup for keyboard input. Use inside docs or paired with
`ui.ShortcutHint`.

```go
html.Paragraph(html.TextConfig{},
    render.Text("Press "),
    html.Kbd(html.TextConfig{}, render.Text("Esc")),
    render.Text(" to dismiss."),
)
```

## 2. AvatarGroup — `framework/ui.AvatarGroup`

Overlapping avatar stack with `+N` overflow.

```go
ui.AvatarGroup(ui.AvatarGroupConfig{
    Avatars: []ui.AvatarConfig{
        {Name: "Ada Lovelace"}, {Name: "Grace Hopper"},
        {Name: "Alan Turing"},  {Name: "Edsger Dijkstra"},
        {Name: "Margaret Hamilton"}, {Name: "Linus Torvalds"},
    },
    Max:   4,           // → renders 4 avatars + "+2"
    Label: "Project team",
})
```

ARIA: `role="group"`, `aria-label`, overflow chip has `aria-label="N more"`.

## 3. CopyButton — `framework/ui.CopyButton`

Clipboard button wired through the existing `data-fui-copy-text-from`
runtime hook + new SR announcement hook.

```go
ui.CopyButton(ui.CopyButtonConfig{
    Target:       "#api-token",
    Label:        "Copy token",
    CopiedLabel:  "Copied!",
    AnnounceText: "Token copied to clipboard",
})
```

Keyboard: Tab focus, Enter/Space to copy. Screen readers hear the
`role="status"` `aria-live="polite"` sibling announce the copied state.

Runtime attributes used: `data-fui-copy-text-from`, `data-fui-copy-status`,
`data-fui-copy-announce`.

## 4. ShortcutHint — `framework/ui.ShortcutHint`

Renders a chord as styled `<kbd>` chips. The Mod key auto-resolves
to ⌘ on Mac / Ctrl elsewhere (via `<html data-fui-os>`).

```go
ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Mod+K"})
ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "/"})
ui.ShortcutHint(ui.ShortcutHintConfig{Chord: "Shift+Tab"})
```

Hidden on touch-only devices via `@media (pointer: coarse)`.

## 5. SegmentedControl — `framework/ui.SegmentedControl`

Native radiogroup styled as a pill toggle bar with a sliding
indicator. Browser provides Arrow-key + Space/Enter nav for free.

```go
ui.SegmentedControl(ui.SegmentedControlConfig{
    Name:     "view-mode",
    Label:    "View mode",
    Selected: "week",
    Options: []ui.SegmentedOption{
        {Label: "Day",   Value: "day"},
        {Label: "Week",  Value: "week"},
        {Label: "Month", Value: "month"},
    },
    // Optional RPC: fire on every change.
    RPCPath:   "/views/set",
    RPCSignal: "current-view",
})
```

## 6. ConfirmAction — `framework/ui.ConfirmAction`

Trigger button + alertdialog Modal preset for destructive
confirmations. Cancel autofocus by default; opt in to `AutofocusConfirm: true`
for non-destructive prompts.

```go
trigger, modalBuilder := ui.ConfirmAction(ui.ConfirmActionConfig{
    Name:         "delete-user-42",
    TriggerLabel: "Delete",
    Title:        "Delete user?",
    Body:         "This permanently removes the user and their data.",
    ConfirmLabel: "Yes, delete",
    CancelLabel:  "Cancel",
    RPCPath:      "/users/42/delete",
})
def := modalBuilder.Build()
widget.Mount(r, &def)
// render `trigger` inline wherever the destructive button belongs
```

ARIA: `role="alertdialog"`, `aria-modal="true"`, `aria-labelledby`,
`aria-describedby`. Escape closes; backdrop click closes; focus returns
to trigger.

## 7. FilterChipBar — `framework/ui.FilterChipBar`

Toolbar of dismissible filter chips above a table/search result.

```go
ui.FilterChipBar(ui.FilterChipBarConfig{
    Filters: []ui.FilterChip{
        {Label: "Status: Active",  DismissPath: "/filters/status/clear"},
        {Label: "Tag: urgent",     DismissPath: "/filters/tag/urgent/clear", Variant: ui.StatusWarning},
    },
    ClearAllPath: "/filters/clear-all",
    Label:        "Active filters",
    RPCSignal:    "filter-bar",
    SignalName:   "filter-bar", // pair with server re-render
})
```

ARIA: `role="toolbar"`. Each chip's × button has `aria-label="Remove <label>"`.

## 8. InfiniteScroll — `core-ui/patterns/infinitescroll.Render`

Sentinel-based infinite scroll with `<noscript>` fallback "Load more".

```go
infinitescroll.Render(infinitescroll.Config{
    ID:        "feed",
    RPCPath:   "/feed/page",
    AriaLabel: "Activity feed",
    Items:     firstPageHTML, // SSR-rendered first page
    Cursor:    "page-1-end",
})
```

Server contract for the RPC:

```go
// Request: form-encoded body `cursor=<token>` (POST).
// Response: HTML fragment (appended into the items container).
//           Header X-Gofastr-Infinite-Cursor: <next> — empty/missing = end of feed.
http.HandleFunc("/feed/page", func(w http.ResponseWriter, r *http.Request) {
    cursor := r.FormValue("cursor")
    // … fetch next page …
    w.Header().Set("X-Gofastr-Infinite-Cursor", nextCursor)
    w.Write(htmlFragment)
})
```

`aria-busy` flips true → false across each fetch. End-of-feed removes the sentinel.

New runtime attributes: `data-fui-infinite-scroll`, `data-fui-infinite-sentinel`,
`data-fui-infinite-cursor`, `data-fui-infinite-items`, `data-fui-infinite-root-margin`.

## 9. Combobox — `core-ui/patterns/combobox.Render`

WAI-ARIA 1.2 combobox: debounced input + RPC dropdown listbox.

```go
combobox.Render(combobox.Config{
    ID:          "city",
    Name:        "q",
    Label:       "Pick a city",
    RPCPath:     "/cities/search",
    SignalName:  "city-results",
    DebounceMs:  250,
    Placeholder: "Type to search…",
})
```

Server returns `<li role="option" id="...">…</li>` fragments. Options
SHOULD carry `data-value` (used as the input's selected value); without
it the option's textContent is used.

Keyboard (handled at runtime): ArrowUp/Down/Home/End move the highlight,
Enter selects, Esc closes/clears, Tab closes naturally. The runtime
maintains `aria-expanded` and `aria-activedescendant`.

## 10. TreeView — `core-ui/patterns/tree.Render`

WAI-ARIA tree with optional RPC lazy-load on expand.

```go
tree.Render(tree.Config{
    ID:           "files",
    Label:        "Project files",
    SignalPrefix: "files-tree",
    Nodes: []tree.Node{
        {ID: "src", Label: "src", Expanded: true, Children: []tree.Node{
            {ID: "src-main", Label: "main.go", Href: "/files/src/main.go"},
        }},
        {ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
    },
})
```

Server lazy-load handler returns `<li role="treeitem" …>…</li>`
fragments that get swapped into the child `<ul role="group">` via the
signal binding.

Keyboard: Up/Down (next/prev visible), Right (expand or focus first child),
Left (collapse or focus parent), Home/End, Enter/Space (toggle), type-ahead.

New runtime attribute: `data-fui-tree-toggle`.

## 11. CommandPalette — `framework/ui.CommandPalette`

⌘K-triggered modal-backed combobox for fuzzy command/search.

```go
trigger, paletteBuilder := ui.CommandPalette(ui.CommandPaletteConfig{
    Name:        "command-palette", // default value, can be omitted
    RPCPath:     "/commands/search",
    Placeholder: "Type a command…",
    Shortcut:    "Meta+K",       // default; the runtime accepts either Cmd or Ctrl
})
def := paletteBuilder.Build()
widget.Mount(r, &def)
// render `trigger` somewhere in your global chrome (Sidebar, top nav)
```

The trigger is screen-reader-only — keyboard shortcut + AT users reach
it; sighted mouse users discover it via the visible chord hint shipped
next to the search input.

Modal: `role="dialog"`, `aria-modal="true"`, focus trap, Esc closes,
backdrop click closes, focus returns to trigger.

Internal combobox: `role="combobox"` + listbox; same RPC-and-pick
contract as the standalone Combobox.

---

## Done means

For each component:

- [x] Unit tests green
- [x] chromedp e2e tests green for `/components/new`
- [x] Renders the right ARIA roles + labels
- [x] Keyboard nav handled by runtime (where applicable)
- [x] Mobile / touch: ≥ 44×44 px targets via `--spacing-touch-target`
- [x] Docs (this file)
- [x] Example wiring in `examples/website/screen_new_components.go`
- [x] New runtime attrs documented in `core-ui/ARCHITECTURE.md`
- [x] Runtime drift test green
