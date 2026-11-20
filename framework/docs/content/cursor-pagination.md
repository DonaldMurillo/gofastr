# Cursor pagination

Cursor (keyset) pagination gives stable, constant-time paging over large or changing tables without `OFFSET` drift or deep-skip scans.

## Enabling cursor pagination

Declare `Pagination.CursorFields` on your entity with the columns that form the deterministic sort order (always include a unique tie-breaker such as `id` as the last column):

```go
app.Entity("events", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "created_at", Type: schema.Time},
	},
	Pagination: &framework.PaginationConfig{
		CursorFields: []string{"created_at", "id"},
		MaxListLimit: 100,
	},
})
```

In `gofastr.yml`:

```yaml
pagination:
  cursor_fields: [created_at, id]
  max_list_limit: 100
```

## Page size defaults

- CRUD entity list endpoints (`GET /<table>`) **default to 20 rows** per page on both offset and cursor requests (including an empty `?cursor=`), clamped to `Pagination.MaxListLimit` **(100 when unset**, hard cap `1000`).
- Lower-level `core/pagination` helpers use their own defaults: `pagination.DefaultPageSize` is 25 and `pagination.MaxPageSize = 100`.

## Using cursors over HTTP

Start a cursor walk by passing `?cursor=` (empty string) or omitting `page`/`offset` and reading `next_cursor` from the response:

```bash
# First page
curl "http://localhost:8080/api/events?cursor=&limit=20"

# Next page
curl "http://localhost:8080/api/events?cursor=<next_cursor>&limit=20"
```

Response envelope:

```json
{
  "data": [ /* ... */ ],
  "next_cursor": "eyJjcmVhdGVkX2F0IjoiMjAyNi0xMS0yMFQxMDowMDowMFoiLCJpZCI6NDJ9",
  "has_more": true
}
```

When `has_more` is `false`, `next_cursor` is empty and you have reached the end.

## Rules and guarantees

- **Sort order:** cursor queries order by `CursorFields` (or their exact reverse when descending). Requesting a `?sort=` that does not match `CursorFields` returns `400`.
- **Opaque tokens:** treat cursors as opaque strings. Tampered, malformed, or column-mismatched tokens return `400`.
- **Filters stay in the query string:** pass the same filter parameters (`?status=open&cursor=...`) on each subsequent page; the cursor encodes only the keyset position.
- **Scopes apply on every page:** `Scope.OwnerField`, `Scope.MultiTenant`, `Scope.SoftDelete`, and `ReadScope` apply identically to cursor queries.
- **Index your cursor columns:** add a composite SQL index matching `CursorFields` (for example `(created_at, id)`) so keyset seeks stay `O(log N)`.

## Common mistakes

- **Omitting a unique tie-breaker column from `CursorFields`:** if two rows share the same `created_at`, paging without `id` as the final column can skip or duplicate rows.
- **Mixing `?cursor=` with `?page=` or `?offset=`:** pick one pagination mode per request.
- **Changing `?sort=` while passing `?cursor=`:** cursor pagination requires the sort order to match `CursorFields`.
- **Confusing CRUD defaults with `core/pagination`:** CRUD routes default to 20 rows (capped by `MaxListLimit`, 100 when unset), whereas `core/pagination` uses `pagination.DefaultPageSize` (25) and `pagination.MaxPageSize = 100`.
- **Forgetting a composite index on `CursorFields`:** without an index on `(created_at, id)`, the database still has to sort the table.
