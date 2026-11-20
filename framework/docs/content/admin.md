# Admin panel (`battery/admin`)

`battery/admin` mounts a role-gated back-office over your app's entities, audit log, background jobs, feature flags, and email templates — composed directly from `framework/entityui` and `framework/ui` with zero bespoke CSS.

## Quickstart

```go
import "github.com/DonaldMurillo/gofastr/battery/admin"

adm := admin.New(admin.Config{
	Prefix: "/admin",
	Roles:  []string{"admin"},
})
app.RegisterBattery(adm)
```

Because `battery/admin` reuses your app's `*entityui.UI` (via `App.EntityUI`), every entity's `Display` config, `States` transitions, `Extensions`, hooks, and scopes work identically in the admin panel.

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

- **Fail-closed:** `battery/admin` requires an authenticated user holding one of the configured `Roles` (default `["admin"]`). Anonymous callers redirect to login (HTML) or get `401` (API); signed-in users without the role get `403`.
- **Same entity pipeline:** admin reads and writes go through the entity's CRUD handlers, hooks, validation, and `AuditLog` — never raw SQL bypasses.
- **Tenant scoping:** when the request context carries a tenant ID, `/admin/audit`, entity lists, and overview metrics scope strictly to that tenant (`WHERE tenant_id = $1`). Platform operators with no tenant context see cross-tenant records.
- **CSRF & origin checks:** every form POST, bulk action, flag update, and job replay runs through GoFastr's CSRF and same-origin guards.

## Email previews

Register sample payloads in `Config.EmailPreviews` so operators and developers can inspect transactional email templates right inside `<prefix>/emails` before sending:

- Renders subject, plain-text body, and HTML body (inside a sandboxed `<iframe>` so template HTML/CSS cannot touch the admin session or layout).
- Works with templates loaded via `email.LoadFromDir` or `email.LoadFromFS` (see [Email](email.md)).

## Customizing the admin

- Add custom entity columns, record tabs, views, and actions once via `app.EntityUI(entityui.Extensions{...})` — both app screens and admin inherit them.
- Customize navigation, roles, email previews, and dashboard options via `admin.Config`.

## Common mistakes

- **Calling `app.EntityUI(...)` twice (once for app screens and once for admin):** `App.EntityUI` panics on a second call. Build it once and let `battery/admin` share it.
- **Leaving `Roles` empty or forgetting to mount auth middleware:** without session/auth middleware wired, every admin request sees an anonymous caller and refuses access.
- **Expecting `/admin/flags` to edit flags when using a read-only `Store`:** editing requires a `featureflag.MutableStore` (`MemoryStore` or `NewSQLStore`).
- **Rendering user-supplied HTML previews outside the sandboxed frame:** always use `Config.EmailPreviews` rather than building an un-sandboxed preview handler.
