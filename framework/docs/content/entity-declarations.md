# Entity Declarations

> ⚠️ **Per-user data warning.** Auto-generated CRUD does **not** scope
> rows by user unless you set `OwnerField`. An entity exposed via
> `app.Entity(...)` (or `app.GroupEntity(...)`) with no `OwnerField`
> lets every authenticated user read every row. For per-user data:
>
> ```go
> app.Entity("logs", entity.EntityConfig{
>     Fields:     []schema.Field{ /* … */ },
>     OwnerField: "user_id", // CRUD auto-scopes by current user; auto-stamps on Create
> })
> ```
>
> When `battery/auth` is imported, the framework's owner extractor is
> wired automatically — no extra setup needed. See the **Per-user
> scoping (`OwnerField`)** section below for details.

GoFastr supports JSON entity declarations for agent-friendly app generation.
Declarations live in `entities/*.json` and can be loaded at runtime or used by
the CLI code generator. In the general codegen system, entity JSON is one
built-in source/generator pair, not a special architecture boundary.

YAML blueprints are a separate CLI-only codegen surface. Use
`gofastr generate --from=gofastr.yml` when you want a broader app blueprint
that can generate entities plus screens and Go stubs. Runtime loading through
`EntityFromFile` and `EntitiesFromDir` remains JSON-only.

## Runtime Loading

```go
app := framework.NewApp(framework.WithDB(db))
if err := app.EntitiesFromDir("entities"); err != nil {
    log.Fatal(err)
}
```

For a single declaration:

```go
entity, err := app.EntityFromFile("entities/posts.json")
```

## JSON Shape

```json
{
  "name": "posts",
  "table": "posts",
  "soft_delete": true,
  "multi_tenant": false,
  "owner_field": "user_id",
  "crud": true,
  "mcp": true,
  "fields": [
    { "name": "title", "type": "string", "required": true, "max": 200 },
    { "name": "body", "type": "text" },
    { "name": "status", "type": "enum", "values": ["draft", "published"], "default": "draft" },
    { "name": "author_id", "type": "relation", "to": "users" }
  ]
}
```

`owner_field` mirrors `EntityConfig.OwnerField` — set it to the column
that holds the row owner's id (e.g. `"user_id"`) and the JSON-declared
entity gets the same per-user auto-CRUD scoping as a Go-declared one
(see **Per-user scoping** below). Omit the key to keep pre-existing
behaviour.

Supported field types: `string`, `text`, `int`, `float`, `decimal`, `bool`,
`enum`, `uuid`, `timestamp`, `date`, `json`, `relation`, `image`, and `file`.

### Column naming

The `name` you put in a field declaration is the SQL column name verbatim —
case preserved, no snake-casing applied. A field named `flareVerdict` creates
a column called `flareVerdict`, not `flare_verdict`. The same name is also the
JSON property on REST responses when `WithJSONCase(...)` is unset (default is
`camel`).

If you want snake_case columns, write them snake_case in the declaration:
`flare_verdict` → column `flare_verdict`. The framework never rewrites field
names; the only auto-casing happens at the JSON layer via `WithJSONCase`,
which converts column names to/from `camel` or `snake` on the wire and leaves
the underlying column untouched.

Rule of thumb: name fields in whatever case you want the column to be in.
camelCase is the convention used in the example apps; snake_case is the
SQL-traditional choice. Pick one per project and stick with it.

## Per-user scoping (`OwnerField`)

Set `EntityConfig.OwnerField` to the DB column that holds the row owner's
id, and auto-CRUD becomes per-user automatically:

| Operation | Behaviour with `OwnerField: "user_id"` |
|---|---|
| `GET /api/<entity>` (List)   | `WHERE user_id = <ctx user id>` injected into both the data and count queries. |
| `GET /api/<entity>/{id}` (Get) | `WHERE id = ? AND user_id = <ctx user id>`. Cross-user requests return 404. |
| `POST /api/<entity>` (Create) | `user_id` is stamped from the current request — clients can omit it (or send it; it's overwritten). |
| `PUT /api/<entity>/{id}` (Update) | UPDATE is scoped by owner. Cross-user requests return 404. |
| `DELETE /api/<entity>/{id}` (Delete) | DELETE is scoped by owner. Cross-user requests return 404. |

The owner id comes from `framework/owner.Get(ctx)`. Any battery that
registers an extractor wires this up — `battery/auth` does so in
`init()`, pulling from `auth.GetCurrentUser(ctx).GetID()`. If no
extractor is registered, `OwnerField` is inert (no scoping, no
stamping) — so adding the field to an entity config in an app that
hasn't wired auth is harmless.

Pair with **session middleware** so cookie-authenticated requests
appear as a User in context:

```go
app.Use(auth.SessionMiddleware(mgr))
```

JWT-authenticated requests (via `auth.RequireAuth`) already populate
the User in context.

### Auth entities are NOT auto-private

When you register the `users` / `sessions` entities for `battery/auth`,
use the pre-built configs so they don't get exposed via REST or MCP:

```go
app.Entity("users",    auth.UserEntityConfig())    // CRUD=false, MCP=false
app.Entity("sessions", auth.SessionEntityConfig()) // CRUD=false, MCP=false
```

`auth.UserEntityFields()` and `auth.SessionEntityFields()` remain for
hosts that want full control; the `*EntityConfig()` helpers are the
safer default.

## Code Generation

```bash
gofastr generate
```

This reads `entities/*.json` and writes generated Go files into
`.gofastr/entities/`:

- `register.go` with `RegisterAll(app *framework.App)`
- `models.go` with basic entity model structs
- `columns.go` with typed column constants
- `repo.go` with typed repositories
- `events.go` with typed lifecycle subscriptions
- `client/client.go` with a standalone Go HTTP client

Useful flags:

- `--dry-run` lists generated files without writing.
- `--json` emits machine-readable output.
- `--entities=<dir>` reads declarations from another directory.
- `--out=<dir>` writes generated files somewhere else.
- `--no-clean` preserves existing files in the output directory.

For configurable generation, use `gofastr.codegen.yml`:

```yaml
version: 1
codegen:
  output: .gofastr
  generators:
    - name: go/entities
      source:
        type: json_dir
        path: entities
      output: entities
```

See [Codegen](codegen.md) for config discovery, extension support, and
manifest-based cleaning.

`gofastr build` runs generation automatically when it finds a codegen config
or, without config, when an `entities/` directory is present. Pass
`--no-generate` to skip that step.

## MCP Tools

When an entity sets `"mcp": true`, GoFastr registers CRUD tools:

- `{entity}_list`
- `{entity}_get`
- `{entity}_create`
- `{entity}_update`
- `{entity}_delete`

The tools use the same validation and CRUD handler behavior as HTTP routes.

## Custom Endpoints

Custom endpoint handlers are Go behavior and should be registered from Go code:

```go
app.Entity("posts", framework.EntityConfig{
    Fields: []schema.Field{{Name: "title", Type: schema.String}},
    Endpoints: []framework.Endpoint{{
        Method: http.MethodPost,
        Path: "{id}/publish",
        Handler: publishHandler,
        MCP: true,
        Name: "posts_publish",
        MCPHandler: publishTool,
    }},
})
```

Endpoint paths can be absolute (`/posts/{id}/publish`) or relative to the
entity table path (`{id}/publish`). Both `{id}` and `:id` parameter syntax are
accepted.
