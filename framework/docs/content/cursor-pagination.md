# Cursor pagination

Cursor (keyset) pagination is the default for high-volume entities and
the only way to page consistently when rows are being inserted while
the user pages.

## Two pagination modes

Both are exposed on every CRUD list endpoint. The mode is chosen by
which query parameters you send.

| Mode    | Query parameters         | Response shape   |
|---------|--------------------------|------------------|
| Offset  | `?page=2&limit=20`       | `ListResponse`   |
| Cursor  | `?cursor=<opaque>&limit=20` | `CursorPage`  |

If both are sent, `cursor` wins.

`ListResponse` always carries `data`, `total`, `page`, `perPage` and
`totalPages`; `CursorPage` always carries `data`, `cursor` and
`hasMore`. The OpenAPI document describes the list route's 200 as
`oneOf` the two and marks those keys `required`, so a body matches
exactly one of them.

## Opting in

```bash
# First page: send empty cursor to choose cursor mode:
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
- `Pagination.MaxListLimit` caps the cursor path too: a `limit` above
  the entity's cap is clamped to it, same as the offset and streaming
  list paths. (An oversized `limit` is clamped, never reset to the
  default.)
- A `cursor` longer than 16 KiB, or a multi-field cursor carrying more
  than 64 fields, is rejected as invalid (400 on the list routes).
  Cursors are client-echoed input, so decode work is bounded by these
  fixed caps, never by the payload size the client chose. A cursor the
  server mints carries the last row's sort values and key when they fit
  both caps, and the key alone when they do not, so a long sort value
  (a Text column) never pushes it past the cap. A key that does not fit
  (a key value of more than about 12 KiB, which only a Text or unbounded
  String key on SQLite can hold) answers 500 with reason
  `cursor_key_too_long` on the page that would mint the cursor, rather
  than a token the next request refuses; page such a list with `?page=`.
  `pagination.CursorFits` is the check, for code that mints its own
  cursors. `pagination.NewCursorPage`, the helper for building a cursor
  page by hand, returns an error wrapping `pagination.ErrCursorTooLong`
  in that case, and the page carries no cursor:

  ```go
  page, err := pagination.NewCursorPage(rows, "id", limit)
  if errors.Is(err, pagination.ErrCursorTooLong) {
      // key the list on a shorter column, or page it by offset
  }
  ```
- `direction` defaults to `"forward"`; only `"forward"` and
  `"backward"` are accepted.

## Cursor field configuration

Cursor mode keysets on the entity's primary key by default. Override
with `EntityConfig`:

```go
// Single-field cursor on created_at:
framework.EntityConfig{
    Name: "posts",
    Pagination: &framework.PaginationConfig{CursorField: "created_at"},
    …
}

// Composite cursor: tuple-compare on (created_at, id):
framework.EntityConfig{
    Name: "posts",
    Pagination: &framework.PaginationConfig{CursorFields: []string{"created_at", "id"}},
    …
}
```

Both spellings guarantee unique ordering by appending each primary-key
column the list does not already name: `CursorField: "created_at"`
pages on `(created_at, id)`. Without a unique tiebreak, the second of
two rows with identical sort keys would be skipped. The key columns are
cursor columns like any other, so `entity.Define` refuses a `Hidden` or
`NoQuery` key column behind a `CursorField` too: the token carries the
key. A `CursorFields` list that names one column twice fails `Define`.

**A cursor column must never hold NULL.** The keyset comparison
`(created_at, id) > ($1, $2)` is never true for a row whose
`created_at` is NULL, so those rows would be skipped, and SQLite sorts
NULLs first where Postgres sorts them last. `entity.Define` panics on a
cursor column that is not a key column, not `Required`, and not
framework-generated (`AutoGenerate`, which covers the injected
`created_at` and an auto id); the app spec refuses the same column at
decode time. If a row's keyset value is NULL anyway (a raw INSERT that
bypassed the API), the page answers 500 and logs the column rather than
minting a cursor that compares against `null`.

## Wire format

The cursor is opaque base64 JSON. Clients must not decode or modify
it, only echo it back on the next request. It carries the last row
served (the anchor): its `CursorField(s)` values and its key, and the
next page starts after those values. When the values would push the
token past the 16 KiB cap (a long Text sort column), the cursor carries
the anchor's key alone, and the next request reads the sort values back
from the table by that key, one indexed read per page. An entity
cursored on its own key always carries just the key. One column uses
the single-field encoding, several the multi-field one.

The values, carried or read back, are bound the way crud stores them. A timestamp
is written in the layout crud stores timestamps with, UTC with six
fractional digits kept (`2026-10-27T07:05:07.500000Z`), so on SQLite,
which compares `DATETIME` columns as text, the keyset compares against
the stored string byte for byte. crud writes every Timestamp in that
layout, a client's `2026-10-27T09:05:07+02:00` included (same instant,
stored as `2026-10-27T07:05:07.000000Z`); a row written in another
spelling before v0.96.0, or by SQL outside crud, still compares as its
own text on SQLite. A Date value is the date (`2026-10-27`). A key
value that is not of its column's type (an edited token naming `id`
`"abc"` on an integer key) is a 400, the same check a filter value
gets. A token the server
did not mint is decoded under the same no-ambiguity rule as every
other client-borne JSON: duplicate and case-folded keys, at any
depth of the token, are rejected as invalid (the 16 KiB / 64-field
caps above bound the work). Stdlib `encoding/json` resolves such
keys last-wins; the keyset must not resume paging under a token any
first-read intermediary parsed differently.

## Behaviour & guarantees

- **Consistent under inserts.** Rows inserted after the current page
  appear on a later page, never as duplicates on the current one.
- **A cursor that carries its values survives edits to the anchor.**
  If the anchor row is deleted, or its sort value changes, between
  pages, the next page still starts after the values the cursor holds,
  so no row between is skipped. A row whose sort value moved past the
  cursor (an `updated_at` feed) is served again on a later page.
- **A key-only cursor needs its anchor row.** When the cursor carries
  only the key and that row is deleted (or soft-deleted) between pages,
  the next request answers 400 with reason `cursor_anchor_gone`: start
  again from the first page (`?cursor=`). Serving a page from a guessed
  position would skip or repeat rows without saying so. A cursor naming
  a row the caller may not read (another owner's or tenant's, or one a
  read scope or `BeforeList` hook excludes) gets the same answer, so a
  crafted cursor never reveals where such a row sorts. The client's
  own `?q=` and `?where=` do not apply to that lookup: an anchor row
  edited so it no longer matches the search still marks the position,
  the same as with a plain `?status=open` filter. If a key-only
  cursor's anchor changed its sort value, the next page continues after
  the new value, so rows between the old and the new position are
  skipped (moved forward) or served again (moved back). Keep sort
  columns short enough for the cap when a feed must see every row.
- **`?sort=` is ignored in cursor mode.** Keyset pagination requires
  the ORDER BY to match the cursor key. Configure cursor fields
  instead.
- **No total count.** `CursorPage.Total` is reserved but not
  populated. Cursor mode's point is avoiding the table scan a count
  requires. Use offset mode if you need a total.
- **Backward paging starts from `hasMore`'s end.** Backward responses
  use `<` instead of `>` and reverse the ORDER BY direction.
- **Offset pages break ties on the key.** Every offset-paged read (the
  list endpoint, `?stream=true`, `ListAll`, and a typed query with
  `Offset`) orders by the `?sort=` columns and then by each primary-key
  column the sort does not name, ascending. With no `?sort=` the list
  pages oldest first (`created_at`, then the key) when the entity has
  timestamps, and in key order when it has none. Without the key, rows
  tied on the sort column can come back in a different order for each
  page on Postgres, so a walk would serve some rows twice and others
  never.

## When to use which mode

- **Offset**: for admin tables with stable data, "page 5 of 23", or
  any UI that exposes the page number to the user.
- **Cursor**: for infinite-scroll feeds, real-time-ish lists, or
  anything where the source data churns while users page.

## Common mistakes

- **Forgetting to send `cursor=` on the first request.** Without it,
  the endpoint serves offset mode. The empty string is the opt-in
  signal.
- **Caching cursors across schema changes.** A cursor is tied to the
  entity's key columns. Changing the key invalidates every outstanding
  cursor. After a change to which columns `CursorField(s)` names, a
  cursor that carries sort values answers 400 (start again with
  `cursor=`), and a key-only one pages from its anchor under the new
  order. Reordering the same columns keeps a cursor working: it names
  the anchor row's values by column, and the next page starts after
  that row in the new order.
- **Trying to "jump to page N" with cursor mode.** You can't. Cursors
  are sequential by design.
