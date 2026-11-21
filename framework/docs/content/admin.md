# Admin panel (`battery/admin`)

`battery/admin` mounts a role-gated back-office over your app's entities, audit log, background jobs, feature flags, and email templates — composed directly from `framework/entityui` and `framework/ui` with zero bespoke CSS.

## Quickstart

Mount a `UIHost` first (`framework.NewUIHostApp`), build your `EntityUI` once, and opt into exposed entities with `Entities` or `AllEntities`:

```go
import "github.com/DonaldMurillo/gofastr/battery/admin"

entityUI := app.EntityUI(entityui.Extensions{})

adm := admin.New(admin.Config{
	PathPrefix:  "/admin",
	AdminRole:   "admin", // or provide a custom Authorize func
	LoginPath:   "/login",
	UI:          entityUI,
	AllEntities: true, // or Entities: []string{"users", "orders"}
})
app.RegisterBattery(adm)
```

Because `battery/admin` reuses your app's `*entityui.UI` (passed via `Config.UI`), every entity's `Display` config, `States` transitions, `Extensions`, hooks, and scopes work identically in the admin panel.

## What it mounts

| Route | Purpose |
| --- | --- |
| `<prefix>` | Overview dashboard (entity counts, recent audit entries, queue stats) |
| `<prefix>/entities/<name>` | List view with query box, columns menu, facets, trash view, saved views, bulk actions, and CSV export |
| `<prefix>/entities/<name>/create` | Create record form |
| `<prefix>/entities/<name>/<id>` | Record detail with Edit, Related, Activity (audit trail), and API JSON tabs |
| `<prefix>/audit` | Filterable audit log viewer (tenant-scoped when a tenant is in context) |
| `<prefix>/jobs` | `battery/queue` job inspector and dead-letter replay |
| `<prefix>/flags` | `core/featureflag` toggle/rollout manager (when a `MutableStore` is wired) |
| `<prefix>/emails` | Preview registered email templates with sample data |

## Security posture

- **Fail-closed:** `battery/admin` requires an authenticated user holding `AdminRole` (default `"admin"`) or passing `Config.Authorize`. Anonymous callers get `401` (or redirect to `LoginPath` on HTML requests when `LoginPath` is set); signed-in users without access get `403`.
- **Same entity pipeline:** admin reads and writes go through the entity's CRUD handlers, hooks, validation, and `AuditLog` — never raw SQL bypasses.
- **Tenant scoping:** when the request context carries a tenant ID, `/admin/audit`, entity lists, and overview metrics scope strictly to that tenant (`WHERE tenant_id = $1`). Platform operators with no tenant context see cross-tenant records.
- **CSRF & origin checks:** every form POST, bulk action, flag update, and job replay runs through GoFastr's CSRF and same-origin guards.

## Email previews

Register sample payloads in `Config.EmailPreviews` so operators and developers can inspect transactional email templates right inside `<prefix>/emails` before sending:

- Renders subject, plain-text body, and HTML body (inside a sandboxed `<iframe>` so template HTML/CSS cannot touch the admin session or layout).
- Works with templates loaded via `email.LoadFromDir` or `email.LoadFromFS` (see [Email](email.md)).

## Customizing the admin

- Add custom entity columns, record tabs, views, and actions once via `app.EntityUI(entityui.Extensions{...})` and pass the returned `*entityui.UI` in `admin.Config.UI`.
- Customize navigation, `AdminRole`/`Authorize`, `LoginPath`, email previews, and dashboard options via `admin.Config`.

## Common mistakes

- **Calling `app.EntityUI(...)` twice (once for app screens and once for admin):** `App.EntityUI` panics on a second call. Build it once and pass it into `admin.Config.UI`.
- **Forgetting `Entities` / `AllEntities` or `UI`:** by default `Entities` is empty, so no entity panels appear until you opt in and pass `UI: app.EntityUI(...)`.
- **Forgetting to mount auth middleware or `LoginPath`:** without session/auth middleware wired, every admin request sees an anonymous caller and refuses access; set `LoginPath` if you want browser visits redirected to `/login`.
- **Expecting `/admin/flags` to edit flags when using a read-only `Store`:** editing requires a `featureflag.MutableStore` (`MemoryStore` or `NewSQLStore`).
- **Rendering user-supplied HTML previews outside the sandboxed frame:** always use `Config.EmailPreviews` rather than building an un-sandboxed preview handler.
