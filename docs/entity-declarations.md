# Entity Declarations

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

Supported field types: `string`, `text`, `int`, `float`, `decimal`, `bool`,
`enum`, `uuid`, `timestamp`, `date`, `json`, `relation`, `image`, and `file`.

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
