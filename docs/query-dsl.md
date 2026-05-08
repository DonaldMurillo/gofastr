# Query DSL

GoFastr includes a small agent-friendly query DSL for validating declarative
queries against registered entities and compiling them to the core query
builder.

```go
qb, err := framework.BuildDSLQuery(app.Registry,
    `posts.where(status="published", views>=10).include(author).order(created_at DESC).limit(10)`,
)
```

Supported calls:

- `where(field=value, field>=value)` with `=`, `!=`, `>`, `<`, `>=`, `<=`, `contains`, and `in`.
- `include(relation)` validates that the relation exists.
- `order(field ASC)` or `order(field DESC)`.
- `limit(N)`.
- `after(cursor)` is parsed and reserved for cursor-aware generated queries.

The parser rejects unknown entities, fields, relations, operators, and invalid
sort directions before SQL is produced.
