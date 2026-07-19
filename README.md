# GoFastr

> One `gofastr.yml` blueprint becomes a server-rendered UI **and** a REST API with secure per-user scopes — generated as plain Go you read, commit, and own.

GoFastr is an experimental Go full-stack framework built around that one wedge. A blueprint *scaffolds* the whole app — database schema and migrations, REST CRUD with validation and owner/RBAC scoping, a server-rendered UI with island hydration, plus an OpenAPI spec and MCP tools for agents — and emits it as plain Go. The blueprint is an on-ramp, not a runtime: once it has scaffolded, the **generated Go is the source of truth**, owned and edited like any code you wrote by hand — and you never give up `database/sql`, `net/http`, or compile-time safety.

The start-to-finish walkthrough is the [blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md).

## Why this exists

This is a personal project first — a way to practice building something
large alongside AI. A few things I wanted to dig into:

- **Solidify my web-tech foundations.** Rebuild the stack from the
  socket up so the fundamentals stop feeling like magic.
- **Attack UI generation from a different angle.** My background is in
  Node, so I wanted to see what server-rendered, server-driven UI looks
  like when you take the heavy client framework off the table and
  generate the markup in a compiled language instead.
- **Work in a compiled language.** Most of what I've built is in Node,
  where mistakes surface at runtime, in production. I wanted to know what
  it's like when a compiler catches them first — when you ship one binary
  and types actually hold up under a refactor.
- **Skip the convention-vs-configuration false choice.** When it's your
  own framework you don't have to pick a side — you get opinionated
  defaults *and* a hatch down to plain stdlib code in the same app.
- **Build something large, fun, and open source with AI.** Most of this
  repo was written alongside coding agents, so the workflow itself is
  part of the experiment.
- **Make a framework that's AI-first both ways.** AI *authors* the app
  — a small blueprint is a cheap prompt target — and AI *consumes* the
  running app, because every entity emits MCP tools over a live server.
  Low surface to author, high surface generated.

> **Status:** early / `v0.x` — MIT-licensed and usable, but the API may change
> between releases, so pin a version (`go get …@v0.x.y`). A `v1.0.0` tag will
> mark the stability promise. Ship at your own risk until then.

> **Validation status (honest version).** The declaration → surfaces pipeline is
> proven end-to-end by [`examples/meridian`](examples/meridian/) — the flagship:
> one `gofastr.yml` becomes a SaaS billing console (customers, subscriptions,
> invoices with status workflows, MRR + charts) *and* its marketing site, auth,
> RBAC, and admin back-office, with writable app screens (add/edit/delete) — and
> by [`examples/ecommerce`](examples/ecommerce/), which adds owner-scoped per-user
> data (orders/order-items per customer, anonymous → 401, cross-user → 404). Both
> are secure by default and carry a generated end-to-end test suite — every
> screen, the full create→edit→delete lifecycle, and RBAC asserted — with zero
> hand-written app code. The blueprint quickstart
> below is CI-gated by an executable-README test
> (`cmd/gofastr/readme_quickstart_test.go`) that extracts the fenced blocks,
> generates, builds, boots, and curls the documented app. GoFastr's own
> build-mode tooling and docs site are built on the framework (see
> [Built with GoFastr](#built-with-gofastr)). What's still open is external
> production adoption and load — this is a thesis framework, and that's stated
> plainly rather than dressed up.

**The promise:** opinionated input, boring output, small runtime, easy escape
hatches. You write a typed declaration; the framework emits plain Go you can
read, debug, and step through — then gets out of the way. It's a **code-
generation platform for CRUD-heavy and AI-authored apps**, not a universal
framework that owns your control flow. When it's in your way, drop to `core/`
and write `net/http`.

A corollary for the AI era, and it cuts both ways. On the **output** side, an
agent writing the code is a reason for it to be *more* inspectable, not less —
normal Go on disk, no reflection injection, no hidden registries, no runtime
mutation. On the **input** side, an agent doesn't need to learn the framework to
author an app; it needs the blueprint schema — small, documented YAML. Low
surface to author, high surface generated: one tool call scaffolds a multi-entity
app. And once that app is running, agents reach it through **MCP into the live
server** — introspecting its routes, config, and docs and driving the per-entity
tools — instead of re-reading a file. Author-time is the blueprint; run-time is
the live MCP surface.

---

## Why

Most Go web frameworks assume a human will hand-write every route, query, validator, migration, form, and authorization check. That glue is exactly what a declaration can generate — and increasingly the declaration is written by an AI agent, which makes inspectable output matter more, not less. GoFastr's bet:

- **The blueprint is a generator, not a source of truth.** A single `gofastr.yml` scaffolds a SQL schema, typed Go models, REST routes, *and* server-rendered screens with nav and islands — both halves of the app, emitted together in one pass so they start consistent. Then it gets out of the way: the generated Go is yours to own and edit, and the running app doesn't need the blueprint at all. It's an on-ramp — `rails g scaffold` / `ng generate` for a whole Go backend *and* UI — not a declaration you live in. See [`examples/meridian`](examples/meridian/) for the whole pipeline — a polished SaaS console + marketing site — live and tested.
- **Secure scopes are part of the declaration.** `owner_field` makes auto-CRUD per-user (anonymous → 401, cross-user → 404), `access:` gates operations behind RBAC permissions (fail-closed 403), `multi_tenant` scopes by tenant — and `gofastr validate` errors when per-user data is exposed without any of them.
- **You own the output.** The generated code is normal Go you read, debug, commit, edit, and compose from your own `main` — not a black box the tool rewrites behind your back. No reflection magic, no hidden registries, no runtime mutation, no platform between your binary and your server.
- **Agent surfaces come free.** The same declaration emits an OpenAPI 3 spec and five MCP tools per entity (`products_list`, `products_create`, …) that respect the same owner/RBAC scopes — a byproduct of the pipeline, not a bolt-on.
- **Two-layer architecture.** A small `core/` of stdlib-first primitives sits under an opinionated `framework/`. Drop down to core when the framework is in your way. (The one external touchpoint is `core/middleware/tracing.go`, which pulls in OpenTelemetry; the rest of `core/` is stdlib-only.)
- **Batteries included, not embedded.** Auth, cache, email, queue, search, storage are independent packages behind narrow interfaces — swap any one without forking.

## The repo in 60 seconds

| Directory | What it is | Depend on it when… |
|---|---|---|
| `core/` | Stdlib-only primitives — router, query, schema, render, mcp, openapi, migrate. Each usable on its own. | you want plain Go building blocks, no framework. |
| `framework/` | The opinionated entity layer (`App`, `EntityConfig`, CRUD, hooks, migrations). A thin facade re-exporting ~25 subpackages. | you want one declaration → SQL + REST + OpenAPI + MCP. |
| `core-ui/` | Server-driven UI runtime — `html` primitives, `patterns`, `widget` islands, signals, the vanilla-JS runtime. Independently usable. | you're rendering HTML from Go. |
| `battery/` | Opt-in infrastructure — admin, auth, cache, email, embed, log, notify, print, queue, search, storage, webhook. Each behind a small interface. | you need a real subsystem; import only the ones you use. |
| `cmd/gofastr` | The CLI — `init`, `generate`, `pack` (generate's inverse), `migrate`, `dev`, `docs`. | you're scaffolding or generating code. |
| `kiln` | Experimental agent build-mode runtime (mutate an in-memory IR over HTTP). | you're driving the app from an agent. |
| `examples/` | Runnable reference apps — the `meridian` blueprint flagship (a SaaS billing console + marketing site), the `ecommerce` blueprint pipeline, plus blog, api-tour, spa, and the docs site. | you want to see it wired end-to-end. |

You import `framework` and the batteries you opt into — not 17 packages. The
subpackage split is an internal seam (see `framework/ARCHITECTURE.md`); the
public surface is `framework.X` plus the batteries you reach for.

## Built with GoFastr

The framework runs on itself — GoFastr's own tooling and reference apps are built
on the same `framework`, `core-ui`, and batteries a user app imports:

- **`examples/site`**, the docs site and canonical component gallery, runs on
  `framework` + `framework/ui` + `framework/uihost` + the `core-ui` pattern
  presets + `battery/print`.
- **`examples/meridian`**, the declaration-first flagship, *is* generated from a
  `gofastr.yml` blueprint — a believable SaaS billing console (customers,
  subscriptions, invoices with status workflows, MRR + charts) *and* its public
  marketing site, auth, RBAC, and admin back-office, with writable app screens
  (add/edit/delete) and the generated end-to-end test suite green.
- **`examples/ecommerce`**, a second blueprint pipeline (five entities,
  owner-scoped orders), is generated the same way and exercised by its own
  surface test.

These are the tools the project uses on itself, not demos wired up for a
screenshot. External production adopters are the part still ahead of us — see
the validation status near the top.

## Quickstart

Requires Go 1.26+.

**The smallest app.** One entity is a complete server — a migrated table, REST
CRUD, an OpenAPI spec, and MCP tools. Add only what you need from there.

```go
package main

import (
	"database/sql"
	"log"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, _ := sql.Open("sqlite3", "app.db")
	app := framework.NewApp(framework.WithDB(db))

	// CRUD is auto-on when a DB is set (CRUD *bool: nil = auto).
	app.Entity("posts", framework.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	})

	log.Fatal(app.Start(":8080")) // GET/POST /posts, /openapi.json, MCP — all live
}
```

That's the whole program. No config files, no codegen step, no registration
boilerplate — and nothing you didn't ask for. Reach for entities-as-JSON,
batteries, the UI runtime, or the generator only when a real need shows up.
For how a flat app grows into `internal/<domain>/` as boundaries appear, see
[project structure](framework/docs/content/project-structure.md) — structure
follows the app, not the other way around.

Install the CLIs straight from GitHub:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
```

Or clone for development on the framework itself:

```bash
git clone https://github.com/DonaldMurillo/gofastr.git
cd gofastr
go test ./...                        # full suite needs Docker (Postgres testcontainers) and Chrome (chromedp e2e)
go run ./cmd/gofastr --help          # CLI overview
go run ./examples/blog               # minimal blog with auto-CRUD on SQLite
```

Linked Git worktrees automatically get isolated local ports and database
paths when isolation is enabled in `gofastr.yml`; see
[`framework/docs/content/isolation.md`](framework/docs/content/isolation.md).

### Updating GoFastr

The module dependency and the installed CLI are versioned
independently — keep them on the same release. Read the [release
notes](https://github.com/DonaldMurillo/gofastr/releases) for the
release you're moving to first (breaking changes are marked), then:

```bash
go list -m -versions github.com/DonaldMurillo/gofastr    # what's available
go get github.com/DonaldMurillo/gofastr@vX.Y.Z           # the app dependency
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@vX.Y.Z  # the CLI (doesn't update with go.mod)
go mod tidy && go build ./... && go test ./...
go list -m github.com/DonaldMurillo/gofastr              # confirm the selected version
```

Or let the CLI guide it: `gofastr upgrade` reads your `go.mod`, lists
every migration note between your version and the target, and points at
the affected lines in your code (`--apply` runs the steps). Full guide:
[`framework/docs/content/upgrading.md`](framework/docs/content/upgrading.md)
or `gofastr docs upgrading`.

With the blog example running (`go run ./examples/blog`), open
<http://localhost:8080> and try:

```bash
curl http://localhost:8080/posts
curl 'http://localhost:8080/posts/search?q=gofastr'   # custom route the example adds
# /openapi.json is auth-gated by default (it enumerates every route).
# Browse it via Swagger UI at /api/docs/, or expose the raw spec with
# framework.WithPublicOpenAPI() and then:
curl http://localhost:8080/openapi.json | jq .info     # auto-generated spec
```

### Declare an app (blueprint)

A `gofastr.yml` blueprint is the declaration-first format — one file describing
entities, screens, nav, endpoints, and seed data:

```yaml
# gofastr.yml
app:
  name: Blog
  module: example.com/blog

entities:
  - name: users
    crud: true
    access:            # email is PII — gate it; `gofastr validate` rejects
      read: users:read #   auto-exposed PII with no owner_field/access/tenant
      create: users:write
      update: users:write
      delete: users:admin
    fields:
      - name: name
        type: string
        required: true
      - name: email
        type: string
        required: true
        unique: true

  - name: posts
    crud: true
    mcp: true
    soft_delete: true
    public: true      # blog posts are public content; anonymous read AND write.
                       # Omit this and auto-CRUD requires a session by default.
    fields:
      - name: title
        type: string
        required: true
      - name: body
        type: text
      - name: status
        type: enum
        values: [draft, published]
        default: draft
      - name: author_id
        type: relation
        to: users
```

Generate the app — SQL schema + REST + OpenAPI + MCP + UI — as plain Go you
commit. From an empty directory containing that `gofastr.yml`:

```bash
go mod init example.com/blog
gofastr generate --from=gofastr.yml    # scaffolds main.go + app.go + screens.go + entities/ — owned Go you commit
go mod tidy                            # pulls gofastr from the module proxy
gofastr dev                            # hot-reload dev server — users + posts CRUD under /api, OpenAPI, MCP on :8080
```

The blueprint mounts its REST API under `/api` by default (`GET /api/posts`),
leaving bare paths free for HTML `screens`. Set `app.api_prefix: ""` to serve
entities at the bare path instead. MCP tools and the OpenAPI spec follow the
prefix automatically.

`gofastr generate` is a one-shot generator: it emits ordinary owned Go
(flat `package main` at the root) and gets out of the way. It refuses to
overwrite an existing project — pass `--force` to regenerate, or use
`generate --add` / `generate entity <name>` to scaffold *new* files into
an existing app (owned files are never touched). The blueprint is a
scaffold you can delete once the generated Go is yours — see
[ARCHITECTURE.md](framework/ARCHITECTURE.md).

See [`examples/meridian`](examples/meridian/) for the flagship blueprint — a SaaS
console + marketing site generated, built, and tested end-to-end — or
[`examples/ecommerce`](examples/ecommerce/) for a five-entity owner-scoped pipeline.

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

Both forms produce the same OpenAPI and MCP tools. The route shapes below are
identical too — the blueprint just serves them under its `app.api_prefix`
(default `/api`), while the Go form above mounts at the bare path unless you
add `framework.WithAPIPrefix`.

## What you get from one entity declaration

| Surface          | Auto-generated                                                                  |
|------------------|---------------------------------------------------------------------------------|
| HTTP             | `GET / POST /posts`, `GET / PUT / PATCH / DELETE /posts/{id}`                   |
| Batch endpoints  | `POST / PATCH / DELETE /posts/_batch` — atomic; one tx for all items            |
| SSE stream       | `GET /posts/_events` — entity.created/updated/deleted, scoped per tenant        |
| Filtering        | `?status=published&views_gte=10&sort=-created_at&page=2`                        |
| Eager loading    | `?include=author.profile,comments` — flat or nested, validated against the registry |
| Cursor paging    | `?cursor=&limit=50` — keyset by `EntityConfig.CursorField` (defaults to PK)     |
| Multipart upload | `multipart/form-data` on `Image`/`File` fields → streamed through `WithFileStorage` |
| Validation       | Required, unique, enum, min/max, regex pattern, multi-tenant scope              |
| Migrations       | Production-hardened versioned runner — advisory-lock serialization, checksum-drift + dirty-state guards, `NoTransaction` escape hatch, reversible down; declarative incremental generation; real-Postgres tested |
| FK constraints   | BelongsTo relations emit `FOREIGN KEY` clauses; `AutoMigrate` topo-sorts tables |
| Transactions     | `Create/Update/Delete` + hooks share one tx; `TxFromContext(ctx)` exposes it    |
| OpenAPI 3        | `/openapi.json` and Swagger UI at `/docs/`                                      |
| MCP              | `posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete`       |
| Soft delete      | `deleted_at` column + automatic filter                                          |
| Multi-tenant     | `tenant_id` column + automatic scope from request context                       |
| Hooks            | `BeforeCreate`, `AfterUpdate`, etc. for custom behaviour                        |
| Custom routes    | `EntityConfig.Endpoints` with optional MCP exposure                             |
| Client SDKs      | `gofastr generate sdk` — downloadable Go module + JS/TS client, hosted by the app with a live docs site (`framework/sdkdocs`) |
| Customer CLI     | `gofastr generate cli` — a branded terminal client for your customers, scoped API-token auth |

### Walkthrough: the read/write API

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

# Sparse update — omitted fields are preserved:
curl -X PATCH http://localhost:8080/posts/p1 \
  -H 'Content-Type: application/json' -d '{"status":"published"}'
# → {"data":{"id":"p1", …, "status":"published"}}
```

Single-record create, get, PUT, and PATCH responses all use
`{"data": {...}}`; list responses use `{"data": [...]}` plus pagination
metadata. Errors retain their `{"error": ..., "success": false, "code": ...}`
shape, and DELETE returns no body.

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

### `core/` — nineteen stdlib-first primitives

`config`, `dotenv`, `fanout`, `featureflag`, `handler`, `i18n`, `markdown`, `mcp`, `middleware`, `migrate`, `openapi`, `query`, `render`, `router`, `schema`, `static`, `stream`, `upload`, `yaml`. Each is independently usable; the framework just composes them. All are stdlib-only except `core/middleware/tracing.go` (OpenTelemetry).

### `framework/` — opinionated entity layer

`framework.App`, `EntityConfig`, JSON declarations, `CrudHandler`, `Hooks`, query DSL (`posts.where(status="published").order(created_at DESC).limit(10)`), MCP CRUD wiring, OpenAPI synthesis.

### `core-ui/` — server-driven UI runtime

Start from the [UI capability map](framework/docs/content/ui-capability-map.md) when the question is product-shaped — live dashboard, optimistic board, master/detail workspace, server-authoritative reactive state, static export, or deliberate SPA integration — rather than a package name.

A separate, independently usable system for rendering interactive UIs from Go: signals, HTML primitives (`core-ui/html`), composed UI patterns (`core-ui/patterns`), server-side islands, dev server with SSE hot-reload, a static-site compiler, a linter, and a vanilla-JS runtime. Fresh `gofastr init` apps mount the adaptive `framework/ui/theme.Default()` palette, so light/dark behavior is complete from the first render. See `examples/site` for an app that exercises every feature — including the 90+ `framework/ui` primitives, compact operational composition (`RecordSummary` + `MetricBand`), modal/drawer/popover/toast widgets, and CRUD-by-island patterns. Each component has a live demo at `/components/<slug>` on that site; the one-page catalog is [`ui-new-components.md`](framework/docs/content/ui-new-components.md) (`gofastr docs ui-new-components`), and `go doc github.com/DonaldMurillo/gofastr/framework/ui` lists every constructor.

### `battery/` — pluggable infrastructure

`admin`, `auth`, `cache`, `email`, `embed`, `log`, `notify`, `print`, `queue`, `search`, `storage`, `webhook`. Each behind a small interface with at least one in-memory implementation suitable for tests and small examples; production swaps in Redis, S3, Postgres FTS, etc. (`battery/admin` is the auto-generated back-office; `battery/experimental` is internal and not part of the supported surface.)

`battery/embed` is the local semantic-search battery: in-process vector index with brute-force cosine, optional hybrid keyword fusion, MMR diversity, snapshot/WAL persistence, and a fsnotify-free polling watcher. See [`framework/docs/content/embed.md`](framework/docs/content/embed.md).

### `cmd/gofastr` — CLI

```text
gofastr init <name>                 Scaffold a project (framework UI + DESIGN.md + entities + git + agent onboarding)
gofastr docs                        Browse/search embedded framework docs (no internet needed)
gofastr docs <topic>                Read a specific doc topic
gofastr docs --grep <term>          Search across every doc topic
gofastr agents sync                 Refresh AGENTS.md and agents/ detail files
gofastr theme init                  Scaffold a typed theme/theme.go you own
gofastr generate --from=<bp.yml>    Generate Go (SQL + REST + OpenAPI + MCP + UI) from a blueprint
gofastr pack <app-dir>              Reconstruct the blueprint YAML from a generated app (inverse of generate)
gofastr build                       Generate, vet, accessibility-check, then build the root package
gofastr build --pkg ./cmd/server    Run the same pipeline for a main package below the project root
gofastr dev                         Start dev server with hot-reload
gofastr migrate up | down | status  Run versioned migrations (advisory-locked, checksum + dirty-state guarded)
gofastr migrate up --create-db      Create the target database first if it doesn't exist
gofastr migrate generate <name> --from=<bp.yml>   Diff blueprint entities vs the committed snapshot → numbered reversible SQL
gofastr migrate force <version>     Reconcile the tracking table by hand (dirty-state recovery / baseline adoption)
gofastr migrate force <version>     Reconcile a dirty/baselined migration
gofastr test                        Run project tests
gofastr audit a11y --url <base>     Axe audit with honest page coverage (--email/--password for login)
gofastr embed index <path>          Index a project for semantic search
gofastr embed watch <path>          Index + poll-watch for changes
gofastr embed query "<text>"        Top-K semantic hits as JSON
```

### `kiln/` — agent-driven build mode (experimental)

Kiln lets you build a GoFastr app live by chatting with a coding agent.
OMP with GLM-5.2 is the turnkey/default driver; Claude Code, Pi, Codex, and
custom commands remain adapters. The agent mutates an in-memory world over
HTTP, the running app re-renders, and the schema migrates in-process.
Freeze the journal when done and graduate to a `gofastr.yml` blueprint
you commit. It's an experiment and not part of the supported framework
surface — the full tool list, the agent wiring, and the safety model
live in [kiln.md](framework/docs/content/kiln.md).

## Repository layout

```
core/        stdlib-first primitives (router, query, mcp, openapi, …)
framework/   entity system, app wiring, declarations, query DSL, hooks
core-ui/     server-driven UI runtime (signals, components, islands)
kiln/       agent-driven build mode (experimental)
battery/     pluggable infra (admin, auth, cache, email, embed, log, notify, print, queue, search, storage, webhook)
cmd/gofastr/ CLI: generate, build, migrate
cmd/kiln/   CLI: serve, mcp, acp
framework/docs/content/  feature docs, embedded into the binary — browse with `gofastr docs`
examples/    meridian (blueprint flagship: SaaS console + marketing), ecommerce (owner-scoped blueprint pipeline), site (SSR + 90+ UI primitives), blog, api-tour (cursor/include/batch/SSE/uploads), backoffice (entity admin), embed-demo, spa (Vue+API), static-site
ROADMAP.md   forward-looking proposals not yet built
```

## Documentation

Every doc below is embedded into the `gofastr` binary — `gofastr docs` browses
them offline, and the `framework_docs_*` MCP tools expose them to agents
connected to a running app.

- [Blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md) — **the thesis walkthrough**: blueprint → generated UI + API → auth + owner scoping + RBAC → customize in plain Go → deploy
- [Kiln (experimental)](framework/docs/content/kiln.md) — agent-driven build mode
- [UI capability map](framework/docs/content/ui-capability-map.md) — **start from the job**: architecture, state ownership, delivery/scaling semantics, runnable proof, and explicit non-goals
- [UI getting started](framework/docs/content/ui-getting-started.md) — **the 15-minute path**: scaffold → design direction → theme → framework-native composition
- [UI composition recipes](framework/docs/content/ui-composition-recipes.md) — product-shaped page structures built entirely from `framework/ui` primitives
- [UI components index](framework/docs/content/ui-new-components.md) — **the catalog**: every component the framework ships, with its `go doc` path and live demo at `/components/<slug>` in `examples/site`
- [core-ui architecture](core-ui/ARCHITECTURE.md) — **deeper UI/runtime reference** (SSR, hydration, islands, component CSS, data-fui-* primitives)
- [Interactive patterns](framework/docs/content/interactive-patterns.md) — every `data-fui-*` behavior, plus **"Writing a hand-written island, end to end"** (no-reload updates on your own screens) and themed confirms
- [framework architecture](framework/ARCHITECTURE.md) — package layout, layering rules, cycle-breaking interfaces
- [Entity declarations](framework/docs/content/entity-declarations.md) — JSON schema reference
- [Migrations](framework/docs/content/migrations.md) — versioned migrations and the CLI
- [Query DSL](framework/docs/content/query-dsl.md) — `Entity.where(...).order(...).limit(N)`
- [Search](framework/docs/content/search.md) — the `battery/search` interface
- [Embed](framework/docs/content/embed.md) — local semantic search via `battery/embed`
- [Security](framework/docs/content/security.md) — defaults, headers, and limits
- [Deployment](framework/docs/content/deploy.md) — single-binary build, graceful shutdown, production checklist
- [Horizontal scaling](framework/docs/content/scaling.md) — what's process-local by default and the replica-safe alternative for each
- [Observability](framework/docs/content/observability.md) — metrics and tracing
- [PWA](framework/docs/content/pwa.md) — installable app manifest + versioned offline shell via `uihost.WithPWA`
- [Agent-ready](framework/docs/content/agent-ready.md) — the discovery surface for AI agents (llms.txt, agent card, MCP)

The full, per-topic index lives in the docs site catalogue (`gofastr docs --list`, or `examples/site/docs_catalog.go`), which a parity test keeps in sync with every embedded page. The list above is a curated subset.

## Project status

GoFastr is pre-1.0 and explicitly not stable:

- The `core/` primitives are usable and tested in isolation.
- The `framework/` entity layer is solid for SQLite + Postgres CRUD apps.
- `core-ui/` is the active research frontier — APIs change between commits.
- The CLI binary blank-imports only `github.com/mattn/go-sqlite3`. To run migrations against Postgres, build a custom binary that imports your driver of choice.

## Contributing

This repo is a personal research tree at the moment. Issues and PRs are welcome but expect strong opinions about scope: the goal is a framework an AI agent can drive end-to-end, not a kitchen-sink CMS.

Before pushing, the `.githooks/pre-push` gate runs `go test -race -count=1 ./...` and `govulncheck`. Enable hooks once with:

```bash
git config core.hooksPath .githooks
```

## License

GoFastr is released under the [MIT License](LICENSE) — free to use, modify, and
distribute, including in commercial and closed-source projects, provided the
copyright notice and license text are preserved. The software is provided "as
is", without warranty; see [`LICENSE`](LICENSE) for the full terms.
