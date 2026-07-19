# Batch endpoints

Any entity with `CRUD` enabled gets three transactional batch
endpoints under the `_batch` suffix. All items in one request run in
a single database transaction: the first per-item failure rolls back
the whole transaction.

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
- Earlier successes still show their `data` so you can see what
  would have happened, but `committed: false` means none of it was
  saved.

## Limits

- `MaxBatchSize = 100` items per request. Go over that and you get
  `400 Bad Request` before any item runs.
- `items` (or `ids`) can't be empty.

## Behaviour & guarantees

- **Atomic.** One transaction; one commit or one rollback.
- **Hooks run inside the transaction.** `BeforeCreate`, `AfterUpdate`,
  etc. fire per item. A hook error rolls back the whole batch.
- **Events fire only on commit.** `entity.created` etc. fire after a
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

- **Treating a non-committed response as a 200.** It's a 400. Check
  `committed` before you trust `results[*].data`.
- **Counting on event ordering across batches.** Within a batch,
  events fire in input order. Across overlapping batches, order
  depends on transaction commit order — don't count on it.
- **Sending more than 100 items.** Split it client-side.
- **Mixing batch and per-item requests in a saga.** Mixing makes
  rollback semantics ambiguous. Pick one.
