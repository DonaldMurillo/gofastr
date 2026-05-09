# GoFastr

> A Go full-stack framework for the AI era. Declare your entities once, get a working app: typed CRUD, migrations, OpenAPI, MCP tools, and a server-driven UI runtime.

GoFastr is an experimental framework that treats AI agents as first-class authors of web applications. You describe your domain in JSON or Go, and the framework generates everything around it — database schema, REST endpoints, OpenAPI spec, MCP tool surface, and admin-grade UI primitives — without giving up `database/sql`, `net/http`, or compile-time safety.

> **Status:** pre-alpha research. APIs change. Use it to learn, not to ship customer code.

---

## Why

Most Go web frameworks assume a human will hand-write every route, query, validator, migration, and form. AI agents already generate this code — but no framework treats their output as the canonical source. GoFastr inverts that:

- **One declaration, many surfaces.** A single `entities/posts.json` produces a SQL table, REST routes, OpenAPI ops, a typed Go model, and five MCP tools (`posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete`).
- **No reflection magic.** Generated code lives in `.gofastr/entities/` and is normal Go you can read, debug, and edit.
- **Two-layer architecture.** A small `core/` of stdlib-only primitives sits under an opinionated `framework/`. Drop down to core when the framework is in your way.
- **Batteries included, not embedded.** Auth, cache, email, queue, search, storage are independent packages behind narrow interfaces — swap any one without forking.

## Quickstart

Requires Go 1.26+.

```bash
git clone https://github.com/DonaldMurillo/gofastr.git
cd gofastr
go test ./...                        # everything green on a fresh clone
go run ./cmd/gofastr -- help         # CLI overview
go run ./examples/blog               # minimal blog with auto-CRUD on SQLite
```

Open <http://localhost:8080>, then try:

```bash
curl http://localhost:8080/posts
curl http://localhost:8080/posts/search?q=gofastr
curl http://localhost:8080/openapi.json | jq .info     # auto-generated spec
```

### Declare an entity (JSON)

```json
// examples/blog/entities/posts.json
{
  "name": "posts",
  "soft_delete": true,
  "fields": [
    { "name": "title",  "type": "string", "required": true },
    { "name": "body",   "type": "text" },
    { "name": "status", "type": "enum", "values": ["draft", "published"], "default": "draft" },
    { "name": "author_id", "type": "relation", "to": "users" }
  ],
  "crud": true,
  "mcp": true
}
```

Then either load at runtime:

```go
app := framework.NewApp(framework.WithDB(db))
_ = app.EntitiesFromDir("entities")    // CRUD + OpenAPI + MCP wired automatically
log.Fatal(app.Start(":8080"))
```

…or generate Go code:

```bash
go run ./cmd/gofastr generate          # writes .gofastr/entities/{register,models}.go
```

### Declare an entity (Go)

```go
app.Entity("posts", framework.EntityConfig{
    SoftDelete: true,
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
        {Name: "body", Type: schema.Text},
        {Name: "status", Type: schema.Enum,
            Values: []string{"draft", "published"}, Default: "draft"},
        {Name: "author_id", Type: schema.Relation, To: "users"},
    },
    MCP: true,
})
```

Both forms produce the exact same routes, OpenAPI, and MCP tools.

## What you get from one entity declaration

| Surface       | Auto-generated                                                    |
|---------------|-------------------------------------------------------------------|
| HTTP          | `GET / POST /posts`, `GET / PUT / DELETE /posts/{id}`             |
| Filtering     | `?status=published&views_gte=10&sort=-created_at&page=2`          |
| Validation    | Required, unique, enum, min/max, regex pattern, multi-tenant scope |
| Migrations    | Versioned SQL with up/down, applied via `gofastr migrate`         |
| OpenAPI 3     | `/openapi.json` and Swagger UI at `/openapi`                      |
| MCP           | `posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete` |
| Soft delete   | `deleted_at` column + automatic filter                            |
| Multi-tenant  | `tenant_id` column + automatic scope from request context         |
| Hooks         | `BeforeCreate`, `AfterUpdate`, etc. for custom behaviour          |
| Custom routes | `EntityConfig.Endpoints` with optional MCP exposure               |

## Surfaces

### `core/` — twelve stdlib-only primitives

`router`, `handler`, `middleware`, `query`, `mcp`, `schema`, `migrate`, `render`, `static`, `upload`, `stream`, `openapi`. Each is independently usable; the framework just composes them.

### `framework/` — opinionated entity layer

`framework.App`, `EntityConfig`, JSON declarations, `CrudHandler`, `Hooks`, query DSL (`posts.where(status="published").order(created_at DESC).limit(10)`), MCP CRUD wiring, OpenAPI synthesis.

### `core-ui/` — server-driven UI runtime

A separate, independently usable system for rendering interactive UIs from Go: signals, components, elements, server-side islands, dev server with SSE hot-reload, a static-site compiler, a linter, and a 670-line vanilla-JS runtime. See `examples/core-ui-demo` for an app that exercises every feature.

### `battery/` — pluggable infrastructure

`auth`, `cache`, `email`, `queue`, `search`, `storage`. Each behind a small interface with at least one in-memory implementation suitable for tests and small examples; production swaps in Redis, S3, Postgres FTS, etc.

### `cmd/gofastr` — CLI

```text
gofastr generate                    Generate Go from entities/*.json
gofastr generate entity post t:s    Scaffold a single entity in code
gofastr build                       Generate then go build
gofastr migrate up | down | status  Run versioned migrations
```

### `kiln/` — agent-driven build-mode runtime

Build a GoFastr app live by chatting with an external agent (Claude Code, Codex, Cursor, Pi). The agent calls Kiln's tool surface; the world IR mutates; the running app re-renders; the schema migrates — all in-process. Freeze when done to emit canonical `entities/*.json` you can drop into a regular GoFastr project.

```bash
go install ./cmd/kiln

kiln agent -p "build me a blog"   # starts kiln serve, installs skill, execs pi
kiln serve                        # HTTP only — open http://localhost:8765/kiln/chat
kiln mcp                          # MCP over stdio (subprocess harnesses)
kiln acp                          # ACP over stdio
```

`kiln agent` is the turnkey path: it starts a managed `kiln serve` subprocess in the current directory, waits for the HTTP server to come online, exports `KILN_URL` into the environment, ensures `~/.claude/skills/kiln/SKILL.md` is installed (so pi automatically loads framework knowledge), then execs `pi` with whatever args you pass. Pi reads the skill, sees `$KILN_URL`, and drives the build with curl-against-HTTP — no MCP startup race. The serve subprocess is SIGTERM'd on pi exit; you watch the live preview at <http://localhost:8765/kiln/chat>.

Wire into Claude Code instead:

```json
{
  "mcpServers": {
    "kiln": { "command": "kiln", "args": ["mcp", "--no-http"] }
  }
}
```

Or hit it via HTTP directly:

```bash
kiln serve --addr :8765 &
curl -X POST http://localhost:8765/kiln/tool/add_entity \
  -H 'Content-Type: application/json' \
  -d '{"entity":{"name":"posts","fields":[{"name":"title","type":"string","required":true}]}}'
curl http://localhost:8765/posts            # CRUD live
curl http://localhost:8765/kiln/world      # current IR
```

Tools: `add_entity`, `update_entity`, `delete_entity`, `add_field`, `delete_field`, `add_page`, `delete_page`, `add_hook`, `delete_hook`, `add_route`, `delete_route`, `add_seed`, `set_app_config`, `propose_plan`, `approve_plan`, `undo`, `world_get`, `chat`. See `kiln/protocol/descriptors.go` for full schemas.

## Repository layout

```
core/        stdlib-only primitives (router, query, mcp, openapi, …)
framework/   entity system, app wiring, declarations, query DSL, hooks
core-ui/     server-driven UI runtime (signals, components, islands)
kiln/       agent-driven build-mode runtime + chat panel + MCP/ACP servers
battery/     pluggable infra (auth, cache, email, queue, search, storage)
cmd/gofastr/ CLI: generate, build, migrate
cmd/kiln/   CLI: serve, mcp, acp
docs/        feature docs (entity declarations, migrations, query DSL, …)
examples/    blog, core-ui-demo, demo, spa, static-site
plan/        proposal-driven task tracker
```

## Documentation

- [Entity declarations](docs/entity-declarations.md) — JSON schema reference
- [Migrations](docs/migrations.md) — versioned migrations and the CLI
- [Query DSL](docs/query-dsl.md) — `Entity.where(...).order(...).limit(N)`
- [Search](docs/search.md) — the `battery/search` interface
- [Security](docs/security.md) — defaults, headers, and limits
- [Architecture review](docs/project-architecture-review.md) — design notes and trade-offs
- [Agent notes](docs/agent-notes.md) — running notes for AI contributors

## Project status

GoFastr is pre-1.0 and explicitly not stable:

- The `core/` primitives are usable and tested in isolation.
- The `framework/` entity layer is solid for SQLite + Postgres CRUD apps.
- `core-ui/` is the active research frontier — APIs change between commits.
- The CLI binary blank-imports only `github.com/mattn/go-sqlite3`. To run migrations against Postgres, build a custom binary that imports your driver of choice.
- The module path in `go.mod` (`github.com/gofastr/gofastr`) does not yet match this repository's path; clone the repo locally and use a `replace` directive until the module is published.

## Contributing

This repo is a personal research tree at the moment. Issues and PRs are welcome but expect strong opinions about scope: the goal is a framework an AI agent can drive end-to-end, not a kitchen-sink CMS.

Before pushing, the `.githooks/pre-push` gate runs `go test -race -count=1 ./...` and `govulncheck`. Enable hooks once with:

```bash
git config core.hooksPath .githooks
```

## License

No license has been chosen yet. Until one is added, treat the code as **all rights reserved** — fine to read and learn from, not yet redistributable. A permissive license (likely MIT or Apache-2.0) will land before the first tagged release.
