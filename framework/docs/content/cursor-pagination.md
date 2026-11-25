# Cursor pagination

Cursor (keyset) pagination gives stable, constant-time paging over large or changing tables without `OFFSET` drift or deep-skip scans.

## Enabling cursor pagination

Declare `Pagination.CursorFields` with the columns that lead the sort order. GoFastr appends any primary-key column missing from the list as a tie-breaker. This example pages on the generated `created_at` column, then `id`:

```go
app.Entity("events", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
	},
	Pagination: &framework.PaginationConfig{
		CursorFields: []string{"created_at"},
		MaxListLimit: 100,
	},
})
```

In `gofastr.yml`:

```yaml
pagination:
  cursor_fields: [created_at]
  max_list_limit: 100
```

## Page size defaults

- CRUD entity list endpoints (`GET /<table>`) **default to 20 rows** per page on both offset and cursor requests (including an empty `?cursor=`), clamped to `Pagination.MaxListLimit` **(100 when unset**, hard cap `1000`).
- Lower-level `framework/pagination` helpers use their own defaults: `pagination.DefaultPageSize` is 25 and `pagination.MaxPageSize = 100`.

## Using cursors over HTTP

Start a cursor walk by explicitly passing `?cursor=` (empty string) — omitting `cursor` stays in offset mode even when `page` is omitted — then pass the returned `cursor` token on each next request:

```bash
# First page
curl "http://localhost:8080/api/events?cursor=&limit=20"

# Next page
curl "http://localhost:8080/api/events?cursor=<cursor>&limit=20"
```

Response envelope:

```json
{
  "data": [ /* ... */ ],
  "cursor": "eyJjcmVhdGVkX2F0IjoiMjAyNi0xMS0yMFQxMDowMDowMFoiLCJpZCI6NDJ9",
  "hasMore": true
}
```

When `hasMore` is `false`, `cursor` is empty and you have reached the end.

## Rules and guarantees

- **Cursor column types:** every cursor column must be non-null and not JSON; `Define` panics on a JSON or nullable cursor column. To page by a value inside a JSON document, copy it into its own `Required` scalar column and name that column in `CursorFields`.
- **Sort order:** cursor queries order by `CursorFields`, then any primary-key column missing from it. If `?sort=` is supplied in cursor mode, it is validated (rejecting unknown/`Hidden`/`NoQuery` columns) and then ignored.
- **Opaque tokens:** treat cursors as opaque strings. Tampered, malformed, or column-mismatched tokens return `400`.
- **Filters stay in the query string:** pass the same filter parameters (`?status=open&cursor=...`) on each subsequent page; the cursor encodes only the keyset position.
- **Scopes apply on every page:** `Scope.OwnerField`, `Scope.MultiTenant`, `Scope.SoftDelete`, and `ReadScope` apply identically to cursor queries.
- **Index the full cursor order:** add a composite SQL index on `CursorFields` plus the appended key columns (for the example above, `(created_at, id)`) so keyset seeks stay `O(log N)`.

## Common mistakes

- **Omitting `?cursor=` on the first page:** without `cursor=`, the endpoint uses offset pagination.
- **Expecting `?sort=` to change cursor ordering:** cursor pagination validates `?sort=` and ignores it in favor of `CursorFields`.
- **Confusing CRUD defaults with `framework/pagination`:** CRUD routes default to 20 rows (capped by `MaxListLimit`, 100 when unset), whereas `framework/pagination` uses `pagination.DefaultPageSize` (25) and `pagination.MaxPageSize = 100`.
- **Declaring your own nullable `created_at`:** a `created_at` field you declare replaces the generated one, and a cursor column that can hold `NULL` makes `Define` panic. Mark it `Required`, or leave it out and use the generated column.
- **Forgetting the composite index:** without an index on `(created_at, id)`, the database still has to sort the table.
