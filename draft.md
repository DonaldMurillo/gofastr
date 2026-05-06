# GoFastr — Working Notes

> Not a spec. A conversation becoming a framework.

---

## What We're Building

A Go fullstack framework in two layers:
- **Core** — low-level primitives (router, query builder, MCP, HTTP handler building blocks)
- **Framework** — high-level AI-friendly fast prototyping (entities, auto-CRUD, auto-MCP, hooks, validators, rendering)

AI agents generate entity declarations → framework builds on core → ships a webapp in seconds.

The framework is a fast prototyping platform focused on AI delivering webapps.
But the core primitives are always there if you want to drop down and hand-craft.

### The Core Thesis

- **Coding agent first** — AI is the primary code author
- **MCP-native** — the app you build IS an MCP server. First-class citizen.
- **Compile-time checks** — entity declarations → codegen → `go build` catches everything
- **Security paramount** — baked in, not bolted on
- **Tiny footprint** — single binary, small RAM, fast cold start
- **Batteries included but extensible** — high-level defaults, low-level escape hatches

---

## Architecture: Core + Framework

```
framework/   ← AI-friendly layer (entities, auto-CRUD, hooks, validators, rendering)
    ↕ uses
core/        ← low-level primitives (router, query, MCP, HTTP, middleware)
    ↕ built on
Go stdlib
```

Someone could use just `core/` and ignore the entity system.
The sweet spot is `framework/` for AI-driven fast prototyping.

---

## All Decisions

1. **Query language** ✅ Mix — core uses type-safe query structs, framework adds DSL parser that codegens to structs
2. **Codegen** ✅ Custom build step (`gofastr build`) + `gofastr dev` with hot-reload file watching
3. **Templates** ✅ Build our own type-safe engine (Templ-inspired)
4. **Relationships** ✅ Flexible structs + DSL + smart auto-loading. Codegen validates at compile time.
5. **Name** ✅ GoFastr
6. **Auth** ✅ Pluggable — core interface, built-in battery, bring your own
7. **Architecture** ✅ `core/` (primitives) + `framework/` (entity system). Core usable standalone.
8. **Admin panel** ✅ V2, after template engine
9. **Migrations** ✅ Auto-diff in dev, explicit versioned for prod
10. **Real-time** ✅ SSE in v1, WebSocket v2. Stream primitive defines interface.
11. **Background jobs** ✅ Built-in goroutine pool default, pluggable for external queues
12. **Plugin system** ✅ Plugin registry — struct implementing optional interface (AddRoutes, AddMiddleware, AddTools, etc.)
13. **Testing** ✅ In-memory test harness in v1 (`gofastr.Test(t, app).Get("/posts").AssertStatus(200)`)
14. **Multi-tenancy** ✅ TenantID field on entities + middleware auto-scopes queries
15. **Pagination** ✅ Cursor-based by default, offset available. Auto on list endpoints.
16. **Soft delete** ✅ Entity toggle `{SoftDelete: true}` → adds deleted_at, auto-filters
17. **Events** ✅ In-process pub/sub. Sync hooks for modifying/canceling, async events for notifications.
    - Framework auto-emits: `entity.created`, `entity.updated`, `entity.deleted`
    - Custom events: `app.Emit("order.shipped", order)` / `app.Subscribe("order.shipped", handler)`
18. **Custom endpoints → MCP** ✅ Opt-in with `MCP: true`. NOT auto — you choose what's exposed as a tool.

---

## Entity System (PayloadCMS-inspired)

Declare an entity → framework derives everything:
- DB table + migrations
- Type-safe Go structs
- Auto-CRUD routes (GET/POST/PUT/DELETE) with cursor pagination
- Auto-MCP tools (list/get/create/update/delete)
- OpenAPI docs
- Built-in validation from field declarations
- Relationships (auto both sides, auto include/filter queries)
- Access control per operation
- Lifecycle hooks (before/after create/update/delete) — sync, can cancel
- Events (entity.created, entity.updated, entity.deleted) — async, fire-and-forget
- Custom endpoints on top of auto-CRUD
- Custom validators
- Custom MCP tools
- Soft delete option
- Multi-tenant scoping

```go
app.Entity("orders", gofastr.EntityConfig{
    Fields: []gofastr.Field{
        {Name: "status", Type: gofastr.Enum, Values: []string{"pending", "paid", "shipped"}},
        {Name: "total", Type: gofastr.Decimal},
        {Name: "customer", Type: gofastr.Relation, To: "users"},
        {Name: "tenant", Type: gofastr.Relation, To: "tenants"}, // multi-tenant
    },
    CRUD:       true,
    MCP:        true,
    SoftDelete: true,

    Endpoints: []gofastr.Endpoint{
        {Method: "POST", Path: "/orders/:id/cancel", Handler: cancelOrder, MCP: true}, // explicit opt-in to MCP
        {Method: "GET", Path: "/orders/dashboard", Handler: orderDashboard}, // HTTP only, no MCP
    },
    Validators: []gofastr.Validator{
        {Field: "total", Fn: validatePositiveAmount},
    },
    Hooks: gofastr.Hooks{
        BeforeUpdate: enforceStatusRules,
        AfterCreate:  notifySubscribers,
    },
    Access: gofastr.Access{
        Read:   gofastr.OwnerOrAdmin,
        Update: statusBasedAccess,
    },
})

app.Subscribe("order.created", func(ctx context.Context, order Order) {
    searchIndex.Update(ctx, order)
})

app.Subscribe("order.created", func(ctx context.Context, order Order) {
    email.SendConfirmation(ctx, order)
})
```

Also loadable from JSON:
```go
app.EntityFromFile("entities/posts.json")
app.EntitiesFromDir("entities/")
```

---

## Core Primitives (12)

These live in `core/`. Irreducible building blocks that `framework/` is built on.

1. **Router** — register routes, match patterns, extract params
2. **Handler** — `func(ctx, input) (output, error)` — typed in/out
3. **Middleware** — `func(next) handler` — pipeline
4. **Query** — build SQL queries, parameterized, composable
5. **MCP** — register tools, handle tool calls, serve MCP protocol
6. **Schema** — define field types, validate input, generate JSON Schema
7. **Migrate** — versioned schema changes
8. **Render** — write HTML to a writer (type-safe engine, Templ-inspired)
9. **Static** — serve static files (embed, cache, fingerprint)
10. **Upload** — file upload handler (multipart, validate, storage backends)
11. **Stream** — SSE (v1), WebSocket (v2), chunked responses
12. **OpenAPI** — auto-generate spec from routes + schemas, serve Swagger UI

Every primitive is usable standalone. The framework layer combines them.

---

## Framework Layer

Built on core primitives. The AI-friendly fast prototyping layer.

| Framework Concern | Core Primitives It Combines |
|---|---|
| Entity system | Schema + Query + Router + MCP + OpenAPI + Migrate |
| Auto-CRUD | Router + Handler + Query + Schema + OpenAPI |
| Hooks | Handler (wraps execution, sync) |
| Events | Stream + background jobs (async pub/sub) |
| Validators | Schema (extends validation) |
| Access control | Middleware (route guard + MCP tool guard) |
| Rendering | Render + Schema (entity-aware list/detail/form) |
| File/image fields | Upload + Static (auto-wired from entity field type) |
| Custom endpoints | Router + Handler + MCP + OpenAPI |
| Pagination | Query (cursor-based default) |
| Soft delete | Query (auto-filter) + Schema (deleted_at field) |
| Multi-tenancy | Middleware (tenant scope) + Query (auto-filter) |
| Testing | Handler + Router (in-memory, no real HTTP) |

---

## Pluggable Batteries

Core defines interfaces. Batteries ship implementations. Swap anything.

```
core/    → interface (what the framework calls)
battery/ → built-in implementation (sessions, OAuth2, passkeys, local storage, SMTP, in-memory cache, goroutine pool)
yours/   → implement the interface (Clerk, Auth0, S3, Redis, Postmark, RabbitMQ, etc.)
```

| Concern | Core Interface | Built-in Battery | Swap For |
|---|---|---|---|
| Auth | GetCurrentUser, HasRole, IsOwner | Sessions + OAuth2 + Passkeys | Clerk, Auth0, Firebase |
| Storage | Upload, Download, Delete | Local filesystem | S3, GCS, Azure Blob |
| Cache | Get, Set, Delete | In-memory | Redis, Memcached |
| Email | Send | SMTP | Postmark, SendGrid |
| Search | Search, Index | Postgres full-text | Meilisearch, Elasticsearch |
| Queue | Enqueue, Process | Goroutine pool | Redis, RabbitMQ, SQS |

---

## Plugin System

```go
type Plugin interface {
    Name() string
}

// Optional methods — framework calls whatever the plugin implements
type RoutesPlugin interface {
    AddRoutes(router *core.Router)
}
type MiddlewarePlugin interface {
    AddMiddleware(stack *core.MiddlewareStack)
}
type ToolsPlugin interface {
    AddTools(server *core.MCPServer)
}
type HooksPlugin interface {
    AddHooks(app *framework.App)
}
type CLIPlugin interface {
    AddCommands(root *cobra.Command)
}

// Usage
app.Register(stripeplugin.New("sk_live_..."))
```

---

## Event System

```go
// Subscribe to entity lifecycle events (auto-emitted)
app.Subscribe("post.created", func(ctx context.Context, post Post) { ... })
app.Subscribe("post.updated", func(ctx context.Context, post Post) { ... })
app.Subscribe("post.deleted", func(ctx context.Context, post Post) { ... })

// Custom events
app.Emit("order.shipped", order)
app.Subscribe("order.shipped", func(ctx context.Context, order Order) {
    email.SendShippingNotification(ctx, order)
})

// Sync hooks vs async events:
// Hooks: BeforeCreate, AfterCreate — sync, can cancel/modify
// Events: entity.created — async, fire-and-forget, multiple subscribers
```

---

## Testing

```go
func TestListPosts(t *testing.T) {
    res := gofastr.Test(t, app).Get("/posts")
    res.AssertStatus(200)
    res.AssertJSON(t, []Post{{Title: "Hello"}})
}

func TestCreatePost(t *testing.T) {
    res := gofastr.Test(t, app).
        AsUser(user).           // authenticated request
        Post("/posts", Post{Title: "New", Body: "Hello"})
    res.AssertStatus(201)
    res.AssertJSON(t, Post{Title: "New"})
}
```

In-memory, no real HTTP server. Fast.

---

## Build Pipeline

```
gofastr init "blog with posts and tags"   → scaffold project + entities
gofastr dev                               → hot-reload: watch → generate → build → restart
gofastr build                             → codegen + go build → production binary
gofastr migrate create "add age to users" → explicit migration for prod
gofastr migrate                           → run pending migrations
gofastr test                              → run tests with framework helpers
```

All commands support `--json` for AI agent consumption.

---

## V1 Scope

**In:**
- All 12 core primitives
- Entity system with auto-CRUD, MCP, OpenAPI, validation, hooks, events
- Pluggable auth, storage, cache, email, search, queue
- Type-safe template engine
- SSE streaming
- Plugin system
- Testing harness
- Cursor pagination, soft delete, multi-tenancy
- CLI with hot-reload

**V2:**
- Admin panel (auto-generated from entities)
- WebSocket support
- Admin dashboard UI

---

## Research Files

- [JS Frameworks](./research-js-frameworks.md)
- [Go Ecosystem](./research-go-frameworks.md)
- [Security](./research-security.md)
- [Performance](./research-perf.md)
- [Agent-First Design](./research-agent-first.md)
- [Agentic Protocols (AG-UI, MCP, A2UI)](./research-agentic-protocols.md)
- [JSON-Driven Architecture](./research-json-driven.md)
