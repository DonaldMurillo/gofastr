# Site search (`battery/sitesearch`)

`battery/sitesearch` indexes the rendered text of every public screen in your app (expanding dynamic routes via `StaticPaths`) and serves instant ranked search in both `ui.CommandPalette` and a dedicated `/search` page — working identically on live servers and static exports (`gofastr export`).

> Use `battery/sitesearch` to search your site's pages and docs. For searching database records, use [`battery/search`](search.md).

## Quickstart

<!-- gofastr:compile
import (
	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)
var site = coreapp.NewApp("docs")
-->
```go
import (
	"github.com/DonaldMurillo/gofastr/battery/sitesearch"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func main() {
	search := sitesearch.New(sitesearch.Config{
		Groups: []sitesearch.Group{{Prefix: "/docs/", Label: "Docs"}},
	})
	site.Register("/search", sitesearch.Screen(search), nil)
	app := framework.NewApp().Mount(uihost.New(site))
	if err := app.Batteries.Register(search); err != nil {
		panic(err)
	}

	paletteConfig := ui.CommandPaletteConfig{
		Name:         "site-command-palette",
		Search:       true,
		Commands:     []ui.PaletteCommand{{Label: "Docs", Href: "/docs/"}},
		SearchPage:   "/search",
		FallbackHref: "/search",
	}
	_, palette := ui.CommandPalette(paletteConfig)
	widget.MountBuilder(app.Router(), palette)
}
```

Render the trigger per request with `ui.CommandPaletteTrigger(ctx, paletteConfig)`. To mount the full search page at a custom path, pass `sitesearch.ScreenPath`:

<!-- gofastr:compile
import (
	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/battery/sitesearch"
)
var site = coreapp.NewApp("docs")
var search = sitesearch.New(sitesearch.Config{})
-->
```go
site.Register("/find", sitesearch.Screen(search, sitesearch.ScreenPath("/find")), nil)
```

## What gets indexed and how it updates

- **Extracted content:** indexes each page's `<main>` (title from `<h1>`/`<title>`, deep-linkable sections from `<h2 id>`/`<h3 id>`), skipping `<nav>`, `<aside>`, `<footer>`, `<dialog>`, `<script>`, `<style>`, form controls, hidden/inert elements, `data-search-ignore` regions, `noindex` pages, and auth-gated pages.
- **Configuration:** group results by URL prefix (`Groups`), skip routes (`Exclude`), inject extra non-route pages (`Sources`), and refresh live content with `MaxAge` (background rebuild on stale query) or `(*Battery).Refresh(ctx)`.
- **Live vs. static:** live servers answer at `/__gofastr/search/query`; `gofastr export` emits content-hashed shards under `/__gofastr/search/` ranked client-side by the `sitesearch-static` module with identical scoring.
- **Search page (`sitesearch.Screen`):** works without JS via GET forms and `?page=` links, and enhances progressively with live in-place filtering (`ui.SearchResults`, `data-search-page`).

## Common mistakes

- **Building a sidebar out of `<div>` inside `<main>`:** its links get indexed on every page; use `<nav>`/`<aside>` or mark it `data-search-ignore`.
- **Indexing live demo counters:** mark dynamic sample regions `data-search-ignore`.
- **Omitting `id` attributes on `<h2>`/`<h3>` headings:** prevents deep-linked section hits.
- **Expecting DB-backed pages to refresh automatically with `MaxAge: 0`:** set `MaxAge` or call `Refresh(ctx)` after edits.
- **Setting `Search: true` on `CommandPaletteConfig` with empty `Commands`:** panics at construction.
