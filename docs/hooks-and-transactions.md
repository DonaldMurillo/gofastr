# Hooks & transactions

Lifecycle hooks run inside the same transaction as the entity write
they observe. A hook that errors rolls back the parent write. This
is how the framework keeps audit logs, denormalisations, and side-
effect SQL atomic with the change that triggered them.

## Hook points

| Constant       | Fires                                              | `data` argument             |
|----------------|----------------------------------------------------|------------------------------|
| `BeforeCreate` | After validation, before INSERT                    | `map[string]any` (body)     |
| `AfterCreate`  | After INSERT, before tx commit                     | `map[string]any` (record)    |
| `BeforeUpdate` | After validation, before UPDATE                    | `map[string]any` (patch)    |
| `AfterUpdate`  | After UPDATE, before tx commit                     | `map[string]any` (record)    |
| `BeforeDelete` | Before DELETE / soft-delete                        | `string` (record id)        |
| `AfterDelete`  | After DELETE, before tx commit                     | `string` (record id)        |
| `BeforeList`   | Before SELECT                                      | `map[string]any` (filters)   |
| `AfterList`    | After SELECT, before response                      | `[]map[string]any` (rows)    |

Hooks run in registration order. The first error stops execution and
returns to the caller. For `Before*` hooks the error cancels the
operation. For `After*` hooks the error rolls back the transaction.

## Registering hooks

```go
app.HookRegistry("posts").RegisterHook(framework.AfterCreate,
    func(ctx context.Context, data any) error {
        record := data.(map[string]any)
        return enqueueIndexing(ctx, record)
    })
```

`HookRegistry(entityName)` lazily creates a registry for that entity.
Each entity has its own registry — hooks do not cross entities.

## Transactions

Hooks see the active transaction via `framework.TxFromContext`:

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

The `*sql.Tx` returned by `TxFromContext` is the same transaction the
CRUD handler will commit. Any work performed through it is committed
or rolled back atomically with the parent operation.

## Running your own code in a transaction

`App.InTx` opens a transaction for arbitrary code paths — seeders,
batch jobs, multi-entity writes — and puts the `*sql.Tx` into the
context so any nested hook participates:

```go
err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
    if _, err := postsRepo.Create(ctx, p); err != nil { return err }
    if _, err := tagsRepo.Attach(ctx, p.ID, tags); err != nil { return err }
    return nil
})
```

A non-nil error from `fn` rolls back. A panic from `fn` rolls back
via the `Recovery` middleware higher up the stack.

## Batch behaviour

In a `_batch` request, every item shares one transaction:

- All `Before*` and `After*` hooks fire per item, in input order.
- The first per-item hook error rolls back the entire batch.
- Lifecycle events emit only on a successful commit — never on
  rollback — in input order.

## Typed hooks

For entities generated with `gofastr generate`, typed hook helpers
hand you the concrete struct instead of `map[string]any`:

```go
framework.OnAfterCreate[Post](app, "posts",
    func(ctx context.Context, p *Post) error {
        log.Printf("created post %q", p.Title)
        return nil
    })
```

Available helpers:

- `OnBeforeCreate[T]`, `OnAfterCreate[T]`
- `OnBeforeUpdate[T]`, `OnAfterUpdate[T]`
- `OnBeforeDelete`, `OnAfterDelete` (ID is a string, no generic)

Each takes the `App`, the entity name, and a typed callback. The
helpers wrap the underlying `HookRegistry`; typed and untyped hooks
can coexist on the same entity.

## Common mistakes

- **Calling `app.DB.ExecContext` from inside a hook.** That bypasses
  the transaction. Use `TxFromContext` to get the active tx.
- **Returning an error from an `AfterDelete` hook expecting the
  delete to "stand".** It won't — `After*` errors roll back. If you
  want a side effect that survives hook failure, do it after commit
  (e.g. subscribe to the event bus from `EventStream`).
- **Long-running work inside a hook.** Hooks hold the transaction
  open. Push slow side effects onto a queue and ack quickly.
- **Mutating `data` in an `AfterCreate` hook expecting the response
  to change.** The HTTP response is serialised from the post-write
  record; modifying the map in the hook does flow back to the
  response, but this is undocumented and may change — prefer
  computing the value before write or via a `BeforeCreate` hook.
