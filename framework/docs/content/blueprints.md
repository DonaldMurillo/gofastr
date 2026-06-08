# Blueprints

A `gofastr.yml` blueprint is GoFastr's single declaration format. It is a
deterministic CLI codegen input — declare your entities, screens, nav, seed
data, and endpoint/middleware stubs in one file, then generate Go from it:

```bash
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
fields, relations, CRUD, MCP, timestamps, soft delete, multi-tenant, cursor
settings, indices, and `properties`. Entity-owned endpoints and top-level
endpoints generate Go handler stubs plus router registration under
`gen/blueprint`.

When `app.module` is present, blueprint generation also emits
`gen/main.go`: a runnable app entrypoint that opens the configured SQLite
database, registers generated entities, exposes generated MCP tools at `/mcp`,
wires generated screens/endpoints/middleware/plugins through
`blueprint.RegisterGenerated`, mounts the UI host, and serves `app.static_dir`
through the generated UI host.

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

The generator rejects:

- unknown top-level or section keys, except `x_` / `x-` extension keys
- duplicate entity names, routes, endpoints, middleware, plugins, or helpers
- relations, endpoint entity references, and entity list blocks that target
  unknown entities
- entity list blocks that target non-CRUD entities or fields the entity does
  not define
- unsupported HTTP methods or screen types
- unsupported UI action events, duplicate action names, duplicate action events
  on one block, unreachable combined click actions, or missing `client_js`
- custom endpoint MCP declarations without Go MCP handlers
- unsafe output directories

## Testing contract

Blueprint changes should be proven with a generated-app E2E test: run the real
CLI against a blueprint, compile the generated `gen` app binary, start that
generated binary as a separate process, exercise HTTP CRUD, OpenAPI, MCP tools,
static assets, and drive generated UI in a real browser so islands, widgets,
runtime actions, and DOM updates are covered together. Do not satisfy this with
a test-only hand-written app shell that imports generated packages; that misses
the app-generator boundary.
