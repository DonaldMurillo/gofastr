# Admin UI

`battery/admin` is an admin back-office battery with two halves:

- **Entity CRUD** — generated list / create / edit / delete screens for
  your entities, rendered **through your app's UI host** so they hydrate
  with `runtime.js`: the list is a server-driven `DataTable` island
  (paginate without a reload), delete is a `data-fui-confirm` button, and
  forms are server-rendered. **No bespoke JavaScript.**
- **Ops dashboards** — read-only **Queue** and **Audit log** pages on top
  of data the framework already collects (`battery/queue`,
  `framework.WithAuditLog`). These are self-contained HTML and don't need a
  UI host.

Every screen is gated: see [Authorization](#authorization).

## Quick start

```go
import (
    "github.com/DonaldMurillo/gofastr/battery/admin"
    appui "github.com/DonaldMurillo/gofastr/core-ui/app"
    "github.com/DonaldMurillo/gofastr/framework"
    "github.com/DonaldMurillo/gofastr/framework/uihost"
)

site := appui.NewApp("My App")
host := uihost.New(site)

app := framework.NewUIHostApp(host, framework.WithDB(db))
app.Use(auth.SessionMiddleware(mgr)) // puts the signed-in user on the request

app.Entity("products", productsConfig)
app.Entity("customers", customersConfig)

app.RegisterBattery(admin.New(admin.Config{Title: "Back office"}))
```

With an empty `Entities`, the battery **auto-exposes every registered entity
whose CRUD is enabled** — the "generate the whole back-office" default.
Entities shipped with `CRUD=false` (e.g. `battery/auth`'s `users` /
`sessions`) are skipped automatically, so the default never exposes
credential tables. Name entities explicitly to override:

```go
admin.New(admin.Config{Entities: []string{"products", "orders"}})
```

The entity screens mount at `<PathPrefix>/e/<table>`:

| Route                              | Screen                              |
|------------------------------------|-------------------------------------|
| `GET  /admin/e/<table>`            | List (DataTable island)             |
| `GET  /admin/e/<table>/new`        | Create form                         |
| `GET  /admin/e/<table>/edit/:id`   | Edit form                           |
| `POST /admin/e/<table>/_create`    | Create (→ 303 to list)              |
| `POST /admin/e/<table>/_update/{id}` | Update (→ 303 to list)            |
| `DELETE /admin/e/<table>/_delete/{id}` | Delete RPC (returns refreshed table) |
| `GET  /admin/e/<table>/_rows`      | DataTable island fragment           |

> **A UI host is required for the entity screens.** The battery discovers
> the host the app mounted (via `framework.App.Mountables()`) and registers
> the screens on it. If you list `Entities` but no host is mounted,
> `RegisterBattery` returns an error. (In auto mode with no host, the entity
> screens are simply skipped and you still get the ops dashboards.)

### How the interactions work (no JavaScript)

Everything is a declarative `data-fui-*` primitive the runtime already
understands — the battery ships zero JS:

- **List** uses `ui.DataTable` with `IslandSignal`/`IslandEndpoint`. Page
  links fire a `GET` RPC to `_rows`, which returns the new table fragment;
  the runtime swaps it in place and pushes the new URL.
- **Delete** is a `<button data-fui-confirm="…" data-fui-rpc="…/_delete/{id}"
  data-fui-rpc-method="DELETE" data-fui-rpc-signal="…">`. The runtime runs
  the native confirm, fires the DELETE, and swaps the returned (refreshed)
  table into the list signal. (It does **not** navigate to the list path —
  that would hit the SPA cache and show a stale row.)
- **Forms** are plain SSR `ui.Form`s (CSRF auto-stamped from context). On
  success the handler 303-redirects to the list; on a validation error it
  redirects back to the form with a one-shot flash token (`?e=…`) so the
  re-render is a full host page with field errors + the submitted values
  retained.

Because every write goes through the entity's **own `CrudHandler`** with the
request context forwarded, validation, `OwnerField`/tenant scoping, hooks,
and events all apply exactly as on the JSON API — the admin never
re-implements CRUD, pagination, or filter logic.

## Ops dashboards (queue + audit)

```go
q, _ := queue.NewDBQueue(db)
app.RegisterBattery(admin.New(admin.Config{
    Queue: q,   // enables /admin/queue
    DB:    db,  // enables /admin/audit
}))
```

| Route              | Purpose                                       |
|--------------------|-----------------------------------------------|
| `GET /admin`       | Overview with summary cards                   |
| `GET /admin/queue` | Jobs list with `?status=` filter chips        |
| `GET /admin/audit` | Audit log entries newest-first                |

When neither `Queue` nor `DB` is wired, the sub-pages render a "not wired"
stub instead of 404'ing. Tune list caps via `QueueListLimit` /
`AuditListLimit` (defaults 200, max 1000). The audit page shows
`created_at`, `entity`, `op`, `record_id`, `actor_id`; the default table
name is `audit_log` (`Config.AuditTable` to override).

## Authorization

Every admin surface is gated. By default the battery requires **any
authenticated, non-nil user** in the request context — set by your auth
middleware (`battery/auth`'s `SessionMiddleware` does this). Anonymous
callers get `401` on both the SSR screens (via the host policy chain) and
the RPC/form routes (via middleware).

Supply a stricter predicate — e.g. an admin-role check — via
`Config.Authorize`:

```go
admin.New(admin.Config{
    Authorize: func(ctx context.Context) bool {
        u := auth.GetCurrentUser(ctx)
        return u != nil && u.HasRole("admin")
    },
})
```

> The default checks the user is **non-nil**, not merely present:
> `SessionMiddleware` seeds a nil user on every request so `GetCurrentUser`
> works, so a naive "is a user set?" check would let anonymous callers
> through. The battery handles this for you.

## CSRF

Forms embed the framework's `_csrf` hidden field automatically (`ui.Form`
reads the token from context). The delete RPC carries the token via the
`X-CSRF-Token` header, which the runtime reads from
`<meta name="csrf-token">` — make sure your layout emits that tag when CSRF
is enforced.

## Common mistakes

- **Don't expose `/admin` to the public.** It surfaces entity data, actor
  ids, and job counts. The default gate requires auth; don't disable it.
- **Per-user data needs `OwnerField`.** The admin honours it (a user only
  sees/edits their own rows), but only if the entity declares it. See
  [Entity Declarations](entity-declarations.md) → per-user scoping.
- **The ops dashboards are read-only on purpose.** Retry / dequeue /
  dead-letter workflows live in your app code.

## See also

A runnable example lives in `examples/backoffice` — SQLite, two entities,
a demo login, and `admin.New(admin.Config{})` generating the whole
back-office.
