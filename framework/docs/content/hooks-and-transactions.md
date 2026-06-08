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
| `BeforeList`   | Before SELECT (both data + count queries)          | `*hook.ListPayload`          |
| `AfterList`    | After SELECT, before response                      | `*hook.ListPayload`          |
| `BeforeGet`    | Before single-row SELECT (`/api/<entity>/{id}`)    | `*hook.GetPayload`           |
| `AfterGet`     | After single-row SELECT, before response           | `*hook.GetPayload`           |

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

## List & Get hooks — scoping reads

`BeforeList` and `BeforeGet` let you inject `WHERE` clauses into the
read query. The clauses apply to both the data and (for List) the
count query, so totals match the filtered result. Use this when you
need per-row scoping the standard `OwnerField` knob doesn't cover —
e.g. visibility flags, soft-state filters, or role-based redaction.

```go
import "github.com/DonaldMurillo/gofastr/framework/hook"

app.HookRegistry("posts").RegisterHook(framework.BeforeList,
    func(ctx context.Context, data any) error {
        p := data.(*hook.ListPayload)
        // Hide drafts from non-editors.
        if !isEditor(p.Request) {
            p.AddWhere("status = $1", "published")
        }
        return nil
    })

app.HookRegistry("posts").RegisterHook(framework.BeforeGet,
    func(ctx context.Context, data any) error {
        p := data.(*hook.GetPayload)
        // p.ID is the id from the URL; scope on team membership.
        team := teamOf(p.Request)
        p.AddWhere("team_id = $1", team)
        return nil
    })
```

`AfterList` and `AfterGet` see the fetched rows on the payload and
may mutate them in place — handy for redaction:

```go
app.HookRegistry("users").RegisterHook(framework.AfterList,
    func(ctx context.Context, data any) error {
        p := data.(*hook.ListPayload)
        for _, row := range p.Results {
            delete(row, "password_hash")
        }
        return nil
    })
```

> **`AfterList` and streaming are mutually exclusive.** The streaming
> list path (`?stream=true`) writes rows straight to the wire and never
> materialises the full slice an `AfterList` redactor needs, so running
> the hook there would be impossible — and silently *skipping* it would
> leak the very fields the redactor exists to hide. When an entity has
> any `AfterList` hook registered, an explicit `?stream=true` request is
> refused with **400**. An auto-streamed request (a very large `limit`)
> instead falls back to the buffered path so the hook still runs. Net:
> `AfterList` is never bypassed.

For the common case of per-user row scoping, use
`EntityConfig.OwnerField` instead — it's a single line and covers
all four read/write operations. See
[`framework/docs/content/entity-declarations.md`](entity-declarations.md#per-user-scoping-ownerfield).

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

`App.InTx` also **joins an ambient transaction** already in the
context instead of opening a second, independent one. So calling
`App.InTx` from inside a CRUD hook (or any code already running under a
transaction) reuses that transaction and leaves the commit/rollback to
the outer owner — your `fn` runs but does not commit on its own. This
keeps nested boundaries atomic rather than silently splitting them.

### Composing CRUD operations in one transaction

Auto-CRUD writes are individually transactional, and they also **join an
ambient transaction** when one is in the context. So several CRUD
operations called inside `App.InTx` commit or roll back as a single unit:

```go
ordersCH := app.MustCrudHandler("orders") // in-process handler for a registered entity
linesCH  := app.MustCrudHandler("order_lines")

err := app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
    // Both CreateOne calls run on the SAME transaction — if the second
    // fails, the first is rolled back too.
    if _, err := ordersCH.CreateOne(ctx, order); err != nil { return err }
    if _, err := linesCH.CreateOne(ctx, line); err != nil { return err }
    return nil // commit
})
```

`App.CrudHandler(name)` (and the panicking `MustCrudHandler`) return a
fully-wired in-process handler — the same shape the HTTP routes use.

For a multi-tenant or owner-scoped entity, put the tenant/owner into the
`ctx` first (the in-process methods require it, just like an HTTP request
would carry it): `ctx = tenant.SetTenantID(ctx, "acme")` before the calls.

Pass the `ctx` you receive from `InTx` into the CRUD call — that's what
carries the transaction (via `TxFromContext`). The query builder is
transaction-agnostic, so any hand-written `query.QueryBuilder` SQL you run
on the provided `tx` is part of the same unit. Without an ambient
transaction (the normal HTTP path), each CRUD write opens and commits its
own transaction as before.

## Batch behaviour

In a `_batch` request, every item shares one transaction:

- All `Before*` and `After*` hooks fire per item, in input order.
- The first per-item hook error rolls back the entire batch.
- Lifecycle events emit only on a successful commit — never on
  rollback — in input order.

## Hook-skip matrix

Not every operation fires every hook. The table below captures the
paths that skip hooks entirely — by design, not accident:

| Operation | Hooks skipped | Why |
|---|---|---|
| `TypedQuery.UpdateAll` | all Before/AfterUpdate | Bulk SQL UPDATE; no per-row callback, no tx wrapping per row |
| `TypedQuery.DeleteAll` | all Before/AfterDelete | Bulk SQL DELETE/soft-delete; same reason |
| Upsert (`UpsertOne`) that inserts | BeforeCreate/AfterCreate fire | An upsert-as-insert IS a Create |
| Upsert (`UpsertOne`) that updates | BeforeUpdate/AfterUpdate **do not fire** | Indistinguishable from create at the DB level; use Create/Update if you need update hooks |
| Streaming list (`?stream=true`) | AfterList (blocked when any AfterList hook is registered; auto-falls back to buffered path for large `limit`) | Rows stream directly to wire; slice unavailable for redaction |
| `_batch` requests | none — per-item Before/After* all fire | Each item runs its hooks before the batch tx commits |

Keep this matrix in mind when writing bulk operations or tests that
assert side-effect counts.

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

Available helpers for write operations:

- `OnBeforeCreate[T]`, `OnAfterCreate[T]`
- `OnBeforeUpdate[T]`, `OnAfterUpdate[T]`
- `OnBeforeDelete`, `OnAfterDelete` (ID is a string, no generic needed)

### Typed List and Get hooks

`OnBeforeList` and `OnAfterList` work like their untyped counterparts
but skip the `data.(type)` assertion — the payload is already
`*hook.ListPayload`:

```go
import "github.com/DonaldMurillo/gofastr/framework/hook"

// Scope reads to published posts for non-editors.
framework.OnBeforeList(app, "posts",
    func(ctx context.Context, p *hook.ListPayload) error {
        if !isEditor(p.Request) {
            p.AddWhere("status = $1", "published")
        }
        return nil
    })

// Redact a field after the rows are fetched.
framework.OnAfterList(app, "posts",
    func(ctx context.Context, p *hook.ListPayload) error {
        for _, row := range p.Results {
            delete(row, "internal_notes")
        }
        return nil
    })
```

`OnBeforeGet` and `OnAfterGet` follow the same pattern with
`*hook.GetPayload`:

```go
// Scope a single-row lookup to the caller's team.
framework.OnBeforeGet(app, "posts",
    func(ctx context.Context, p *hook.GetPayload) error {
        team := teamOf(p.Request)
        p.AddWhere("team_id = $1", team)
        return nil
    })

// Redact a field on a single-row response.
framework.OnAfterGet(app, "posts",
    func(ctx context.Context, p *hook.GetPayload) error {
        delete(p.Result, "internal_notes")
        return nil
    })
```

`p.ID` on a `GetPayload` is the id from the URL path. `p.Request` on
both payload types is the live `*http.Request` so you can inspect
headers, cookies, or auth context.

Each typed helper wraps the underlying `HookRegistry` — typed and
untyped hooks can coexist on the same entity in any registration order.

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
