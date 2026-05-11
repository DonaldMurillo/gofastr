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

| Surface          | Auto-generated                                                                  |
|------------------|---------------------------------------------------------------------------------|
| HTTP             | `GET / POST /posts`, `GET / PUT / DELETE /posts/{id}`                           |
| Batch endpoints  | `POST / PATCH / DELETE /posts/_batch` — atomic; one tx for all items            |
| SSE stream       | `GET /posts/_events` — entity.created/updated/deleted, scoped per tenant        |
| Filtering        | `?status=published&views_gte=10&sort=-created_at&page=2`                        |
| Eager loading    | `?include=author.profile,comments` — flat or nested, validated against the registry |
| Cursor paging    | `?cursor=&limit=50` — keyset by `EntityConfig.CursorField` (defaults to PK)     |
| Multipart upload | `multipart/form-data` on `Image`/`File` fields → streamed through `WithFileStorage` |
| Validation       | Required, unique, enum, min/max, regex pattern, multi-tenant scope              |
| Migrations       | Versioned SQL with up/down, applied via `gofastr migrate`                       |
| FK constraints   | BelongsTo relations emit `FOREIGN KEY` clauses; `AutoMigrate` topo-sorts tables |
| Transactions     | `Create/Update/Delete` + hooks share one tx; `TxFromContext(ctx)` exposes it    |
| OpenAPI 3        | `/openapi.json` and Swagger UI at `/docs/`                                      |
| MCP              | `posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete`       |
| Soft delete      | `deleted_at` column + automatic filter                                          |
| Multi-tenant     | `tenant_id` column + automatic scope from request context                       |
| Hooks            | `BeforeCreate`, `AfterUpdate`, etc. for custom behaviour                        |
| Custom routes    | `EntityConfig.Endpoints` with optional MCP exposure                             |

### Walkthrough: the v2 read/write surface

Every endpoint below is auto-generated from a registered entity. There's a
runnable demo in [`examples/api-tour`](examples/api-tour/README.md) that
exercises all of it end-to-end against SQLite.

```bash
# Eager-load a relation graph in one round trip (no N+1):
curl 'http://localhost:8080/posts/p1?include=author.profile,comments'

# Cursor pagination — opt in by sending the cursor key (even empty):
curl 'http://localhost:8080/posts?cursor=&limit=20'
# → {"data":[…], "cursor":"<opaque>", "hasMore":true}
curl 'http://localhost:8080/posts?cursor=<opaque>&limit=20'

# Atomic batch — all items succeed or none do:
curl -X POST http://localhost:8080/posts/_batch -d '{"items":[
  {"title":"A"}, {"title":"B"}, {"title":"C"}
]}'
# → {"committed":true, "results":[{"index":0,"data":{…}}, …]}

# Server-sent events for entity lifecycle:
curl -N http://localhost:8080/posts/_events
# event: entity.created
# data: {"type":"entity.created","data":{"entity":"posts","record":{…}}}

# Multipart upload to an Image field:
curl -X POST http://localhost:8080/users \
  -F 'name=Carol' -F 'avatar=@/path/photo.png'
```

Hooks now run inside the same transaction as the write:

```go
app.HookRegistry("posts").RegisterHook(framework.AfterCreate,
  func(ctx context.Context, data any) error {
    tx, _ := framework.TxFromContext(ctx)        // *sql.Tx — atomic with the parent INSERT
    _, err := tx.ExecContext(ctx, "INSERT INTO audit_log …")
    return err
  })
```

If the hook errors, the parent write is rolled back.

### Testing against Postgres

The framework's tests fan over both SQLite and Postgres. With Docker
running, every dialect-aware test runs on both engines automatically:

```bash
make test            # SQLite only, fast
make test-pg         # both dialects via testcontainers (Docker)
make test-pg-env     # both dialects, points at TEST_POSTGRES_DSN
make test-race       # race detector across the whole repo
```

Each Postgres test gets its own schema for isolation; the container is
shared across the whole `go test` invocation so cold-start is amortised.

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

Build a GoFastr app live by chatting with a coding agent (Claude Code, pi, Codex, …). The agent drives Kiln's typed tool surface; the world IR mutates; the running app re-renders; the schema migrates — all in-process. Freeze the journal when done to emit canonical `entities/*.json` and graduate to regular Go source you commit.

```bash
go install ./cmd/kiln

kiln serve --agent claude-code    # auto-uses ~/.claude/.credentials.json
kiln serve --agent pi             # uses pi's installed config
kiln serve --agent auto           # picks the first installed CLI on PATH
kiln serve --agent "<freeform>"   # any command you want; prompt is appended

kiln mcp   --no-http              # MCP over stdio (subprocess harnesses)
kiln acp   --no-http              # ACP over stdio
kiln freeze --dir build/          # journal → build/entities/*.json + world.json
```

#### How it works

`kiln serve` runs an HTTP server (panel + SSE + REST tool dispatch + MCP at `/mcp`) and a floating chat widget that auto-mounts on every URL. When `--agent <name>` is set, kiln subscribes to its own SSE bus: every `chat_user` event spawns the configured CLI as a subprocess with `KILN_URL` injected. The CLI reads `~/.claude/skills/kiln/SKILL.md` (auto-installed), sees `$KILN_URL`, and drives the build with `curl` against HTTP. Stdout is journaled as `chat_assistant` so the panel renders the reply.

**Bring-your-own auth.** Kiln does not manage credentials. Each adapter spawns its CLI which manages its own login (`claude` reads `~/.claude/.credentials.json`, `pi` reads its own config, etc.). Adding a new agent is a one-entry change in `cmd/kiln/adapters.go`.

#### Safety: plan-gated destructive ops

Destructive tools (`delete_entity`, `delete_field`, `delete_page`, `delete_hook`, `delete_route`) are enforced at the protocol layer:

1. Agent calls `propose_plan` listing each destructive op in `targets`:

   ```json
   { "plan_id": "p1", "steps": ["drop posts"], "targets": [{"op":"delete_entity","name":"posts"}] }
   ```

2. The panel renders a plan card with **Approve** / **Reject** buttons.
3. After Approve, the agent retries the destructive call with `plan_id` set.

Without an approved plan whose `Targets` list matches, `delete_*` returns `{"ok":false,"kind":"needs_plan"}`. Each `(plan, target)` is single-use; reuse needs a new plan.

#### Wire into Claude Code as an MCP server

```json
{
  "mcpServers": {
    "kiln": { "command": "kiln", "args": ["mcp", "--no-http"] }
  }
}
```

#### Or hit the HTTP API directly

```bash
kiln serve --agent none --addr :8765 &
curl -X POST http://localhost:8765/kiln/tool/add_entity \
  -H 'Content-Type: application/json' \
  -d '{"entity":{"name":"posts","fields":[{"name":"title","type":"string","required":true}]}}'
curl http://localhost:8765/posts          # CRUD live
curl http://localhost:8765/kiln/world     # current IR
```

#### Tool surface

`world_get`, `set_app_config`, `add_entity`, `update_entity`, `delete_entity`, `add_field`, `delete_field`, `add_page`, `delete_page`, `add_hook`, `delete_hook`, `add_route`, `delete_route`, `add_seed`, `propose_plan`, `approve_plan`, `reject_plan`, `undo`, `chat`. See `kiln/protocol/descriptors.go` for full JSON schemas.

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
examples/    blog, api-tour (cursor/include/batch/SSE/uploads), core-ui-demo, demo, spa, static-site
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
