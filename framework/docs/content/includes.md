# Includes (eager loading)

Pass `?include=` on any CRUD `GET /<table>` or `GET /<table>/{id}` request to embed related records in the same response without an N+1 round trip.

```
GET /api/posts?include=author,comments
GET /api/posts/42?include=author,comments.author
```

Supported relation types (declared in `EntityConfig.Relations`):
- `belongs_to` and `has_one` — attached as a nested JSON object (or `null`)
- `has_many` and `many_to_many` — attached as a JSON array (empty `[]` when no rows match)

Nested paths (`comments.author`) use dot notation up to the configured depth limit.

## Security and scoping

Every included relation runs through the target entity's full read pipeline:
- **Exposure check:** including a `CRUD: false` entity returns `403`.
- **Access control:** the caller must satisfy the target entity's `Exposure.Access.Read` permission and any per-row `Decider`.
- **Row isolation:** `Scope.OwnerField`, `Scope.MultiTenant`, `Scope.SoftDelete`, `ReadScope`, and `AfterGet` hooks all apply to included rows. Rows the caller may not read are filtered out (for `has_many`/`many_to_many`) or returned as `null` (for `belongs_to`/`has_one`).
- **Hidden fields:** `Hidden` columns on included entities are stripped just like on top-level reads.

## Caps and limits

Includes are bounded to keep queries predictable:
- **Depth cap:** nested chains deeper than `4` levels return `400`.
- **Path & row caps:** a single request is capped at `32` include paths and a `20,000`-row total include budget so one list request cannot blow up memory.
- **Unknown relations:** requesting a name not declared in `EntityConfig.Relations` returns `400`.
- **Batching:** GoFastr collects parent keys per level and fetches each included entity in one batched `IN (...)` query (composite keys use tuple comparisons).

## Eager loading from Go

In-process Go code can run the batched loader directly with `framework.EagerLoad`:

```go
related, err := framework.EagerLoad(ctx, db, postsEnt, postsEnt.Config.Relations, postIDs, app.Registry)
```

> **Always pass `app.Registry`:** calling `EagerLoad` without a registry returns unscoped raw rows, including `Hidden` columns and rows outside the target entity's owner/tenant/soft-delete scopes.

See [Entity declarations](entity-declarations.md#relations-and-composite-keys) for declaring relations and composite foreign keys, and [Query DSL](query-dsl.md) for filtering across relations (`?author.role=editor`).

## Common mistakes

- **Including a `CRUD: false` entity over HTTP:** returns `403` by design so hidden entities cannot be read sideways through a parent route.
- **Using the foreign key column name instead of the relation name:** `?include=author_id` fails with `400`; pass the relation's `Name` (`?include=author`).
- **Expecting unbounded `has_many` lists inside `?include=`:** large child collections should be fetched via the child entity's own paginated list route (`GET /api/comments?post_id=42`).
- **Forgetting that target scopes apply to includes:** if a parent row points at a soft-deleted or another user's child record, the included object comes back `null` (or absent from the slice).
