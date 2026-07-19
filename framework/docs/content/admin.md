# Admin UI

`battery/admin` is an admin back-office battery with two halves:

- **Entity CRUD** — generated list / create / edit / delete screens for
  your entities, rendered **through your app's UI host** so they hydrate
  with `runtime.js`: the list is a server-driven `DataTable` island
  (paginate without a reload), delete is a `data-fui-confirm` button, and
  forms are server-rendered. **No bespoke JavaScript.** The island
  mechanics behind these (`data-fui-rpc`, signals, fragment swaps) are
  catalogued in [interactive-patterns](interactive-patterns.md).
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

app.RegisterBattery(admin.New(admin.Config{Title: "Back office", AllEntities: true}))
```

Exposure is **opt-in**. An empty `Entities` exposes nothing — a
zero-value config must not silently turn every table into an editable
back-office. Either name the entities:

```go
admin.New(admin.Config{Entities: []string{"products", "orders"}})
```

or set `AllEntities: true` for the whole back-office: every registered
entity whose CRUD is enabled. Entities shipped with `CRUD=false`
(e.g. `battery/auth`'s `users` / `sessions`) are skipped automatically,
so `AllEntities` never exposes credential tables.

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
> `RegisterBattery` returns an error. (With `AllEntities` and no host, the
> entity screens are simply skipped and you still get the ops dashboards.)

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

That includes `WithAuditLog`: configure it after registering entities, and
create/update/delete actions from the admin write the same transactional audit
rows as the JSON CRUD routes. The admin obtains its handler from the app; a
separately constructed `crud.NewCrudHandler` would not carry the app's hook
registry.

The proxy preserves the app handler's configured `JSONCase`; it converts
response rows back to entity field names only at the admin rendering boundary.
Custom hooks and `AuditConfig.Redact` therefore receive the same key casing for
admin and JSON API writes. The proxy also forwards the parent request's
`RemoteAddr` and headers, so audit metadata records the real client IP and
`User-Agent` rather than an in-process test-request default.

## Ops dashboards (queue + audit)

```go
q, _ := queue.NewDBQueue(db)
app.RegisterBattery(admin.New(admin.Config{
    Queue: q,   // enables /admin/queue
    DB:    db,  // enables /admin/audit
}))
```

| Route                          | Purpose                                            |
|--------------------------------|----------------------------------------------------|
| `GET /admin`                   | Overview with summary cards                        |
| `GET /admin/queue`             | Jobs list with `?status=` filter chips             |
| `POST /admin/queue/_replay/{id}` | Re-queue a failed job (gated; failed view only)  |
| `GET /admin/audit`             | Audit log entries newest-first                     |

On the `?status=failed` view, each row gets a **Replay** button when the
wired queue supports it (`DBQueue` does; in-memory / Redis don't yet). The
replay route mutates state, so it runs behind the same admin gate as every
other route and carries a CSRF token — there is no unauthenticated way to
re-fire jobs.

When `Queue` is nil, the overview section and Queue navigation item are hidden;
the direct route retains a "not wired" diagnostic. The audit page uses
`Config.DB` when supplied and otherwise the app's DB; without either, it shows
its own "not wired" diagnostic. Tune list caps via `QueueListLimit` /
`AuditListLimit` (defaults 200, max 1000). The audit page shows
`created_at`, `entity`, `op`, `record_id`, `actor_id`; the default table
name is `audit_log` (`Config.AuditTable` to override).


## RBAC management (roles + user roles)

When `Config.Policy` + `Config.GrantStore` are wired, the admin exposes a
**role→permission matrix** at `<PathPrefix>/rbac/roles`. When
`Config.Auth` is wired, it exposes a **user→role assignment** screen at
`<PathPrefix>/rbac/users`. Both are behind the same admin default-deny gate
as every other route.

```go
policy := framework.NewRolePolicy()
store := framework.NewGrantStore(db, policy)
store.EnsureSchema(ctx)
store.LoadInto(ctx, policy)

app.RegisterBattery(admin.New(admin.Config{
    DB:         db,
    Policy:     policy,
    GrantStore: store,
    Auth:       authManager, // from battery/auth
}))
```

| Route                                | Purpose                              |
|--------------------------------------|--------------------------------------|
| `GET  /admin/rbac/roles`             | Role→permission matrix + grant forms |
| `GET  /admin/rbac/users`             | User list + role-edit forms          |
| `POST /admin/rbac/_grant`            | Grant a permission to a role (RPC)   |
| `POST /admin/rbac/_revoke`           | Revoke a permission from a role (RPC)|
| `POST /admin/rbac/_assign`           | Replace a user's roles (RPC)         |

The selectable permissions shown in the grant dropdown are the **union of
all currently-granted permissions** — there is no capability catalog.
Free-text entry for new permission strings is allowed.

Every mutation (grant, revoke, assign-roles) writes an **audit row** via
`framework.AppendAuditEvent` with entity `"access"` and op in
`{"grant","revoke","assign-roles"}`, so changes appear at `/admin/audit`.
The actor ID is the authenticated admin's user ID.

## Process-module lifecycle

When `Config.ProcessModules` is wired to `app.ProcessModules()` (the
process-isolated module supervisor — see
[process-modules](process-modules.md)), the admin exposes an operator
lifecycle screen at `<PathPrefix>/modules`, behind the same default-deny
gate. It lists every registered module's live state — surfacing the
disabled (404) vs crashed-but-enabled (503) distinction, the restart count,
and the prominent circuit-open / lease-failing flags — and offers guarded
actions: enable, disable, **bump generation** (the circuit-reset recovery
lever), and revoke a granted capability. Each action writes an audit row
(`module_enable` / `module_disable` / `module_bump` / `module_revoke`) and
never leaks a raw error or JSON. When `Config.ProcessModules` is nil the
screen is not mounted.

## Authorization

Every admin page is gated and requires authentication by default: the battery
requires an authenticated user that holds the **admin role** (default
`"admin"`). A user satisfies this when its `GetRoles() []string` includes
the role — `battery/auth`'s `User` does. Anonymous callers get `401`;
authenticated users who lack the role get `403` — on both the SSR screens
(via the host policy chain) and the RPC/form routes (via middleware).

> **BREAKING (since the admin default-deny change):** the default used to
> accept **any** authenticated user, so a freshly-registered reader could
> reach full admin CRUD. It now requires the admin role. If you relied on
> the old behaviour, either grant users the `admin` role or supply a
> custom `Config.Authorize`.

Change the required role with `Config.AdminRole`, or replace the check
entirely with `Config.Authorize`:

```go
admin.New(admin.Config{
    AdminRole: "superuser", // default is "admin"
})

// …or a fully custom predicate (overrides the role check):
admin.New(admin.Config{
    Authorize: func(ctx context.Context) bool {
        u := auth.GetCurrentUser(ctx)
        return u != nil && slices.Contains(u.GetRoles(), "admin")
    },
})
```

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
