# Blueprints

A `gofastr.yml` blueprint is an optional way to scaffold a GoFastr app:
declare your entities, screens, nav, seed data, and endpoint/middleware
stubs in one file, then generate owned Go from it. The blueprint is not
a source of truth: after scaffolding, the generated Go is canonical and
you can delete `gofastr.yml`. Generate with:

```bash
gofastr validate gofastr.yml
gofastr generate --from=gofastr.yml
gofastr generate --from=blueprints/ --dry-run --json
```

Blueprints are not runtime declarations: the CLI reads `.yml`, `.yaml`, or
`.json` blueprint files (or a directory of them), validates them, and scaffolds
owned Go into an idiomatic, module-root layout by default — a flat
`package main` at the root (`main.go`, `app.go`, `screens_register.go`, one
`screen_<name>.go` per screen, and, when needed,
`resource.go`/`stubs.go`/`resource_test.go`) plus the `entities/`
package (set `--out=<dir>` or `app.output_dir` to scaffold into a subpackage
instead). `generate` is one-shot: it refuses to overwrite an existing project
(pass `--force`), because the emitted code is yours to own. `--add` is the
additive alternative: it writes only the new files from a partial yml, never
overwriting — see [Additive generation](#additive-generation---add). At runtime your app
registers the **generated** entity package (`entities.RegisterAll(app)`) — there
is no file-based runtime loader. The blueprint's `entities:` list uses the same
entity shape and field types documented in
[Entity Declarations](entity-declarations.md).

The blueprint root keys are `app`, `entities`, `screens`, `nav`, `seed`,
`endpoints`, `middleware`, `plugins`, `helpers`, and `isolation`.

Blueprints are separate from general codegen config. `gofastr generate
--from=gofastr.yml` means "treat this file as a blueprint." Plain
`gofastr generate` discovers `gofastr.codegen.yml` / `.yaml`, or a
`codegen:` section in `gofastr.yml`, and runs the configurable codegen engine.
Use [Codegen](codegen.md) when you want arbitrary configured generators or
external extensions rather than a full app blueprint.

## What the blueprint generates vs. what you hand-write

The blueprint scaffolds a working app, then gets out of the way. This table
is what it produces *today* — everything else is ordinary owned Go you write
against the framework (the emitted code is the starting point, not a ceiling).

| Area | Generated from the blueprint | Hand-written Go |
|---|---|---|
| **Marketing pages** | `hero`, `page_header`, `card`, `pricing`, `stat_row`, `callout`, `markdown`, `link_button` blocks composed from `framework/ui`; auth-aware header | Any bespoke section not in the block catalog |
| **Auth** | Register/login/logout/me wiring, session middleware, `login_form`/`signup_form` screens, guest-only redirects, bootstrap admin | Custom auth flows (SSO, MFA policy, email verification UX) |
| **Dashboard stat/chart blocks** | `stat_card` (count/sum), `bar_chart`/`pie_chart`/`line_chart` bound to an entity `source: {entity, group_by}` | Custom metrics, multi-entity joins, computed series |
| **Entity list** | Table with server-side **search**, **sort**, **pagination**, and **facet filters** (`filters:` → a `ui.FilterToolbar` of enum/bool/relation facets); optional "New" button | Filtering on computed/derived columns, range/date filters, multi-select facets |
| **Entity detail / form** | Read view + create/edit `<form>` (enum & relation `<select>`s), status-transition buttons | Multi-step wizards, custom field widgets, cross-field validation UX |
| **Child-collection islands** | — (not generated) | Hand-written — see [Interactive patterns](interactive-patterns.md) for the island RPC recipe |
| **Admin back-office** | Full CRUD admin over every entity (`app.admin`), role-gated | Custom admin actions beyond CRUD |
| **Seed data** | Explicit `rows:` + auto-generated `count:`/`weights:` demo rows | Fixtures with complex relations / business invariants |
| **Theme + dark mode** | Color + font tokens, dark sub-map, header theme toggle | Per-component theme overrides |
| **Fonts** | Self-hosted webfonts fetched to `static/fonts/` at generate time | Supplying `.woff2` files when generating offline |

`app.theme` can override canonical color tokens for generated UI apps. Supported
keys are `primary`, `primary-fg`, `secondary`, `background`, `surface`,
`surface-soft`, `text`, `text-muted`, `text-subtle`, `border`,
`border-strong`, `accent`, `success`, `warning`, `danger`, and `info`.
It also accepts the font tokens `font_heading`, `font_body`, and
`font_display` — named Google Fonts families that drive the `--font-*`
tokens. An `app.theme.dark` sub-map overrides any of the same color
tokens for the dark scheme (the header's theme toggle flips to it).
Generated apps call `site.WithTheme(...)`, so the values are emitted
through `/__gofastr/app.css` as computed CSS custom properties.

**Fonts are self-hosted, not CDN-linked.** The generated app ships a
strict Content-Security-Policy (`default-src 'self'`) that deliberately
blocks the Google Fonts CDN, so a `<link>` to `fonts.googleapis.com`
would be refused at runtime. Instead, `generate` **fetches** each named
family's latin `woff2` subset at generate time and writes it to
`static/fonts/<slug>.woff2` (the `<slug>` is the lower-cased,
hyphenated family name, e.g. `Bricolage Grotesque` →
`bricolage-grotesque.woff2`). The emitted `fontFaceCSS` `@font-face`
rules point at `/fonts/<slug>.woff2`, which the app serves from that
static dir — so a named font just *works* in a fresh app with no manual
step. When no `static_dir` is set but the theme names a font, the
generator defaults it to `static/`.

Generation may run offline. If the fetch fails, the app is still emitted
(it falls back to system fonts) but `generate` prints a **loud warning**
naming the exact `static/fonts/*.woff2` files you must supply yourself.
The generated `main.go` also boot-checks for those files and logs a
warning at startup if one is missing — there is no silent 404 path.

### Entity APIs live under `/api`; screens own the bare path

`app.api_prefix` (default `"api"`) is the URL prefix every entity's JSON
CRUD API mounts under, so `GET /api/posts` returns JSON while the bare
`/posts` path is free for an HTML `screen`. Without this, a `screen` routed at
`/posts` would collide with — and be shadowed by — the auto-generated CRUD
handler at the same path. MCP tools and the OpenAPI spec follow the prefix
automatically. Set `api_prefix: ""` to mount entity APIs at the bare
`/<table>` (the historical behavior); a screen must then not reuse an entity
path. The generated `entity_list` / `entity_form` / `entity_detail` blocks
fetch and submit against `<api_prefix>/<entity>` accordingly.

## YAML subset

GoFastr ships its own small YAML parser in `core/yaml`; no external YAML parser
is required. Supported syntax:

- indentation-based maps and lists
- strings, quoted strings, booleans, numbers, and `null`
- inline scalar lists such as `[draft, published]`
- comments starting with `#` outside quoted strings

Unsupported syntax fails with line/column errors. Inline maps, anchors, aliases,
block scalars, tabs for indentation, and mixed advanced YAML forms are not part
of the blueprint format.

**No inline (flow-style) maps.** `{type: relation, to: users}` is invalid —
every map must be written with indented `key: value` lines:

```yaml
# Invalid — inline map
- {name: author_id, type: relation, to: users}

# Valid — indented map
- name: author_id
  type: relation
  to: users
```

## Shape

```yaml
app:
  name: Demo
  description: Freight quotes in seconds.   # site meta description (derived from entities when omitted)
  base_url: https://demo.example            # sitemap origin fallback; APP_BASE_URL env wins
  module: example.com/demo
  theme:
    background: "#101820"
    primary: "#F2AA4C"
    text: "#F7F4EA"
  db:
    driver: sqlite
    url: file:demo.db

entities:
  - name: users
    fields:
      - name: email
        type: string
        required: true
        unique: true

  - name: posts
    crud: true
    mcp: true
    access:
      read: posts:read
      create: posts:write
      update: posts:write
      delete: posts:admin
    cursor_field: id
    cursor_fields: [created_at, id]
    properties:
      label: Posts
      icon: newspaper
    indices:
      - name: idx_posts_status
        columns: [status]
    fields:
      - name: title
        type: string
        required: true
        max: 120
      - name: status
        type: enum
        values: [draft, published]
      - name: author_id
        type: relation
        to: users
    relations:
      - type: belongs_to
        name: author
        entity: users
        foreign_key: author_id
    endpoints:
      - name: publish_post
        method: POST
        path: "{id}/publish"
        handler: publishPost

screens:
  - name: home
    route: /
    title: Home
    description: Demo homepage
    body:
      - type: heading
        level: 1
        text: Demo
      - type: paragraph
        text: Generated from YAML.
      - type: link
        text: Docs
        href: /docs/
      - kind: section
        props:
          class: details-section
        children:
          - kind: heading
            props:
              level: 3
              text: Details
      - kind: div
        island: live_status
        props:
          class: live-status
        children:
          - kind: paragraph
            props:
              text: Island content
      - kind: button
        widget: save_button
        props:
          id: save-action
          text: Save
        actions:
          - name: save_click
            event: click
            client_js: "G.updateText('[data-action-result]', 'Saved')"
      - kind: paragraph
        props:
          text: Waiting
          data-action-result: true

endpoints:
  - name: health_check
    method: GET
    path: /health
    handler: healthCheck

middleware:
  - request_logger

plugins:
  - name: analytics

helpers:
  - name: normalize_slug
```

## Generated output

Entity declarations reuse the existing `entities` package generator, including
`owner_field`, `cross_owner_read` (role-based cross-owner read; requires
`owner_field`), `search_fields` (multi-field free-text `?q=` search),
per-operation `access` RBAC, cursor settings, indices, and `properties`.
The `access` map (keys `read`, `create`, `update`, `delete`;
each value a permission string) is emitted as `Access:
framework.AccessControl{...}` in the generated registration — see
[entity-declarations](entity-declarations.md) for the semantics and
[access-control](access-control.md) for wiring roles + policy. Entity-owned endpoints and top-level
endpoints generate Go handler stubs (in `stubs.go`) plus router registration.

When `app.module` is present, blueprint generation also emits a
`main.go`: a runnable app entrypoint that opens the configured SQLite
database, registers generated entities, wires generated
screens/endpoints/middleware/plugins through `RegisterGenerated` (in
`app.go`, same `package main`), mounts the UI host, and serves
`app.static_dir` through the generated UI host.

The generated app mounts MCP by default: `framework.WithMCP()` mounts
`/mcp` (POST JSON-RPC + GET SSE) plus the discovery well-knowns
(`/mcp/server-card`, `/.well-known/mcp/server-card.json`,
`/.well-known/mcp/catalog.json`), serving the per-entity CRUD tools
(`mcp: true`) alongside the `framework.WithMCPIntrospection()` set
(`app_routes`, `app_readiness`, `framework_docs_search`, …). Those tools
are read-only but let a caller read the app's routes and config — remove
`WithMCPIntrospection()` from `main.go` if `/mcp` is reachable by
untrusted callers in production.

The generated `main.go` also registers battery/log with its canonical
zero config: per-app file sink, access log, panic recovery. Under
`gofastr dev` the framework additionally auto-enables the mutating
control tools (`app_module_enable` / `app_module_disable`), the log
debug tools (`log_recent`, `log_filter`, `log_metrics`,
`log_set_level`), and MCP data tools for every CRUD entity (even
without `mcp: true`), so a connected agent can read recent requests
and errors, read and write app data, and toggle modules during
development — none of which registers on a production boot. See
[agent-ready](agent-ready.md) and [dev-livereload](dev-livereload.md).

### Generated screen files

Screens are emitted one file per screen, behind a fixed seam — never as one
aggregated `screens.go`:

- `screens_register.go` — a fixed seam that names **no** screen, entity, or
  app. It defines `screenRegistrar{order int, fn func(fwApp *framework.App,
  site *app.App, db *sql.DB)}`, a `screenRegistrars` slice, and
  `mountGenerated(...)`, which sorts the registrars by `order` and runs them.
  Byte-identical for any screen/entity count. Add a screen by dropping in a new
  `screen_<name>.go`; never edit the seam.
- `screen_<snake>.go` — one per authored non-CRUD screen (marketing pages,
  dashboards, auth forms): the screen struct, its `Screen*` methods, `Render`,
  a `mount<Screen>` func holding the `site.Register`/`site.RegisterScreen`
  call, and an `init()` that appends one
  `screenRegistrars = append(screenRegistrars, screenRegistrar{order: N, fn: mount<Screen>})`.
- `screen_<entity>_crud.go` — one per entity with CRUD screens (or referenced
  by a data source): that entity's list/detail/new/edit screens, their mount
  funcs, **and** its `appResources["<entity>"] = ResourceConfig{...}` wiring
  inside the primary mount func (it needs `fwApp`). An entity's resource
  wiring lives here, never in `app.go`.

`app.go` still owns layouts, nav, theme, `authPolicy`/`guestPolicy`, auth,
toasts, and endpoints. `appLayout`/`marketingLayout` are package-level vars
assigned in `RegisterGenerated`, which calls `mountGenerated(fwApp, site, db)`
and names no screen type or `appResources` entry. The generated screen order is
recovered by `gofastr pack` from each file's `screenRegistrar{order: …}` — see
[Packing](#packing-gofastr-pack-the-inverse-of-generate).


## Additive generation (`--add`)

`gofastr generate --from=<yml> --add` is additive: it reads *any partial yml*
(e.g. just two new entities, or a couple of new screens — not the canonical
`gofastr.yml`) and emits only
the new full-fidelity pieces into an existing project, never touching owned
files. It never overwrites anything — if a target file already exists, it is
skipped (and listed). In an empty directory, `--add` produces the same output
as plain `generate`.

```bash
# Add two entities to an existing project from a partial yml.
gofastr generate --from=additions.yml --add
```

Partial ymls are legal: an entities-only (or screens-only) fragment omits
`app.module`, so the module is derived from the enclosing `go.mod` exactly as
in a full generate. This means `main.go`, `app.go`, `entities/register.go`, and
`screens_register.go` are still rendered (the generator needs them for a valid
package) — but since they already exist in your project, `--add` skips them.
Every generated app ships both registration seams and their call sites
(`entities.RegisterAll` in `main.go`, `mountGenerated` in `app.go`) even when
it has no entities or screens yet, so a later `--add` registers and mounts its
pieces without editing any owned file. If you removed one of those calls,
`--add` warns with the exact line to restore.
Only the new files are written: `entities/<name>.go` for new entities,
`screen_<name>.go` for new authored screens, and `screen_<entity>_crud.go` for
an entity that gains CRUD screens.

**Entity order continuity.** `--add` reads the existing `entities/` directory
and assigns the new entities declaration orders that continue after the
existing set (e.g. if the project has entities at orders 0 and 1, the first
added entity gets order 2). This keeps `RegisterAll`'s declaration order and
`gofastr pack`'s order recovery coherent across the union.

**Screen order continuity.** `--add` reads the existing `screen_<name>.go`
files and assigns the new screens `screenRegistrar{order: …}` values that
continue after the project's existing max screen order — mirroring entity
order continuity, so `mountGenerated`'s sort and `gofastr pack`'s order
recovery stay coherent across the union.

**Referencing existing entities.** A screens-only fragment may reference an
entity that lives in the *project* rather than in the fragment — e.g. an
`entity_list` over an entity you generated last month. `--add` reads the
project's `entities/` declarations and validates the fragment against the
union, so there is no need to re-declare the entity just to build UI on it.

**Re-declaring an existing entity or screen** is a no-op: the file already
exists, so it lands in the skipped list and the existing file is left
untouched. There is no diffing or merging — `--add` only writes files that
don't exist.

**The `client.go` caveat.** `entities/client/client.go` is an aggregate over
*all* entities. When it already exists and `--add` introduces new entities,
the generated client was not updated and does not include them. `--add` prints
a warning naming the stale file; regenerate it from a full blueprint with
`--force`, or extend it by hand.

**Pre-0.15 layouts.** `--add` requires the per-piece layout on both sides: the
per-entity `entities/` files and the per-screen `screen_<name>.go` files. If
`entities/register.go` holds the `app.Entity` calls inline (the aggregated
entity layout older generators emitted), or the project still uses one
aggregated `screens.go` instead of the per-screen files, `--add` refuses
rather than drop in a file the package can't compile with. Recover your
blueprint with `gofastr pack` (which still reads both legacy layouts as a
fallback), merge the new pieces into it, and regenerate with `--force`.

### Quick scaffolds (`generate entity|screen`)

For an Angular-schematics-style fast stub with **no yml at all**, use the
scaffold subcommands. They synthesize a minimal one-piece blueprint fragment
in memory and route it through the *same* additive machinery as `--add`, so
skip-existing, entity/screen order continuity, union validation, the
legacy-layout refusal, the `client.go` staleness warning, and the
`--dry-run`/`--json` output shapes all hold for free:

```bash
gofastr generate entity posts        # entities/posts.go + a placeholder field
gofastr generate screen contact      # screen_contact.go at /contact
```

Each flag accepts `--out=DIR`, `--dry-run`, and `--json`. `--force` and
`--add` are rejected — scaffolding *is* additive (it never overwrites), so
those flags have no meaning here. The stub is a deliberate starting point you
immediately edit:

* `generate entity <name>` emits one entity with a single placeholder `name`
  field (a required string) — rename it and add the rest. CRUD stays default,
  so the entity also gains a JSON API.
* `generate screen <name>` emits one screen at `/<kebab-name>` with a heading
  and a stub paragraph — replace its `Render`.

Scaffolds and yml fragments are complementary, not competing: the **stub** is
basic scaffolding (one thing, fast); a blueprint **yml** is full intention
(fields, relations, RBAC, theme, nav, seed, multi-screen layouts). For
anything beyond a single placeholder, write a partial yml and use `--add`.

`generate theme <name>` is intentionally not provided: theme tokens live in
the owned `app.go` (there is no new-file representation to scaffold).

## Packing: `gofastr pack` (lossy app→blueprint snapshot)

`gofastr pack [app-dir]` reconstructs a best-effort `gofastr.yml` from a generated
app's Go source — a lossy snapshot, not a round-trip inverse of `gofastr
generate`. It reads the real artifacts via the
Go AST (it does **not** stash a manifest): the per-entity `entities/<name>.go`
files for entities (falling back to a legacy `entities/register.go`),
`app.go` for app config + theme + auth/admin + nav, `stubs.go`
for seed, and the per-screen `screen_<name>.go` files for screens (reading
each file's `screenRegistrar{order: …}` to recover authored order, with a
legacy aggregated `screens.go` fallback for apps older generators emitted),
reversing the emitted `framework/ui` grammar (`ui.Hero` → `hero`,
`appResources["x"].…List(ctx)` → `entity_list`, and so on). The result
prints to stdout, or to a file with `-o`:

```sh
gofastr pack examples/meridian -o recovered.yml
```

Synthesized `/new` + `/{id}/edit` form screens are dropped (they weren't
authored). Generate and pack are a matched inverse pair for the declarative
pieces — `app`, `entities`, `screens`, `nav`, and `seed` — so the invariant
`parse(yml)` ≡ `parse(pack(generate(yml)))` holds for a blueprint of those
constructs (modulo comments + formatting); the Meridian example round-trips
exactly, gated by a test. When you add a new construct to that set, teach
**both** the generator and pack, or that test fails.

`endpoints`, `middleware`, `plugins`, and `helpers` are **not** recovered by
pack: the generator emits them as `stubs.go` signatures you fill with your own
Go, and pack cannot reverse a hand-written handler body back into a blueprint
declaration. If your `gofastr.yml` declares any of these, keep the YAML — pack
reconstructs the rest of the app around your owned stub code but will not
re-emit their declarations.

### Data blocks (`entity_list`, `entity_form`, `entity_detail`)

A top-level `entity_list` or `entity_detail` makes its screen a **server-rendered**
(request-time) screen that queries the entity's `CrudHandler` and composes real
`framework/ui` components — no client-side fetch. The generator emits an owned
engine at `resource.go` (and a `resource_test.go`) that the
screens call:

- `entity_list` → `ui.PageHeader` + `ui.SearchInput` + `ui.DataTable` +
  `ui.Pagination`/`ui.EmptyState`, with **humanized headers** ("Generic Name"),
  formatted cells (bool → Yes/No status badge, enum → status badge, `decimal` →
  `$` money, dates trimmed), and **relation columns resolved to the related
  record's display name** (not the raw id). Search/sort/pagination are
  URL-driven and run server-side. `fields:` picks/orders the columns; `search:`
  names the LIKE-search field; `limit:` sets the page size.
- **`filters:`** — an optional list of columns to expose as **facet filters**
  above the table via `ui.FilterToolbar` (one responsive, URL-driven GET form
  that also absorbs the search box, so the screen is a single form, never two).
  Each column must be an **enum**, **bool**, or **relation** — anything else is
  a blueprint error. Enums render as pills when they hold ≤4 short values and as
  a `<select>` otherwise; bools render as Yes/No pills; relations render as a
  `<select>` of the related records' display names. A selected facet applies a
  server-side equality filter that composes with search, sort, and pagination
  (sort-header and page links preserve the active facets; applying a facet
  resets to page 1). Filtering is **explicit** — omit `filters:` and the list
  renders exactly as before, with no toolbar. Example:
  `filters: [status, assignee_id]`.
- `entity_detail` reads the route `{id}`, loads the record server-side, and
  renders the fields with the same formatting + relation resolution.
- `entity_form` renders a `<form data-fui-rpc="<api_prefix>/<entity>">` (enum →
  `<select>` of values; relation → `<select>` populated from the related entity).

The generated `ResourceConfig` registry (`appResources`) is populated in each
entity's `screen_<entity>_crud.go` mount func (run via `mountGenerated`), from
that entity's `CrudHandler`, fields, and relations — see [Generated screen
files](#generated-screen-files). `appBaseCSS()` (mounted
ahead of `static/app.css`) is an owned, empty-by-default extension point for
app-specific base CSS — every generated screen composes `framework/ui`
components and `core-ui/app` layouts that ship their own styling, so the
generator itself emits zero bespoke CSS.

#### Writable app screens (`create:`, edit, delete)

App-side entity screens are read/write, not read-only:

- Add `create: true` to an `entity_list` block → the list shows a **"New <Singular>"**
  button and the generator synthesizes a `<route>/new` create-form screen
  (rendered server-side by the resource engine's `Form(ctx, "")`).
- Every `entity_detail` screen gets **Edit** + **Delete** actions in its header and
  a synthesized `<detail-route>/edit` screen with the form **prefilled** from the
  record (enum/relation `<select>`s render their options + selection server-side).

The forms submit as islands: `data-fui-rpc` POSTs/PUTs JSON to the entity's
`<api_prefix>/<entity>` endpoint, then `data-fui-rpc-navigate` returns to the
list/detail on success. Delete is a `DELETE` with a native confirm. The synthesized
screens inherit the source screen's `layout` + `access` and are not added to `nav`.

To add your own no-reload behavior to a hand-written screen — a status
`<select>` that swaps a badge, a comment form that appends to a thread — follow
[interactive-patterns.md](interactive-patterns.md), in particular "Writing a
hand-written island, end to end" (it covers the traps: the posted JSON key is
the input's `name`, the two route-param syntaxes, and manual route
registration) and "Themed confirmation (`ui.ConfirmAction`)".

#### RBAC for writable APIs

When any entity declares an `access:` block, its auto-CRUD API is permission-gated
— so the app must install a policy or every write 403s. The generator emits an
`access.RolePolicy` that grants the **admin role** (`app.admin.role`) the wildcard
(`*`) and an `access.Middleware` that resolves the signed-in user's roles, mounted
after the session middleware. The admin operator can therefore manage every entity
through its own gated API (the same API the back-office uses); add finer
per-role `Grant`s in `RegisterGenerated` as you define more roles.

### UI component blocks (the framework/ui catalog)

Any screen body can compose the framework's UI components directly via block
`kind`s — the generator emits the matching `ui.X(...)` call:

`page_header` · `hero` · `section` (with child blocks) · `card` · `stack` ·
`cluster` · `grid` · `stat_row` · `stat_grid` · `stat_card` · `bar_chart` ·
`pie_chart` · `line_chart` · `link_button` · `callout` · `divider` ·
`markdown` · `pricing`.

The layout blocks map directly to `framework/ui`: `stack` and `cluster` accept
semantic `gap`, `align`, and `justify` props (`cluster` also accepts
`no_wrap`); `grid` accepts `min` and `gap`; `stat_grid` is the dashboard grid
variant with a `12rem` default minimum. Spacing must use the shared
`none|xs|sm|md|lg|xl|2xl` tokens—blueprints do not create a second styling system.

**Long-form content** — `markdown` renders formatted prose (`ui.Markdown`) from a
`text:` string (headings, **bold**, lists, etc.) at a readable measure; the plain
`type: heading` / `type: paragraph` blocks are also typeset to a comfortable
column on marketing pages. **Pricing** — a `pricing` block takes
`props: {plans: [{name, price, period, description, features: […], cta_text,
cta_href, featured}]}` and renders a row of `ui.PricingCard`s (featured plan
highlighted), so a pricing page reads like marketing, not an admin grid.

**Data-bound dashboard widgets:** `stat_card` and the charts accept a `source:`
that computes a live metric server-side —
`source: {entity: customers, agg: sum, field: mrr}` (or `agg: count` with an
optional `filter: status=active`) for a `stat_card`, and
`source: {entity: customers, group_by: status}` for a chart. For the chart
kinds `source` (with both `entity` and `group_by`, targeting a declared
entity) is **required** — validation rejects a chart without one rather
than letting the block silently vanish from the page. A chart with a
`title` renders inside a `ui.Card` with that heading.

### Layouts (`screen.layout`)

`layout: marketing` wraps the screen in a `ui.SiteHeader` + `ui.SiteFooter` shell
(for the public/front-of-house pages); `layout: app` uses the sidebar shell
(`nav`). Omitted → the default (sidebar if `nav` is set).

### Navigation (`nav`)

`nav` is a list of sidebar entries — each `{label, href}`, with an optional
`icon` and nested `items`. The app shell renders them as the sidebar (and, at
narrow viewports, the hamburger drawer); the theme toggle lives in the sidebar
footer, so there is no separate app top bar.

A nav item may carry `role:` to restrict it to users holding that role:

```yaml
nav:
  - label: Overview
    href: /app
  - label: Admin
    href: /admin
    role: admin     # only signed-in admins see this link
```

Role-gated items are filtered by the signed-in user's roles at render time, on
**both** the desktop sidebar and the mobile drawer — a link a user can't use
never appears (and is never a dead end into a 403). Items without `role:` are
visible to everyone. This is the nav-visibility complement to the server-side
gate on the destination itself (`access:` / the admin battery's `role`); the
route stays protected regardless.

### Seed data

When the blueprint declares `seed:`, generation emits `seedData()`
and `main.go` applies it via `App.WithSeed` after auto-migration. Seeding is
**ordered** (entities load in declared order, so a relation target is inserted
before the rows that reference it) and **idempotent** (an entity whose table
already has rows is skipped). Rows go through the CRUD `CreateOne` path, so
validation, id generation, and timestamps apply; `decimal` values are coerced
to the decimal-string form the validator expects. A row that fails validation
is logged and skipped rather than aborting startup.

When any entity declares `owner_field` (per-user scoping) **and** an admin
account is bootstrapped (`app.admin.seed_email` / `seed_password`), the seed
runs *as that admin*: the generated hook looks the admin up and stamps every
seeded owner column with their id. The demo data therefore belongs to the
admin, and a freshly registered user starts with an empty, owner-scoped
workspace they fill themselves — no other user's rows ever leak in. Without an
admin to attribute the rows to, an owner-scoped seed would have no valid owner
and fail closed.

#### Auto-generated rows (`count` / `weights`)

Hand-writing dozens of demo rows is tedious and tends to produce a flat
round-robin (`open, in_progress, resolved, closed, open, …`) that reads as
obviously fake. Instead, give a seed entry a `count:` and the generator
fabricates that many rows with deterministic, realistic-looking values:

```yaml
seed:
  - entity: tickets
    count: 40                 # generate 40 demo rows
    weights:                  # optional: skew an enum column
      status:
        open: 5
        in_progress: 3
        resolved: 8
        closed: 2
```

Enum columns get a **varied, non-uniform** distribution: with `weights`
the values appear roughly in proportion; without them the generator derives a
deterministic skew seeded from the entity + column name, so demos never look
like an even split. Scalar columns are filled from naming conventions
(`name`/`title` → `Ticket 1`, `email` → `ticket1@example.com`, `slug` →
`ticket-1`, numeric/bool columns get a stable spread). Generation is
reproducible — there is no runtime randomness, so regenerating the app yields
the same rows and the diff never churns.

`count` fills scalar and enum columns only; it leaves relations and
system/auto-generated columns alone. For entities with a **required** relation,
use explicit `rows:` (with `@entity.field=value` refs) instead — a generated
row can't fabricate a valid foreign key. `count` and explicit `rows:` can be
combined: the explicit rows are seeded first, then the generated ones.

### Auth (`app.auth`)

```yaml
app:
  auth:
    enabled: true
    dev_mode: true     # optional, defaults to true — see below
    base_path: /auth   # optional, defaults to /auth
    jwt_secret: ...    # optional
```

With `enabled: true` the generated app wires the [auth battery](auth.md):
an `AuthManager` backed by entity stores over auto-created `auth_users` /
`auth_sessions` tables, with the core plugin's register/login/logout/me
endpoints mounted under `base_path`. The generated app also mounts
`auth.SessionMiddleware` on the router, so the session cookie issued at
login resolves to a user on every request — this is what makes
`owner_field` and `access` scoping work for logged-in users on the
generated CRUD and MCP endpoints. Without a valid session, owner-scoped
entities fail closed (401/403) for reads and writes alike.

The generated app also wires the **auth UX** so the session is reflected
everywhere, not just enforced:

- **Sign out** — the app sidebar footer and (on marketing pages) the header
  carry a `ui.SignOut` control that POSTs to `/auth/logout` and returns home.
- **Auth-aware marketing header** — a signed-in visitor sees a *Dashboard* link
  + *Sign out* instead of *Sign in*; the header re-renders per request from the
  session (`app.NewContextComponent`).
- **Guest-only auth screens** — the login and signup screens redirect an
  already-signed-in visitor to the app instead of showing a form they're past.

The bootstrap admin (`app.admin.seed_email` / `seed_password`) is created on a
fresh database, and the auth battery creates its own `auth_users` /
`auth_sessions` tables in `Init` — the generated app ships no DDL.

#### Secrets never land in generated source

Generated Go reads every secret from the environment, so committing the
generated app commits no credentials:

- `JWT_SECRET` → `auth.AuthConfig.JWTSecret`
- `DATABASE_URL` → the DB DSN (a credentialed DSN is also stripped from
  the emitted `dbURL` constant and the `getEnv` fallback)
- `ADMIN_SEED_PASSWORD` → the bootstrap admin's password (no env var →
  no admin is seeded)

When the blueprint holds any of these values, the generator also emits a
`.env` carrying them (so the app runs without extra setup) plus a
`.gitignore` that excludes it. The generated `main.go` loads
`.env.local`/`.env` before opening the DB; a real process env var
always wins over the files. `generate` is one-shot and refuses to
overwrite an existing `.env` (or any other file) unless you pass `--force`.

#### `dev_mode`

`dev_mode` maps to `auth.AuthConfig.DevMode` and **defaults to true**
when omitted. The default is deliberate: a freshly generated app serves
plain HTTP, and the production cookie defaults (`__Host-session` name +
`Secure` flag) only round-trip over HTTPS — with `dev_mode: false` on
plain HTTP, register/login appear to succeed but the browser never sends
the session cookie back, so every authenticated request fails. Dev mode
uses an HTTP-friendly `session_id` cookie and mints a random per-process
JWT secret at startup (JWT bearer tokens invalidate on restart; cookie
sessions are DB-backed via `auth_sessions` and survive).

`gofastr generate` prints a warning whenever an auth-enabled blueprint
generates in dev mode, and the generated wiring carries the same notice.
Before deploying, turn dev mode off with a real signing key: set
`DevMode: false` and a `JWTSecret` (sourced from a secret manager, not
committed) in the `auth.AuthConfig` in the generated `app.go` — that code
is yours to edit, so change it there rather than re-running the generator.
Serve over HTTPS. Unknown keys under `app.auth` are rejected, like every
other blueprint section.

#### `jwt_secret` is required with `dev_mode: false`

`dev_mode: false` without `jwt_secret` is a **validation error**:
`gofastr validate` and `gofastr generate` both refuse the blueprint.
The generated app could never boot anyway — the auth battery fails
closed at `Init` when `DevMode=false` and `JWTSecret` is empty, because
an empty signing key yields forgeable JWTs. The blueprint check moves
that failure to generate time, with the remedy in the error: set
`jwt_secret` from your secret store, or set `dev_mode: true` for local
development.

#### CSRF

The generated app does **not** mount `auth.CSRF`. The generated API
is JSON-first (REST CRUD + `/mcp`), and the CSRF middleware rejects any
unsafe-method request that doesn't echo the CSRF cookie back as an
`X-CSRF-Token` header (or `_csrf` form field) — plain JSON and MCP
clients don't, so mounting it would 403 the entire generated API for
non-browser clients. Two mitigations bound the exposure: session cookies
are issued `SameSite=Strict`, so modern browsers don't attach them to
cross-site form posts, and requests authenticated by `Authorization` /
`X-API-Key` headers aren't CSRF-able at all. If you add browser HTML
forms to a generated app, mount `auth.CSRF` on the routes that serve
them (every form then needs `auth.CSRFInputFromCtx`) — see
[auth](auth.md) for the pattern.

### Login screen (`login_form` block)

A `login_form` screen block renders a plain HTML sign-in form that posts
(urlencoded, no JavaScript) to the auth battery's `POST <action>` handler,
which detects the form post, sets the session cookie, and `303`-redirects to
`?next=`. Put it on a screen to give the app a real, clickable login:

```yaml
screens:
  - name: login
    route: /login
    body:
      - kind: login_form
        text: Sign in            # form heading
        props:
          action: /auth/login    # default /auth/login
          next: /admin           # where to land after login (default /)
          register_href: /signup  # optional link
```

### Admin back-office (`app.admin`)

```yaml
app:
  admin:
    enabled: true
    path: /admin                # default /admin
    role: admin                 # required role (default "admin")
    login_path: /login          # unauthenticated GET → redirect here
    seed_email: admin@you.com   # bootstrap admin account (created if absent)
    seed_password: change-me
```

With `enabled: true` the generated app registers the [admin battery](../../../battery/admin/),
an auto-generated HTML back-office at `path`: an overview and audit page,
and an editable CRUD screen for every registered entity at `<path>/e/<table>`
(View / Edit / Delete / Create), all proxying each entity's own CrudHandler so
validation, owner/tenant scope, hooks, and events apply. Access is gated by
`role`; an unauthenticated GET is redirected to `login_path` (pair it with a
`login_form` screen) instead of a bare 401, and a signed-in user without the
role gets 403. When `seed_email`/`seed_password` are set, the app bootstraps
that admin account on a fresh database (idempotent — created only when absent),
so the back-office is reachable on first boot. Requires `app.auth.enabled`.
The Queue navigation item appears only when the host explicitly supplies a
`queue.Browsable` backend to the admin battery; generated apps do not imply a
queue they have not configured.

### Installable PWA (`app.pwa`)

```yaml
app:
  pwa:
    enabled: true
    name: Meridian          # optional — defaults to app.name at serve time
    short_name: Meridian    # optional home-screen label
    description: ...        # optional
    start_url: /            # optional, default /
    scope: /                # optional, default /
    display: standalone     # optional: standalone | fullscreen | minimal-ui | browser
    theme_color: "#4f46e5"  # optional; also drives the placeholder icon color
    background_color: "#ffffff"
```

With `enabled: true` the generated app passes `uihost.WithPWA(...)` to the
UI host — manifest, service worker, offline screen, registration script;
see the [PWA guide](pwa.md) for the full runtime behavior — and
scaffolds three **replaceable placeholder icons** under
`<static_dir>/icons/`: `icon-192.png`, `icon-512.png`, and
`icon-maskable.png` (colored from `theme_color`, falling back to the
theme's `primary` token). A fresh `app.pwa` app is installable
immediately; swap the PNG files for real branding when you have it.
When no `static_dir` is set, the generator defaults it to `static/` so
the icons have a home (same rule as self-hosted fonts). Only the fields
you declare are emitted into `main.go`; the framework applies defaults
at serve time. A custom `api_prefix` or auth `base_path` is emitted as
`DenyPaths` so the service worker's never-precache/never-intercept
guarantee follows the app's real mounts.

### Strict SEO + a11y defaults (always on)

Generated apps ship `uihost.WithStrict()` plus a surface that passes
every strict check (see `gofastr docs strict-mode`):

- **Site description** — `app.description` when set, otherwise derived
  from the app name and entities ("Demo — manage products and
  orders."). Write a real one; the derivation is a floor, not copy.
- **Sitemap + robots** — `/sitemap.xml` rooted at the `APP_BASE_URL`
  env var (fallback: `app.base_url`, then `http://localhost:8080` —
  set the env when deploying), `/robots.txt` keeping `/__gofastr/` and
  the admin back-office (when enabled) out of the index. The sitemap
  excludes the admin path too.
- **Per-screen SEO** — every screen's title falls back to its name; a
  screen without a blueprint `description` emits the documented
  zero-value `ScreenSEO` opt-out instead of empty filler. Fill the
  description field for pages that should rank.
- **Axe gate** (`axe_test.go`) — boots the built binary, logs in as
  the seeded admin when one exists, and runs axe-core over every
  sitemap page under both color schemes. Its scans record the
  axe-coverage manifest strict dev boots verify, so a hand-added
  screen without a scan fails `gofastr dev` with a guided finding
  (first boot before any test run warns instead of failing).

The consequence to know: after adding a screen by hand, run
`go test ./...` once (needs Chrome) so the manifest covers it —
`gofastr dev` names the missing screen until then.

### Public LLM markdown (`app.llm_md`)

```yaml
app:
  llm_md: true
```

Emits `uihost.WithPublicLLMMD()`, which mounts the LLM-friendly page
documentation on the generated app: `/llm-pages.md` (an index of every
registered screen) and `/<screen-path>/llm.md` per screen (`/` → `/llm.md`,
`/docs/` → `/docs/llm.md`). Application-level and per-screen `NoLLMMD`
opt-outs keep working. Off by default because the documents enumerate every
screen and its data shape — useful for AI agents in trusted environments,
schema disclosure elsewhere. `app.pwa` and `app.llm_md` are independent:
enabling one never emits the other.

### app.module and the enclosing go.mod

Generated imports are `<app.module>/<output-dir>` — by default the output
directory is the module root (so the only project-local import is
`<app.module>/entities`, since the app files are a flat `package main` with no
importable subpackage); with `--out=<dir>` or `app.output_dir` set, it's
that subpackage path relative to the working directory. Either way `app.module`
must match the Go module you generate into:

- `module:` omitted → it is derived from the go.mod enclosing the working
  directory (plus the relative path from the module root when generating in a
  subdirectory). Inside a module, omitting `module:` therefore also emits a
  `main.go`.
- `module:` set and equal to the enclosing module → fine.
- `module:` set but different → `gofastr generate` and `gofastr validate`
  fail with the expected value; generated code importing a module the
  enclosing go.mod does not declare cannot compile. Fix `module:` or remove
  it to derive automatically.
- No enclosing go.mod → the declared value stands unchecked (you are
  generating a standalone tree).

`gofastr validate` anchors this check at the blueprint file's directory;
`gofastr generate` anchors it at the working directory (where the output is
written).

### Startup banner

The generated `main.go` registers its `Server running at http://...` banner
via `framework.App.OnReady`, which fires only after auto-migrate, seeds,
hooks, and the port bind all succeeded. A migrate failure exits non-zero
without ever printing a success banner.

Screen body blocks support the legacy sugar keys (`type`, `text`, `level`,
`class`, `href`) and property-based nodes through `kind`, `props`, and
`children`. Supported render kinds include structural tags, text tags, forms,
buttons, links, inputs, images, lists, tables, and raw nodes. `island` wraps a
block in `island.NewIsland`; `widget` wraps it in `component.NewWidget`.
`actions` generate GoFastr screen actions and add the required `data-action`
attributes to the rendered node.

Use `kind: entity_list` for a browser-refreshable table backed by a generated
CRUD JSON endpoint. It requires an `entity` with `crud: true` and known
`fields`, accepts `limit` and `empty_text`, and registers a generated client
action that fetches `/<entity>?limit=<limit>` from the generated app.

```yaml
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        text: Latest posts
        entity: posts
        fields: [title, status]
        search: title
        filters: [status, author_id]   # enum + relation facets above the table
        limit: 5
        empty_text: No posts yet.
```

`filters:` accepts only enum, bool, and relation columns; the generator renders
them as a `ui.FilterToolbar` above the table and applies each as a server-side
equality filter that composes with search, sort, and pagination.

Supported UI action events are `click`, `input`, `change`, and `submit`.
`client_js` is copied into generated Go as a string and compiled by the normal
GoFastr action compiler. Use the public `G` runtime helper (`window.__gofastr`)
inside `client_js`; custom server behavior still belongs in Go. When one block
declares multiple actions, the first action uses the standard `data-action`
attribute and later event-specific actions use `data-action-<event>`. A block
can declare at most one action per event. If a `click` action is combined with
other action events on the same block, put the `click` action first so the
standard click delegate can reach it.

Custom behavior is intentionally not inferred from YAML. Auto-generated entity
CRUD, OpenAPI, and MCP tools are real framework behavior. Endpoint handlers,
middleware bodies, plugin internals, and helpers are emitted as Go stubs because
the generator cannot safely invent application-specific implementation logic.

## Validation

Run all validations without generating anything:

```bash
gofastr validate gofastr.yml      # or a directory of blueprint files
```

`gofastr validate` parses the blueprint, runs every generate-time check
(including the `app.module` / go.mod consistency check and a full render
pass), and exits 0 when valid, 1 otherwise. Errors are written to be iterated
on: each names the blueprint file (and the line, where the parser provides
one), what is wrong, and the remedy. `gofastr generate` runs the same checks
before writing any file.

The generator rejects:

- unknown top-level or section keys, except `x_` / `x-` extension keys
- duplicate entity names, routes, endpoints, middleware, plugins, or helpers
- relation-typed fields (`type: relation`) without a `to:` target, or whose
  `to:` names an entity the blueprint does not declare — without this check
  the built app would crash at startup with "auto-migrate: entity has
  BelongsTo to unknown entity"
- relations, endpoint entity references, and entity list blocks that target
  unknown entities
- `app.module` values that conflict with the enclosing go.mod (see above)
- entity list blocks that target non-CRUD entities or fields the entity does
  not define
- unsupported HTTP methods or screen types
- unsupported UI action events, duplicate action names, duplicate action events
  on one block, unreachable combined click actions, or missing `client_js`
- custom endpoint MCP declarations without Go MCP handlers
- unsafe output directories

### Unscoped entities (`gofastr generate` warning)

`gofastr generate` warns on **every** auto-exposed entity (`crud`
defaults on, or `mcp: true`) that sets none of `owner_field`, `access`
(with at least one non-blank permission), or `multi_tenant` — regardless
of what its fields are named. CRUD is secure-by-default, so such an
entity already requires an authenticated session (anonymous requests get
`401`); the residual risk the warning names is **cross-user**: every
*signed-in* caller can read, create, update, and delete every OTHER
user's row. Genuinely public data is a legitimate shape — declare it
with `public: true` to opt all the way out of the session gate — but for
per-user rows set `owner_field`, and gate by role with `access:`. The PII
rule below is the stricter subset that also fails `gofastr validate`.

### Unscoped PII (`gofastr validate` error, `gofastr generate` warning)

Per-user data must be scoped before auto-CRUD/MCP exposure. When an entity
is auto-exposed (`crud` defaults on, or `mcp: true`), declares PII-shaped
fields (names containing tokens like `email`, `phone`, `address`, `ssn`,
`password`, `token`, `secret`, `card`, …), and sets none of `owner_field`,
`access` (with at least one non-blank permission), or `multi_tenant`,
every *authenticated* user could read and write every other user's row on
the generated API (the secure-by-default gate already stops anonymous
callers — the exposure here is cross-user). Enabling `app.auth` alone does
**not** suppress the rule: a session is necessary to reach the API but
not sufficient to scope it — only per-entity scoping does that.
`gofastr validate` reports this as an error (exit 1),
naming the entity, the matched fields, and the remedies; `gofastr generate`
prints the same finding as a warning and proceeds (suppressed under `--json`
to keep stdout machine-parseable). `gofastr audit lint` also reports it
(rule `unscoped-pii`) when a conventional `gofastr.yml` / `gofastr.yaml` /
`gofastr.json` sits at the audited root. Fix it with any of:

```yaml
entities:
  - name: patients
    owner_field: user_id   # per-user rows (see entity-declarations.md)
    # or: access: { read: patients:read, ... }   # RBAC
    # or: multi_tenant: true                     # tenant scoping
```

The match is heuristic and name-based: relation-typed FK columns are
skipped (the target entity is checked on its own), and matching is
per-token (`creditCard` and `credit_card` both match `card`;
`cardinality` does not).

## Testing contract

Blueprint changes should be proven with a generated-app E2E test: run the real
CLI against a blueprint, compile the generated app binary, start that
generated binary as a separate process, exercise HTTP CRUD, OpenAPI, MCP tools,
static assets, and drive generated UI in a real browser so islands, widgets,
runtime actions, and DOM updates are covered together. Do not satisfy this with
a test-only hand-written app shell that imports generated packages; that misses
the app-generator boundary.

### Generated `e2e_test.go` is driver- and OS-portable

Every generated app ships an `e2e_test.go` (gated by `-short`) that builds the
binary, boots it as a child process, and drives the real HTTP endpoints. Two
portability rules are built into that template:

- **Windows binary name.** The build target gets a `.exe` suffix when
  `runtime.GOOS == "windows"` so `exec.Command(bin)` resolves on Windows —
  the bare `"app"` the old template produced cannot be exec'd there.
- **Database bootstrap follows `db.driver`.** A SQLite/empty-driver
  blueprint boots against a throwaway `DATABASE_URL=file:<tmp>/e2e.db`. A
  `postgres` blueprint links only `lib/pq`, which cannot open that file DSN —
  so the test instead carves a disposable database from the env-provided
  `TEST_POSTGRES_DSN` admin DSN and points the child at it
  (`DATABASE_URL` + `DB_DRIVER=postgres`). When `TEST_POSTGRES_DSN` is unset or
  Postgres is unreachable the test **skips** (`t.Skip`), keeping driverless CI
  green-by-skip instead of timing out with a misleading "server did not become
  ready". Set `TEST_POSTGRES_DSN` to a `postgres://` DSN whose role can
  `CREATE DATABASE` / `DROP DATABASE` to exercise the postgres path locally.

## Common mistakes

- **Deploying with `dev_mode` left at its default.** Omitting
  `app.auth.dev_mode` means **true**: an HTTP-friendly session cookie
  and a per-process JWT secret that invalidates bearer tokens on every
  restart. Before deploying, set `DevMode: false` **and** a real
  `JWTSecret` (from a secret manager) in the generated `app.go`
  `auth.AuthConfig` — the emitted code is yours, so edit it rather than
  regenerating. Serve HTTPS. (If you'd rather set it *before* the
  one-shot generate, set `dev_mode: false` + `jwt_secret` in the
  blueprint — but `dev_mode: false` without `jwt_secret` fails
  validation, and the generated app's auth battery would refuse to boot.)
- **Expecting `app.auth: enabled` to protect entity data.** The
  session middleware is pass-through for anonymous requests. Only
  per-entity `owner_field`, `access`, or `multi_tenant` gate the
  generated CRUD and MCP endpoints — that's why the unscoped-PII check fires
  even with auth enabled.
- **Writing flow-style inline maps.** `core/yaml` rejects
  `{name: x, type: relation}` — every map must be indented
  `key: value` lines. Anchors, aliases, block scalars, and tabs are
  also out.
- **Setting `module:` to something other than the enclosing go.mod.**
  `gofastr validate` and `gofastr generate` fail with the expected
  value, because the generated imports could never compile. Omit the
  key to derive it from go.mod automatically.
- **Declaring `type: relation` without `to:`.** Validation rejects it
  (and a missing target entity too) — without the check, the built app
  would crash at startup with "auto-migrate: entity has BelongsTo to
  unknown entity".
