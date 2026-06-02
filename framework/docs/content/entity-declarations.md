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

For Go-defined configs, `RegisterEntities` is sugar over multiple
`Entity(...)` calls. Map iteration order is randomised, but FK ordering
is still handled correctly because AutoMigrate sorts entities
topologically:

```go
app.RegisterEntities(map[string]entity.EntityConfig{
    "foods":  foodsConfig,
    "meals":  mealsConfig,
    "users":  usersConfig,
})
```

## Seeding

`EntityConfig.Seed` runs once per entity after `AutoMigrate` creates the
table. The framework tracks completion in the `_gofastr_seeded` ledger;
subsequent restarts short-circuit on the ledger row. Errors abort
`App.Start`, so a failed seed prevents a half-up server.

```go
app.Entity("foods", entity.EntityConfig{
    Fields: []schema.Field{ /* … */ },
    Seed: func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `INSERT INTO foods (name)
            VALUES ('apple'), ('banana') ON CONFLICT DO NOTHING`)
        return err
    },
})
```

`Seed` should be idempotent. The ledger is best-effort tracking that
survives normal restarts but cannot guarantee atomicity between user
inserts and the ledger row; prefer `INSERT … ON CONFLICT DO NOTHING` or
a pre-check inside `Seed`.

### Embedded seed data (`SeedFS` + `SeedPath`)

Single-binary deploys benefit from seeding from `//go:embed` data rather
than loose JSON files on disk:

```go
//go:embed seed/foods.json
var seedFoods embed.FS

app.Entity("foods", entity.EntityConfig{
    Fields:   []schema.Field{ /* … */ },
    SeedFS:   seedFoods,
    SeedPath: "seed/foods.json",
    Seed: func(ctx context.Context, db *sql.DB) error {
        raw, err := entity.SeedDataFromContext(ctx)
        if err != nil {
            return err
        }
        var rows []FoodRow
        if err := json.Unmarshal(raw, &rows); err != nil {
            return err
        }
        for _, r := range rows {
            // …INSERT…
        }
        return nil
    },
})
```

`entity.SeedDataFromContext(ctx)` returns the bytes pointed to by `SeedPath`
within `SeedFS`. The framework wires the context just before calling
`Seed`; hosts never need to attach it manually.

`App.Entity` panics at registration time if `SeedFS` is set but
`SeedPath` is empty — a misconfiguration that would otherwise silently
record the entity as seeded with empty data on first run.

### Observability

Attach a `*slog.Logger` so each seed emits structured lifecycle events:

```go
ctx := migrate.WithSeedLogger(context.Background(), logger)
// (the framework calls migrate.RunSeeds with the App's lifecycle ctx
// during App.Start, so this matters mostly for tests + custom flows)
```

Events: `seed ledger read` (once per RunSeeds), `seed start`, `seed
done` (with elapsed duration), `seed skip` (when the ledger already
records the entity), `seed failed` (on error). When no logger is
attached, events go to a discard handler.

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
`gen/entities/`:

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
  output: gen
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
