# Blueprints

GoFastr blueprints are deterministic CLI codegen inputs:

```bash
gofastr generate --from=gofastr.yml
gofastr generate --from=blueprints/ --dry-run --json
```

Blueprints are not runtime declarations. The CLI reads `.yml`, `.yaml`, or
`.json` blueprint files, validates them, and writes generated Go under
`.gofastr/`. Runtime loading through `app.EntityFromFile` and
`app.EntitiesFromDir` remains JSON-only.

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

Entity declarations reuse the existing `.gofastr/entities` generator, including
fields, relations, CRUD, MCP, timestamps, soft delete, multi-tenant, cursor
settings, indices, and `properties`. Entity-owned endpoints and top-level
endpoints generate Go handler stubs plus router registration under
`.gofastr/blueprint`.

Screen body blocks support the legacy sugar keys (`type`, `text`, `level`,
`class`, `href`) and property-based nodes through `kind`, `props`, and
`children`. Supported render kinds include structural tags, text tags, forms,
buttons, links, inputs, images, lists, tables, and raw nodes. `island` wraps a
block in `island.NewIsland`; `widget` wraps it in `component.NewWidget`.
`actions` generate GoFastr screen actions and add the required `data-action`
attributes to the rendered node.

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
- relations and endpoint entity references that target unknown entities
- unsupported HTTP methods or screen types
- unsupported UI action events, duplicate action names, duplicate action events
  on one block, unreachable combined click actions, or missing `client_js`
- custom endpoint MCP declarations without Go MCP handlers
- unsafe output directories

## Testing contract

Blueprint changes should be proven with a generated-app E2E test: run the real
CLI against a blueprint, compile the generated `.gofastr` packages, start the
framework app, exercise HTTP CRUD, OpenAPI, MCP tools, and drive generated UI in
a real browser so islands, widgets, runtime actions, and DOM updates are covered
together.
