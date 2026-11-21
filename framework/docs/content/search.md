# Search

`battery/search` provides a pluggable full-text search `Backend` interface with three built-in implementations: `Memory` (dev/tests), `PostgresSearch` (`TSVECTOR` + `ts_rank` + field weights), and `SQLiteFTS` (FTS5 + BM25).

> For searching your app's own rendered web pages and docs from a command palette, use [`battery/sitesearch`](site-search.md). Use `battery/search` for database records.

## Backend comparison

| Backend | Durable | Ranking | Weighted fields | Notes |
| --- | --- | --- | --- | --- |
| `search.NewMemory()` | No | Term frequency | No | In-process; call `Close()` or register via `search.NewBattery` |
| `search.NewPostgres(db, cfg)` | Yes | `ts_rank` | Yes (`'A'`..`'D'`) | Auto-managed GIN indexes + unstemmed prefix column |
| `search.NewSQLiteFTS(db, cfg)` | Yes | BM25 | No | Works out of the box with GoFastr's pure-Go SQLite driver (`-tags sqlite_fts5` needed only with `mattn/go-sqlite3`) |

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/search"

idx, err := search.NewPostgres(db, search.PostgresConfig{
	WeightedFields: map[string]byte{"title": 'A', "summary": 'B'},
})
if err != nil {
	return err
}
if err := idx.EnsureSchema(ctx); err != nil {
	return err
}

_ = idx.Index(ctx, search.Document{
	ID:     "post-1",
	Type:   "posts",
	Text:   "GoFastr framework release notes",
	Owner:  userID, // enables automatic deletion in App.EraseUserData
	Fields: map[string]any{"tenant": "acme", "title": "Release notes"},
})

results, err := idx.Search(ctx, search.Query{
	Text:        "framework",
	Type:        "posts",
	FieldEquals: map[string]string{"tenant": "acme"}, // in-query tenant/owner scoping
	Limit:       10,
})
```

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

Register with `app.Batteries.Register(search.NewBattery(idx))` so other batteries can declare `"search"` as a dependency and `Memory` cleans up on shutdown.

## Query semantics, scoping, and erasure

- **Prefix matching & AND terms:** all three backends AND query terms together and prefix-match the final term (backed by dedicated unstemmed prefix indexes in Postgres and SQLite FTS5 so autocomplete works past stem boundaries).
- **In-query scoping (`FieldEquals`):** string-only exact match on `Document.Fields` (JSONB `@>` in Postgres, `json_each` in SQLite). Always put `tenant` or `owner` in `Fields` and filter via `FieldEquals` for multi-tenant/per-user search.
- **User erasure (`Document.Owner`):** set `Owner` on documents carrying personal data; all three backends register erasers so `App.EraseUserData` deletes that user's indexed documents automatically.
- **Tokenizer differences:** FTS5 folds diacritics by default while Postgres `english` and `Memory` keep them; Postgres and FTS5 stem while `Memory` does not. Normalize text yourself before `Index`/`Search` if you need identical tokenization across engines.

## Common mistakes

- **Using `search.NewMemory()` in production:** drops the entire index on restart and scans `O(n)`.
- **Forgetting `Document.Owner` on user-owned documents:** `App.EraseUserData` won't know which search documents belong to an erased user.
- **Post-filtering results in Go instead of using `Query.FieldEquals`:** returns fewer than `Limit` results and wastes work; filter in the query.
- **Building `SQLiteFTS` with `mattn/go-sqlite3` without `-tags sqlite_fts5`:** `EnsureSchema` fails unless FTS5 is enabled at compile time (GoFastr's default pure-Go SQLite driver ships with FTS5 enabled automatically).
- **Forgetting to pass request `ctx` into `Index` / `Search`:** prevents cancellation propagation on client disconnects.
