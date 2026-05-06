# GoFastr — Proposal

> A Go fullstack framework for the AI era. Core primitives + AI-friendly entity system. MCP-native. Compile-time safe. Tiny footprint.

---

## Problem

Building webapps is slow. Frameworks assume humans write every route, handler, query, and migration by hand. AI coding agents can generate this code, but no framework is designed for them as the primary author. Meanwhile, every app needs the same things: CRUD, auth, validation, API docs, file uploads, real-time — rebuilt every time.

## Solution

A two-layer Go framework:

1. **`core/`** — 12 low-level primitives (router, handler, middleware, query, MCP, schema, migrate, render, static, upload, stream, OpenAPI). Usable standalone. No magic.

2. **`framework/`** — entity system built on core. Declare an entity, get everything: DB table, type-safe Go structs, auto-CRUD routes, auto-MCP tools, validation, relationships, hooks, access control, OpenAPI docs, admin rendering. PayloadCMS-inspired.

AI agents generate entity declarations (Go or JSON). The framework compiles them into a working webapp. Humans drop to core primitives when they need custom behavior.

## Architecture

```
┌─────────────────────────────────────────────┐
│  framework/  (AI-friendly entity system)     │
│  Entity declarations → auto-CRUD, MCP, etc. │
│                 ↕ uses                       │
│  core/  (12 low-level primitives)            │
│                 ↕ built on                   │
│  Go stdlib                                   │
└─────────────────────────────────────────────┘

Batteries: auth, storage, cache, email — plug in or swap
```

## Core Primitives

| # | Primitive | Purpose |
|---|-----------|---------|
| 1 | Router | Register routes, match patterns, extract params |
| 2 | Handler | `func(ctx, input) (output, error)` — typed in/out |
| 3 | Middleware | `func(next) handler` — pipeline |
| 4 | Query | Build SQL queries, parameterized, composable |
| 5 | MCP | Register tools, handle calls, serve MCP protocol |
| 6 | Schema | Define field types, validate input, generate JSON Schema |
| 7 | Migrate | Versioned schema changes |
| 8 | Render | Write HTML to a writer |
| 9 | Static | Serve static files (embed, cache, fingerprint) |
| 10 | Upload | File uploads (multipart, validate, storage backends) |
| 11 | Stream | SSE, WebSocket, chunked responses |
| 12 | OpenAPI | Auto-generate spec from routes + schemas |

## Entity System (Framework Layer)

Declare an entity. The framework generates everything.

```go
app := gofastr.New("myapp")

app.Entity("posts", gofastr.EntityConfig{
    Fields: []gofastr.Field{
        {Name: "title", Type: gofastr.String, Required: true, Max: 200},
        {Name: "slug", Type: gofastr.String, Unique: true, Auto: "slugify(title)"},
        {Name: "body", Type: gofastr.Text},
        {Name: "published", Type: gofastr.Bool, Default: false},
        {Name: "author", Type: gofastr.Relation, To: "users"},
        {Name: "tags", Type: gofastr.Relation, To: "tags", Many: true},
        {Name: "image", Type: gofastr.Image}, // auto-wires Upload + Static
    },
    CRUD: true,   // → GET/POST/PUT/DELETE /posts
    MCP:  true,   // → list_posts, get_post, create_post, update_post, delete_post

    Endpoints: []gofastr.Endpoint{
        {Method: "POST", Path: "/posts/:id/publish", Handler: publishPost},
    },
    Validators: []gofastr.Validator{
        {Field: "title", Fn: noProfanity},
    },
    Hooks: gofastr.Hooks{
        BeforeCreate: generateSlug,
        AfterCreate:  notifySubscribers,
    },
    Access: gofastr.Access{
        Read:   gofastr.Public,
        Create: gofastr.Authenticated,
        Update: gofastr.Owner,
        Delete: gofastr.Role("admin"),
    },
})

// Or from JSON (AI-generated)
app.EntityFromFile("entities/posts.json")
app.EntitiesFromDir("entities/")

app.Run()
```

### What `CRUD: true, MCP: true` generates:

| Auto-generated | Routes | MCP Tools | OpenAPI |
|---|---|---|---|
| List | `GET /posts` | `list_posts` | ✅ |
| Get | `GET /posts/:id` | `get_post` | ✅ |
| Create | `POST /posts` | `create_post` | ✅ |
| Update | `PUT /posts/:id` | `update_post` | ✅ |
| Delete | `DELETE /posts/:id` | `delete_post` | ✅ |

Plus: DB migration, Go struct, JSON Schema, validation, relationship queries.

## Pluggable Batteries

Core defines interfaces. Batteries ship defaults. Swap anything.

| Concern | Core Interface | Built-in Battery | Swap For |
|---|---|---|---|
| Auth | GetCurrentUser, HasRole, IsOwner | Sessions + OAuth2 + Passkeys | Clerk, Auth0, Firebase |
| Storage | Upload, Download, Delete | Local filesystem | S3, GCS, Azure Blob |
| Cache | Get, Set, Delete | In-memory | Redis, Memcached |
| Email | Send | SMTP | Postmark, SendGrid |
| Search | Search, Index | Postgres full-text | Meilisearch, Elasticsearch |

## Compile-Time Safety

```
1. Declare entities (Go or JSON)
2. gofastr generate → Go code (structs, routes, queries, MCP defs, OpenAPI)
3. go build → compiler validates EVERYTHING
4. Binary ships with zero "route not found" or "field doesn't exist" bugs
```

Go's compilation step is a feature. The generated code is type-safe Go, not runtime reflection.

## MCP-Native

The app IS an MCP server. Every entity with `MCP: true` exposes tools. Custom tools via `app.Tool(...)`. AI agents at runtime can:
- Discover available tools
- Call CRUD operations
- Call custom endpoints
- Read OpenAPI schema
- Query entity relationships

Same Go functions serve HTTP and MCP. Dual interface, zero extra code.

## Security

- CSRF, XSS prevention, security headers — non-negotiable defaults
- Parameterized queries only — the query builder can't produce injection-vulnerable SQL
- Access control per entity operation — enforced on both HTTP and MCP
- Input validation from field declarations — every field validated
- Rate limiting built-in
- Secure cookies, sessions with rotation
- OWASP Top 10 covered

## Footprint Targets

| Metric | Target |
|---|---|
| Binary size | < 10 MB |
| RAM (basic app) | < 32 MB |
| Cold start | < 10 ms |
| Requests/sec | 10K+ (simple handler) |
| Docker image | < 20 MB |

Single static binary. Zero runtime deps. Go stdlib first.

## Build Pipeline

```
gofastr init "blog with posts and tags"   → scaffold project
gofastr generate                          → entity declarations → Go codegen
gofastr dev                               → hot-reload dev server
gofastr build                             → production binary
gofastr migrate                           → run DB migrations
```

All commands support `--json` for AI agent consumption.

## What's NOT in v1

- WASM frontend
- Complex component system (just basic HTML rendering)
- Multi-agent orchestration
- Event sourcing
- i18n
- Admin panel (v2, auto-generated from entities)

## Decisions Resolved

1. **Query language** ✅ Mix — core uses type-safe query structs, framework adds DSL parser that codegens to structs
2. **Codegen approach** ✅ Custom build step (`gofastr build`) with `gofastr dev` for hot-reload file watching
3. **Template rendering** ✅ Build our own type-safe template engine (Templ-inspired)
4. **Relationships** ✅ Flexible core structs + AI-friendly DSL + smart auto-loading. Codegen validates relations at compile time
5. **Name** ✅ GoFastr (can rename later)
6. **Auth** ✅ Pluggable — core interface, built-in battery, bring your own
7. **Architecture** ✅ `core/` (primitives) + `framework/` (entity system). Core usable standalone.
8. **Admin panel** ✅ V2
9. **Migrations** ✅ Auto-diff in dev, explicit versioned for prod
10. **Real-time** ✅ SSE v1, WebSocket v2
11. **Background jobs** ✅ Built-in goroutine pool + pluggable external
12. **Plugins** ✅ Registry with optional interface methods
13. **Testing** ✅ In-memory harness in v1
14. **Multi-tenancy** ✅ TenantID field + auto-scope middleware
15. **Pagination** ✅ Cursor-based default, offset fallback
16. **Soft delete** ✅ Entity toggle
17. **Events** ✅ In-process pub/sub. Sync hooks + async events.
18. **Custom endpoints → MCP** ✅ Opt-in with `MCP: true`. Not auto.

## Next Step

Build a spike: the Router + Handler + Schema primitives. Just enough to register a route with typed input/output and validate it. Prove the core API feels right before building the full entity system.
