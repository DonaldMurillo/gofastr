# 026 — Pagination

**Phase:** 3 (Framework) | **Depends on:** 019, 006

## Goal
Cursor-based pagination by default, offset fallback. Auto on list endpoints.

## Deliverables
- [ ] Cursor struct: `{Value, Field, Direction}`
- [ ] Cursor encoding: base64-encoded JSON string (opaque to client)
- [ ] Cursor pagination: `WHERE field > cursorValue ORDER BY field LIMIT n`
- [ ] Offset pagination: `OFFSET page * limit LIMIT limit` (fallback)
- [ ] Auto-pagination on list: parse `?cursor=` or `?page=` from query params
- [ ] Response metadata: `{items, next_cursor, has_more, total_count}`
- [ ] Total count: opt-in per entity (can be expensive)
- [ ] Default page size from entity config, override via `?limit=` (capped at max)

## Acceptance Criteria
- Cursor pagination: no skipped/duped rows when data changes between pages
- Offset pagination: correct page calculation
- Next cursor is null when no more results
- Limit capped at entity max (e.g., max 100)
- `?cursor=` takes precedence over `?page=`
