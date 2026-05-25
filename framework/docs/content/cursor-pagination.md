# Cursor pagination

Cursor (keyset) pagination is the default for high-volume entities and
the only way to page consistently when rows are being inserted while
the user pages.

## Two pagination modes

Both are exposed on every CRUD list endpoint. The mode is chosen by
which query parameters you send.

| Mode    | Query parameters         | Response shape   |
|---------|--------------------------|------------------|
| Offset  | `?page=2&limit=20`       | `OffsetPage`     |
| Cursor  | `?cursor=<opaque>&limit=20` | `CursorPage`  |

If both are sent, `cursor` wins.

## Opting in

```bash
# First page — send empty cursor to choose cursor mode:
curl 'http://localhost:8080/posts?cursor=&limit=20'
# → {"data":[…], "cursor":"<opaque>", "hasMore":true}

# Subsequent page:
curl 'http://localhost:8080/posts?cursor=<opaque>&limit=20'

# Backward:
curl 'http://localhost:8080/posts?cursor=<opaque>&direction=backward'
```

## Defaults & caps

- `DefaultPageSize = 25`
- `MaxPageSize = 100` (`limit` is clamped silently)
- `limit < 1` falls back to `DefaultPageSize`
- `direction` defaults to `"forward"`; only `"forward"` and
  `"backward"` are accepted.

## Cursor field configuration

Cursor mode keysets on the entity's primary key by default. Override
with `EntityConfig`:

```go
// Single-field cursor on created_at:
framework.EntityConfig{
    Name: "posts",
    CursorField: "created_at",
    …
}

// Composite cursor: tuple-compare on (created_at, id):
framework.EntityConfig{
    Name: "posts",
    CursorFields: []string{"created_at", "id"},
    …
}
```

Composite cursors guarantee unique ordering by automatically appending
the primary key if it isn't already the tiebreak column. Without a
unique tiebreak, two rows with identical sort keys break paging.

## Wire format

The cursor is opaque base64 JSON. Clients must not decode or modify
it — only echo it back on the next request. The exact encoding
(single-field vs multi-field) is chosen by the server based on the
entity's `CursorField` / `CursorFields` config.

## Behaviour & guarantees

- **Consistent under inserts.** Rows inserted after the current page
  appear on a later page, never as duplicates on the current one.
- **`?sort=` is ignored in cursor mode.** Keyset pagination requires
  the ORDER BY to match the cursor key. Configure cursor fields
  instead.
- **No total count.** `CursorPage.Total` is reserved but not
  populated — cursor mode's point is avoiding the table scan a count
  requires. Use offset mode if you need a total.
- **Backward paging starts from `hasMore`'s end.** Backward responses
  use `<` instead of `>` and reverse the ORDER BY direction.

## When to use which mode

- **Offset** — for admin tables with stable data, "page 5 of 23", or
  any UI that exposes the page number to the user.
- **Cursor** — for infinite-scroll feeds, real-time-ish lists, or
  anything where the source data churns while users page.

## Common mistakes

- **Forgetting to send `cursor=` on the first request.** Without it,
  the endpoint serves offset mode. The empty string is the opt-in
  signal.
- **Caching cursors across schema changes.** A cursor is tied to the
  entity's `CursorField(s)`. Changing the config invalidates every
  outstanding cursor.
- **Trying to "jump to page N" with cursor mode.** You can't. Cursors
  are sequential by design.
- **Configuring a non-unique single cursor field.** Use `CursorFields`
  to add a tiebreak, or paging stalls on ties.
