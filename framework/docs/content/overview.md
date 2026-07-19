# Overview

GoFastr is a full-stack Go framework. You build the whole app in Go — database
schema and migrations, a REST API, and a server-rendered UI — plus opt-in
batteries for auth, background jobs, search, and storage. Everything it
generates is plain Go on disk that you own: no reflection, no runtime you're
stuck inside. When it's in your way, drop to `core/`, `net/http`, or
`database/sql`.

Agents work with it two ways. In production, the agents your users bring call
your data over MCP, with the same login and permissions your users have. In
development, `gofastr dev` gives your coding agent (Claude Code, Codex) the
running app's routes, config, and logs over MCP, so it can help you build and
debug.

This page lists every feature and links to its doc. Read it once, then jump to
what you need. New here? Start with
[Get started](/get-started) and [Project structure](/docs/project-structure).

## Two layers

GoFastr is two layers, and you can work at either one.

**`core/` + `core-ui/` — the primitives.** stdlib-first building blocks: router,
query, schema, render, mcp, openapi (core); HTML primitives, signals, the
runtime (core-ui). Each works on its own, no framework required:

```go
// core only — a router and a handler.
r := router.New()
r.Get("/", render.HTMLHandler(func(req *http.Request) render.HTML {
    return render.Tag("h1", nil, render.Text("Hello from core."))
}))
http.ListenAndServe(":8080", r)
```

**`framework/` + `framework/ui/` — the opinionated layer.** Built on those
primitives. Declare an entity and the framework wires them together for you — a
migrated table, a REST API with filtering/sorting/pagination, MCP tools, and an
OpenAPI spec:

```go
// framework — one entity, wired end to end.
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", framework.EntityConfig{
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
        {Name: "body",  Type: schema.Text},
    },
})
app.Start(":8080")
```

See [Entity declarations](/docs/entity-declarations). Generated code is regular
Go at the module root (`main.go`, `app.go`, `screens.go`, `entities/`) — read
it, edit it, commit it. When the framework is in your way, drop back to `core/`.
You can also scaffold a set of entities from a `gofastr.yml` with the
[code generator](/docs/codegen); the running app never needs the file.

(Only `core/middleware` pulls in a dependency — OpenTelemetry, for tracing.
Everything else in `core/` is stdlib-only.)

## Modeling your domain

Declare entities; get their tables, routes, and tools.

- **[Entity declarations](/docs/entity-declarations)** — Go or a `gofastr.yml`;
  both produce the same tables, routes, and tools. Set `OwnerField` for
  per-user data.
- **[Filter DSL](/docs/query-dsl)** —
  `?status=published&views_gte=10&sort=-created_at` parses to a typed `Where`.
- **[Eager loading](/docs/includes)** — `?include=author.profile` flattens the N+1.
- **[Cursor pagination](/docs/cursor-pagination)** — keyset paging, opt-in.
- **[Hooks & transactions](/docs/hooks-and-transactions)** — `BeforeCreate` /
  `AfterUpdate` hooks share the parent transaction.
- **[Batch endpoints](/docs/batch-endpoints)** — create, update, or delete many
  rows in one request. **[Migrations](/docs/migrations)** — versioned, ordered,
  reversible.
- **[Multi-tenant scope](/docs/multi-tenant)** — automatic `tenant_id` filtering.

## Serving HTTP

The middleware between the socket and your handler, on by default.

- **[Auth](/docs/auth)** — login, OAuth, magic-link, 2FA, password reset; each a
  plugin. **[Access control](/docs/access-control)** — roles, permissions,
  policies.
- **[Security defaults](/docs/security)** — CSP, CSRF, rate limit, headers.
- **[Idempotency](/docs/idempotency)** — an `Idempotency-Key` header replays
  mutations safely. **[Webhooks](/docs/webhooks)** — signed outbound delivery
  with retry. **[Notifications](/docs/notifications)** — multi-channel delivery.
- **[Health checks](/docs/health-checks)** · **[Plugins](/docs/plugins)** — the
  lifecycle every battery uses.

## Building UI

Server-rendered with islands: every page is full HTML on first load; a small
runtime attaches handlers to the existing DOM; in-page changes (sort, paginate,
add a row) are island calls that swap one fragment — no hard refresh, no client
re-render.

- **[Getting started (UI)](/docs/ui-getting-started)** — scaffold, theme,
  screen, custom component. **[UI wiring](/docs/ui-wiring)** — adding the UI to
  a plain `framework.App` by hand.
- **[Theming](/docs/theming)** — token catalog, dark mode, section overrides,
  `--ui-*` vars. **[Runtime contract](/docs/runtime-contract)** — the
  SSR/hydration/island/SSE model and the full `data-fui-*` reference.
- **[New components](/docs/ui-new-components)** — the minimal-register,
  SSR-inline, hydrate contract. **[Widget builder](/docs/widgets)** — islands
  that hydrate against a registered handler.
- **[Interactive patterns](/docs/interactive-patterns)** ·
  **[Signal store](/docs/signal-store)** — client state shared by many
  consumers from one declaration.
- **[Forms](/docs/form-module)** — server-validated, with island-swapped error
  states.
- **[PWA](/docs/pwa)** — installable manifest and an offline shell via
  `uihost.WithPWA`; works live and in static exports.
- **[Image pipeline](/docs/image)** — pure-Go resize and WebP.
  **[Print documents](/docs/print)** — print-friendly HTML and PDF.
  **[Runtime modules](/docs/runtime-minification)** — split per feature, so a
  page ships only the JS it uses.
- Browse every component live in the **[gallery](/components/)**.

## Persisting & migrating

SQLite and Postgres, dialect-aware.

- **[Audit log](/docs/audit-log)** — a row per Create, Update, and Delete.
- **[Isolation](/docs/isolation)** — a separate local DB per git worktree.
- **[Factories](/docs/factories)** — fixtures for tests.
  **[Full-text search](/docs/search)**.
- **[Uploads](/docs/uploads)** — file and image fields with pluggable storage.
- **[Env / .env](/docs/dotenv)** — auto-loaded by `NewApp`.

## Working with agents

- **[Agent-readiness](/docs/agent-ready)** — per-entity MCP tools, auto
  `llm.md`, and the discovery endpoints your app serves.
- **[Embed](/docs/embed)** — local semantic search, no API key.
  **[Audit deps](/docs/audit-deps)** — flag packages an agent shouldn't import.
- **[Kiln](/docs/kiln)** — experimental build-mode binary: an agent edits an
  in-memory model over HTTP.

## Operations

- **[Logging](/docs/log)** — structured JSON, with MCP query tools.
- **[Feature flags](/docs/feature-flags)** · **[i18n](/docs/i18n)** ·
  **[Cron](/docs/cron)** · **[Events](/docs/events)** ·
  **[Admin UI](/docs/admin)**.

## Reference

- **[Benchmarks](/docs/benchmarks)** ·
  **[Performance results](/docs/perf-results)** — how it performs and how
  that's measured.
- **[Code generation](/docs/codegen)** — what the scaffold writes and how to
  read it.
- The **[full A–Z index](/docs/)** lists every doc.

## Where to go next

1. **[Get started](/get-started)** — a running app in a few minutes.
2. **[Entity declarations](/docs/entity-declarations)** — the core of the model.
3. **[Examples](/examples)** — reference apps, smallest first.
4. **[Components](/components/)** — every UI component, one page each.

Every doc is grounded in the code, and each guide ends with a "common mistakes"
note. The same content is available offline with `gofastr docs`.
