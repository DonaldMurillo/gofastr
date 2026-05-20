# Admin UI

`battery/admin` is a small read-only admin battery — stock screens on
top of the data the framework already collects:

- **Queue** — jobs by status, attempt counts, schedule times. Wires
  against a `queue.Browsable` (the bundled `DBQueue` implements it).
- **Audit log** — recent entries from the audit table the framework
  populates when `App.WithAuditLog` is set.
- **Overview** — per-section summary cards at `/admin`.

Pages are self-contained server-rendered HTML — they don't pull in
`framework/uihost` or the runtime, so the admin endpoints work even
in apps that don't otherwise have a UI. The look is intentionally
plain (system fonts, neutral palette, dark-mode follow) so you can
gate it behind your auth middleware and not have to retheme to ship.

## Wiring

```go
import (
    "github.com/DonaldMurillo/gofastr/battery/admin"
    "github.com/DonaldMurillo/gofastr/battery/queue"
)

q, err := queue.NewDBQueue(db)
if err != nil { /* ... */ }

app := framework.NewApp(framework.WithDB(db)).
    WithAuditLog(framework.AuditConfig{}).
    Entity(...).
    RegisterBattery(admin.New(admin.Config{
        Queue: q,
        DB:    db, // for /admin/audit
    }))
```

The battery mounts:

| Route             | Purpose                                       |
|-------------------|-----------------------------------------------|
| `GET /admin`      | Overview with summary cards                   |
| `GET /admin/queue`| Jobs list with `?status=` filter chips        |
| `GET /admin/audit`| Audit log entries newest-first                |

Override the path prefix via `Config.PathPrefix`. Tune list caps via
`QueueListLimit` and `AuditListLimit` (defaults 200, max 1000).

When neither `Queue` nor `DB` is wired, the sub-pages render a
"not wired" stub instead of 404'ing — clearer signal for "I haven't
set this up yet" than a missing route.

## Authentication

`battery/admin` is intentionally not opinionated about auth. Wire any
middleware you like in front of `/admin*` — typical patterns:

```go
admin := admin.New(admin.Config{Queue: q, DB: db})
app.RegisterBattery(admin)

// Gate every /admin* route behind an auth check:
app.Router.Group("/admin", func(r *router.Router) {
    r.Use(requireAdminMiddleware)
})
```

Or mount the admin battery on a separate router that's only reachable
from your internal network.

## Listing jobs without battery/admin

If you want the data without the bundled UI, type-assert your queue
to `queue.Browsable`:

```go
b, ok := q.(queue.Browsable)
if !ok { return errors.New("queue doesn't support browsing") }
jobs, _ := b.ListJobs(ctx, "failed", 50)
stats, _ := b.Stats(ctx)
```

`DBQueue` implements `Browsable`; memory and Redis queues currently
don't (they will when the admin story moves out of "stock screen" and
into a richer dashboard).

## Audit log

The `Audit` page renders rows from the `audit_log` table the
framework populates via `WithAuditLog`. The default table name is
`audit_log`; override via `Config.AuditTable` if you renamed it.

Columns shown: `created_at`, `entity`, `op`, `record_id`, `actor_id`.
The `diff` column is captured but not rendered in the table because
it's typically large JSON; future versions will provide an expandable
row.

## Common mistakes

- **Don't expose `/admin` to the public.** The pages contain entity
  identifiers, actor ids, and job payload counts that are useful for
  reconnaissance. Always gate.
- **Don't rely on `/admin` for write operations.** The bundled
  battery is read-only on purpose. Retry / dequeue / dead-letter
  workflows live in your app code.
- **Don't bundle the admin battery into a public-facing binary.**
  The CSS and route paths don't change between dev and prod; an
  internal-only binary (or a feature flag on registration) keeps the
  surface area tight.
