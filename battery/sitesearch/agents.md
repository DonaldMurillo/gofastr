# battery/sitesearch

Full-text search over the app's rendered pages, for docs and content
sites. The battery renders every page itself (the static export's page
set), indexes each page's `<main>` with its headed sections as deep
links, and serves the index live or writes it into a static export,
where the browser searches it with no server.

**Use this when** the prompt mentions: site search, docs search,
search box over pages, ⌘K search, Pagefind, Algolia DocSearch, search
on a static site.

**Import:** `github.com/DonaldMurillo/gofastr/battery/sitesearch`

**Shape:**
```go
app.Batteries.Register(sitesearch.New(sitesearch.Config{
    Groups:  []sitesearch.Group{{Prefix: "/docs/", Label: "Docs"}},
    Exclude: []string{"/lab"},
}))
trigger, palette := ui.CommandPalette(ui.CommandPaletteConfig{
    Name:     "site-command-palette",
    Search:   true,
    Commands: starterCommands, // shown before the first keystroke
})
```

`gofastr export <dir>` writes the pages and the index together.

**AI-typical anti-pattern.** If you're about to write any of these,
stop and register this battery instead:
- A hand-kept list of pages and keywords for the palette to filter
- A palette RPC handler that `strings.Contains`-matches page titles
- A crawler that fetches your own pages over HTTP to index them
- Indexing records from the database here: that is `battery/search`

**Content that changes without a restart** (database-backed pages, a
`Source`): set `Config.MaxAge`, or call `Refresh` after the change.

**Keep out of the index:** mark live demo output or polled numbers
`data-search-ignore`; draw sidebars as `<nav>`/`<aside>`.

Guide: `gofastr docs site-search`.
