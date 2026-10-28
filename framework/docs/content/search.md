# Search

The `battery/search` package defines a pluggable full-text search interface.
The framework ships three backends: an in-memory backend for dev/tests, a
Postgres full-text-search backend for production, and a SQLite FTS5 backend
for SQLite-first apps. Other engines (Bleve, Meilisearch, etc.) plug in
behind the same `Backend` interface.

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
| `Owner`  | `string`         | Optional. User id the document belongs to.       |

`Owner` is a storage tag, not a search filter: scope searches through
`Fields` and `FieldEquals`. Its job is erasure. The SQLite and Postgres
backends register a data eraser, so `App.EraseUserData` deletes every
document indexed for the erased user. Set it on any document whose text
or fields carry one user's data, or erasure leaves that copy behind.

### `Query`

| Field    | Type               | Notes                                            |
|----------|--------------------|--------------------------------------------------|
| `Text`   | `string`           | Query text. Empty string matches all documents.  |
| `Type`   | `string`           | Optional. Restricts results to one `Document.Type`. |
| `Limit`  | `int`              | Max hits. `0` means no limit (engine default).   |
| `Offset` | `int`              | Skip first N hits, useful for pagination.       |
| `FieldEquals` | `map[string]string` | Optional. Exact-match filter on `Document.Fields`: the scope hook (see below). |

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
is no registration step; wire your backend wherever you would have
used `search.NewMemory()`.

## The in-memory backend

`search.NewMemory()` returns a goroutine-safe in-process store. It is
appropriate for tests, single-binary demos, and small read-only sites.
The backend copies the top-level `Document.Fields` map when indexing and
returning results. Changing that map cannot alter the stored scope fields;
nested values remain caller-owned. Canceled contexts return an error.
Documents are stored in a map, so:

- Restarting the process drops the index.
- Memory usage is `O(total text size)`.
- Search is `O(n)` over the corpus.

The implementation is in `battery/search/memory.go`; read it before
using it in anything user-facing.

## The Postgres backend

`search.NewPostgres(db, cfg)` returns a backend that stores documents in a
single Postgres table with a generated `TSVECTOR` column, so a Postgres-first
app gets ranked full-text search without standing up separate search
infrastructure. Call `EnsureSchema` once on boot, then `Index`/`Search` like
any backend:

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

`EnsureSchema` is idempotent: it creates the table and GIN indexes on the
stemmed `tsv` column and the unstemmed `tsv_prefix` column (see Prefix
matching) if they don't already exist, so it's safe to call on every startup.
The indexes are named `<table>_tsv_idx` and `<table>_tsv_prefix_idx`. When
`<table>_tsv_prefix_idx` would pass Postgres's 63-byte limit, the table part
is cut short and an 8-hex-digit hash of the full table name goes before the
suffix, so the two indexes keep distinct names. `<table>_tsv_idx` keeps the
name Postgres's own cut at 63 bytes gives it, the name earlier releases
created, unless that cut is the table's own name (a 63-byte table name); then
it takes the hashed form too.
A trigger, `<table>_tsv_prefix`, computes `tsv_prefix` on every insert and on
every update of `text` or `fields`, so a writer that never names the column
keeps it current too: a replica still on the previous release during a rolling
upgrade inserts rows that prefix-match, and its rewrites never leave the old
text's prefixes behind. `EnsureSchema` installs the trigger and its function
(also named `<table>_tsv_prefix`), and replaces the trigger when
`WeightedFields` changes; replicas booting together take turns on an advisory
lock. The role needs to create functions and triggers, as it already creates
the table.

A table created before `tsv_prefix` existed gains the column and has it filled
from the stored rows on the first `EnsureSchema`. The column is added in a
statement of its own and the rows are filled in committed batches of 1000, so
other replicas keep searching and indexing during the backfill; a row not yet
filled still matches through `tsv` and only misses prefixes past its stem. A
replica that starts while another is filling helps finish the rows left. Once
every row is filled the column carries a comment, and later boots skip the
backfill after a catalog read and the trigger check.

### Weighted fields

The document body (`Document.Text`) is always indexed at weight `'A'`. Promote
structured fields so a hit in a title outranks one in the body:

<!-- gofastr:compile
stmt: _ = idx
import "github.com/DonaldMurillo/gofastr/battery/search"
import "database/sql"
var db *sql.DB
-->
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
the query. The filter is in-query, not post-filtered:

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
`:*`, so partial input matches as the user types, useful for command palettes
and autocomplete. Every prefix of a word matches it: `"pag"`, `"pagina"` and
`"paginatio"` all find `"pagination"`.

The English config stems both the document and the query, and a stemmed
prefix is no prefix of the stemmed word (`"pagina"` stems to `"pagina"`, the
word to `"pagin"`), so the stemmed column alone loses every prefix past the
stem. The last term is also matched against `tsv_prefix`, the same text under
the `simple` config with no stemming, and the other terms must still match
the stemmed column.

Query text splits into terms at any rune that is not a letter or digit, the
way the tokenizer splits documents: `"won't"` is `won & t:*`, and
`"load-balance"` is two terms. A query made only of stopwords (`"don't"` is
`don` and `t`) matches nothing.

### What it deliberately doesn't do

This backend does **not** use `pg_trgm`. That extension needs `CREATE EXTENSION`
(a superuser privilege many managed-database roles lack), so fuzzy/trigram
matching is left to a future backend that can opt into the extension explicitly.
All query text is sanitized before reaching `to_tsquery` (operators and SQL
metacharacters are stripped), so hostile input neither errors nor matches
every document.

## The SQLite FTS5 backend

`search.NewSQLiteFTS(db, cfg)` returns a backend backed by a single SQLite FTS5
virtual table, so a SQLite-first app gets ranked BM25 full-text search without
standing up separate search infrastructure. Call `EnsureSchema` once on boot,
then `Index`/`Search` like any backend:

```go
idx, err := search.NewSQLiteFTS(db, search.SQLiteFTSConfig{})
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

### Build tag

FTS5 is an SQLite compile-time option. The `mattn/go-sqlite3` driver bundles
FTS5 **only** when built with the `-tags sqlite_fts5` build tag:

```sh
go build -tags sqlite_fts5 .
go test -tags sqlite_fts5 ./...
```

Without the tag, `EnsureSchema` returns an actionable error naming the tag.
The test suite probes FTS5 availability at runtime and `t.Skip`s when absent,
so the default `go test ./...` stays green.

### Configuration

`SQLiteFTSConfig`:

| Field   | Type     | Notes                                              |
|---------|----------|----------------------------------------------------|
| `Table` | `string` | Destination FTS5 virtual table. Defaults to `search_documents`. |

The schema is fixed: `CREATE VIRTUAL TABLE ... USING fts5(id UNINDEXED, type UNINDEXED, fields UNINDEXED, text, tokenize='porter unicode61')`. Only `Document.Text` is tokenised; structured fields are stored (UNINDEXED) for `FieldEquals` filtering via `json_extract`.

Beside it `EnsureSchema` keeps `<table>_prefix`, a contentless FTS5 table over
the same text with the `unicode61` tokenizer and no stemming. The porter
tokenizer stems a prefix query too (`"pagina"*` finds nothing, since the index
holds `pagin`), so `Search` also runs the last term's prefix scan, `"pagina"*`,
against the unstemmed table, with the other terms still matched stemmed in the
main one, and a row matches when either does. Every prefix of a word finds it,
however many indexed words share the prefix. A table created before the
companion existed has it filled from the stored rows on the first
`EnsureSchema`, which also drops the `<table>_prefix_vocab` view earlier
releases created.

Query text splits into terms at any rune that is not a letter or digit, the
way `unicode61` splits documents, so `"won't"` matches `won't`.

### Ranking

Results are ranked by BM25. The SQLite FTS5 `bm25()` function returns negative
values (more negative = better match), so the backend exposes
`Score = -bm25`; callers sort descending consistently with the other backends.
A row both the stemmed and the unstemmed prefix leg match scores the sum of
the two, as the Postgres backend sums `ts_rank` over `tsv` and `tsv_prefix`.
The tiebreak is `id ASC`.

### Scoping with `FieldEquals`

`FieldEquals` uses `json_extract(fields, '$."key"') = ?`: the key is validated
against `^[A-Za-z0-9_]+$` before the JSON path is interpolated, and the value
is parameterised. A `json_type(fields, '$."key"') = 'text'` check beside it
keeps matching string-only: `json_extract` returns an array or object as its
JSON text, so without it a stored `tenant: ["acme"]` would equal the filter
value `["acme"]`.

### Indexing

FTS5 has no upsert, so `Index` is `DELETE` by id + `INSERT` in a single
transaction: re-indexing the same id replaces in place with no duplicate rows.
The prefix companion row shares the main row's rowid and is replaced and
deleted in the same transaction.

## Choosing a backend

| Backend | Durable | Ranked | Weighted fields | Infrastructure | Build tag |
|---------|---------|--------|-----------------|----------------|-----------|
| `Memory` | No (in-process) | Term frequency | No | None | None |
| `PostgresSearch` | Yes | `ts_rank` | Yes (`A`..`D`) | Postgres | None |
| `SQLiteFTS` | Yes | BM25 | No | SQLite | `sqlite_fts5` |

**When to pick:**

- **Memory**: tests, single-binary demos, small read-only sites. Loses
  everything on restart.
- **PostgresSearch**: a Postgres-first production app. Ranked search
  without standing up separate search infrastructure, plus weighted fields
  for title-vs-body ranking.
- **SQLiteFTS**: a SQLite-first app (embedded, single-file, edge). Ranked
  BM25 search without standing up Postgres. Needs the `sqlite_fts5` build tag.

All three share AND-of-terms semantics, prefix matching on the last term,
the same empty-query (match-all) and pure-punctuation (match-none) edge
cases, and the string-only `FieldEquals` scoping contract.

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
`framework.Battery` only: `Name` and `Init` (a no-op); there is no
`OnStop`. A future backend that starts a background goroutine will need
an `OnStop` added to this wrapper (it does not exist today): implement
`io.Closer` on that backend type and call `Close` from the new hook.

## Common mistakes

- **Indexing inside a hook without context.** Pass the request context
  to `Index` so cancellations propagate. Don't use `context.Background()`
  from inside a request-scoped hook; you'll leak goroutines on client
  disconnect.
- **Expecting `Type` to be required.** It isn't. Searches without a
  `Type` filter scan all documents. Set `Type` consistently when you
  index, or be ready for cross-entity results.
- **Treating the memory backend as durable.** It isn't. Wire a real
  backend before relying on the index surviving restart.
- **Forgetting the `sqlite_fts5` build tag.** The `SQLiteFTS` backend
  needs the driver compiled with FTS5 support. Without `-tags sqlite_fts5`,
  `EnsureSchema` fails with an actionable error; the test suite skips.
