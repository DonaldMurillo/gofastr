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
- Every batch response carries `Cache-Control: no-store`, as the
  single-row writes do, and audit rows a batch writes carry the
  request's client IP and user agent.
- The first per-item failure populates `error` on that index, and
  `reason` when the refusal has a code (a hook's own wording has none):
  the same message and code the single-row write answers for that
  item. A validation, state, transition, unique or foreign-key
  refusal says what was wrong, with `fields` and `field_reasons` where
  the single write names fields. Any other error reads
  `internal server error`, and the cause stays in the server log.
  Later indices are marked `"skipped": true`.
- On `committed: false` no result carries `data`. Errors and
  `fields` remain.

## Limits

- `MaxBatchSize = 100` items per request. Go over that and you get
  `400 Bad Request` before any item runs.
- `items` (or `ids`) can't be empty.
- A batch update item names its record by every key column, under the
  column's wire name. An item that omits one answers `missing <key>`
  with `reason: "missing_key"`. Two keys in one item that name the same
  column (`bodyText` and `body_text` on a camelCase handler) refuse the
  batch with 400 before it runs, naming both spellings. For a composite key, a batch delete's `ids` are
  `entity.JoinKey` strings (`"s1/c1"`).

## Behaviour & guarantees

- **Atomic.** One transaction; one commit or one rollback.
- **Hooks run inside the transaction.** `BeforeCreate`, `AfterUpdate`,
  etc. fire per item. A hook error rolls back the whole batch.
- **Permission is checked per item.** With an `Exposure.Access` block, the
  route checks the op's permission once up front, then `PATCH` and `DELETE`
  ask again for every item with `Ref{Type: <entity>, ID: <item id>}`, so a
  resource-aware Decider that denies one record refuses it here as it does
  on `PATCH /<entity>/{id}`. A refused item reports `"error": "access denied"`
  and the batch rolls back.
- **Events fire only on commit.** `entity.created` etc. fire after a
  successful commit, in input order, one per item. They never fire on
  rollback.
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
  depends on transaction commit order, so don't count on it.
- **Sending more than 100 items.** Split it client-side.
- **Mixing batch and per-item requests in a saga.** Mixing makes
  rollback semantics ambiguous. Pick one.
