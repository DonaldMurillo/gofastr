# Site search

`battery/sitesearch` indexes the text of every page the app renders and
answers queries from the command palette. It renders the pages itself, the
same set a [static export](static-export.md) writes (dynamic routes expanded
through `StaticPaths`), so nothing has to be listed by hand. The same index
works two ways:

- **Live.** On a running server the battery builds the index in the
  background when the app starts and answers at `/__gofastr/search/query`;
  a query that arrives first waits for that build.
- **Static.** In an export the battery writes the index as JSON files under
  `/__gofastr/search/`, and the browser ranks results from them with no
  server. The ranker is a runtime module of its own, `sitesearch-static`,
  that only exported pages load. It ranks exactly as Go does (a chromium
  test runs both on one corpus).

For searching records in your database (posts, customers) use
[`battery/search`](search.md); site search covers what the pages say.

## Quickstart

Register the battery and turn on `Search` in the palette:

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
	app := framework.NewApp().Mount(uihost.New(site))
	if err := app.Batteries.Register(sitesearch.New(sitesearch.Config{
		Groups: []sitesearch.Group{{Prefix: "/docs/", Label: "Docs"}},
	})); err != nil {
		panic(err)
	}
	// trigger goes in the site header; the palette mounts as a widget.
	trigger, palette := ui.CommandPalette(ui.CommandPaletteConfig{
		Name:   "site-command-palette",
		Search: true,
		Commands: []ui.PaletteCommand{
			{Label: "Get started", Href: "/get-started"},
			{Label: "Docs", Href: "/docs/"},
		},
	})
	_ = trigger
	widget.MountBuilder(app.Router(), palette)
}
```

`Search: true` marks the palette's list with `data-search`, which loads the
`sitesearch` runtime module. The palette opens on its `Commands`, the short
list a reader sees before typing, so `Search` requires them. Typed queries
add result rows below the commands that match. A query still loading
after 150 ms shows a disabled "Loading…" row, and one that matches
nothing shows "No matches" (both localized: `ui.combobox.loading`,
`ui.combobox.noResults`).

## What gets indexed

Each page contributes its `<main>` (or `role="main"`), or `<body>` when it
has none. The `<h1>` is the title, falling back to `<title>`. Each `<h2>` and
`<h3>` starts a section; a heading with an `id` becomes a deep link, so a
result can land on `/docs/theming#dark-mode`.

Left out:

- `nav`, `aside`, `footer`, `dialog`, `script`, `style`, `template`, `svg`,
  form controls, and `<header>` when the root is `<body>`.
- Anything `hidden`, `inert`, `popover`, `aria-hidden="true"`, or with role
  `dialog`, `alertdialog` or `navigation`.
- Anything marked `data-search-ignore`, such as a live demo's output.
- Pages with `<meta name="robots" content="noindex">`.
- Pages whose text repeats an earlier page exactly (a twin route).

## Configuration

```go
sitesearch.Config{
	// Label results by URL prefix, on whole segments; the longest wins.
	Groups: []sitesearch.Group{{Prefix: "/docs/", Label: "Docs"}},
	// Keep paths out: "/lab" covers /lab and everything under it, never
	// /labs. Matches expanded paths, so one value of a dynamic route can go.
	Exclude: []string{"/examples/headless/dense"},
	// Add pages no route renders. Runs on every build, live and export.
	Sources: []sitesearch.Source{changelogPages},
}
```

A `Source` returns `[]sitesearch.Page{URL, Title, Group, Sections}`. The live
index is built once, at start; call `(*Battery).Refresh(ctx)` after content
changes that do not restart the server.

## Ranking

Queries match whole words and word prefixes (`pal` finds `palette`), and
every term must match. `CommandPalette` also indexes as `command` and
`palette`. A trailing plural `s` is dropped from terms of four or more
letters, so `forms` finds `form`. A hit in a title weighs more than one in a
heading, which weighs more than one in body text. Each result shows up to
three matching sections with an excerpt, and hits are wrapped in `<mark>`.

## Static export

`gofastr export` (or `App.ExportStatic`) writes the index next to the pages:

```bash
gofastr export --base /docs _site
```

```
_site/__gofastr/search/manifest.json       shard and page file names
_site/__gofastr/search/t-<hex>-<hash>.json shard: terms sharing a two-letter prefix
_site/__gofastr/search/p<n>-<hash>.json    one page's title and sections
```

A query fetches the manifest, the shards for its terms, and the page files of
the top results. Shard and page names carry a content hash, so they can be
cached forever (`Cache-Control: immutable`); the manifest is served
`no-cache`. Two exports of the same pages produce the same files.

Any battery can add files to an export the same way: implement
`static.Contributor` (`ExportPage(path, html)` per rendered page, then
`ExportFiles(ctx)` once) and `App.ExportStatic` calls it. `static.Walk`
renders the same page set at runtime.

## Limits

- The query endpoint refuses `q` over 256 bytes (414) and a `limit` below 1
  (400); `limit` defaults to 8 and caps at 20.
- Each section keeps its first 2000 characters.
- Words of one letter are not searchable.

## Common mistakes

- **A sidebar drawn with `<div>` inside `<main>`.** Its links get indexed on
  every page and crowd the results. Draw it as `<nav>` or `<aside>`, or mark
  it `data-search-ignore`.
- **Live numbers in the index.** A demo's output or a polled counter is
  indexed as whatever it said at build time. Mark the region
  `data-search-ignore`.
- **Headings without an `id`.** Their sections are searchable but get no
  result row of their own, since there is no anchor to land on.
- **Expecting new content to show up on a live server.** The index is built
  once; call `Refresh` after changing content without a restart.
- **`Search: true` with no `Commands`.** The palette panics at construction:
  an empty palette before the first keystroke reads as broken.
