# Search

The `battery/search` package defines a pluggable full-text search interface.
The framework ships one in-memory backend; production deployments swap in
a real engine (Postgres FTS, Bleve, Meilisearch, etc.) behind the same
`Backend` interface.

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

| Field    | Type     | Notes                                            |
|----------|----------|--------------------------------------------------|
| `Text`   | `string` | Query text. Empty string matches all documents.  |
| `Type`   | `string` | Optional. Restricts results to one `Document.Type`. |
| `Limit`  | `int`    | Max hits. `0` means no limit (engine default).   |
| `Offset` | `int`    | Skip first N hits — useful for pagination.       |

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
