# Query DSL

Every CRUD list route (`GET /<table>`, `GET /<table>?stream=true`, and the generated `<table>_list` MCP tool) supports filtering, full-text search, sorting, field selection, and pagination through URL query parameters — all evaluated server-side in SQL.

## Filter operators

Pass column names directly for equality, or append an operator suffix (`<field>_<op>=<value>`):

| Suffix | Meaning | Example |
| --- | --- | --- |
| *(none)* | Equals | `?status=open` |
| `_ne` | Not equal | `?status_ne=archived` |
| `_gt`, `_gte` | Greater than / or equal | `?amount_gte=100` |
| `_lt`, `_lte` | Less than / or equal | `?created_at_lt=2026-01-01T00:00:00Z` |
| `_like` | Substring / pattern match (`LIKE`) | `?title_like=invoice` |
| `_in` | Comma-separated set | `?status_in=draft,sent` |

Multiple query parameters combine with `AND`. Repeating the same parameter or using `_in` matches any listed value. Unknown fields, `Hidden` fields, and `NoQuery` fields are rejected with `400`.

### Relation filters

Filter across a declared `belongs_to` relation with `<relation>.<field>` (and any operator suffix):

```
GET /api/posts?author.role=editor&author.active=true
```

Relation filters enforce the target entity's read access, `OwnerField`, `MultiTenant`, `SoftDelete`, and `CRUD` visibility. Filtering across an entity the caller cannot read (or a `CRUD: false` entity) returns `403`.

## Search (`?q=`)

`?q=term` searches every column listed in `EntityConfig.SearchFields` (case-insensitive substring match, combined with `OR` across search columns and `AND` with other filters). Sending a non-blank `?q=` on an entity without `SearchFields` returns `400 search_not_configured`.

With `SearchFields` configured, use `q[eq]=value` or the column's distinct wire name to filter a column named `q` alongside the search.

## Sorting (`?sort=`)

Pass a comma-separated list of columns; prefix with `-` for descending:

```
GET /api/tickets?sort=-priority,created_at
```

When `?sort=` is omitted, the handler falls back to primary key order. Sorting on `Hidden` or `NoQuery` fields is rejected with `400`.

When cursor pagination is active, `?sort=` is validated and then ignored—cursor walks always order by `EntityConfig.Pagination.CursorFields` (see [Cursor pagination](cursor-pagination.md)).

## Pagination and streaming

- **Offset mode:** `?page=2&limit=20` or `?offset=40&limit=20`. `limit` defaults to `20` and clamps to `Pagination.MaxListLimit` (`100` when unset, max `1000`). Deep skips past `MaxOffset` return `400 offset_too_large`.
- **Cursor mode:** `?cursor=<token>&limit=20` when `EntityConfig.Pagination.CursorFields` is configured.
- **NDJSON streaming:** `?stream=true` streams rows as newline-delimited JSON instead of buffering the full page in memory, applying the same filters, scopes, and offset ceiling.

## Sparse fieldsets (`?fields=`)

Request a subset of columns with `?fields=id,title,status` (primary key fields are always included when needed for links/cursors; `Hidden` fields remain excluded).

## Programmatic DSL (`framework/dsl`)

For saved views or server-side query strings (such as `DisplayConfig.Views`), `framework/dsl` parses and compiles chained DSL expressions against your entity registry:

```go
parsed, err := dsl.ParseDSL(`tickets.where(status="open").order(-created_at).limit(20)`)
qb, err := dsl.BuildDSLQuery(app.Registry, `tickets.where(status="open").order(-created_at).limit(20)`)
```

> **Security warning:** `dsl.BuildDSLQuery` checks field names, `Hidden`, and `NoQuery`, but it applies **no** `OwnerField`, `MultiTenant`, `ReadScope`, or `SoftDelete` scoping. Always append your caller/tenant/soft-delete predicates to `qb` before executing it on behalf of a user.

## SQL builders (`core/query`)

### Programmatic inserts

When using `core/query`, pass one value per column to `Columns` and `Values`, in
the same order. `InsertBuilder.Build` panics if the counts differ. If both lists
are empty, the builder emits `INSERT INTO … DEFAULT VALUES`.

### Programmatic updates

When building an update with `query.Update`, call `Set` for each assignment.
With no assignments, `UpdateBuilder.Build` panics with
`query: UPDATE <table> has no Set assignments` instead of producing invalid
SQL. When building updates from optional fields, handle the no-fields case
before calling `Build`.

## MCP tool arguments

The generated `<table>_list` MCP tool accepts the same filter keys as typed JSON arguments (`status`, `amount_gte`, `status_in: ["draft", "sent"]`). For `_in`, pass a non-empty JSON array whose elements contain no commas.

## Common mistakes

- **Putting `CursorFields` at the top level of `EntityConfig`:** cursor fields live under `EntityConfig.Pagination.CursorFields`.
- **Filtering or sorting on `Hidden` or `NoQuery` columns:** both are rejected with `400` to prevent leaking masked values through side channels.
- **Passing commas inside an `_in` value:** `_in` splits on commas; values containing commas cannot be matched via `_in`.
- **Filtering across a `CRUD: false` relation over HTTP:** cross-entity filters respect target exposure and return `403`.
- **Deep offset pagination (`?page=50000`):** rejected with `400 offset_too_large`. Configure `EntityConfig.Pagination.CursorFields` for large datasets.
