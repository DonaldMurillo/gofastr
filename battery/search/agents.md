# battery/search

Pluggable text search behind a `Backend` interface
(`Index` / `Delete` / `Search`). Ships two backends: an in-process `Memory`
backend (dev/tests) and a Postgres FTS backend (`PostgresSearch`) for
production. Both share AND-of-terms semantics, weighted fields, and
`Query.FieldEquals` scoping; external engines (Bleve / Elastic) implement
the same interface.

**Use this when** the prompt mentions: search, full-text search, "find
records containing X", autocomplete, query, indexing.

**Import:** `github.com/DonaldMurillo/gofastr/battery/search`

**Shape:**
```go
idx := search.NewMemory()
_ = idx.Index(ctx, search.Document{
    ID:     "post-42",
    Type:   "posts",
    Text:   "Hello world",
    Fields: map[string]any{"status": "published"},
})
results, _ := idx.Search(ctx, search.Query{
    Text:  "hello world",
    Type:  "posts",
    Limit: 20,
})
```

**AI-typical anti-pattern** — if you're about to write any of these,
stop and use a `Backend` instead:
- `db.Query("SELECT ... WHERE title LIKE ?", "%"+q+"%")` over a domain
  table — tanks the planner, gives no ranking, and you'll re-write it
  the moment data grows
- A range loop over `[]Post` doing `strings.Contains(p.Title, q)`
- A regex match across every row
- An ad-hoc `strings.Fields` tokeniser to "approximate" full-text

Push documents into a `Backend` (`Memory` in dev, durable in prod)
and call `Search` — the swap is a one-line change at the call site.
**Backend choice:** `Memory` is fine for tests and small-volume dev data;
it loses everything on restart. For a Postgres-first app, use
`search.NewPostgres(db, cfg)` — ranked full-text search straight out of the
database with zero extra infrastructure. Call `EnsureSchema` once on boot,
then `Index`/`Search` like any backend. See `framework/docs/content/search.md`
for construction, weighted fields, and the tenant-scoping example.
