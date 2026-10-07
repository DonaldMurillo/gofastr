# Admin UI

`battery/admin` is the back office: a dashboard, the entity screens, and
the operations pages (jobs, audit log, roles, user roles, process
modules), drawn in one shell through your app's UI host. Every page is a
host screen, so it renders with `runtime.js`, navigates client-side, and
shows toasts from the host's toast stack. The battery ships no CSS and no
JavaScript: the shell is `ui.Sidebar` beside a toolbar with breadcrumbs,
the command palette, the theme toggle and the account menu.

Every page and route sits behind one default-deny gate: see
[Authorization](#authorization).

## Quick start

<!-- gofastr:compile
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/battery/admin"
import appui "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
var productsConfig, customersConfig entity.EntityConfig
-->
```go
site := appui.NewApp("My App")
app := framework.NewUIHostApp(uihost.New(site), framework.WithDB(db))

app.Entity("products", productsConfig)
app.Entity("customers", customersConfig)

app.RegisterBattery(admin.New(admin.Config{
    Title:    "Back office",
    UI:       app.EntityUI(entityui.Extensions{}),
    Entities: []string{"products", "customers"},
}))
```

The admin needs a UI host: `Init` fails without one. It also needs
`Config.UI` (the app's `App.EntityUI`) whenever it exposes an entity,
because the entity screens are `framework/entityui`'s list and record.

Exposure is opt-in. An empty `Entities` exposes none, so a zero-value
config never turns every table into an editable back office. Name the
entities, or set `AllEntities: true` for every registered entity whose
CRUD is on. CRUD-off entities (`battery/auth`'s `users` and `sessions`)
stay hidden, so `AllEntities` never exposes credential tables. Naming an
entity in `Entities` exposes it even with CRUD off. A name the app does
not register fails boot.

## Pages and routes

| Route | Page |
|---|---|
| `GET /admin` | Dashboard: a count card per entity, failed jobs, recent activity, the app's cards |
| `GET /admin/search?q=` | Search results: the palette's scriptless twin |
| `GET /admin/entities/<name>` | Entity list |
| `GET /admin/entities/<name>/create` | Create form |
| `GET /admin/entities/<name>/:id` | Record (opens as a drawer from the list) |
| `GET /admin/queue` | Jobs, with `?status=` filter chips (needs `Queue`) |
| `GET /admin/audit` | Audit log, newest first |
| `GET /admin/rbac/roles` | Role permissions (needs `Policy` + `GrantStore`) |
| `GET /admin/rbac/users` | User roles (needs `Auth`) |
| `GET /admin/modules` | Process modules (needs `ProcessModules`) |

A page whose backing is not wired is not mounted and has no sidebar
entry. `Config.PathPrefix` moves everything off `/admin`.

### Entity screens

The list, record and create screens are `entityui` screens, so they
carry everything the entity declares: its `Display` names, fields and
nav group, its `States` transitions, relations (a Related tab on the
record), bulk actions, CSV export, and the record's activity tab. The
admin also turns on every optional `entityui` tool: the query box, the
columns menu, the trash view of a soft-deleting entity, saved views
(with `Config.SavedViews`), the record's API tab and the status
override. The admin reads each entity under the caller's own context
plus the elevation described below, and points the screens' writes at
its own routes:

| Route | Purpose |
|---|---|
| `POST /admin/api/<name>` | Create |
| `PUT`/`PATCH /admin/api/<name>/{id}` | Update |
| `DELETE /admin/api/<name>/{id}` | Delete |
| `POST /admin/api/<name>/{id}/transitions/{key}` | A `States` transition |
| `POST /admin/api/<name>/{id}/_override` | Status override (`States` only) |
| `POST /admin/api/<name>/{id}/_restore` | Restore from the trash (`SoftDelete` only) |
| `POST /admin/api/<name>/{id}/_purge` | Delete permanently (`SoftDelete` only) |
| `POST /admin/api/<name>/_bulk` | Bulk action |
| `GET /admin/api/<name>/_export.csv` | CSV export |
| `POST /admin/api/<name>/_views` | Save a view (`Config.SavedViews`) |
| `POST /admin/api/<name>/_views/_delete/{id}` | Delete a saved view |
| `GET /admin/_count/<name>` | A dashboard count card (polled) |

Every write goes through the app's own CRUD handler, so validation,
hooks, events, `WithAuditLog` rows, tenant and `OwnerField` scope, and
the field write checks apply exactly as on the JSON API.

**Elevation.** The admin routes and the admin's own screens run with
`crud.WithElevation`, naming the entities the admin exposes, which lifts
one check on those entities only: their `Exposure.Access` permissions.
An entity locked to `posts:write` on the app API is still editable from
the admin by a caller the gate admits; an entity the admin does not
expose keeps its check, so a relation label, a picker or a stat that
reaches it reads as the caller. Elevation never lifts tenant scope,
owner scope, soft delete, the field read and write checks, a
transition's `Permission` or the `<name>:override_state` capability the
status override needs, and it never reaches app code: a `Page` or
`Card` builds, and a page's `Access` answers, with the caller's own
context; an entityui action, tab, view func or field kind, and the
component it returns, gets the caller's context with the elevation
removed (`crud.WithoutElevation`); and every lifecycle hook an admin
write or read fires runs without it.

### App pages, cards, links and commands

<!-- gofastr:compile
import "context"
import "net/http"
import "time"
import "github.com/DonaldMurillo/gofastr/battery/admin"
import "github.com/DonaldMurillo/gofastr/core-ui/component"
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/framework/ui"
var reports, revenue func(*http.Request) (component.Component, error)
var isFinance func(context.Context) bool
-->
```go
admin.New(admin.Config{
    Pages: []admin.Page{{
        Path:   "/reports",
        Title:  "Reports",
        Nav:    &entity.EntityNav{Group: "insights", Icon: "chart"},
        Access: isFinance, // optional, narrows beyond the gate
        Build:  reports,
    }},
    Cards: []admin.Card{{Key: "revenue", Title: "Revenue", Build: revenue, Poll: 30 * time.Second}},
    Links:    []admin.Link{{Group: "help", Label: "Docs", Href: "/docs"}},
    Commands: []ui.PaletteCommand{{Label: "Open docs", Href: "/docs"}},
})
```

- A **Page** draws in the shell under `<PathPrefix><Path>` with a crumb
  and a palette entry. Its path may not take one of the admin's own
  (`/`, `/search`, `/queue`, `/audit`, `/rbac`, `/modules`, `/entities`,
  `/api`, or anything under `/_`); boot fails if it does. `Access`
  refuses with 403, hides the nav entry and the palette entry, and Build
  never runs for a refused caller.
- A **Card** draws on the dashboard. A positive `Poll` redraws it from
  `GET <PathPrefix>/_card/<key>` on that interval (`data-cui-poll`).
- Build failures are contained: an error, a panic or a nil component
  draws a generic notice in the shell and logs `app slot failed` with
  the slot name, never what the page read.
- **Links** must be same-origin paths; boot refuses anything else.

### Command palette

The toolbar's search field opens the palette. It lists the pages, each
entity's list and create screen, and your `Commands`. Typing searches the
exposed entities' `SearchFields` through `entityui.SearchRecords`, in the
caller's scope, and lists the matching records. The palette posts to
`POST <PathPrefix>/_palette`; the body is capped at 4 KiB and the query
at 200 runes. Without JavaScript the field submits to
`<PathPrefix>/search`.

## Operations pages

The operations pages draw through the UI host like every other admin
page: an app that mounts the admin only for its queue or audit log still
builds with `framework.NewUIHostApp`, or `Init` fails.

<!-- gofastr:compile
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/framework"
import appui "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
var app = framework.NewUIHostApp(uihost.New(appui.NewApp("Ops")))
import "github.com/DonaldMurillo/gofastr/battery/admin"
import "github.com/DonaldMurillo/gofastr/battery/queue"
-->
```go
q, _ := queue.NewDBQueue(db)
app.RegisterBattery(admin.New(admin.Config{
    Queue: q,  // the Jobs page and the dashboard's failed jobs
    DB:    db, // the audit log; defaults to the app's DB
}))
```

| Route | Purpose |
|---|---|
| `POST /admin/queue/_replay/{id}` | Re-queue one failed job |
| `POST /admin/queue/_replay_all` | Re-queue every failed job listed |

Replay is offered on failed jobs when the queue supports it (`DBQueue`
does). Each replay writes an audit row (entity `queue`, op `replay`)
naming the actor. A failed list or stats read shows a generic notice and
logs the driver error; the page never prints it. `QueueListLimit` and
`AuditListLimit` cap the rows (default 200). The audit page reads
`AuditTable` (default `audit_log`) and, when the request carries a
tenant, only that tenant's rows.

**Audit filters.** The audit page carries a GET filter form, so a filter
lives in the page's own query string and works without script:
`?actor=<user id>`, `?entity=<exposed entity name>`, `?op=<operation>`,
`?from=YYYY-MM-DD`, `?to=YYYY-MM-DD` (`to` inclusive). The operation
select offers the fixed set the audit log writes — `create`, `update`,
`delete`, `restore`, `purge`, `state_override` and entityui's `bulk`
summary — plus "any"; the entity select offers the exposed entities.
Every value is checked server-side before it reaches SQL, and values
travel as placeholders. An invalid value is ignored with a warning
naming the parameter, never a 500; a link clears the filter.

### Saved views

<!-- gofastr:compile
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/battery/admin"
-->
```go
admin.New(admin.Config{SavedViews: true, DB: db})
```

`Config.SavedViews` turns on the admin's saved-view store over the
admin's database (`Config.DB`, default the app's): one named
filter/columns set per user per entity, in `admin_saved_views`
(`Config.SavedViewsTable`; a lowercase identifier). The admin's lists
offer it; `(*Battery).SavedViews()` returns the store for an app's own
screens (`UI.WithSavedViews`). The store reads the owner and the tenant from the caller's
context only, keeps every owner's and tenant's views apart, caps a user
at `entityui.SavedViewCap` views per entity, and refuses blank, duplicate
and over-long names, filters and column lists with the errors the
`SavedViewStore` contract names. A generated app's admin config sets
`SavedViews: true`.

### Bulk jobs in the background

A bulk action over more than `entityui.InRequestCap` (100) records runs
outside the request, on `Config.Queue`'s queue, through
`admin.NewBulkJobs`:

<!-- gofastr:compile
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/battery/admin"
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "github.com/DonaldMurillo/gofastr/battery/queue"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
var app = framework.NewApp(framework.WithDB(db))
var authManager *auth.AuthManager
-->
```go
q, _ := queue.NewDBQueue(db)
jobs, _ := admin.NewBulkJobs(q, admin.AuthPrincipal(authManager))

app.RegisterBattery(admin.New(admin.Config{
    UI:       app.EntityUI(entityui.Extensions{Jobs: jobs}),
    Entities: []string{"posts"},
    BulkJobs: jobs,
}))
```

The wiring order is fixed by the two directions: the UI needs the
runner at `app.EntityUI` time (`Extensions.Jobs`), and the runner needs
the UI to run jobs, so `Config.BulkJobs` closes the cycle — the admin's
`Init` binds the runner to `Config.UI` and then calls
`UI.ResumeBulkJobs`, handing back any job a crash left between its
snapshot and its `Enqueue`. The queue job's payload carries only the job
id; the confirmed selection lives in the host's snapshot store.

`PrincipalFunc` (the second argument) rebuilds the confirming user's
request context before every chunk: the user and their current roles,
read fresh, and the run's tenant. The tenant is the one the selection
was confirmed in and stays fixed for the run, because the snapshot's
records belong to it. An error stops the run, so a creator who is gone
runs nothing. `admin.AuthPrincipal(am)` builds one from `battery/auth`,
loading the user and their current roles through the manager's user
store; it has no notion of tenant membership and stamps the run's
tenant as given. An app that does track membership writes its own
`PrincipalFunc` and returns an error when the user no longer belongs
to the tenant it is handed. The admin then puts `Config.Policy` on a context that has no
policy and runs its own gate again: a creator it admits runs elevated,
as the admin's bulk route does; one whose admin role was revoked runs
as a plain caller, so the writes pass only that user's own permissions.

### Roles and user roles

<!-- gofastr:compile
import "context"
var ctx context.Context
import "database/sql"
var db *sql.DB
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "github.com/DonaldMurillo/gofastr/battery/admin"
import "github.com/DonaldMurillo/gofastr/battery/auth"
var authManager *auth.AuthManager
-->
```go
policy := framework.NewRolePolicy()
store := framework.NewGrantStore(db, policy)
_ = store.EnsureSchema(ctx)
_ = store.LoadInto(ctx, policy)

app.RegisterBattery(admin.New(admin.Config{
    Policy:     policy,
    GrantStore: store,
    Auth:       authManager, // the User roles page
}))
```

| Route | Purpose |
|---|---|
| `POST /admin/rbac/_grant` | Grant a permission to a role |
| `POST /admin/rbac/_revoke` | Revoke a permission from a role |
| `POST /admin/rbac/_assign` | Replace a user's roles |

When the policy declares capabilities, the grant form offers them as a
select and marks granted permissions the app does not declare. Under
`StrictCapabilities` a grant of an undeclared permission is refused.

Every change writes an audit row (entity `access`, op `grant`, `revoke`
or `assign-roles`). A caller may grant or revoke only a permission its
own roles hold (or the wildcard), and assign only roles it holds or
whose permissions its own roles imply; a refusal is a 403 with a
`grant-refused`, `revoke-refused` or `assign-roles-refused` row, so a
narrower admin tier cannot mint a role above its own.

### Process modules

With `Config.ProcessModules` set to `app.ProcessModules()` (see
[process-modules](process-modules.md)), `/admin/modules` lists each
module's state, restart count, and the circuit-open and lease-failing
flags. Its levers (enable, disable, bump generation, revoke a grant) post
to `/admin/modules/_enable`, `_disable`, `_bump` and `_revoke`, and need
the `modules:manage` permission on top of the gate, held through the
caller's roles in `Config.Policy`; without it, or without a policy, the
page is read-only. Each lever writes an audit row (`module_enable`, ...), and
a refused one a `*_refused` row. A failed lever answers a generic
notice and logs the error.

### How ops posts answer

Each ops form posts two ways. With the runtime it is a form RPC: success
is `204` with a toast, a refusal is JSON `{"error": ...}` with its
status. A plain post redirects (`303`) back to the page with
`?result=<name>`, which the page draws as a notice. Bodies are capped at
1 MiB (`413` past it).

## Authorization

The gate admits an authenticated user whose `GetRoles()` holds
`Config.AdminRole` (default `"admin"`); `battery/auth`'s `User` does.
`Config.Authorize` replaces the role check with your own predicate. Two
refusals run before either:

- an `embed` grant on the request (an embedded surface never reaches the
  back office);
- an `access.Decider` on the request returning `DecisionDeny`.

A signed-out caller gets `401`, a signed-in caller without the role
`403`, on every page, route and shell widget (the nav drawer and the
palette). With `Config.LoginPath` set, a signed-out GET redirects to
`LoginPath?next=<path>` instead; a signed-out post is still a `401`.

<!-- gofastr:compile
import "context"
import "slices"
import "github.com/DonaldMurillo/gofastr/battery/admin"
import "github.com/DonaldMurillo/gofastr/battery/auth"
-->
```go
admin.New(admin.Config{AdminRole: "superuser", LoginPath: "/login"})

// ...or a custom predicate:
admin.New(admin.Config{
    Authorize: func(ctx context.Context) bool {
        u := auth.GetCurrentUser(ctx)
        return u != nil && slices.Contains(u.GetRoles(), "staff")
    },
})
```

`Config.SignOutPath` is where the account menu's Sign out posts; it
defaults to `Auth`'s logout route.

## Response headers

Every response under the prefix, refusals included, carries
`Cache-Control: no-store` and the security headers
(`middleware.SecurityHeaders`), whether or not the app installs its
default middleware. Admin pages hold per-caller, tenant-scoped data, and
HTTP's storage rules cover Authorization headers, not cookie sessions.

## CSRF

The battery refuses every forgeable cross-site request to a mutating
route (a browser post whose `Sec-Fetch-Site` is `cross-site`, or whose
`Origin` host differs from the request host) with `403`, before the gate
runs. Non-browser clients send neither header and pass. This holds
without the optional CSRF middleware. Mount `middleware.CSRF` app-wide
for a token on top: the kit's forms stamp `_csrf` from the context and
the runtime sends `X-CSRF-Token` from `<meta name="csrf-token">`.

## Common mistakes

- **Don't expose `/admin` to the public.** It shows entity data, actor
  ids and job counts.
- **Per-user data needs `OwnerField`.** Elevation keeps owner scope, so
  an admin sees only the rows the entity's scope allows; declare it. See
  [Entity Declarations](entity-declarations.md) → per-user scoping.
- **Set `UI`.** Exposing an entity without `Config.UI` fails boot.

## See also

`examples/backoffice` is a runnable back office: SQLite, entities, a
demo login, and the admin.
