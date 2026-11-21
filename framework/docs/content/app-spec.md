# App specs

A `gofastr.yml` app spec is an optional, one-shot scaffolder. Declare your app settings, entities, screens, navigation, seed data, and endpoint stubs in YAML (or JSON), run `gofastr generate app`, and get plain Go files you own and edit from that point on.

```bash
gofastr validate gofastr.yml
gofastr generate app gofastr.yml
gofastr generate app specs/ --dry-run --json
```

**The generated Go is the source of truth, not `gofastr.yml`.** There is no runtime YAML loader — at runtime your app calls `entities.RegisterAll(app)` and mounts its screen functions. Once generated, edit the Go directly; you can keep `gofastr.yml` as documentation or delete it.

`gofastr generate app` refuses to overwrite an existing project unless you pass `--force`. If you do pass `--force`, GoFastr checks file provenance markers (see [Codegen](codegen.md#provenance-markers)) and warns loudly about every file you modified since generation before overwriting. Plain `gofastr generate` (which `gofastr build` calls) only runs derived generators listed in [`gofastr.settings.yml`](settings.md) and never regenerates your app scaffold.

Passing a directory (`specs/`) loads every `.yml`/`.yaml`/`.json` file in alphabetical order, concatenates list sections, and merges `app:` key by key (duplicate keys across two files are a hard error).

## What the app generator writes vs. what you hand-write

| Area | Generated from `gofastr.yml` | Hand-written Go |
| --- | --- | --- |
| **Marketing & content pages** | `hero`, `page_header`, `card`, `pricing`, `stat_row`, `callout`, `markdown`, `link_button` blocks from `framework/ui` | Bespoke layouts outside the block catalog |
| **Auth** | Register/login/logout/me routes, session middleware, `login_form`/`signup_form` screens, bootstrap admin | Custom SSO, MFA flows, verification UX |
| **Dashboard charts & stats** | `stat_card`, `bar_chart`, `pie_chart`, `line_chart` bound to `{entity, group_by}` | Multi-entity joins, computed series |
| **Entity lists & records** | `entity_list`, `entity_detail`, and `create:` screens rendered via `framework/entityui` | Multi-step wizards, custom widgets, cross-field validation UX |
| **Client islands** | — | Hand-written (see [Interactive patterns](interactive-patterns.md)) |
| **Admin back-office** | Full CRUD admin (`app.admin`), role-gated | Custom admin workflows |
| **Seed data** | Explicit `rows:` and generated demo rows (`count:`, `weights:`) | Complex relational fixtures |
| **Theme & self-hosted fonts** | Color + font tokens, `app.theme.dark`, header toggle, local `.woff2` files | Per-component CSS token overrides |

## YAML subset

GoFastr parses app specs with its built-in `core/yaml` parser (no third-party YAML dependency).

Supported:
- Indentation-based maps and lists (spaces only; tabs are rejected)
- Quoted and unquoted strings (YAML 1.2 escapes in double quotes; unknown escapes like octal `\101` are errors)
- Inline scalar lists (`[draft, published]`; trailing commas allowed, empty items like `[a,,b]` rejected)
- Booleans (`true`/`True`/`TRUE`, `false`/`False`/`FALSE`), nulls (`null`/`Null`/`NULL`/`~`), `int64` integers (including `0o` octal and `0x` hex), and 64-bit floats
- Comments starting with `#` outside quotes

Rejected with line/column errors:
- **Inline/flow maps** (`{type: relation, to: users}` is invalid — always use indented `key: value` lines)
- Anchors, aliases, block scalars, YAML tags (`!env`, `!!str` — quote literal `"!important"`), and invalid UTF-8 strings
- Non-boolean constraint flags: `fields[].required`, `fields[].unique`, and `indices[].unique` accept only booleans or `"true"`/`"false"` (not `yes`, `on`, or `1`)

Quote hex/octal-looking string values (for example `values: ["0x01", "0x02"]`) so YAML 1.2 does not parse them as integers.

## Top-level structure

Root keys: `app`, `entities`, `screens`, `nav`, `seed`, `endpoints`, `hooks`, `middleware`, `plugins`, and `helpers`.

```yaml
app:
  name: Notes
  description: Fast team notes.
  base_url: https://notes.example     # sitemap fallback; APP_BASE_URL wins
  module: example.com/notes
  api_prefix: api                     # default "api"; set "" for bare entity paths
  db:
    driver: sqlite
    url: file:notes.db
  auth:
    enabled: true
  admin:
    enabled: true
    path: /admin
    roles: [admin]
  theme:
    primary: "#2563eb"
    font_heading: Bricolage Grotesque
    font_body: Inter
    dark:
      background: "#0b0f19"
      surface: "#111827"
```

### API prefix and route ownership

- `app.api_prefix` defaults to `"api"`, mounting entity JSON APIs at `/api/<table>` so bare `/<table>` paths stay free for HTML screens. MCP tools, OpenAPI, and generated `entityui` screens follow the prefix automatically.
- With `app.auth.enabled: true`, screens cannot claim `{base_path}/me` (default `/auth/me`) because the auth JSON API owns that `GET` route.

### Theme and self-hosted fonts

`app.theme` accepts canonical color tokens (`primary`, `primary-fg`, `secondary`, `background`, `surface`, `surface-soft`, `text`, `text-muted`, `text-subtle`, `border`, `border-strong`, `accent`, `success`, `warning`, `danger`, `info`) plus `font_heading`, `font_body`, and `font_display`.

- `app.theme.dark` overlays GoFastr's built-in dark palette (`framework/ui/theme`); include matching `-fg` tokens whenever you change a fill like `primary` or `danger`.
- **Fonts are self-hosted:** because generated apps ship a strict `default-src 'self'` CSP, `gofastr generate app` downloads each named font's `.woff2` subset into `static/fonts/<slug>.woff2` and emits local `@font-face` rules. When offline, generation still succeeds and logs the exact `.woff2` files to supply manually.

## Entities and composite keys

`entities:` mirrors `framework.EntityConfig` in [Entity declarations](entity-declarations.md), including `search_fields`, `states`, and `display`:

```yaml
entities:
  - name: invoices
    crud: true
    search_fields: [number]
    fields:
      - name: number
        type: string
        required: true
      - name: amount
        type: decimal
        required: true
      - name: status
        type: enum
        values: [draft, open, paid, void]
        default: draft
      - name: paid_on
        type: date
    states:
      field: status
      initial: [draft]
      transitions:
        - key: mark_paid
          label: Mark paid
          from: [open]
          to: paid
          stamp: paid_on
          variant: primary
    display:
      singular: Invoice
      plural: Invoices
      title_fields: [number]
      columns: [number, amount, status]
      facets: [status]
      form:
        main:
          - row: [number, amount]
          - section: dates
            items: [paid_on]
        side: [status]
```

### Composite keys (`primary_key:`)

Set `primary_key:` to a YAML list—one column name for a custom single key (such as `primary_key: [code]`) or multiple column names for a composite primary key:

```yaml
entities:
  - name: enrollments
    crud: true
    primary_key: [student_id, course_id]
    fields:
      - name: student_id
        type: string
        required: true
      - name: course_id
        type: string
        required: true
      - name: grade
        type: string
```

A relation to a composite-keyed entity names one foreign-key column per key column, in key order:

```yaml
  - name: lines
    crud: true
    fields:
      - name: order_store
        type: string
      - name: order_no
        type: int
      - name: sku
        type: string
    relations:
      - type: belongs_to
        name: order
        entity: orders
        foreign_key: [order_store, order_no]
```

Single-column and composite keys follow the rules in [Record ids](entity-declarations.md#record-ids) and [Relations and composite keys](entity-declarations.md#relations-and-composite-keys).

## Screens and blocks

Each entry in `screens:` defines a server-rendered page with a `name`, `route`, `title`, optional auth/role gate, and `body:` blocks:

```yaml
screens:
  - name: home
    route: /
    title: Notes
    body:
      - type: heading
        level: 1
        text: My Notes
      - kind: entity_list
        entity: notes
        fields: [title, status]
        limit: 20
        create: true
```

Common catalog blocks include plain links, pricing tiers, and data-bound dashboard widgets:

```yaml
- type: link
  href: /docs
```

```yaml
- kind: pricing
  props:
    plans:
      - name: Starter
        price: "29"
        period: month
        description: For small teams
        features: [Shared workspace, Email support]
        cta_text: Start
        cta_href: /signup
        featured: true
```

```yaml
- kind: stat_card
  props:
    label: Monthly revenue
    source:
      entity: customers
      agg: sum
      field: mrr
- kind: bar_chart
  props:
    source:
      entity: customers
      group_by: status
```

Block rules enforced at generation time:
- HTML boolean props use presence semantics (`true` emits `disabled`/`checked`/`required`/`multiple`; `false` omits them). ARIA and `data-*` booleans remain strings.
- Renderer-owned attributes are case-sensitive (`ID` or `Class` cannot override `id` or `class`).
- Links render `text` before child blocks. Generation rejects images without `src`, fieldsets without `legend`, text-only labels without `for`, and node-rendered links without text, children, or an accessible name (`aria-label` / `aria-labelledby`).
- `entity_list` uses the entity's `display.facets` for one-click filters (the old block-level `filters:` key is rejected).

### Generated screen files

By default, `gofastr generate app` writes a flat `package main` at the module root (or `--out=<dir>` / `app.output_dir`):

- `main.go` — entrypoint and server startup
- `app.go` — `RegisterGenerated` wiring (DB, auth, theme, admin, entityui)
- `screens_register.go` — mounts every screen in declaration order
- `screen_<name>.go` — one file per screen
- `entities/` — one `<entity>.go` per entity plus `register.go` and `events.go`
- `extensions.go` / `stubs.go` — generated only when hooks, middleware, plugins, or custom endpoints are declared

## Custom endpoints (`endpoints:`)

Declare non-CRUD routes under the top-level `endpoints:` list (entity-nested `endpoints:` keys are rejected) to wire custom entity routes, OpenAPI entries, and optional MCP tools. Paths must be relative to the entity (no leading `/`), and `entity` and `handler` are required:

```yaml
endpoints:
  - name: publish_digest
    entity: digests
    method: POST
    path: publish
    handler: entities/digests.go#PublishDigest
    description: Publish the current digest draft.
    mcp: true
```

Allowed keys per endpoint: `name`, `entity`, `method`, `path`, `handler`, `description`, `mcp`, `public`, and `hints`. See [Custom endpoints](endpoints.md) for implementing the handler, configuring `MCPGate`, and setting MCP tool hints.

## Additive generation (`--add`)

To scaffold new entities, screens, or endpoints into an app you have already edited, pass `--add` with a partial YAML file containing the additions:

```bash
gofastr generate app new-billing.yml --add
```

`--add` merges new `endpoints:` into `gofastr.settings.yml`, writes new entity/screen files, and rebuilds derived generated files without clobbering your existing hand-edited files.

## Quick scaffolds (`generate entity`/`screen`)

For small additions without writing YAML at all, scaffold a single entity or screen directly from the CLI (supported flags: `--out`, `--dry-run`, `--json`, and `--from-a11y=<file>` on `screen`):

```bash
gofastr generate entity invoices
gofastr generate screen reports
```

## Common mistakes

- **Using inline YAML maps (`{name: title, type: string}`):** `core/yaml` rejects flow maps. Write indented `key: value` blocks instead.
- **Re-running `gofastr generate app --force` after editing Go files:** `--force` discards your edits. Edit the Go directly, or use `--add` / `gofastr generate entity` for new pieces.
- **Mounting a screen at `/auth/me` when `app.auth.enabled: true`:** that route is reserved by the auth JSON API.
- **Setting `filters:` on an `entity_list` block:** moved to `display.facets` on the entity.
- **Setting `api_prefix: ""` and reusing an entity's path for a screen:** the CRUD API and screen will collide. Keep the default `/api` prefix whenever entities and screens share names.
- **Forgetting `app.theme.dark.primary-fg` when overriding `app.theme.dark.primary`:** dark fills inherit the light ink color unless you specify the matching `-fg` token.
