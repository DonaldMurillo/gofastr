# What changed in v0.87.0

v0.87.0 changes how an app draws its data and how the kit looks. Like
v0.86.0 it deprecates nothing: a removed API is gone, and
`gofastr upgrade` points at every line of an app that still uses one.

Five changes make up most of the release:

1. **A new default look.** `style.DefaultTheme()` and `theme.Default()`
   are a neutral zinc theme, and stroke, line-height, letter-spacing and
   opacity are tokens. One `:root` block can redraw the whole kit, and
   `ui.ThemePicker` switches a page between registered themes.
2. **Entities describe their screens.** `EntityConfig.Display` holds the
   names, columns, views, facets, form layout and field hints that list
   and record screens read.
3. **Entities can have states.** `EntityConfig.States` names a state
   field and the moves that change it. The CRUD layer enforces the moves
   on every write path and audits each one.
4. **`framework/entityui` draws entity screens.** Lists, records, create
   forms, bulk actions, CSV export and saved views come from the entity's
   schema, `Display` and `States`, with no islands. It replaces
   `framework/ui/resource`, which is deleted.
5. **The admin is rebuilt on entityui.** `battery/admin` renders every
   page through the app's UI host and ships no CSS.

Around them: the kernel's attributes are `data-cui-*` and its classes
`cui-*`, the `framework_docs_*` MCP tools moved out of package framework,
the repo builds with Go 1.27.2, and the 2026-10-04 security audit of the
request path landed with its fixes.

This page is the full account. The changelog entry is the summary.

## Moving an app

Install the v0.87.0 CLI and run the upgrade report before you touch
`go.mod`:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@v0.87.0
gofastr upgrade --to v0.87.0
```

If `go.mod` already names v0.87.0, pass `--from` with the release the
code was written for. The report lists lines to edit, lines to check and
notes no scan matched. [Upgrading](upgrading.md) covers the flags.

**Build with Go 1.27.2.** The repo pins `toolchain go1.27.2` in
`go.mod`. The `go 1.27.0` floor for apps is unchanged, so an app still
builds on 1.27.0, but 1.27.2 fixes thirteen standard-library advisories
govulncheck reports against 1.27.0, among them GO-2026-6599 and
GO-2026-6600 in `html/template`. Build and ship with 1.27.2.

An order that works for most apps:

1. Fix the theme. A `theme.go` that an earlier `gofastr theme init` or
   `gofastr theme edit` wrote builds its `ShadowSet` field by field and
   panics at boot until it gains `Shadows.XS` (see [Theme](#theme)).
2. Rewrite owned sheets the token checks now refuse (`outline-offset:
   2px`, `line-height: 1.6`, `padding-inline: 16px`), then run
   `gofastr gen styles`.
3. Rename kernel attributes and classes: `data-fui-rpc` is
   `data-cui-rpc`, `fui-hidden` is `cui-hidden` (see
   [Prefixes](#prefixes-data-cui--and-cui--belong-to-the-kernel)).
4. Replace `framework/ui/resource` screens with `App.EntityUI`, and
   move labels, columns, filters and transitions onto the entity's
   `Display` and `States`.
5. Update the admin config: set `Config.UI`, mount a UI host, and
   delete the removed fields.
6. Add `framework.WithMCPTools(mcptools.Register)` where agents read the
   framework docs over `/mcp`.
7. Mark the users your own code creates verified (see
   [Security](#security-changes-that-break-callers)).
8. A generated app: edit `gofastr.yml` for the removed screen keys, then
   regenerate with `gofastr generate --force`.

## Theme

### The default look is zinc

`style.DefaultTheme()` and `theme.Default()` now draw a near-black
primary (`#18181B`, was indigo `#4F46E5`) on a white page, one hairline
border and one soft surface, with a matching dark palette (near-white
primary on `#09090B`). Fonts lead with the system stack; `Inter` and
`JetBrains Mono` stay only as later fallbacks. Radii are 6/8/10/14px
(were 4/8/12/16) and shadows are softer.

Three tokens change role as well as shade:

| Token | Now | Was |
| --- | --- | --- |
| `--color-secondary` | light fill `#F4F4F5` under near-black `--color-secondary-fg` | grey `#6B7280` under white |
| `--color-border-strong` | `#D4D4D8`, a hover edge | `#A1A1AA`, a 3:1 control edge |
| `--color-accent` | blue `#2563EB` in both themes | violet `#7C3AED` (`style.DefaultTheme()`), cyan `#0891B2` (`theme.Default()`) |

What to check in an app:

- Text or an icon drawn in `var(--color-secondary)` is near-invisible on
  white. Draw it in `var(--color-text-muted)`, or put
  `var(--color-secondary-fg)` on a `var(--color-secondary)` fill.
- A control border drawn in `var(--color-border-strong)` falls below
  3:1. Move it to `var(--color-text-subtle)`.
- Set `theme.Overrides.Primary` to keep a brand colour.

The components follow one set of rules: every focus ring is `2px solid
var(--color-text-subtle)`, checkbox and radio borders clear WCAG's 3:1,
an inline `ui.Link` is underlined, status badges, tags and chips are
soft fills, and text controls go back to 16px below the md breakpoint so
iOS does not zoom into a focused field. `ui.Form` is capped at 42rem
(`--ui-form-max`), with 16px between fields and 24px above its actions.

### New tokens

| Group | Field | Tokens |
| --- | --- | --- |
| Strokes | `Theme.Strokes` (`style.StrokeSet`) | `--stroke-thin` 1px, `--stroke-thick` 2px, `--stroke-focus` 2px, `--stroke-focus-offset` 2px |
| Line height | `Theme.Leading` | `--leading-tight` 1.2, `-snug` 1.4, `-normal` 1.5, `-relaxed` 1.6 |
| Letter spacing | `Theme.Tracking` | `--tracking-tighter` -0.03em through `--tracking-wider` 0.08em |
| Opacity | `Theme.Opacities` | `--opacity-faint` 0.2, `-disabled` 0.5, `-muted` 0.6 |
| Shadow | `style.ShadowSet.XS` | `--shadow-xs`, the lift under a resting control |
| Knobs | `Theme.Knobs` (`map[string]string`, keys `ui-…`) | any `--ui-<component>-<part>` knob, set in the theme's `:root` block |

The four new groups are optional: an unset slot emits the default value,
so a `theme.go` written before them keeps its look and every
`var(--stroke-*)` resolves. `Shadows.XS` is required like every other
shadow step.

Every kit padding, margin, gap, font size, line height, letter spacing,
opacity, shadow, colour and component dimension now reads a token or a
`--ui-*` knob, with its old value as the fallback. A theme alone can
redraw the kit: `theme.Brutal()` sets square corners, 2px strokes and
hard shadows with no component CSS. Shared press knobs
(`--ui-press-hover-translate`, `--ui-press-active-shadow` and their
siblings) lift buttons, cards and tags on hover; letter-case knobs
(`--ui-button-case`, `--ui-badge-case`) set the case of labels; and
`ui.Stack`, `ui.Cluster` and `ui.Grid` read their gaps from
`--ui-layout-gap-<step>` knobs. A value that sat between steps snapped to
the nearest token, so opacities 0.55, 0.45 and 0.18 now draw at 0.6, 0.5
and 0.2.

### Page themes: `ui.ThemePicker`

Register each extra theme with `style.RegisterThemeOverride` and list it
in `ThemePickerConfig.Themes`. The picker draws as `ui.ThemeToggle`'s
pill with a Default option for the app's own theme. Picking a theme puts
its `cui-theme-<hash>` class on `<html>`, stores it in
`localStorage["gofastr.theme"]`, and the colour-scheme bootstrap puts it
back before first paint on the next load. [Theming](theming.md#page-themes-uithemepicker)
lists what a page theme does not reach.

### Owned sheets read the new tokens

The owned-style check (`gofastr gen styles`, `gofastr verify`) now
compares more properties against tokens. A matching literal in a
`.style.css` is a GOFASTR1807 error, and that sheet's Go is not
generated. The siteheader, sitefooter and docpage sheets the blueprint
wrote carry several of these.

| Property | Write instead |
| --- | --- |
| `outline-offset: 2px` | `var(--stroke-focus-offset)` |
| `border-width: 1px`, `outline-width: 1px` | `var(--stroke-thin)` |
| `border-width: 2px` | `var(--stroke-thick)` |
| `outline: 2px solid …` | `var(--stroke-focus) solid …` |
| `line-height` 1.2 / 1.4 / 1.5 / 1.6 | `var(--leading-tight)` / `-snug` / `-normal` / `-relaxed` |
| `letter-spacing` -0.03em / -0.02em / -0.01em / 0.04em / 0.08em | `var(--tracking-tighter)` / `-tight` / `-snug` / `-wide` / `-wider` |
| `opacity` 0.2 / 0.5 / 0.6 | `var(--opacity-faint)` / `-disabled` / `-muted` |
| `padding-inline`, `margin-block` (and their sides) 2 / 4 / 8 / 16 / 24 / 32 / 48px | `var(--spacing-xs)` / `-sm` / `-md` / `-lg` / `-xl` / `-2xl` / `-3xl` |

GOFASTR1807 reads a number the way the browser does: `.6`, `1.60` and
`-.01em` are `0.6`, `1.6` and `-0.01em`. A two-value shorthand
(`padding-inline: 8px 16px`) passes.

A new rule, GOFASTR1823, holds the design system's own CSS to the
tokens: a bare border or outline width, px radius, duration up to
500ms, z-index above 10, spacing, type size, opacity, shadow, colour
literal, ease keyword or numeric font weight in design-system CSS is an
error. The contract catalog holds 78 rules.

## Entity `Display`: names, columns, views and facets

`EntityConfig.Display` is one block of screen hints: singular and plural
names, the title fields that name a record, list columns, named views (a
DSL `Where` and a `Sort`), facets, the record form (main and side
columns, rows, sections), card fields, nav placement, page sizes,
`NoDuplicate` and `NoBulk`, and per-field `Label`, `Help`,
`Placeholder`, `Locked`, `Omit` and `ShowWhen`. nil means every default.

`App.Entity` checks every name at registration and refuses a bad one,
naming it: an unknown or Hidden field, a NoQuery field in facets, a
duplicate entry, a view `Where` the query DSL refuses, a `Sort` outside
the `?sort=` grammar, page sizes above `Pagination.MaxListLimit`. Display
changes how screens draw, never what the API accepts, and it does not
change the SDK schema hash. [Entity
declarations](entity-declarations.md#screen-hints-display) has every
setting.

With it:

- `framework/i18nui` translates the names: `EntitySingular`,
  `EntityPlural`, `FieldLabel`, `FieldHelp`, `ViewLabel`,
  `TransitionLabel` and others read `entity.<entity>.*` catalog keys,
  then the Display value, then a derived fallback.
- `dsl.ParsePredicate` parses filter text (`status in ["open",
  "past_due"] and due_on < "2026-10-01"`) into a validated
  `*filter.Predicate`, and `dsl.ParseSort` parses `amount DESC, number
  ASC`. Views and URLs share the one grammar.
- A not-equal filter: `?field_ne=`, `ne` in `?where=`, `?rel.field_ne=`.
- `crud.ListOptions` gains `Where` (a validated predicate inside every
  owner, tenant and soft-delete scope), `Fields` (a column projection)
  and `Deleted` (soft-deleted rows only).
- `crud.SumAll` and `crud.GroupCountAll` total a numeric field or count
  rows per value under the same scopes as `ListAll`.
- `CrudHandler.RestoreOne` and `PurgeOne` restore and permanently delete
  soft-deleted rows in process, with permission checks, hooks and audit
  rows.

## Entity `States`: moves, audit and overrides

`EntityConfig.States` names the Enum field that holds the state, the
values a create may start at, and the named moves that change it:

```go
States: &framework.StatesConfig{
    Field:   "status",
    Initial: []string{"draft"},
    Transitions: []framework.Transition{
        {Key: "issue", From: []string{"draft"}, To: "issued", Stamp: "issued_on"},
        {Key: "mark_overdue", From: []string{"issued"}, To: "overdue", System: true},
    },
},
```

Unless `Advisory` is set, the state field and every stamp change only
through a move, on every write path: REST create and update, `_batch`,
cascade writes, `UpsertOne`, in-process `CreateOne` and `UpdateOne`, and
hooks. Any other change is a 422 naming the open moves.

- A move runs through `CrudHandler.RunTransition`, the route `POST
  <api>/<entity>/<id>/transitions/<key>`, or the MCP tool
  `<entity>_<key>`. A `System` move has no route or tool.
- A move needs the entity's update permission plus its own `Permission`,
  held by name: a Wildcard grant does not satisfy it.
- Each move writes an audit row with op `transition:<key>`.
- Generated clients (OpenAPI, MCP tools, the Go client, the JS SDK, the
  CLI) gain one call per non-system move.
- Trusted Go code (seeds, backfills, repair jobs) writes outside a move
  with `crud.WithStateOverride(ctx, reason)`. It needs a reason and an
  audited entity, and the audit row carries op `state_override` and the
  reason in a new nullable `reason` column.

`App.WithAuditLog` adds that `reason TEXT` column to an existing audit
table at boot. A database role without `ALTER TABLE` on the audit table
fails startup: add the column first with a migration role. With
`AuditConfig.Actor` unset, an audit row now records the request user's
id instead of no actor. [States](states.md) is the reference.

## `framework/entityui`: entity screens

`App.EntityUI(ext)` builds the app's one `*entityui.UI`. A second call
panics, so the admin and the app's own pages share it. `List`, `Record`
and `Create` render from the entity's schema, `Display` and `States`:

```go
appUI := fwApp.EntityUI(entityui.Extensions{})
list := appUI.List("invoices").Columns("number", "amount").RenderCtx(ctx)
record := appUI.Record("invoices", "inv-42").Delete().RenderCtx(ctx)
```

- **No islands.** A list keeps sort, page, search, filter, view and
  facets in its query string, and writes are form RPCs to the REST
  routes.
- **Lists** draw a table or cards with view tabs, facets, filter chips,
  a pager with "1–25 of 40" and a rows-per-page menu. Optional tools: a
  query box (`QueryBox()`), a columns menu (`ColumnsMenu()`), a trash
  view (`Deleted()`), saved views (`SavedViews()` with
  `UI.WithSavedViews(store)`), tab counts (`TabCounts()`), inline
  editing (`InlineEdit()`) and a Table / Cards switch
  (`LayoutSwitch()`).
- **Records** show the state as a badge, a button per open move, Edit,
  Related and Activity tabs, a Details column, and a header menu with
  copy link, duplicate and delete. `API()` adds an API tab, `Override()`
  a status override form for a holder of `<entity>:override_state`,
  `Steps()` previous and next in a drawer, and `Undo()` an Undo button
  on the delete toast.
- **Bulk and export.** `.Bulk()` adds a select column, a bulk bar and an
  Export CSV link. `App.EntityUI` mounts `POST <api>/<entity>/_bulk` and
  `GET <api>/<entity>/_export.csv`. Up to 100 records run in the
  request; more need `Extensions.Jobs`, which stores job snapshots in
  `gofastr_bulk_jobs` and `gofastr_bulk_items`.
- **Gates.** New, Duplicate, Delete, the moves and the edit form follow
  the caller's create, update and delete access, and every read of
  another entity passes that entity's own read gate.
- **Dashboards.** `StatValue`, `GroupBars`, `GroupSlices` and
  `LineChart` read through the same gates.

[Entity screens](entityui.md) is the reference.

Drawers changed with it: intercepted drawers stack up to four, a list
inside a drawer stays in the drawer, `app.InterceptFrom` takes more
than one origin, and a save in a drawer returns to the page under it.
`data-hui-leave-guard` on a form asks before unsaved edits are lost.

## The admin, rebuilt on entityui

`battery/admin` draws entity screens through `framework/entityui`:

| Old route | New route |
| --- | --- |
| `/admin/e/<entity>` | `/admin/entities/<entity>` |
| `/admin/e/<entity>/new` | `/admin/entities/<entity>/create` |
| `/admin/e/<entity>/view/<id>`, `/edit/<id>` | `/admin/entities/<entity>/<id>` (the record page holds the edit form) |
| entity writes | `POST /admin/api/<entity>` |
| `/admin/admin.css` | none: the admin ships no CSS |
| `POST /admin/rbac/_revoke` | `POST /admin/rbac/_permissions` (uncheck a box and save) |

What an app must do:

- Set `Config.UI` to the app's one entity UI when the admin exposes any
  entity: `appUI := fwApp.EntityUI(entityui.Extensions{})`. Without it
  `Init` fails naming the exposed entities.
- Mount a UI host (`framework.NewUIHostApp(uihost.New(site), ...)`)
  before `app.RegisterBattery(admin.New(cfg))`. Every admin page renders
  through it, a queue- or audit-only admin included.
- Move column and label choices to each entity's `Display`.

New in the admin: a dashboard with a figures strip (`Config.Metrics`),
a count card per entity, failed jobs, recent activity written as
sentences and a Needs attention panel (`Config.Attention`); a command
palette over pages, entities and records; `Config.Pages`, `Cards`,
`Links` and `Commands` for the app's own entries; saved views stored in
`admin_saved_views` (`Config.SavedViews`); bulk runs on `battery/queue`
(`admin.NewBulkJobs`); an account page; a Roles page drawn as a grid of
permissions by roles; and pagers on the Jobs, Audit log and User roles
pages. [Admin UI](admin.md) is the reference.

## Prefixes: `data-cui-*` and `cui-*` belong to the kernel

Every class and attribute now carries the prefix of the tree that
defines it:

| Prefix | Owner |
| --- | --- |
| `data-cui-*`, `cui-*` | the `core-ui` kernel |
| `data-hui-*` | `framework/headless` |
| `fui-*` | `framework/ui` |

Every `data-fui-*` key the runtime read is renamed: `data-fui-rpc` is
`data-cui-rpc`, and the same for `-open`, `-signal`, `-comp`,
`-toast-stack`, `-window-drag` and the rest, with their dataset reads
(`dataset.fuiRpc` is `dataset.cuiRpc`). The classes the kernel mints
moved to `cui-*`: `cui-widget`, `cui-slot`, `cui-pos-*`, `cui-panel`,
`cui-backdrop`, `cui-hidden`, `cui-reveal`, `cui-dropdown`, `cui-flash`,
`cui-loading`, `cui-css-*`, `cui-vt-*`, `cui-intercept`,
`cui-theme-<hash>`, the enter animations, and the singletons
`#cui-backtotop-sentinel`, `#cui-nav-toast` and `#cui-toast-fallback`.

These keep `data-fui-*`, because the framework's own modules own them:
`data-fui-lightbox*`, `data-fui-zoomed`, `data-fui-dropzone-preview*`,
`data-fui-pane*`, `data-fui-z-tier`, `data-fui-network-retry-*`,
`data-fui-plugin*` and `data-fui-page-loading`. The ownership table is
`core-ui/ARCHITECTURE.md` ("Who owns which prefix").

The kernel and headless modules no longer name kit classes:

| Old | New |
| --- | --- |
| `.fui-copied` on a copied control | `[data-hui-copy-state="done"]` |
| `fui-field__error` placed by the form-errors module | the `data-hui-field` / `data-hui-field-error` / `data-hui-choice` hooks |
| the `fui-toast-stack-auto` singleton | a stack the host mounts (`ui.ToastSlot` or `uihost.DefaultToastStack`) |
| `is-leaving` on a dismissed toast | `data-hui-toast-leaving` |
| the kernel's `searchinput` module | `framework/ui`'s own `searchinput.js` |

A tab still running the old runtime after a deploy gets a full page
load instead of a swap it cannot read: the kernel sends its markup
generation as `X-Gofastr-Markup`, and the host answers a mismatch with a
409 the old runtime turns into a reload.

## Docs MCP tools are opt-in

`framework.WithMCPIntrospection()` no longer registers
`framework_docs_list`, `framework_docs_get` and `framework_docs_search`,
and neither does `gofastr dev`. An app whose agents read the framework
docs over `/mcp` adds the new option:

```go
app := framework.NewApp(
    framework.WithMCPIntrospection(),
    framework.WithMCPTools(mcptools.Register), // github.com/DonaldMurillo/gofastr/framework/docs/mcptools
)
```

`WithMCPTools` runs any `func(*mcp.Server) error` against the app's MCP
server during init. Package framework no longer imports the docs
corpus, so an app that leaves the option out keeps the docs out of its
binary.

## Removed and changed APIs

| Removed or changed | Use instead |
| --- | --- |
| `framework/ui/resource` (`Config`, `Registry`, island routes, `PublicIsland`) | `App.EntityUI` and `appUI.List`, `Record`, `Create`; labels and columns on `Display`, transitions on `States`, a custom data source as an `Extensions.Entities` override |
| `admin.Config.Theme`, `Config.FontFaceCSS` | the UI host's theme and fonts |
| `admin.Config.EntityListLimit` | `Display.PageSizes` on the entity |
| `admin.Config.Secret` | none: the admin signs no form flash |
| `admin.Battery.RegisterRoutes` | `app.RegisterBattery(admin.New(cfg))` mounts every route |
| `admin.SortDirOf(v)` | an inline `v == "desc"` |
| `i18nui.LabelForField` | `i18nui.FieldLabel(ctx, tr, entity, field, label)`; catalog key `entity.<entity>.fields.<field>.label` (was `entity.<entity>.field.<field>`) |
| `ui.WorkbenchConfig.RailWidth` as a CSS length | `ui.WorkbenchRailNarrow` (240px), unset (320px) or `ui.WorkbenchRailWide` (480px) |
| `ui.CopyButtonConfig.Target` as a selector | an element id; a leading `#` is accepted |
| `app.App.RenderOverlayResult(ctx, path, as)` | `RenderOverlayResult(ctx, path, origin, as)`; pass `""` when there is no origin |
| `queue.Browsable.ListJobs(ctx, status, limit)` | `ListJobs(ctx, status, limit, offset)`; pass `0` for the first page |
| `framework_docs_*` tools from `WithMCPIntrospection` | `framework.WithMCPTools(mcptools.Register)` |
| a `ShadowSet` without `XS` | add `XS: style.Shadow{Name: "xs", Value: "0 1px 2px 0 rgba(0,0,0,0.05)"}` |

### Blueprint

| Removed | Use instead |
| --- | --- |
| screen `filters:` | the entity's `display: facets:` |
| screen `search:` | the entity's `search_fields:` |
| screen `transitions:` | the entity's `states:` block |
| screen `island:`, `widget:` | nothing: lists are query-param pages |
| `entity_create` block | `create: true` on the entity's `entity_list`; the create screen is `<list>/create` (was `<list>/new`) |
| `entity_edit` block, `<detail>/edit` | the `entity_detail` screen, which holds the edit form |

An entity detail screen must sit at `<list route>/{id}` under one of the
entity's list screens; validation names the expected route. An old
screen `transitions:` names no `From`, so write `advisory: true` on the
`states:` block and give each move a `from:` list, then drop `advisory:`
to let the CRUD handler enforce it. Regenerate with `gofastr generate
--force`; generated apps draw entity screens through entityui from an
owned `extensions.go`.

### Filters

| Change | What to do |
| --- | --- |
| `like` works only on String, Text, Enum and UUID columns; `gt`, `gte`, `lt`, `lte` are refused on Bool and JSON | send `eq` or `in` for those columns; the request answers 400 otherwise |
| `crud.ListOptions.Filters` is validated: an unknown, Hidden or NoQuery column, or an operator the type refuses, returns an error | name queryable columns, or use `ListOptions.Where` with a `filter.Predicate` |

## Security changes that break callers

These came from the 2026-10-04 audit of the request path. Each has a
`*_security_test.go` that failed before the fix.

| Change | What to do |
| --- | --- |
| `EntityUserStore` records `email_verified` (FALSE for existing rows). A magic link or completed reset on an unverified account removes its sessions, API tokens and OAuth links, and a magic link also clears its password | mark users your own code creates verified with `EmailVerifier.MarkEmailVerified` right after `CreateUser` |
| OAuth auto-links a new provider only into a verified account (409 otherwise) | as above |
| `GET /auth/verify-email` needs a full session of the account that asked for the link | tell users to open the link in the browser they signed up in |
| `RequireTwoFA` judges the request principal and refuses JWT and API-token principals | give those clients a session-cookie client or a route without the step-up |
| A JSON or in-process write to an `Image` or `File` field accepts a key only when the same request uploaded it, host code passed it through `crud.WithUploadedKeys`, or the row already holds it | upload through the entity's multipart route, or wrap the context in `crud.WithUploadedKeys(ctx, keys...)` with keys your code saved |
| `upload.Handler` caps a body at 32 MiB (`upload.DefaultMaxSize`) when `Config.MaxSize` is unset | set `MaxSize` for larger uploads |
| `X-Forwarded-For` is read from the right: the client is the rightmost hop that is not a trusted proxy | list every proxy tier in `TrustedProxies` |
| `/.debug` endpoints need the admin role | grant the role, or install `framework.WithDebugAuthorize(fn)` |
| A server action runs its screen's `Policy` first | hand-written calls to `/__gofastr/action` send the page path as `page` |
| An embed grant reaches only the runtime routes uihost mounts, and its scopes bind includes, relation filters and cascade writes | declare `<table>:read` / `<table>:write` scopes for the relations a grant uses |
| A scoped API token needs `<table>:read` for each include and relation filter and `<table>:write` for each cascade child | mint tokens with those scopes |
| A `CRUD: false` entity (`auth.UserEntityConfig` included) is unreachable through `?include=`, `?rel.field=` and cascade writes | expose what clients need through a custom route |

Other fixes from the same audit change no caller code: `_batch` update
and delete ask the Decider per record; `UpsertOne` enforces owner,
tenant and soft-delete scope in the write; write responses respect
`ReadScope`; `?include=` serves only declared columns; open streams
re-check the principal on every delivery; 2FA guesses are capped per
pending session; the SQL rate-limit store admits atomically;
`HMACSHA256Verifier` refuses an empty secret; a revoked capability
survives a `GrantStore` reload; SSRF-guarded transports ignore
`HTTP(S)_PROXY`; API tokens are revoked on password reset; and uihost
escapes `data-cui-fill` and extra-script `src` attributes. Entity MCP
tools list only for callers whose permissions allow them, and
`crud.WithReadHooks` applies the `BeforeList` and `BeforeGet` scopes to
in-process reads.

## Other additions

- **Components.** `ui.Dropdown`, `ui.Selection` (a bulk bar over rows),
  `ui.FormFrame` (form with a side column), `ui.DrawerBar`, `ui.Picker`
  (pick a record from a server-searched list), `ui.InlineEdit`,
  `ui.ShortcutSheet`, `ui.StatStrip`, `ui.SidebarBrand`, `ui.Thumbnail`,
  `ui.ShortID`, `ui.InlineCode`, `ui.Rating`, `ui.DateTimeField`,
  `ui.SegmentedLinks`, `ui.FilterRows` and `ui.LinkTitle`, plus
  options on existing ones (`ButtonConfig.Icon`, `MenuConfig.Avatar`,
  `DataTable` `Responsive: ui.ResponsiveRows`, `Column.Fit`,
  `Column.SelectAll`) and new icons.
- **Confirm dialog.** `data-cui-confirm` asks in the kit's themed
  `<dialog>` instead of `window.confirm`. Tests that stubbed
  `window.confirm` click `[data-cui-confirm-part="accept"]`.
- **Toasts** rise in the bottom-right corner by default and can carry
  one action button (`interactive.Action.OnSuccessToastAction`).
- **Constraint conflicts name the field**: a 409 carries `fields`, so a
  form marks the control "is already in use".
- **`battery/desktop` (experimental)** runs an app in the OS WebView
  from a `CGO_ENABLED=0` binary on macOS/arm64 and Windows/amd64, with
  `gofastr desktop run|build|types`.
- **Tooling.** `gofastr docs serve` browses the docs site offline;
  `gofastr generate screen <name> --from-a11y=<file>` builds a screen
  from an aria snapshot; `gofastr upgrade` matches surviving symbols by
  type shape and skips minified scripts; `go run ./cmd/affected` scopes
  every local test gate to the changed packages.
- **i18n.** `i18nui.Pseudo` and `i18nui.AddPseudo` pseudo-localize
  strings, and Meridian ships a Spanish catalog.

## Fixes

Tables and lists:

- An empty `ui.DataTable` keeps its empty state in view on a phone, and
  a table wider than a phone no longer widens the page.
- DataTable body rows are 52px as their rule says, a sort header's label
  lines up with its column, and cards mode no longer clips or overflows.
- Sorting or paging a list no longer jumps to the top of the page.
- Deleting a row others reference answers 409, not 500, and an empty
  list with hooks or includes answers `"data": []`, not `null`.

Menus, drawers and forms:

- Choosing a `ui.Menu` command row closes the menu, and a menu near the
  viewport edge or inside a table opens in full.
- A link with a query opens its intercepting route as a drawer, and a
  drawer loads its components' stylesheets.
- A blank form field no longer fails validation by type, a `Float` field
  takes decimal text, and a refused form submission is no longer silent.
- `ui.PasswordInput` turns red on a refused submit, and an invalid
  control inside `ui.InputGroup` marks the whole group.

Theme and components:

- A theme can set any radius step to 0.
- Component sheets reference only tokens the theme defines, and a
  partial `theme.Components` map still gets every option's floor.
- `data-cui-flash-on-update` flashes: no sheet defined `.cui-flash`.
- The theme pill answers the arrow keys, a checked `ui.Checkbox` draws
  its check mark, and `ui.BackToTop` anchors bottom-right by default.

Runtime and server:

- A screen that panics is a logged 500, never a silent 404.
- `App.Shutdown` no longer stalls on a connection that never sent a
  request.
- A panic in a battery or plugin `Init`, or a start, ready or seed hook,
  logs its stack.
- One SSE stream per page, and leaving a page mid-action no longer
  caches a dead control (both since v0.86.0).
- `query.UpdateBuilder` binds its args in placeholder order.
- `EnsureAuditTable` adds missing columns on SQLite.
- `battery/rtc`: a late join mirror no longer kicks a peer that already
  moved, and a socket never hears a join for a peer its snapshot
  already lists.
- `textsafe.SanitizeControlBytes` keeps non-ASCII text intact.

Generator and CLI:

- `gofastr generate screen` runs again (v0.86.0 dropped it).
- `gofastr gen styles` writes only inside the project and accepts a
  sheet with a UTF-8 byte-order mark.
- GOFASTR1810 bars every kit class prefix, not only `.fui-*`.
- `gofastr pack` without `-o` keeps stdout clean, and `gofastr upgrade`
  scrubs what it prints.
- A static export writes `_headers` again.
- The generated admin creates its audit table at boot, and the
  generated `.gitignore` covers `bin/` and SQLite files.
- The v0.86.0 upgrade notes name every removed export.

Security hardening (audit findings 83–100):

- Forgot-password sends its email from a bounded queue on eight
  workers, off the request path, so a known address answers as fast as
  an unknown one. `PasswordResetPlugin.OnStop` stops the workers. A
  full queue or a stopped plugin drops the delivery with a warning.
- Expired sessions and magic-link tokens are swept on later writes, at
  most once per interval, and the OAuth state map has a hard ceiling:
  at capacity, callbacks fail closed until expired states are cleared.
- `MagicLinkPlugin` starts its token reaper once, and an `OnStart`
  after `OnStop` is refused.
- `a2a.Config.MaxConcurrentRunsPerOwner` caps one owner's concurrent
  runs, resumes included (0 = 16, negative = no cap).
- `cache.GetOrSet` reports a backend error at either read instead of
  running the loader, and a value-typed cache no longer shares another
  instance's fill.
- The Redis queue's `Dequeue` gives each restore or quarantine write
  its own deadline, so a popped job is never left in no list.
- `battery/relay` checks every address a name resolves to at dial
  time, closing the DNS-rebinding path, and no longer dials through an
  environment proxy.
- `SSEWriter.SetRetry` takes seconds and writes milliseconds, capped
  at `math.MaxInt64`.
- Cron runs when either a restricted day-of-month or a restricted
  weekday matches, as standard cron does, and a huge step no longer
  overflows into unrelated schedule bits.
- `ratelimit.NamespaceScope` encodes `|` and `%` so a scope and a key
  can no longer run together.
- `credstore` keys encode provider and account separately. An
  ambiguous legacy key returns `credstore.ErrAmbiguousLegacyKey` until
  the credential is entered again.

## Common mistakes

- **Bumping `go.mod` before running the report.** `gofastr upgrade` then
  reads v0.87.0 as the current release and finds nothing. Pass `--from`
  with the old release.
- **Shipping on Go 1.27.0.** It builds, and it carries the thirteen
  standard-library advisories 1.27.2 fixes.
- **A `theme.go` without `Shadows.XS`.** It panics at boot with
  `Theme.Shadows.XS: Shadow.Value is empty`. Add the step, or start from
  `style.DefaultTheme()`.
- **Drawing text in `var(--color-secondary)`.** It is a light fill now.
  Use `var(--color-text-muted)`.
- **Renaming `data-fui-lightbox` or `data-fui-pane-*` to `data-cui-*`.**
  Those keys stay `data-fui-*`; only the kernel's keys moved.
- **Calling `App.EntityUI` twice.** The second call panics. Build one UI
  after every entity registers and hand it to the admin as `Config.UI`.
- **Registering the admin on an app with no UI host.** `Init` fails.
  Build the app with `framework.NewUIHostApp`.
- **Seeding an admin user without marking it verified.** Its first magic
  link clears its password.
- **Leaving `WithMCPTools(mcptools.Register)` out and expecting the docs
  tools.** `WithMCPIntrospection` alone no longer installs them.
