# Search

Search is exposed through `battery/search`.

```go
index := search.NewMemory()
_ = index.Index(ctx, search.Document{
    ID: "post-1",
    Type: "posts",
    Text: "GoFastr framework release",
})

results, err := index.Search(ctx, search.Query{
    Text: "framework",
    Type: "posts",
    Limit: 10,
})
```

The in-memory backend is intended for tests, local examples, and small demos. It
implements the same `Backend` interface a production backend should implement:

- `Index(ctx, doc)`
- `Delete(ctx, id)`
- `Search(ctx, query)`

The blog example wires this into `GET /posts/search?q=...`.
