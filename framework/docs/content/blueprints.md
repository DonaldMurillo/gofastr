# Blueprints

A `gofastr.yml` blueprint is GoFastr's single declaration format. It is a
CLI codegen *on-ramp* — declare your entities, screens, nav, seed
data, and endpoint/middleware stubs in one file, then scaffold owned Go from
it. The blueprint is not a source of truth: after scaffolding, the generated
Go is canonical and you can delete `gofastr.yml`. Generate with:

```bash
gofastr validate gofastr.yml
gofastr generate --from=gofastr.yml
gofastr generate --from=blueprints/ --dry-run --json
```

Blueprints are not runtime declarations: the CLI reads `.yml`, `.yaml`, or
`.json` blueprint files (or a directory of them), validates them, and scaffolds
owned Go into an idiomatic, module-root layout by default — `main.go` at the
root plus `entities/` and `blueprint/` packages (set `--out=<dir>` or
`app.output_dir` to scaffold into a subpackage instead). At runtime your app
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

`app.theme` can override canonical color tokens for generated UI apps. Supported
keys are `primary`, `primary-fg`, `secondary`, `background`, `surface`,
`surface-soft`, `text`, `text-muted`, `text-subtle`, `border`,
`border-strong`, `accent`, `success`, `warning`, `danger`, and `info`.
Generated apps call `site.WithTheme(...)`, so the values are emitted through
`/__gofastr/app.css` as computed CSS custom properties.

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
fields, relations, CRUD, MCP, timestamps, soft delete, multi-tenant,
`owner_field`, per-operation `access` RBAC, cursor settings, indices, and
`properties`. The `access` map (keys `read`, `create`, `update`, `delete`;
each value a permission string) is emitted as `Access:
framework.AccessControl{...}` in the generated registration — see
[entity-declarations](entity-declarations.md) for the semantics and
[access-control](access-control.md) for wiring roles + policy. Entity-owned endpoints and top-level
endpoints generate Go handler stubs plus router registration in the
`blueprint` package.

When `app.module` is present, blueprint generation also emits a root
`main.go`: a runnable app entrypoint that opens the configured SQLite
database, registers generated entities, exposes generated MCP tools at `/mcp`,
wires generated screens/endpoints/middleware/plugins through
`blueprint.RegisterGenerated`, mounts the UI host, and serves `app.static_dir`
through the generated UI host.

## Packing: `gofastr pack` (the inverse of generate)

`gofastr pack [app-dir]` reconstructs a `gofastr.yml` from a generated app's Go
source — the inverse of `gofastr generate`. It reads the real artifacts via the
Go AST (it does **not** stash a manifest): `entities/register.go` for entities,
`blueprint/app.go` for app config + theme + auth/admin + nav, `blueprint/stubs.go`
for seed, and `blueprint/screens.go` (+ the `site.Register*` calls) for screens,
reversing the emitted `framework/ui` grammar (`ui.Hero` → `hero`,
`blueprintResources["x"].…List(ctx)` → `entity_list`, and so on). The result
prints to stdout, or to a file with `-o`:

```sh
gofastr pack examples/meridian -o recovered.yml
```

Synthesized `/new` + `/{id}/edit` form screens are dropped (they weren't
authored). Generate and pack are a matched inverse pair, so the invariant
`parse(yml)` ≡ `parse(pack(generate(yml)))` holds (modulo comments + formatting);
the Meridian flagship round-trips exactly, gated by a test. When you add a new
blueprint construct, teach **both** the generator and pack, or that test fails.

### Data blocks (`entity_list`, `entity_form`, `entity_detail`)

A top-level `entity_list` or `entity_detail` makes its screen a **server-rendered**
(request-time) screen that queries the entity's `CrudHandler` and composes real
`framework/ui` components — no client-side fetch. The generator emits an owned
engine at `blueprint/resource.go` (and a `blueprint/resource_test.go`) that the
screens call:

- `entity_list` → `ui.PageHeader` + `ui.SearchInput` + `ui.DataTable` +
  `ui.Pagination`/`ui.EmptyState`, with **humanized headers** ("Generic Name"),
  formatted cells (bool → Yes/No status badge, enum → status badge, `decimal` →
  `$` money, dates trimmed), and **relation columns resolved to the related
  record's display name** (not the raw id). Search/sort/pagination are
  URL-driven and run server-side. `fields:` picks/orders the columns; `search:`
  names the LIKE-search field; `limit:` sets the page size.
- `entity_detail` reads the route `{id}`, loads the record server-side, and
  renders the fields with the same formatting + relation resolution.
- `entity_form` renders a `<form data-fui-rpc="<api_prefix>/<entity>">` (enum →
  `<select>` of values; relation → `<select>` populated from the related entity).

The generated `ResourceConfig` registry is populated in `RegisterGenerated` from
each entity's `CrudHandler`, fields, and relations. `BlueprintBaseCSS()` (mounted
ahead of `static/app.css`) ships a `box-sizing` reset, the themed page surface,
and responsive table/card/form defaults.

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

#### RBAC for writable APIs

When any entity declares an `access:` block, its auto-CRUD API is permission-gated
— so the app must install a policy or every write 403s. The generator emits an
`access.RolePolicy` that grants the **admin role** (`app.admin.role`) the wildcard
(`*`) and an `access.Middleware` that resolves the signed-in user's roles, mounted
after the session middleware. The admin operator can therefore manage every entity
through its own gated API (the same surface the back-office uses); add finer
per-role `Grant`s in `RegisterGenerated` as you define more roles.

### UI component blocks (the framework/ui catalog)

Any screen body can compose the framework's UI components directly via block
`kind`s — the generator emits the matching `ui.X(...)` call:

`page_header` · `hero` · `section` (with child blocks) · `card` · `stat_row` ·
`stat_card` · `bar_chart` · `pie_chart` · `link_button` · `callout` · `divider` ·
`markdown` · `pricing`.

**Long-form content** — `markdown` renders rich prose (`ui.Markdown`) from a
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
`source: {entity: customers, group_by: status}` for a chart.

### Layouts (`screen.layout`)

`layout: marketing` wraps the screen in a `ui.SiteHeader` + `ui.SiteFooter` shell
(for the public/front-of-house pages); `layout: app` uses the sidebar shell
(`nav`). Omitted → the default (sidebar if `nav` is set).

### Seed data

When the blueprint declares `seed:`, generation emits `BlueprintSeedData()`
and `main.go` applies it via `App.WithSeed` after auto-migration. Seeding is
**ordered** (entities load in declared order, so a relation target is inserted
before the rows that reference it) and **idempotent** (an entity whose table
already has rows is skipped). Rows go through the CRUD `CreateOne` path, so
validation, id generation, and timestamps apply; `decimal` values are coerced
to the decimal-string form the validator expects. A row that fails validation
is logged and skipped rather than aborting startup.

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
generated CRUD/MCP surface. Without a valid session, owner-scoped
entities fail closed (401/403) for reads and writes alike.

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
Before deploying, set `dev_mode: false` **and** `jwt_secret` (sourced
from a secret manager, not committed to the blueprint), serve over
HTTPS, and regenerate. Unknown keys under `app.auth` are rejected, like
every other blueprint section.

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

The generated app does **not** mount `auth.CSRF`. The generated surface
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
an auto-generated HTML back-office at `path`: an overview, queue/audit pages,
and an editable CRUD screen for every registered entity at `<path>/e/<table>`
(View / Edit / Delete / Create), all proxying each entity's own CrudHandler so
validation, owner/tenant scope, hooks, and events apply. Access is gated by
`role`; an unauthenticated GET is redirected to `login_path` (pair it with a
`login_form` screen) instead of a bare 401, and a signed-in user without the
role gets 403. When `seed_email`/`seed_password` are set, the app bootstraps
that admin account on a fresh database (idempotent — created only when absent),
so the back-office is reachable on first boot. Requires `app.auth.enabled`.

### app.module and the enclosing go.mod

Generated imports are `<app.module>/<output-dir>` — by default the output
directory is the module root (so imports are `<app.module>/entities`,
`<app.module>/blueprint`); with `--out=<dir>` or `app.output_dir` set, it's
that subpackage path relative to the working directory. Either way `app.module`
must match the Go module you generate into:

- `module:` omitted → it is derived from the go.mod enclosing the working
  directory (plus the relative path from the module root when generating in a
  subdirectory). Inside a module, omitting `module:` therefore also emits a
  root `main.go`.
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
        limit: 5
        empty_text: No posts yet.
```

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

### Unscoped PII (`gofastr validate` error, `gofastr generate` warning)

Per-user data must be scoped before auto-CRUD/MCP exposure. When an entity
is auto-exposed (`crud` defaults on, or `mcp: true`), declares PII-shaped
fields (names containing tokens like `email`, `phone`, `address`, `ssn`,
`password`, `token`, `secret`, `card`, …), and sets none of `owner_field`,
`access` (with at least one non-blank permission), or `multi_tenant`,
every row would be world-readable and -writable on the generated API.
Enabling `app.auth` alone does **not** suppress the rule — the session
middleware is pass-through for anonymous requests; only per-entity scoping
gates the auto-generated surfaces. `gofastr validate` reports this as an error (exit 1),
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

## Common mistakes

- **Deploying with `dev_mode` left at its default.** Omitting
  `app.auth.dev_mode` means **true**: an HTTP-friendly session cookie
  and a per-process JWT secret that invalidates bearer tokens on every
  restart. Before deploying, set `dev_mode: false` **and** `jwt_secret`
  (from a secret manager), serve HTTPS, and regenerate. `dev_mode:
  false` without `jwt_secret` fails validation — and the generated
  app's auth battery would refuse to boot.
- **Expecting `app.auth: enabled` to protect entity data.** The
  session middleware is pass-through for anonymous requests. Only
  per-entity `owner_field`, `access`, or `multi_tenant` gate the
  generated CRUD/MCP surface — that's why the unscoped-PII check fires
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
