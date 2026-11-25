# Hooks & transactions

Entity lifecycle hooks run inside the same database transaction as the write they observe. Returning an error from a `Before*` hook cancels the operation; returning an error from an `After*` hook rolls back the transaction. That keeps audit logs, denormalizations, and side-effect SQL atomic with the write.

## Hook points

| Constant | Fires | `data` argument |
|---|---|---|
| `BeforeCreate` | Before schema validation and `INSERT` | `map[string]any` (mutable body) |
| `AfterCreate` | After `INSERT`, before tx commit | `map[string]any` (response record) |
| `BeforeUpdate` | Before schema validation and `UPDATE` | `map[string]any` (mutable patch) |
| `AfterUpdate` | After `UPDATE`, before tx commit | `map[string]any` (response record) |
| `BeforeDelete` | Before `DELETE` / soft-delete | `string` (record ID) |
| `AfterDelete` | After `DELETE`, before tx commit | `string` (record ID) |
| `BeforeList` | Before `SELECT` (data + count queries) | `*hook.ListPayload` |
| `AfterList` | After `SELECT`, before response | `*hook.ListPayload` |
| `BeforeGet` | Before single-row `SELECT` | `*hook.GetPayload` |
| `AfterGet` | After single-row `SELECT`, before response | `*hook.GetPayload` |

- Hooks run in registration order per entity (`app.HookRegistry("posts").RegisterHook(...)`).
- A `Before*` error refuses the request (`400` on HTTP; `crud.IsHookRefusal(err)` is true in process). A panic is recovered, logged, rolled back, and returned as `*hook.PanicError` (`500` on HTTP).
- `BeforeCreate`/`BeforeUpdate` run **before** schema validation so you can compute or normalize fields that must pass validation.
- `BeforeUpdate`/`BeforeDelete` only run after the target row passes tenant, owner, soft-delete, and `BeforeGet` scope checks—missing or out-of-scope rows return `404` before any hook runs.
- Any `crud.WithElevation` on the outer context is stripped before hooks execute so hooks never inherit elevated privileges.

## Scoping & redacting reads (`BeforeList`/`BeforeGet`, `AfterList`/`AfterGet`)

Use `BeforeList` and `BeforeGet` to add SQL `WHERE` predicates when `Scope.OwnerField` or `Exposure.ReadScope` isn't enough (e.g., team membership or custom visibility rules). `BeforeList` clauses apply to both the rows query and the `COUNT(*)` query:

```go
import "github.com/DonaldMurillo/gofastr/framework/hook"

framework.OnBeforeList(app, "posts", func(ctx context.Context, p *hook.ListPayload) error {
    if !isEditor(p.Request) {
        p.AddWhere("status = $1", "published")
    }
    return nil
})

framework.OnBeforeGet(app, "posts", func(ctx context.Context, p *hook.GetPayload) error {
    p.AddWhere("team_id = $1", teamOf(p.Request))
    return nil
})
```

- **`BeforeGet` scopes bind writes too:** Any row `GET /posts/{id}` hides with `404` via `BeforeGet` is also refused with `404` on `PUT`, `PATCH`, `DELETE`, state transitions, `_batch` updates/deletes, and MCP write tools (checked inside the write transaction). In-process writes honor `BeforeGet` when called with `crud.WithReadHooks(ctx)` (`WithServerWrites` skips it).
- **`AfterList` / `AfterGet` redaction:** Mutate fields in place using **wire JSON keys** (`camelCase` by default, e.g., `"internalNotes"`, not `"internal_notes"`). Register masks on **both** `AfterGet` and `AfterList` so every surface (List, Get, cursor pages, `?include=`, `_events`, create/update responses) stays consistent, and pair with `NoQuery` so callers can't leak masked values via filters or sorts (or use `Hidden` to exclude columns in SQL).
- **`AfterList` must preserve row count:** Never drop or add rows in `AfterList` (returns `500` on HTTP / error in process)—filter visibility in `BeforeList`. Because streaming lists (`?stream=true`) don't buffer rows, `?stream=true` returns `400` when any `AfterList` hook is registered (auto-streaming falls back to buffered).

## Transactions & `App.InTx`

Inside a hook, get the active `*sql.Tx` from `framework.TxFromContext(ctx)` so your SQL commits or rolls back atomically with the CRUD write:

```go
app.HookRegistry("posts").RegisterHook(framework.AfterCreate,
    func(ctx context.Context, data any) error {
        tx, ok := framework.TxFromContext(ctx)
        if !ok {
            return errors.New("expected tx in context")
        }
        _, err := tx.ExecContext(ctx,
            "INSERT INTO audit_log (entity, record_id) VALUES ($1, $2)",
            "posts", data.(map[string]any)["id"])
        return err
    })
```

To wrap multi-entity writes in one transaction from your own handlers, jobs, or seeders, use `App.InTx`:

```go
ordersCH := app.MustCrudHandler("orders")
linesCH  := app.MustCrudHandler("order_lines")

err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
    if _, err := ordersCH.CreateOne(ctx, order); err != nil { return err }
    if _, err := linesCH.CreateOne(ctx, line); err != nil { return err }
    return nil
})
```

- **Ambient join & savepoints:** CRUD reads (`GetOne`, `ListAll`, `CountAll`, `TypedQuery`) and writes (`CreateOne`, `UpdateOne`, `DeleteOne`, `softdelete` helpers) automatically join the `InTx` transaction. Calling `App.InTx` (or a CRUD write) inside an existing transaction uses a **savepoint**, so a failed inner operation rolls back cleanly without poisoning the outer Postgres transaction.
- **Options:** Pass `framework.TxIsolation(sql.LevelSerializable)` or `framework.TxReadOnly()` on the outermost `App.InTx` (passing options on a nested `InTx` returns `framework.ErrTxOptionsNested`).
- **Events on commit:** Lifecycle events emitted inside `App.InTx` are queued and published only after the outer transaction commits. If you manage a `*sql.Tx` manually instead of `App.InTx`, attach a commit queue with `ctx, q := db.WithTxQueue(ctx, tx)` and call `q.RunAfterCommit()` after `tx.Commit()`.
- **Validation errors in process:** Check `var ve *crud.ValidationError; errors.As(err, &ve)` and read `ve.Fields()`.
- **A nil body:** `CreateOne`, `UpsertOne`, `UpdateOne` and each body passed to `BatchCreateMany` or `BatchUpdateMany` treat a nil map as an empty one. Defaults, required-field checks and hooks still apply.

## Hook-skip & behavior matrix

| Operation | Hooks behavior |
|---|---|
| `_batch` requests | One transaction; per-item `Before*`/`After*` fire in order; any error rolls back the entire batch; events publish after commit. |
| `TypedQuery.UpdateAll` / `DeleteAll` | Skips `Before*`/`After*` hooks and lifecycle events (bulk SQL), but joins the ambient transaction. |
| `UpsertOne` | **Refuses entities with any `BeforeUpdate` hook** (prevents TOCTOU—use `CreateOne` or `UpdateOne`). Without `BeforeUpdate`: `BeforeCreate` prepares the body; inserts run `AfterCreate`, conflict updates run `AfterUpdate`, no-op conflicts run no after-hooks. |
| `?include=` children | Runs the child entity's `BeforeList`/`AfterList` (and `BeforeGet`/`AfterGet` for to-one relations). |
| `_events` SSE deliveries | Evaluates each subscriber's `BeforeList`/`BeforeGet` and redacts with `AfterGet` at delivery time. |
| Create/update HTTP responses | Runs `BeforeGet`/`AfterGet` on the returned body (if `BeforeGet` hides the written row, responds with its ID only). |
| In-process reads (`ListAll`, `CountAll`, `GetOne`, `TypedQuery.Find`/`First`/`Count`) | Skips read hooks by default so background code and read-modify-write see stored values. Pass `crud.WithReadHooks(ctx)` whenever rows reach an end user (generated `entityui` screens opt in automatically). |

## Typed hooks

For generated or custom Go structs, use the typed helpers instead of `map[string]any`:

```go
framework.OnAfterCreate[Post](app, "posts", func(ctx context.Context, p *Post) error {
    log.Printf("created post %q", p.Title)
    return nil
})
```

- Write helpers: `OnBeforeCreate[T]`, `OnAfterCreate[T]`, `OnBeforeUpdate[T]`, `OnAfterUpdate[T]`, `OnBeforeDelete`, `OnAfterDelete`.
- Read helpers: `OnBeforeList`, `OnAfterList`, `OnBeforeGet`, `OnAfterGet`.
- `OnBefore*[T]` mutations write to the DB; `OnAfter*[T]` mutations run inside the transaction before commit (errors roll back) and redact the HTTP response body without altering the stored row. Both `camelCase` and `snake_case` `json` struct tags work.

## Pre-image access (old row before update/delete)

Inside `AfterUpdate` or `AfterDelete`, inspect the row's prior state using the pre-image helpers from `framework/crud`:

- **`crud.AuditPreImageAs[T](ctx) (T, bool)`** (preferred): decodes into your struct with standard `json` tags.
- **`crud.AuditPreImageSnakeFromContext(ctx) map[string]any`**: map keyed by `snake_case` DB column names.
- **`crud.AuditPreImageFromContext(ctx) map[string]any`**: raw map keyed by the handler's `JSONCase` (`camelCase` by default—not `snake_case`).

```go
pre := crud.AuditPreImageSnakeFromContext(ctx)
if pre != nil && pre["status_id"] != data.(map[string]any)["statusId"] {
    return notifyStatusChange(ctx, pre["status_id"])
}
```

## Common mistakes

- **Indexing `crud.AuditPreImageFromContext(ctx)` with `snake_case` keys.** It uses the handler's `JSONCase` (`camelCase` by default). Use `AuditPreImageAs[T]` or `AuditPreImageSnakeFromContext`.
- **Using `app.DB.ExecContext` inside a hook.** That bypasses the write transaction—use `framework.TxFromContext(ctx)`.
- **Expecting an `AfterDelete` error to keep the delete.** `After*` errors roll back the transaction; run non-atomic side effects via `EventStream` subscribers after commit.
- **Doing slow network I/O inside a hook.** Hooks hold the DB transaction open—enqueue slow work onto a background queue.
- **Expecting `AfterCreate`/`AfterUpdate` mutations to change stored columns.** Those mutations redact the response body only; modify stored values in `BeforeCreate`/`BeforeUpdate`.
- **Using `AfterList` to filter out rows.** `AfterList` must keep the row count unchanged; put visibility filters in `BeforeList`.
