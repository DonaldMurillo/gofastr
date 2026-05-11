# Audit log

`WithAuditLog` writes an audit row for every Create/Update/Delete on
the registered entities. The row is written **inside the same
transaction** as the change it describes, so a rollback drops the
audit alongside the change — never partial.

## Quickstart

```go
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", framework.EntityConfig{ … })
app.Entity("users", framework.EntityConfig{ … })

// Register entities first, THEN enable audit:
app.WithAuditLog(framework.AuditConfig{
    Table: "audit_log",                // default; can be omitted
    Actor: func(ctx context.Context) string {
        return currentUserID(ctx)
    },
    Entities: []string{"posts", "users"}, // empty = audit everything
})
```

Order matters: `WithAuditLog` walks the registry and attaches hooks,
so it must be called *after* `Entity` registrations. Otherwise the
audit hooks bind to no entities.

## Schema

```sql
CREATE TABLE audit_log (
    id          TEXT       PRIMARY KEY,
    entity      TEXT       NOT NULL,
    op          TEXT       NOT NULL,   -- 'create' | 'update' | 'delete'
    record_id   TEXT       NOT NULL,
    actor_id    TEXT,                  -- nullable
    created_at  TIMESTAMPTZ NOT NULL,  -- DATETIME on SQLite
    diff        TEXT                   -- JSON
);
```

`EnsureAuditTable(db, table)` creates this table; `WithAuditLog` calls
it for you and panics on failure (this is initialisation-time work —
loud failure is preferable to silent log loss).

## Configuration

| Field      | Effect                                                                  |
|------------|--------------------------------------------------------------------------|
| `Table`    | Destination table. Defaults to `audit_log`.                              |
| `Actor`    | Resolves the actor ID (typically user ID) from `context.Context`. Empty string = system write. |
| `Entities` | Allowlist of entity names to audit. Empty = every registered entity.    |

## Row shape

For `create` / `update`, `diff` is the post-write record as JSON:

```json
{"new": {"id": "p1", "title": "First", "status": "published"}}
```

For `delete`, `diff` is `NULL` and `record_id` holds the deleted ID.

There is no automatic old/new diff — `diff.new` is the full record
after the write, not a delta. If you need the old value, query the
audit log for the previous row.

## Transactional behaviour

Audit hooks resolve the active CRUD transaction via
`TxFromContext(ctx)`. The row is inserted through the same
transaction, so:

- A failed `After*` hook later in the chain rolls back the audit row
  with the parent write.
- Batch operations write all audit rows in the same transaction; a
  per-item failure rolls back every audit row in the batch.
- Audit writes outside a transaction (rare — async hooks) fall back
  to the plain connection pool.

## What gets audited

- `AfterCreate` → `op = 'create'`, `diff = {"new": <record>}`
- `AfterUpdate` → `op = 'update'`, `diff = {"new": <record>}`
- `AfterDelete` → `op = 'delete'`, `diff = NULL`

`Before*` hooks are not audited; the audit only records committed
changes (modulo transactional behaviour above).

## Querying the audit log

There are no built-in HTTP endpoints for audit_log. Either:

- Declare `audit_log` as a read-only entity (`CRUD: false` for write,
  manually register a custom endpoint for reads).
- Query directly from your own admin handler.

```sql
SELECT entity, op, record_id, actor_id, created_at
FROM audit_log
WHERE entity = 'posts'
ORDER BY created_at DESC
LIMIT 50;
```

## Common mistakes

- **Calling `WithAuditLog` before `Entity`.** Hooks register against
  whatever is in the registry at call time. Audit nothing if you call
  it first.
- **Filtering `Entities` by table name.** It expects the entity
  *name* (the key passed to `app.Entity`), not the SQL table name.
- **Expecting `diff` to contain a before/after pair.** It contains
  only `new`. Compute deltas client-side if needed.
- **Relying on the audit row for crash recovery.** Same transaction:
  if the parent write commits, the audit committed. If it didn't, no
  audit either.
