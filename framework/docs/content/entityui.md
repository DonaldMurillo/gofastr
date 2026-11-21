# Entity screens (entityui)

`framework/entityui` builds server-rendered list, create, and record screens directly from an entity's schema, `Display` config, and `States`. Generated apps call it, `battery/admin` composes it, and any hand-written screen can embed its builders as ordinary `ui.Component` values.

EntityUI draws no client islands. It reserves three internal routes beneath each entity it owns — `POST _bulk`, `GET _export.csv`, and `POST _pick` — and refuses custom entity endpoints that collide with those method/path pairs in either registration order.

## Setup

Create one `*entityui.UI` per app after registering the entities referenced by your `Extensions` (entities registered later still receive their bulk, export, and picker routes). Calling `App.EntityUI` twice panics so your screens and `battery/admin` always share one instance:

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
var fwApp *framework.App
var ctx = context.Context(nil)
stmt: _ = appUI
-->
```go
appUI := fwApp.EntityUI(entityui.Extensions{})
```

`App.EntityUI` validates every entity, field, and view name in `Extensions` at boot and panics immediately on a mismatch (`UI.CheckEntity` runs the same checks for entities registered afterwards).

## Mounting app-owned screens

Builders return components without an outer page frame (`ui.Container`), so wrap them in your layout container:

| Screen | Default route | Builder |
| --- | --- | --- |
| List | `/deals` | `appUI.List("deals").Base("/deals")` |
| Create | `/deals/create` | `appUI.Create("deals").Base("/deals")` |
| Record | `/deals/{id}` | `appUI.Record("deals", id).Base("/deals")` |

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
import "github.com/DonaldMurillo/gofastr/framework/ui"
var appUI *entityui.UI
var ctx context.Context
-->
```go
page := ui.Container(ui.ContainerConfig{}, appUI.List("deals").Base("/deals").RenderCtx(ctx))
_ = page
```

Key routing details:
- **Composite keys:** register one path segment per key column (for example `/enrollments/{student_id}/{course_id}`) and pass `entity.JoinKey` of the params as the record `id`.
- **Create route:** default links (New, Duplicate, Create another, related-list Add, picker New) target `<base>/create`. To use another path (like `/deals/new`), configure one shared function with `WithCreatePath` and register your screen at the same path:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/entity"
import "github.com/DonaldMurillo/gofastr/framework/entityui"
var appUI *entityui.UI
-->
```go
createPath := func(e *entity.Entity) (string, bool) {
	return "/" + e.GetName() + "/new", true
}
appUI = appUI.WithCreatePath(createPath)
```

Returning `false` hides create links for that entity. Use `WithRecordPath` to customize record URLs and `WithAPIPath` to customize write endpoints.
- **Missing route detection:** the first time a list renders record links pointing at an unregistered route, entityui logs a warning (`entityui: list rows link to a path no screen serves`). Pass `.NoLinks()` to disable row links.
- **Form wire names:** form inputs submit using the CRUD handler's JSON wire key (`WireName` or default camelCase, e.g., `addressLine1` for `address_line_1`), so validation errors bind directly back to each field.
- **Server-owned columns:** `Scope.OwnerField`, `MultiTenant` tenant columns, auto-generated primary keys, and state/stamp fields never render as editable inputs. Caller-assigned primary keys (composite keys or required non-auto keys) render on Create, become read-only on Record, and clear on Duplicate.

## List screens

`appUI.List(name)` renders a responsive `ui.DataTable` (collapsing to two-line `ui.ResponsiveRows` cards on narrow viewports) driven by `Display`:

- Columns from `Display.Columns` (falling back to key columns if empty), with title links to the record
- View tabs (`Display.Views`), one-click facet chips (`Display.Facets`), search box (`EntityConfig.SearchFields`), filter chips, sort headers, and pagination
- Row menu with Open, Duplicate (`.Duplicate()`), and Delete (`.Delete()`)

Optional power-user controls (off by default on app screens, on in `battery/admin`):
- `.QueryBox()` — structured query expression bar
- `.ColumnsMenu()` — toggle visible columns
- `.Deleted()` — trash view + restore for `Scope.SoftDelete` entities
- `.SavedViews()` — save and recall named filter combinations
- `.TabCounts()` — row badges on view tabs
- `.Undo()` — 10-second Undo toast after soft-deleting (posts `<write base>/<id>/_restore`)

## Record and create screens

`appUI.Record(name, id)` renders:
- **Header:** title, current state badge, timestamps, non-danger state transition buttons, primary Save button (`Mod+S`, idle until dirty), and an overflow menu (Copy link, Copy API URL, danger transitions with confirm dialog, Duplicate, Create another, Delete). Copy links trust `X-Forwarded-Proto` only from `AppConfig.TrustedProxies`.
- **Tabs:**
  - **Edit:** form generated from `Display.Form`, plus a Details sidebar (ID with copy button, timestamps, transition stamps).
  - **Related:** embedded lists for related entities, each gated by the related entity's own read scope and offering prefilled Add links.
  - **Activity** (opt-in): newest-first audit timeline using `ui.ChangeList`, omitting `Hidden`, `NoQuery`, and `AfterGet`-masked fields.
  - **API** (opt-in): raw JSON view of the record.
  - Custom tabs added via `Extensions`.
- **Drawer mode:** when opened via `app.InterceptFrom`, the record renders inside `ui.DrawerBar`. Pass `.Steps(list)` with the same `ListBuilder` to enable previous/next stepping through the active filtered order, and `.Panel()` on the full record page to offer an "Open in panel" link.

Relation fields render as `ui.Picker`: focusing lists the first 20 related records, typing searches through `POST <api>/_pick?field=<field>` (POST-only, JSON-only, 4 KB cap, same-origin, gated by both host and target read permissions and per-row `Decider`), and `WithRecordPath` adds Open and New shortcuts.

## Bulk actions and CSV export

`App.EntityUI` mounts `POST <api>/_bulk` and `GET <api>/_export.csv` alongside each entity's CRUD routes:
- **Bulk actions** operate on selected IDs or "every match" (capped by `EveryMatchCap`), honoring the caller's scopes, hooks, and delete/update permissions. Soft-delete bulk actions support `.Undo()`.
- **CSV export** streams the active view/search/filter/sort result using the caller's read scope and omits `Hidden`/`NoQuery` fields.

## Extensions

Pass `entityui.Extensions` to `App.EntityUI` to customize behavior without forking screens:
- **Custom columns and cells** for computed or formatted values
- **Custom tabs** on record pages
- **Named views (`Where`)** and **custom record/bulk actions** with role/permission gates
- **Custom field widgets** and **inline editors**

## Common mistakes

- **Calling `App.EntityUI` more than once:** it panics. Build one `*entityui.UI` at startup and share it between your screens and `battery/admin`.
- **Mounting `/deals/new` without calling `WithCreatePath`:** built-in New, Duplicate, and related Add links default to `<base>/create` and will hit `/deals/{id}` as a missing record named `"create"`.
- **Passing a different `ListBuilder` to `.Steps(list)`:** drawer next/previous navigation must receive the same configured `ListBuilder` as the list screen or it will step through rows the user cannot see in the list.
- **Registering custom entity endpoints at `POST _bulk`, `GET _export.csv`, or `POST _pick`:** those paths are reserved by entityui and rejected at registration.
- **Forgetting `AppConfig.TrustedProxies` behind TLS termination:** Copy link and Copy API URL will emit `http://` instead of `https://`.
- **Expecting `OwnerField` or tenant columns to be editable in forms:** server-scoped columns are always stripped from editable inputs.
