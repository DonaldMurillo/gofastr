# framework/gallery — importable component catalog

A catalog of framework/ui components with previews, code examples, and
wiring notes. The docs site and the `gofastr` theme editor use it to
show components against their own themes.

**Use this when** the prompt mentions: theme previewer, theme
configuration tool, render every component, design-system gallery,
component catalog, "what does the system look like with this theme".

**Import:** `github.com/DonaldMurillo/gofastr/framework/gallery`

## Shape

```go
import "github.com/DonaldMurillo/gofastr/framework/gallery"

// Iterate the catalog in display order.
for _, e := range gallery.Catalog {
    fmt.Println(e.Slug, e.Name, e.Category)
    html := e.Demo() // render.HTML — a preview or a wiring note
}

// Or look up one entry.
entry, ok := gallery.Lookup("button")
notes := gallery.ByCategory("Forms")
groups := gallery.Grouped() // []Group, category-grouped, display order
categories := gallery.Categories()

// Helpers.
gallery.PkgForSlug("button")          // "framework/ui" — for pkg.go.dev links
gallery.IsNoteOnly("datatable")       // true — needs backend wiring
gallery.CodeSnippet("modal")          // example Go source for that slug
gallery.CategorySlug("Buttons & links") // "buttons-links" — fragment-safe
```

## What's where

- **`Catalog`** — the source-of-truth `[]Entry`. Every host iterates this.
- **`CodeSnippet`** — `func CodeSnippet(slug string) string` — a slug's example Go source. The map it reads is private.
- **`IsNoteOnly`** — `func IsNoteOnly(slug string) bool` — true when a
  component's showcase renders an explanatory note instead of a live demo
  (it needs per-page backend wiring: DataTable, ConfirmAction, Gallery,
  Lightbox, etc.). The set itself is private; this is the only accessor.
- **`PkgForSlug`** — maps a slug to its Go source package for deep-linking
  the showcase header to pkg.go.dev.
- **Demo support**: `InitialKanbanColumns`, `RenderKanbanBoard`, `KanbanColumn.SortableConfig`,
  `RenderOptimisticCreateList`/`RenderOptimisticDeleteList`,
  `OptimisticDeleteModals`, `SidebarShowcaseConfig`,
  `DemoSectionMenuConfig`, `DemoCompany` — the seed data + render helpers
  behind the three stateful demos and the sidebar/section-menu showcases.
  The catalog's closures call these directly so a theme previewer with no
  host state still gets a realistic render.

## Layering

Sibling of `framework/sdkdocs`: both compose `framework/ui` + `core-ui/*`
into an importable surface. Like `sdkdocs`, it is **NOT** part of
`framework/uihost` — `uihost` must never import `framework/ui` (always-on
styles would leak into every host's CSS bundle). `framework/gallery`
imports only `framework/ui`, `core-ui/*`, and `core/render`; it does not
import the framework root facade, `framework/crud`, `framework/entity`, or
anything higher up the L1–L5 diagram in `framework/ARCHITECTURE.md`, so it
introduces no cycle and is importable from `cmd/gofastr`, `examples/site`,
and host apps alike.

## Hosts with live stateful demos

Three catalog entries (`sortablelist`, `optimisticcreate`,
`optimisticdelete`) have a live demo normally backed by per-visitor
session state. The gallery closures render a realistic seed view (using
`InitialKanbanColumns()` / `InitialOptimisticNotes`); hosts that wire
live session state — like `examples/site`, which keeps a per-visitor
`demoState` keyed by a site-demo cookie — call the render helpers directly
from their SSR path with the visitor's current data, bypassing the
catalog's `Demo()` closure. See `examples/site/components.go` for the
pattern.

`gallery.DemoFor(slug)` returns the catalog's own closure for a slug, or
nil for an unknown one. A host that needs different data for a demo (a
static export with other defaults, say) calls the render helpers itself,
as above; the catalog has no override hook.

Use `KanbanColumn.SortableConfig(version)` for both the board's lists and
the conflict response. Render the response with `ui.SortableListItems`
and pass the config's `Version` to `ui.RenewSortableVersion`.

## Host routes

Call `MountDemoWidgets(router, DemoWidgetPages{…})` before
mounting the UIHost to scope the modal, drawer, bottom sheet, and sidebar
drawer individually. An empty page list leaves that overlay global.

`MountDemoFormRoute(router, demoPage)` handles stateless demo POSTs at
`DemoFormPath`. The docs site's live StepWizard supplies its own
`POST /forms/wizard` to handle step transitions. The theme editor shows
an inert StepWizard preview.

## Don't reinvent

The gallery is a catalog of *existing* components — adding a new component
belongs in `framework/ui` (composed patterns included,
`core-ui/html` for 1:1 HTML tags). When a new component lands there, add a
matching `Catalog` entry so the showcase and `TestComponentGalleryCoversUI`
in `examples/site` stay complete. Ship zero bespoke CSS — every demo
composes the design system; a surface that appears to need styling is a
missing component upstream.
