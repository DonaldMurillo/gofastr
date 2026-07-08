# Search

The `battery/search` package defines a pluggable full-text search interface.
The framework ships two backends: an in-memory backend for dev/tests and a
Postgres full-text-search backend for production. Other engines (Bleve,
Meilisearch, etc.) plug in behind the same `Backend` interface.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/search"

index := search.NewMemory()

// Index a document.
_ = index.Index(ctx, search.Document{
    ID:   "post-1",
    Type: "posts",
    Text: "GoFastr framework release notes",
    Fields: map[string]any{"author": "carol", "tags": []string{"go"}},
})

// Query it.
results, err := index.Search(ctx, search.Query{
    Text:  "framework",
    Type:  "posts",
    Limit: 10,
})
```

The blog example wires this into `GET /posts/search?q=...`. See
`examples/blog/main.go`.

## Types

### `Document`

| Field    | Type             | Notes                                            |
|----------|------------------|--------------------------------------------------|
| `ID`     | `string`         | Required. Unique per backend.                    |
| `Type`   | `string`         | Optional. Filterable namespace ("posts", etc.).  |
| `Text`   | `string`         | Free text body the engine tokenises.             |
| `Fields` | `map[string]any` | Optional structured fields returned with hits.   |

### `Query`

| Field    | Type               | Notes                                            |
|----------|--------------------|--------------------------------------------------|
| `Text`   | `string`           | Query text. Empty string matches all documents.  |
| `Type`   | `string`           | Optional. Restricts results to one `Document.Type`. |
| `Limit`  | `int`              | Max hits. `0` means no limit (engine default).   |
| `Offset` | `int`              | Skip first N hits — useful for pagination.       |
| `FieldEquals` | `map[string]string` | Optional. Exact-match filter on `Document.Fields` — the scope hook (see below). |

### `Result`

```go
type Result struct {
    Document Document
    Score    float64
}
```

Score is engine-specific. The memory backend uses a naive term-frequency
score; a real engine returns BM25 or similar. Don't compare scores
across backends.

## The `Backend` interface

```go
type Backend interface {
    Index(ctx context.Context, doc Document) error
    Delete(ctx context.Context, id string) error
    Search(ctx context.Context, query Query) ([]Result, error)
}
```

Anything that implements all three methods is a valid backend. There
is no registration step — wire your backend wherever you would have
used `search.NewMemory()`.

## The in-memory backend

`search.NewMemory()` returns a goroutine-safe in-process store. It is
appropriate for tests, single-binary demos, and small read-only sites.
Documents are stored in a map, so:

- Restarting the process drops the index.
- Memory usage is `O(total text size)`.
- Search is `O(n)` over the corpus.

The implementation is in `battery/search/memory.go` — read it before
using it in anything user-facing.

## The Postgres backend

`search.NewPostgres(db, cfg)` returns a backend that stores documents in a
single Postgres table with a generated `TSVECTOR` column, so a Postgres-first
app gets ranked full-text search with zero extra infrastructure. Call
`EnsureSchema` once on boot, then `Index`/`Search` like any backend:

```go
idx, err := search.NewPostgres(db, search.PostgresConfig{})
if err != nil {
    return err
}
if err := idx.EnsureSchema(ctx); err != nil {
    return err
}

_ = idx.Index(ctx, search.Document{
    ID:   "post-1",
    Type: "posts",
    Text: "GoFastr framework release notes",
})

results, err := idx.Search(ctx, search.Query{Text: "framework", Limit: 10})
```

### Configuration

`PostgresConfig`:

| Field           | Type               | Notes                                              |
|-----------------|--------------------|----------------------------------------------------|
| `Table`         | `string`           | Destination table. Defaults to `search_documents`. |
| `Language`      | `string`           | tsvector config. Defaults to `english`; validated against `^[a-z_]+$`. |
| `WeightedFields`| `map[string]byte` | Maps `Document.Fields` keys (string values) to a weight `'A'`..`'D'`. |

`EnsureSchema` is idempotent: it creates the table and a GIN index on the
tsvector if they don't already exist, so it's safe to call on every startup.

### Weighted fields

The document body (`Document.Text`) is always indexed at weight `'A'`. Promote
structured fields so a hit in a title outranks one in the body:

```go
idx, _ := search.NewPostgres(db, search.PostgresConfig{
    WeightedFields: map[string]byte{"title": 'A', "summary": 'B'},
})
```

A field configured in `WeightedFields` is indexed only when its value is a
string; non-string values are skipped. The default `ts_rank` weight multipliers
(`D` < `C` < `B` < `A`) mean a weight-`'A'` hit scores higher than a weight-`'C'` hit.

### Scoping with `FieldEquals`

`Query.FieldEquals` is the multi-tenant / per-owner / permission scope hook.
Put the scope value in `Document.Fields` at index time and filter on it in
the query — in-query, not post-filtered:

```go
_ = idx.Index(ctx, search.Document{
    ID:     "post-1",
    Type:   "posts",
    Text:   "Tenant-scoped content",
    Fields: map[string]any{"tenant": "acme"},
})

// Only documents whose Fields["tenant"] == "acme" can match.
results, _ := idx.Search(ctx, search.Query{
    Text:        "content",
    FieldEquals: map[string]string{"tenant": "acme"},
})
```

Matching is string-only and identical across the memory and Postgres backends:
a document matches iff, for every key/value pair, `Fields[key]` is present and
its value is a string equal to `value`. A field whose value is not a string
never matches. The Postgres backend encodes this as JSONB containment
(`fields @> '{"tenant":"acme"}'`); the memory backend applies the same rule in Go.

### Prefix matching

The query builder joins terms with AND and suffixes the **last** term with
`:*`, so partial input matches as the user types — ideal for command palettes
and autocomplete. A query for `"pagin"` matches documents containing
`"pagination"`.

### What it deliberately doesn't do

This backend does **not** use `pg_trgm`. That extension needs `CREATE EXTENSION`
(a superuser privilege many managed-database roles lack), so fuzzy/trigram
matching is left to a future backend that can opt into the extension explicitly.
All query text is sanitized before reaching `to_tsquery` (operators and SQL
metacharacters are stripped), so hostile input neither errors nor matches
every document.

## Battery wrapper

`search.NewBattery(backend)` wraps any `Backend` in a
`framework.Battery` so the index participates in the App's
dependency-resolved lifecycle.  This lets other batteries declare
`"search"` as a dependency, guaranteeing the index is available before
their own `Init` runs.

```go
idx := search.NewMemory()
app.Batteries.Register(search.NewBattery(idx))

// Retrieve the backend from within another battery:
b, _ := app.Batteries.Get("search")
sb := b.(*search.Battery)
results, _ := sb.Backend().Search(ctx, search.Query{Text: "..."})
```

`search.Memory` has no background goroutine, so `Battery` implements
`framework.Battery` only (no `BatteryLifecycle`).  If a future
production backend starts a background goroutine, implement `io.Closer`
on that type and the `Battery` wrapper's `OnStop` will call `Close()`
automatically.

## Common mistakes

- **Indexing inside a hook without context.** Pass the request context
  to `Index` so cancellations propagate. Don't use `context.Background()`
  from inside a request-scoped hook — you'll leak goroutines on client
  disconnect.
- **Expecting `Type` to be required.** It isn't. Searches without a
  `Type` filter scan all documents. Set `Type` consistently when you
  index, or be ready for cross-entity results.
- **Treating the memory backend as durable.** It isn't. Wire a real
  backend before relying on the index surviving restart.
