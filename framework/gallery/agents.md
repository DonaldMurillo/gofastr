# framework/gallery — importable component catalog

A library that ships every framework/ui + core-ui/patterns component as a
pre-canned, self-contained demo. Originally `examples/site/components.go`,
it was lifted so any tool can render the whole design system against an
arbitrary theme — most importantly the theme-configuration tool that will
live inside the `gofastr` CLI binary, which cannot import `examples/`.

**Use this when** the prompt mentions: theme previewer, theme
configuration tool, render every component, design-system gallery,
component catalog, "what does the system look like with this theme".

**Import:** `github.com/DonaldMurillo/gofastr/framework/gallery`

## Shape

```go
import "github.com/DonaldMurillo/gofastr/framework/gallery"

// Iterate the catalog (139 entries, 16 categories, display order).
for _, e := range gallery.Catalog {
    fmt.Println(e.Slug, e.Name, e.Category)
    html := e.Demo() // render.HTML — self-contained, no host wiring
}

// Or look up one entry.
entry, ok := gallery.Lookup("button")
notes := gallery.ByCategory("Forms")
groups := gallery.Grouped() // []Group, category-grouped, display order
categories := gallery.Categories()

// Helpers.
gallery.PkgForSlug("button")          // "framework/ui" — for pkg.go.dev links
gallery.IsNoteOnly("datatable")       // true — needs backend wiring
gallery.CodeSnippets["button"]        // example Go source
gallery.CategorySlug("Buttons & links") // "buttons-links" — fragment-safe
```

## What's where

- **`Catalog`** — the source-of-truth `[]Entry`. Every host iterates this.
- **`CodeSnippets`** — per-slug example Go source for showcase pages.
- **`NoteOnlySlugs`** — components whose showcase shows an explanatory note
  instead of a live demo (need per-page backend wiring: DataTable,
  ConfirmAction, Gallery, Lightbox, etc.).
- **`PkgForSlug`** — maps a slug to its Go source package for deep-linking
  the showcase header to pkg.go.dev.
- **Demo support**: `InitialKanbanColumns`, `RenderKanbanBoard`,
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

`gallery.RegisterDemo(slug, fn)` is a seam for the rarer case where a host
wants to override the closure itself (e.g. static export with different
default data). It is consulted by `gallery.DemoFor(slug)`; the catalog
slice is unchanged.

## Don't reinvent

The gallery is a catalog of *existing* components — adding a new component
belongs in `framework/ui` (or `core-ui/patterns` for composed patterns,
`core-ui/html` for 1:1 HTML tags). When a new component lands there, add a
matching `Catalog` entry so the showcase and `TestComponentGalleryCoversUI`
in `examples/site` stay complete. Ship zero bespoke CSS — every demo
composes the design system; a surface that appears to need styling is a
missing component upstream.
