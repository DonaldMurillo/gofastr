# Batch endpoints

Every entity with `CRUD` enabled exposes three transactional batch
endpoints under the `_batch` suffix. All items in a single request
share one database transaction: the first per-item failure rolls back
the entire transaction.

## Routes

| Method   | Path             | Body                              |
|----------|------------------|-----------------------------------|
| `POST`   | `/{table}/_batch`| `{"items":[ {…}, {…}, … ]}`       |
| `PATCH`  | `/{table}/_batch`| `{"items":[ {"id":"x", …}, … ]}`  |
| `DELETE` | `/{table}/_batch`| `{"ids":["x","y", …]}`            |

## Response envelope

```json
{
  "committed": true,
  "results": [
    { "index": 0, "data": { "id": "p1", "title": "A" } },
    { "index": 1, "data": { "id": "p2", "title": "B" } }
  ]
}
```

- `committed: true` → HTTP 200, the transaction was applied.
- `committed: false` → HTTP 400, the transaction was rolled back.
- `results` is always in input order; every input index appears.
- The first per-item failure populates `error` (and optionally
  `fields` for validation failures) on that index. Later indices are
  marked `"skipped": true`.
- Earlier successes still have their `data` recorded for diagnostic
  purposes, but `committed: false` means nothing was persisted.

## Limits

- `MaxBatchSize = 100` items per request. Exceeding it returns
  `400 Bad Request` before any item runs.
- `items` (or `ids`) cannot be empty.

## Behaviour & guarantees

- **Atomic.** One transaction; one commit or one rollback.
- **Hooks run inside the transaction.** `BeforeCreate`, `AfterUpdate`,
  etc. fire per item. A hook error rolls back the whole batch.
- **Events fire only on commit.** `entity.created` etc. emit after a
  successful commit, in input order, one per item — never on rollback.
- **No partial success.** If you need "skip failures and keep the
  rest", make `MaxBatchSize` individual calls instead.

## Examples

```bash
curl -X POST http://localhost:8080/posts/_batch \
  -H 'Content-Type: application/json' \
  -d '{"items":[
        {"title":"First"},
        {"title":"Second"},
        {"title":"Third"}
      ]}'

curl -X PATCH http://localhost:8080/posts/_batch \
  -H 'Content-Type: application/json' \
  -d '{"items":[
        {"id":"p1","status":"published"},
        {"id":"p2","status":"archived"}
      ]}'

curl -X DELETE http://localhost:8080/posts/_batch \
  -H 'Content-Type: application/json' \
  -d '{"ids":["p1","p2","p3"]}'
```

## Common mistakes

- **Treating a non-committed response as a 200.** It returns 400.
  Inspect `committed` before trusting `results[*].data`.
- **Counting on event ordering across batches.** Within a batch,
  events fire in input order. Across overlapping batches, ordering is
  determined by transaction commit order — not predictable.
- **Sending more than 100 items.** Split client-side.
- **Mixing batch and per-item requests in a saga.** Mixing makes
  rollback semantics ambiguous. Pick one.
