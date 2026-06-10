# Blueprints

A `gofastr.yml` blueprint is GoFastr's single declaration format. It is a
deterministic CLI codegen input — declare your entities, screens, nav, seed
data, and endpoint/middleware stubs in one file, then generate Go from it:

```bash
gofastr validate gofastr.yml
gofastr generate --from=gofastr.yml
gofastr generate --from=blueprints/ --dry-run --json
```

Blueprints are not runtime declarations: the CLI reads `.yml`, `.yaml`, or
`.json` blueprint files (or a directory of them), validates them, and writes
generated Go under `gen/`. At runtime your app registers the **generated**
entity package (`entities.RegisterAll(app)`) — there is no file-based runtime
loader. The blueprint's `entities:` list uses the same entity shape and field
types documented in [Entity Declarations](entity-declarations.md).

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

Entity declarations reuse the existing `gen/entities` generator, including
fields, relations, CRUD, MCP, timestamps, soft delete, multi-tenant,
`owner_field`, per-operation `access` RBAC, cursor settings, indices, and
`properties`. The `access` map (keys `read`, `create`, `update`, `delete`;
each value a permission string) is emitted as `Access:
framework.AccessControl{...}` in the generated registration — see
[entity-declarations](entity-declarations.md) for the semantics and
[access-control](access-control.md) for wiring roles + policy. Entity-owned endpoints and top-level
endpoints generate Go handler stubs plus router registration under
`gen/blueprint`.

When `app.module` is present, blueprint generation also emits
`gen/main.go`: a runnable app entrypoint that opens the configured SQLite
database, registers generated entities, exposes generated MCP tools at `/mcp`,
wires generated screens/endpoints/middleware/plugins through
`blueprint.RegisterGenerated`, mounts the UI host, and serves `app.static_dir`
through the generated UI host.

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

### app.module and the enclosing go.mod

Generated imports are `<app.module>/<output-dir>` with the output directory
relative to the working directory, so `app.module` must match the Go module
you generate into:

- `module:` omitted → it is derived from the go.mod enclosing the working
  directory (plus the relative path from the module root when generating in a
  subdirectory). Inside a module, omitting `module:` therefore also emits
  `gen/main.go`.
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
CLI against a blueprint, compile the generated `gen` app binary, start that
generated binary as a separate process, exercise HTTP CRUD, OpenAPI, MCP tools,
static assets, and drive generated UI in a real browser so islands, widgets,
runtime actions, and DOM updates are covered together. Do not satisfy this with
a test-only hand-written app shell that imports generated packages; that misses
the app-generator boundary.
