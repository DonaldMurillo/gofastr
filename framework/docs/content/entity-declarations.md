# Entity declarations

Entities are optional in GoFastr. When you want a database table with migrations, typed Go queries, a REST API, OpenAPI, and optional MCP tools from one declaration, register it with `app.Entity(name, framework.EntityConfig{...})`.

> **Security default:** An entity registered without `Scope.OwnerField`, `Exposure.Access`, or `Exposure.Public` requires an authenticated session for every CRUD operation (anonymous callers get `401`). That blocks anonymous access, but it does **not** isolate rows by user — without `Scope.OwnerField`, any signed-in user can read or modify any other user's rows.

```go
crudEnabled := true
app.Entity("tickets", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "status", Type: schema.Enum, Values: []string{"open", "closed"}, Default: "open"},
		{Name: "body", Type: schema.Text},
	},
	Scope: &framework.ScopeConfig{
		OwnerField: "user_id",
		SoftDelete: true,
	},
	Pagination: &framework.PaginationConfig{
		CursorFields: []string{"created_at", "id"},
		MaxListLimit: 100,
	},
	Exposure: &framework.ExposureConfig{
		CRUD: &crudEnabled,
		MCP:  true,
		Access: framework.AccessControl{
			Read:   "tickets:read",
			Create: "tickets:write",
			Update: "tickets:write",
			Delete: "tickets:write",
		},
	},
})
```

In Go, `EntityConfig` always uses the grouped sub-configs: `Scope` (`*framework.ScopeConfig`), `Pagination` (`*framework.PaginationConfig`), and `Exposure` (`*framework.ExposureConfig`). In `gofastr.yml`, flat shorthand keys are accepted alongside the grouped maps, but setting both with conflicting values is a decode error.

## What registering an entity publishes

Omitting `Exposure` does **not** hide the entity — `Exposure.CRUD` is a `*bool` where `nil` means **on**. Registering `"posts"` mounts:

- `GET /posts`, `GET /posts/{id}`, `POST /posts`, `PUT /posts/{id}`, `PATCH /posts/{id}`, `DELETE /posts/{id}`
- `POST /posts/_batch`, `PUT /posts/_batch`, `DELETE /posts/_batch` (see [Batch endpoints](batch-endpoints.md))
- `POST /posts/{id}/transitions/{key}` when `States` declares a non-system transition (see [States](states.md))
- `GET /posts/_events` (SSE stream; see [Entity events](events.md))
- `GET /posts/llm.md` (machine-readable route guide matching the mounted surface, wire casing, and limits)

`GET /openapi.json` stays authenticated unless explicitly exposed, and documents the same pagination limits (`crud.ListLimits`), `409` conflict errors, `413`, and `415` responses.

**MCP tools are off by default** in production (`Exposure.MCP: true` opts in). Under `gofastr dev`, every CRUD-enabled entity exposes its data tools automatically so local agents can inspect and mutate data without extra flags.

To keep an entity internal (migrated and usable from Go via `app.CrudHandler("posts")` and typed queries, with zero generated HTTP routes or MCP tools):

```go
noCRUD := false
app.Entity("posts", framework.EntityConfig{
	Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	Exposure: &framework.ExposureConfig{CRUD: &noCRUD},
})
```

Rules for `CRUD: false`:
- Combining `CRUD: false` with `MCP: true` fails registration (`framework.ValidateExposure` checks the same rule) because generated MCP CRUD tools dispatch through the HTTP handlers.
- Other entities cannot reach a `CRUD: false` entity over HTTP via `?include=`, `?rel.field=` filters, or cascade writes (those return `403`). A `belongs_to` foreign-key write still validates the referenced row against the target's scopes and hooks without leaking target fields.
- Custom `Endpoints` declared on the entity still mount even when `CRUD: false`; give them explicit middleware and `MCPGate` rules (see [Custom endpoints](endpoints.md)).
- When building a handler manually, always use `crud.NewCrudHandler(ent, db)` so the SQL dialect is cached once at construction.

## Fields and wire names

Declare columns with `[]schema.Field`. Core field types (`schema.FieldTypeLabel` maps them for docs and SDKs):

| Type | Go constant | Notes |
| --- | --- | --- |
| String / Text | `schema.String`, `schema.Text` | Short strings (`VARCHAR`) and long bodies (`TEXT`) |
| Numbers | `schema.Int`, `schema.Float`, `schema.Decimal` | Decimals travel on the wire as strings to avoid float drift |
| Boolean | `schema.Bool` | `true`/`false` |
| Time | `schema.Time` | RFC 3339 timestamps |
| Enum | `schema.Enum` | Requires `Values: []string{...}` |
| JSON | `schema.JSON` | Arbitrary JSON values; equality filters compare as JSON |
| Media | `schema.File`, `schema.Image` | Stored keys / URLs (see [Uploads](uploads.md) and [Image processing](image.md)) |
| Relation | `schema.Relation` | Foreign key pointing to `To: "other_entity"` |
| UUID | `schema.UUID` | Auto-generated or caller-supplied UUIDs |

Useful `schema.Field` options: `Required`, `Unique`, `Default`, `Min`, `Max`, `MinLength`, `MaxLength`, `Pattern`, `WireName` (override JSON key; defaults to camelCase), `Hidden` (omitted from HTTP responses), `NoQuery` (masked from filters/sorts/activity diffs), `ReadOnly` (server-owned), and `AutoGenerate`.

## Record ids

By default, every entity gets an auto-increment integer `id` primary key. Override it with `PrimaryKey`:

- **Single caller-assigned or UUID key:** declare the column in `Fields` (e.g., `code` with `Required: true` or `id` with `Type: schema.UUID`, `AutoGenerate: true`) and set `PrimaryKey: []string{"code"}`.
- **Composite primary key:** pass multiple column names (`PrimaryKey: []string{"student_id", "course_id"}`; in YAML, [`primary_key:`](app-spec.md#composite-keys-primary_key)). Routes match one path segment per key column and join them with `entity.JoinKey` in Go.
- **No synthetic `id`:** when `PrimaryKey` is set, GoFastr does not add an extra `id` column.

## Relations and composite keys

Declare relationships in `Relations: []entity.Relation{...}` alongside foreign-key fields:

- `belongs_to` (`ForeignKey` lives on this entity pointing at `Target`)
- `has_many` / `has_one` (`ForeignKey` lives on `Target`)
- `many_to_many` (uses a join table)

For composite keys, `Relation.ForeignKey` and the target's `PrimaryKey` pair column-for-column in declaration order. Foreign keys are validated on write against the target entity's active scopes (owner, tenant, soft delete, `ReadScope`, and `Before` hooks); an inaccessible target returns `404` rather than revealing existence.

Use `?include=` to eager-load relations on HTTP reads — see [Includes](includes.md).

## Per-user owner scoping (`OwnerField`)

Set `Scope.OwnerField` on any entity that holds per-user rows to enable automatic owner scoping:

```go
app.Entity("notes", framework.EntityConfig{
	Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "user_id", Type: schema.String},
	},
	Scope: &framework.ScopeConfig{OwnerField: "user_id"},
})
```

What `OwnerField` enforces automatically:
- **Create:** stamps the column from the authenticated caller's ID, overwriting any client-supplied value.
- **List / Count / Stream / Events:** appends `WHERE <owner_field> = <caller_id>`.
- **Get / Update / Delete / Transitions:** scopes the lookup to the caller's ID so another user's row returns `404`.
- **Fail-closed:** if `OwnerField` is set, every request fails with **401 until an owner extractor is registered**. Importing `battery/auth` registers the extractor automatically; custom auth middleware can register one via `framework/owner`.

### Default CRUD authentication

| Declaration | Anonymous caller | Signed-in user |
| --- | --- | --- |
| Default (no `OwnerField`, `Access`, `Public`) | `401` | Full CRUD on all rows |
| `Scope.OwnerField` set | `401` (and **401 until an owner extractor is registered**) | Full CRUD on own rows only |
| `Exposure.Access` set | `401` (or `403` for missing permission) | Checked per verb (`Read`, `Create`, `Update`, `Delete`) |
| `Exposure.Public: true` | Allowed on every CRUD verb | Allowed on every CRUD verb |

Combine `Scope.OwnerField` with `Exposure.Access` when you need both per-user isolation and role/permission gates (for example, allowing admins to override or restricting `Delete`). See [Access control](access-control.md) for `Bypass` roles and per-row `Decider` hooks, and [Multi-tenancy](multi-tenant.md) for `Scope.MultiTenant`.

Set `Scope.SoftDelete: true` to mark deletions with `deleted_at` instead of removing rows, hiding deleted rows from normal queries and enabling restore endpoints.

## Pagination and offset ceilings

Configure list behavior via `Pagination: &framework.PaginationConfig{...}`:
- `MaxListLimit`: caps `?limit=` (default `100`, hard ceiling `1000`). Default page size is `20` (or `MaxListLimit` if smaller).
- `CursorFields`: allowlist of columns valid for cursor pagination (see [Cursor pagination](cursor-pagination.md)).
- `DefaultSort`: default ordering when `?sort=` is omitted.

Offset pagination is bounded to prevent deep-scan DoS: requests whose `?offset=` or `(page - 1) * limit` exceeds `CrudHandler.MaxOffset` (default `MaxListLimit * 1000`, i.e., `100,000`; configurable via `WithMaxOffset`) fail with `400 offset_too_large`, including integer-overflow page values. An explicit `?offset=` overrides `?page=`, and the response `page` reflects `offset/limit + 1`.

## Screen hints (`display`)

`Display` (`*framework.DisplayConfig`) tells `framework/entityui`, `battery/admin`, and generated screens how to render an entity without coupling schema to HTML:

- `TitleField` / `SubtitleField` / `BadgeField`: record title, mobile card subtitle, and status badge
- `Columns`: ordered list of table columns
- `Facets`: enum, bool, or relation columns rendered as one-click filter chips above the list
- `SearchFields`: columns searched by `?q=` and the list search box
- `Form`: field grouping, sections, help text, and conditional visibility
- `Views`: named filter presets rendered as tabs above the list
- `Actions`: record and bulk actions

See [Entity screens (entityui)](entityui.md) for how `Display` renders into list, create, and record pages, [Generated screen files](app-spec.md#generated-screen-files) for how `gofastr generate app` lays out screens, [Additive generation](app-spec.md#additive-generation---add) for adding new entities later, and [Quick scaffolds](app-spec.md#quick-scaffolds-generate-entityscreen) for single-entity scaffolding.

## The typed repository

Code generation (`gofastr generate`) emits a typed Go repository for each entity so application code never deals in `map[string]any`:

- `<Entity>` struct with typed fields and JSON tags matching the wire contract
- `<Entity>Patch` struct with pointer fields for partial updates (omitting state-managed fields when `States` owns them — see [States](states.md))
- Typed `Create`, `Get`, `List`, `Update`, `Delete`, and relation helpers that run through the same hooks and scopes

See [Codegen](codegen.md) and [App specs](app-spec.md) for generating typed packages and hooks.

## Custom endpoints and MCP hints

Entities can declare custom HTTP + MCP actions in `Endpoints: []entity.Endpoint{...}` alongside generated CRUD routes. Each endpoint can carry MCP tool annotations (`Hints`) and an independent `MCPGate`; see [Custom endpoints](endpoints.md#mcp-tool-hints).

## Common mistakes

- **Omitting `Scope.OwnerField` on user-owned data:** default auth blocks anonymous requests, but every authenticated user can still read and overwrite everyone else's rows until `OwnerField` is set.
- **Expecting `Exposure: nil` to disable routes:** `CRUD` defaults to `true`. Pass `CRUD: &noCRUD` (`false`) to keep an entity Go-only.
- **Setting `MCP: true` with `CRUD: false`:** registration rejects this because MCP CRUD tools route through the HTTP handlers.
- **Constructing `&crud.CrudHandler{...}` directly with a wrapped DB or transaction:** always call `crud.NewCrudHandler(ent, db)` so the SQL dialect is resolved and cached.
- **Putting flat and grouped keys with different values in `gofastr.yml`:** the parser treats conflicting flat/grouped values as an error instead of picking one silently.
- **Forgetting to register an owner extractor when not using `battery/auth`:** `OwnerField` fails closed with `401` until an extractor is registered in `framework/owner`.
