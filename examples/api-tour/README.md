# api-tour

Live demo of the v2 API surface added to the framework. One file. One DB.
A graph of users → profiles, users → posts → comments.

## Run

```bash
go run ./examples/api-tour
# listening on :8080
```

The first run creates `./api-tour.db` (SQLite) and seeds two users + two
posts + two comments + one profile. Re-runs are idempotent.

## Things to try

### Eager loading (flat + nested)

```bash
# Single relation
curl 'http://localhost:8080/posts/post-1?include=comments'

# Nested — author and the author's profile in one response
curl 'http://localhost:8080/posts/post-1?include=author.profile,comments'

# On list — every row gets the same expansion, batched into one query per
# relation regardless of result size (no N+1).
curl 'http://localhost:8080/posts?include=author.profile'
```

Unknown segments return `400` with the bad name in the error body, e.g.
`?include=author.bogus` → `unknown include "author.bogus"`.

### Cursor pagination

```bash
# First page (cursor= is the sentinel that switches to keyset mode)
curl 'http://localhost:8080/posts?cursor=&limit=2'
#  → {"data":[…],"cursor":"<opaque>","hasMore":true}

# Walk the next page using the returned cursor
curl 'http://localhost:8080/posts?cursor=<opaque>&limit=2'
```

This entity declares `CursorField: "created_at"` so pages walk by recency,
not primary key.

### Atomic batch endpoints

```bash
# Create three posts atomically — if any fails, all roll back
curl -X POST http://localhost:8080/posts/_batch \
  -H 'Content-Type: application/json' \
  -d '{"items":[
        {"title":"A","author_id":"u1"},
        {"title":"B","author_id":"u1"},
        {"title":"C","author_id":"u1"}
      ]}'

# Same shape for PATCH (each item must include "id") and DELETE (body is
# {"ids":[…]}).
```

Response is always `{committed: bool, results: [{index, data|error|skipped}]}`.
Status is `200` on commit, `400` on rollback.

### SSE event stream

```bash
curl -N http://localhost:8080/posts/_events &
# In another terminal:
curl -X POST http://localhost:8080/posts \
  -H 'Content-Type: application/json' \
  -d '{"title":"Live","author_id":"u1"}'
# The first curl prints:
# event: entity.created
# data: {"type":"entity.created","data":{...},"timestamp":"…"}
```

Disconnecting the listener cleans up the EventBus subscription
automatically.

### Multipart uploads

```bash
# `users.avatar` is an Image field; multipart parts named `avatar` get
# streamed through the configured Storage and persisted as a URL string
# on the user row.
curl -X POST http://localhost:8080/users \
  -F 'name=Carol' \
  -F 'avatar=@/path/to/photo.png'
```

Files land under `./api-tour-uploads/`. Set `WithFileStorage` to swap
this for any `upload.Storage` impl (S3 etc).

### Foreign-key constraints

`SQLite` enforces FKs only with `PRAGMA foreign_keys = ON`, which the
example's `main.go` sets at boot. Try inserting an orphan post:

```bash
curl -X POST http://localhost:8080/posts \
  -H 'Content-Type: application/json' \
  -d '{"title":"orphan","author_id":"ghost-user"}'
# → 400 {"error":"insert: ... FOREIGN KEY constraint failed"}
```

### OpenAPI

```
http://localhost:8080/openapi.json
http://localhost:8080/docs/
```

The spec describes every new endpoint (cursor params, _batch bodies,
_events stream, oneOf list response).
