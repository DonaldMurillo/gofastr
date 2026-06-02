# Overview — what GoFastr is, and everything it does

GoFastr is a full-stack Go framework where **AI agents are first-class
authors**. You describe a domain — entities, fields, relations — as a typed
declaration, and the framework generates the database schema, a REST API, MCP
tools for agents, OpenAPI, and a typed Go model from that one source. Everything
else is regular, readable Go you can grep, debug, and step through. No
reflection magic, no opaque runtime.

**The promise:** opinionated input, boring output, small runtime, easy escape
hatches. GoFastr is a **code-generation platform for CRUD-heavy and AI-authored
apps** — not a universal framework that owns your control flow. Start with one
entity and add only what you need; when the framework is in your way, drop to
`core/` and write plain `net/http`. And because an agent often writes the code,
the output is built to be *more* inspectable, not less — plain Go on disk, no
reflection injection or hidden registries.

This page is the map. It explains the shape of the framework and links every
feature to its reference doc. If you're new, read this top-to-bottom once, then
jump to the section that matches what you're building.

> New to the framework? Pair this with **[Get started](/get-started)** for the
> cold-machine-to-running-app path, and **[Philosophy](/philosophy)** for the
> convictions behind the design.

## The one idea

Most frameworks assume a human hand-writes every route, query, validator,
migration, and form. Agents already generate that code — but no framework treats
their output as the canonical source. GoFastr inverts that: the **entity
declaration is the source**, and the database, API, agent tools, and admin UI
are generated from it. The agent writes the declaration; so does the human; the
framework is what they both write to.

```go
app := framework.NewApp()
app.Entity("posts", entity.EntityConfig{
    Fields: []schema.Field{
        {Name: "title",     Type: schema.String,  Required: true},
        {Name: "body",      Type: schema.Text},
        {Name: "published", Type: schema.Bool},
    },
})
app.Start(":8080")
```

That declaration alone gives you migrated tables, a full CRUD REST API with
filtering/sorting/pagination, MCP tools an agent can call, OpenAPI, and a typed
Go model — see **[Entity declarations](/docs/entity-declarations)**.

## Two layers

Two packages, no more:

- **`core/`** — twelve stdlib-only primitives (router, query, schema, mcp,
  openapi, render, markdown, …), each independently usable. When the framework
  is in your way, you drop down to `core` and write plain Go.
- **`framework/`** — the opinionated entity layer composed on top: entities,
  CRUD, hooks, migrations, plugins, and the batteries below.

There is no third layer of hidden magic. Generated code is regular Go that lands
on disk under `gen/` — see **[Code generation](/docs/codegen)** and
**[Blueprints](/docs/blueprints)**.

## Modeling your domain

One typed declaration fans out into every surface.

- **[Entity declarations](/docs/entity-declarations)** — JSON or Go; both
  produce the same tables, routes, and tools. Set `OwnerField` for per-user data.
- **[Filter DSL](/docs/query-dsl)** — `?status=published&views_gte=10&sort=-created_at`
  parses to a typed `Where`.
- **[Eager loading](/docs/includes)** — `?include=author.profile` flattens the N+1.
- **[Cursor pagination](/docs/cursor-pagination)** — keyset paging, opt-in.
- **[Hooks & transactions](/docs/hooks-and-transactions)** — `BeforeCreate` /
  `AfterUpdate` hooks share the parent transaction.
- **[Batch endpoints](/docs/batch-endpoints)** — create/update/delete many in one
  request. **[Migrations](/docs/migrations)** — versioned, ordered, reversible.
- **[Multi-tenant scope](/docs/multi-tenant)** — automatic `tenant_id` filtering.

## Serving HTTP

Everything between the wire and your handler is on by default.

- **[Auth](/docs/auth)** — login, OAuth, magic-link, 2FA, password reset; each a
  plugin. **[Access control](/docs/access-control)** — roles, permissions, policies.
- **[Security defaults](/docs/security)** — CSP, CSRF, rate limit, headers.
- **[Idempotency](/docs/idempotency)** — an `Idempotency-Key` header replays
  mutations safely. **[Webhooks](/docs/webhooks)** — signed outbound delivery with
  retry. **[Notifications](/docs/notifications)** — multi-channel fan-out.
- **[Health checks](/docs/health-checks)** · **[Plugins](/docs/plugins)** — the
  lifecycle every battery plugs into.

## Building UI

Server-rendered with islands: every page is fully SSR on first load; the runtime
hydrates handlers onto the existing DOM; in-page state changes are island RPCs
that swap just one fragment — no hard refreshes, no client re-render.

- **[Getting started (UI)](/docs/ui-getting-started)** — scaffold → theme →
  screen → custom component.
- **[New components](/docs/ui-new-components)** — the minimal-register +
  SSR-inline + hydrate contract. **[Widget builder](/docs/widgets)** — islands
  that hydrate against a registered handler.
- **[Interactive patterns](/docs/interactive-patterns)** · **[Signal store](/docs/signal-store)** —
  client state that fans out to many consumers from one declaration.
- **[Forms](/docs/form-module)** — server-validated, island-swapped error states.
- **[Image pipeline](/docs/image)** — pure-Go resize + WebP. **[Print documents](/docs/print)** —
  chrome-free print/PDF. **[Runtime modules](/docs/runtime-minification)** —
  carved per-feature so a page ships only the JS it uses.
- Browse every primitive live in the **[component gallery](/components/)**.

## Persisting & migrating

SQLite and Postgres, dialect-aware.

- **[Audit log](/docs/audit-log)** — a row per Create/Update/Delete.
- **[Isolation](/docs/isolation)** — per-worktree isolated local DBs.
- **[Factories](/docs/factories)** — fixtures for tests. **[Full-text search](/docs/search)**.
- **[Uploads](/docs/uploads)** — file/image fields with pluggable storage.
- **[Env / .env](/docs/dotenv)** — auto-loaded by `NewApp`.

## Working with agents

- **[Kiln](/docs/kiln)** — the agent-driven build-mode binary: mutate an
  in-memory IR over HTTP, no Go/JS/SQL by hand.
- **[Embed](/docs/embed)** — local semantic search via brute-force cosine, no API
  key. **[Audit deps](/docs/audit-deps)** — flag packages an agent shouldn't import.
- **[Blueprints](/docs/blueprints)** — reusable bundles of entities + screens an
  agent can apply. **[Agent notes](/docs/agent-notes)** — append-only review log.

## Operations

- **[Logging](/docs/log)** — structured JSON with MCP query tools.
- **[Feature flags](/docs/feature-flags)** · **[i18n](/docs/i18n)** ·
  **[Cron](/docs/cron)** · **[Events](/docs/events)** · **[Admin UI](/docs/admin)**.

## Reference & internals

- **[Benchmarks](/docs/benchmarks)** · **[Performance results](/docs/perf-results)** —
  how it performs and how that's measured.
- **[Codegen](/docs/codegen)** — what lands under `gen/` and how to read it.
- The **[full A–Z index](/docs/)** lists every embedded doc — nothing is hidden.

## Where to go next

1. **[Get started](/get-started)** — running app in four minutes.
2. **[Entity declarations](/docs/entity-declarations)** — the heart of the model.
3. **[Examples](/examples)** — six reference apps, smallest first.
4. **[Components](/components/)** — every UI primitive, one page each.

Every doc is grounded in the actual code and ends with a "common mistakes"
callout. This same content is browsable offline with `gofastr docs`.
