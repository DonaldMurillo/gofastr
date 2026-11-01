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
	search := sitesearch.New(sitesearch.Config{
		Groups: []sitesearch.Group{{Prefix: "/docs/", Label: "Docs"}},
	})
	// The search page: every result, paged, with or without script.
	site.Register("/search", sitesearch.Screen(search), nil)
	app := framework.NewApp().Mount(uihost.New(site))
	if err := app.Batteries.Register(search); err != nil {
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
		// The results end with "See all results", which opens it.
		SearchPage: "/search",
		// Where the trigger goes without script.
		FallbackHref: "/search",
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

Each page contributes its first `<main>` (or `role="main"`) that is not
itself left out or inside something left out (the list below), or `<body>`
when it has none. The `<h1>` is the title, falling back to `<title>`. Each `<h2>` and
`<h3>` starts a section; a heading with an `id` becomes a deep link, so a
result can land on `/docs/theming#dark-mode`.

Left out:

- `nav`, `aside`, `footer`, `dialog`, `script`, `style`, `template`, `svg`,
  form controls, and `<header>` when the root is `<body>`.
- Anything `hidden`, `inert`, `popover`, `aria-hidden="true"`, or with role
  `dialog`, `alertdialog` or `navigation`.
- Anything marked `data-search-ignore`, such as a live demo's output.
- Pages that set their own robots `noindex` (or `none`, which means
  noindex plus nofollow): `ScreenRobots`, `SEO.Robots`, or
  a `<meta name="robots" content="noindex">` in the page body, outside any
  `data-search-ignore` element (sample markup in a docs page does not drop
  the page). A sitewide
  `uihost.WithRobotsMeta("noindex")` (a staging deploy) keeps crawlers off
  the deploy and leaves every page in the site's own search.
- Pages a screen policy refuses an anonymous visitor. Every reader searches
  the same index, so it is always built as no one in particular, whoever's
  query or `Refresh` call started the build.
- Pages whose text repeats an earlier page exactly (a twin route).

## Configuration

```go
sitesearch.Config{
	// Label results by URL prefix, on whole segments; the longest wins.
	Groups: []sitesearch.Group{{Prefix: "/docs/", Label: "Docs"}},
	// Keep paths out: "/lab" covers /lab and everything under it, never
	// /labs. Matches expanded paths, so one value of a dynamic route can go,
	// and Sources' pages too. An excluded page is never rendered.
	Exclude: []string{"/examples/headless/dense"},
	// Add pages no route renders. Runs on every build, live and export.
	Sources: []sitesearch.Source{changelogPages},
	// Rebuild the live index once it is older than this. Zero never does.
	MaxAge: 10 * time.Minute,
}
```

A `Source` returns `[]sitesearch.Page{URL, Title, Group, Sections}`. `URL`
is a root-relative path on the site (`/docs/forms`); a page with any
other URL, a scheme or another host, is dropped, since every result
links the base path plus it.

The live index is built at start. Content that changes without a restart
(pages that read the database, a `Source`) reaches it two ways:

- **`MaxAge`.** A query that finds the index older than `MaxAge` starts one
  rebuild in the background and is answered from the old index meanwhile,
  so no query waits for the site to render again. The next query after the
  rebuild sees the new content. A negative `MaxAge` panics in `New`.
- **`(*Battery).Refresh(ctx)`** rebuilds at once, for a change you know
  about (after saving a post). Queries keep the old index until the new one
  is built.

Builds run one at a time, and `OnStop` ends a background rebuild and waits
for it. A query or `Refresh` waiting behind a build (a slow `Source` during
the first one) returns its context's error when its request ends first; the
build it waited on carries on.

On a live server, a page that fails to render (a panic, or a fill that
failed) is logged and left out, and the rest of the site stays searchable.
An export refuses such a page instead. A build that fails outright, such as
a `Source` returning an error, is logged and no query starts another for 30
seconds. Meanwhile queries are answered from the last good index, or with a
503 when there is none, and the search page shows a "search index
unavailable" notice in place of results. `Refresh` retries at once and
returns the error.

## Ranking

Queries match whole words and, from three letters, the words they begin
(`pal` finds `palette`; `go` finds only `go`, never `gofastr`), and every
term must match. `CommandPalette` also indexes as `command` and
`palette`. A trailing plural `s` is dropped from terms of four or more
letters, so `forms` finds `form`, and such a term also matches its `-ing`
form as a whole word, so `theme` finds `theming`.

A page whose title holds every query term ranks above every page whose
title does not, so the page named for the query comes first however often
other pages repeat the words. Among those, the title the query covers most
wins. Each distinct title word counts once, a camelCase word once per part,
and a term that only begins a word covers half of it. So `table` finds
`DataTable` (half its title) before `TableOfContents` (a third), and
`datatable` finds `DataTable` (all of it) before `Datatables` (half).
Below that tier, a hit in a title weighs more than one in a heading, which
weighs more than one in body text.

Each result shows up to three matching sections. An excerpt opens on the
first whole-word hit, and hits are wrapped in `<mark>`. With `SearchPage`
set, the rows end with "See all results for “…”" (`ui.searchResults.all`),
which opens the search page for the query.

## Search page

`sitesearch.Screen(b)` is a page of every result, 20 at a time. Mount it
like any screen; `ScreenPath` names the path when it is not `/search`, so
the form and the pager links point at it:

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

The server renders it all from the query string: a GET form, a status line
("12 results for “form”"), the results as links with their sections and
excerpts, and pagination through `?page=`. A reader without script searches
and pages with ordinary links. The page tells crawlers `noindex`, which also
keeps it out of the index.

With script, the `sitesearch-page` runtime module (loaded by the
`data-search-page` marker [`ui.SearchResults`](ui-new-components.md)
renders) updates the page in place as the reader types:

- On a live server it fetches the page's own render for the new query, the
  way the client router fetches a partial, and swaps in the status line,
  the list and the pager. The input is never replaced, so focus and caret
  stay put. Pager links are ordinary links the router follows.
- In a static export there is no server to render the page, so the module
  ranks with `sitesearch-static` and draws the same markup itself. A query
  in the address (`/search?q=table`) runs on load. Instead of page links it
  shows a "More results" button (`ui.searchResults.more`).

Either way the address follows the query (`?q=`) with `replaceState`, so
typing adds no history entries and a reload or a shared link shows the same
results. Submitting the form runs the query at once instead of reloading.

## Static export

`gofastr export` (or `App.ExportStatic`) writes the index next to the pages:

```bash
gofastr export --base /docs _site
```

```
_site/__gofastr/search/manifest.json       the index hash and the shard keys
_site/__gofastr/search/t-<hex>-<hash>.json shard: the terms that begin with its key
_site/__gofastr/search/p<n>-<hash>.json    one page's title and sections
```

A query fetches the manifest, the shards for its terms, and the page files of
the top results. A shard's key starts as two letters, and a shard over 64 KB
splits on the next letter, so a site's common prefixes cost no more to fetch
than its rare ones. Shard and page names carry a content hash, so they can be
cached forever (`Cache-Control: immutable`); the manifest is served
`no-cache`. Two exports of the same pages produce the same files.

Any battery can add files to an export the same way: implement
`static.Contributor` (`ExportStart(ctx)` when a build begins, so a build
that failed partway leaves nothing behind; `ExportPage(path, html)` per
rendered page; then `ExportFiles(ctx)` once) and `App.ExportStatic` calls
it. `static.Walk` renders the same page set at runtime, skipping any path
its `skip` func names before rendering it. A page that fails to render
reaches `fn` with the error, as in `fs.WalkDir`: return nil to walk on
without it.

## Scale

Measured on 10,000 generated pages drawn from these docs' own vocabulary
(six sections of 130 words each), gzipped:

- The manifest is 6.5 KB and the largest shard 29 KB.
- A query downloads 7 KB (`pa`) to 148 KB (`con`, a prefix of hundreds
  of words), manifest and the top eight pages included; `form` takes
  73 KB and `session token` 95 KB.
- On a live server a query takes about 1 ms.

## Limits

- The query endpoint refuses `q` over 256 bytes (414) and a `limit` below 1
  (400); `limit` defaults to 8 and caps at 20. The search page does not
  search such a `q` either and shows a "query too long" notice.
- Each section keeps its first 2000 characters.
- Words of one letter are not searchable, and a term of two letters matches
  only the whole word.
- In a static export the search page needs JavaScript: without it the form
  reloads the page the export rendered with no query.

## Common mistakes

- **A sidebar drawn with `<div>` inside `<main>`.** Its links get indexed on
  every page and crowd the results. Draw it as `<nav>` or `<aside>`, or mark
  it `data-search-ignore`.
- **Live numbers in the index.** A demo's output or a polled counter is
  indexed as whatever it said at build time. Mark the region
  `data-search-ignore`.
- **Headings without an `id`.** Their sections are searchable but get no
  result row of their own, since there is no anchor to land on.
- **Expecting new content to show up on a live server.** With a zero
  `MaxAge` the index is built once. Set `MaxAge`, or call `Refresh` after
  changing content.
- **`Search: true` with no `Commands`.** The palette panics at construction:
  an empty palette before the first keystroke reads as broken.
