# GoFastr

> The full-stack Go framework that doesn't get in the way of you or your agents.

GoFastr is an experimental full-stack Go framework. Declare your domain in Go and get server-rendered screens, REST endpoints, MCP tools, an OpenAPI spec, SQL migrations, and a typed query builder — plain Go you can read, edit, and own. No reflection, no generated code you can't open. Auth, background jobs, search, and storage are opt-in packages, and you can drop to `net/http` or `database/sql` at any point.

It is built for both the agentic web and AI-assisted development. The app you ship joins the agentic web: the agents your users bring call your data over MCP, with the same login and permissions your users have. While you build, `gofastr dev` hands your coding agent — Claude Code or Codex — the app's routes, config, and logs over MCP, to help build and debug it.

Start with [the quickstart](#quickstart). Or scaffold a whole app in one command — screens, API, auth — with the CLI: `gofastr init <name>`, or `gofastr generate` from a one-file declaration ([blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md)).

> **Status:** early / `v0.x` — MIT-licensed and usable, but the API may change
> between releases, so pin a version (`go get …@v0.x.y`). A `v1.0.0` tag will
> mark the stability promise. Ship at your own risk until then.

## Quickstart

Requires Go 1.26+. Install the CLI:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
```

Three complete programs, smallest to fullest — the same three the
site's homepage shows (`examples/site`): plain `core/`, one `framework`
entity, and the full app shape. CI extracts all three from this README,
compiles them, boots them, and curls them
(`cmd/gofastr/readme_quickstart_test.go`).

### Core only

`core/` is stdlib-first building blocks — router, typed handlers,
render, a SQL query builder, schema, migrate, mcp — each usable without
the framework. The basic app is one screen and one API route:

```go
package main

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

type Pong struct {
	Status string `json:"status"`
}

func main() {
	r := router.New()

	// A server-rendered page.
	r.Get("/", render.HTMLHandler(func(req *http.Request) render.HTML {
		return render.Tag("h1", nil, render.Text("Hello from core."))
	}))

	// A typed JSON route — the adapter binds input and serializes output.
	r.Get("/api/ping", handler.HandlerAdapter(func(ctx context.Context, _ struct{}) (Pong, error) {
		return Pong{Status: "ok"}, nil
	}))

	http.ListenAndServe(":8080", r)
}
```

### Framework

One `framework` entity is a complete server — a migrated table, REST
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
	app := framework.NewApp(framework.WithDB(db), framework.WithMCP()) // WithMCP serves the tools at /mcp

	// CRUD is auto-on when a DB is set (CRUD *bool: nil = auto).
	app.Entity("posts", framework.EntityConfig{
		Public: true, // anonymous read AND write; omit it and CRUD requires a session (secure by default)
		MCP:    true, // emit posts_list/get/create/update/delete MCP tools
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	})

	log.Fatal(app.Start(":8080")) // GET/POST /posts, /openapi.json, MCP — all live
}
```

That's the whole program. No config files, no codegen step, no registration
boilerplate. Add entities-as-JSON, batteries, the UI runtime, or the
generator when you need them. For how a flat app grows into
`internal/<domain>/` as boundaries appear, see
[project structure](framework/docs/content/project-structure.md).

### Donald's Way

The full app shape: server-rendered screens with SEO, an owner-scoped
entity API, MCP for agents, and login + sessions — one binary. A screen
is Go too: `Render` returns HTML, and a small JS runtime hydrates it in
place — no React or Vue on the client:

```go
package main

import (
	"database/sql"
	"log"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"
)

// A screen is plain Go: Render returns server-rendered HTML.
type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string { return "Notes" }
func (s *HomeScreen) Render() render.HTML {
	return html.Heading(html.HeadingConfig{Level: 1}, render.Text("My notes"))
}

func main() {
	db, _ := sql.Open("sqlite3", "notes.db")

	// Server-rendered screens. Each also serves an auto llm.md.
	ui := app.NewApp("Notes")
	ui.Register("/", &HomeScreen{}, nil)

	// SEO for those pages.
	host := uihost.New(ui,
		uihost.WithDescription("A tiny notes app."),
		uihost.WithOpenGraph(uihost.OG{Title: "Notes", Type: "website"}),
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://notes.example"}),
	)

	// MCP for agents.
	fwApp := framework.NewUIHostApp(host,
		framework.WithDB(db),
		framework.WithAPIPrefix("/api"),
		framework.WithMCP(),
	)

	// OwnerField scopes rows per user: anonymous → 401, cross-user → 404.
	fwApp.Entity("notes", framework.EntityConfig{
		OwnerField: "user_id",
		MCP:        true,
		Fields:     []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	})

	// Login + sessions.
	authMgr := auth.New(auth.AuthConfig{
		DevMode:      true, // dev only: mints a per-process JWT secret; set JWTSecret in prod
		UserStore:    auth.NewEntityUserStore(db, "auth_users"),
		SessionStore: auth.NewEntitySessionStore(db, "auth_sessions"),
	})
	authMgr.Use(auth.NewCorePlugin())
	if err := authMgr.Init(fwApp); err != nil {
		log.Fatal(err)
	}
	fwApp.Use(auth.SessionMiddleware(authMgr))

	log.Fatal(fwApp.Start(":8080"))
}
```

`GET /` is the rendered screen. Anonymous `GET /api/notes` answers 401 —
`OwnerField` scopes rows per user, and auto-CRUD requires a session
unless the entity is `Public`. `/auth/register` and `/auth/login` come
from the auth battery, and the MCP tools at `/mcp` respect the same
owner scope as the REST API.

### Run it from a clone

To work on the framework itself, or run the examples:

```bash
git clone https://github.com/DonaldMurillo/gofastr.git
cd gofastr
go test ./...                        # full suite needs Docker (Postgres testcontainers) and Chrome (chromedp e2e)
go run ./cmd/gofastr --help          # CLI overview
go run ./examples/blog               # minimal blog with auto-CRUD on SQLite
```

Two Git worktrees of the same app — one per coding agent, for example —
can run side by side: with isolation on, each linked worktree gets its own
local port and database path, so nothing collides
([`framework/docs/content/isolation.md`](framework/docs/content/isolation.md)).

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

## The code you don't write

Routes, validation, migrations, pagination, uploads, spec, agent
tools — the framework emits all of it from one declaration
(`app.Entity` in Go, or an `entities:` entry in a blueprint).
Declarations are optional: `core/` routes and hand-written screens run
without them. A declaration grows the same way it starts — fields,
enums, relations, soft delete:

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

| Output           | Auto-generated                                                                  |
|------------------|---------------------------------------------------------------------------------|
| HTTP             | `GET / POST /posts`, `GET / PUT / PATCH / DELETE /posts/{id}`                   |
| Batch endpoints  | `POST / PATCH / DELETE /posts/_batch` — atomic; one tx for all items            |
| SSE stream       | `GET /posts/_events` — entity.created/updated/deleted, scoped per tenant        |
| Filtering        | `?status=published&views_gte=10&sort=-created_at&page=2`                        |
| Eager loading    | `?include=author.profile,comments` — flat or nested, validated against the registry |
| Cursor paging    | `?cursor=&limit=50` — keyset by `EntityConfig.CursorField` (defaults to PK)     |
| Multipart upload | `multipart/form-data` on `Image`/`File` fields → streamed through `WithFileStorage` |
| Validation       | Required, unique, enum, min/max, regex pattern, multi-tenant scope              |
| Migrations       | Versioned runner — advisory-lock serialization, checksum-drift + dirty-state guards, `NoTransaction` escape hatch, a down section when a safe inverse exists; declarative incremental generation; real-Postgres tested |
| FK constraints   | BelongsTo relations emit `FOREIGN KEY` clauses; `AutoMigrate` topo-sorts tables |
| Transactions     | `Create/Update/Delete` + hooks share one tx; `TxFromContext(ctx)` exposes it    |
| OpenAPI 3        | `/openapi.json` and Swagger UI at `/api/docs/`                                  |
| MCP              | `posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete`       |
| Soft delete      | `deleted_at` column + automatic filter                                          |
| Multi-tenant     | `tenant_id` column + automatic scope from request context                       |
| Hooks            | `BeforeCreate`, `AfterUpdate`, etc. for custom behaviour                        |
| Custom routes    | `EntityConfig.Endpoints` with optional MCP exposure                             |
| Client SDKs      | `gofastr generate sdk` — Go module + JS/TS client artifacts an app can serve via `sdkdocs.Mount`, with a live docs site (`framework/sdkdocs`) |
| Customer CLI     | `gofastr generate cli` — a branded terminal client for your customers, scoped API-token auth |

Try all of it against a running server: [`examples/api-tour`](examples/api-tour/README.md)
is the curl tour — eager loading, cursor paging, atomic batch, SSE,
uploads, sparse updates. Hooks run inside the write's transaction
([hooks-and-transactions](framework/docs/content/hooks-and-transactions.md)).

## The design bets

- **Two layers.** A small `core/` of stdlib-first primitives sits under an opinionated `framework/`. Use the framework for the common path; drop to core and write plain `net/http` when it's in your way. (The one external touchpoint is `core/middleware/tracing.go`, which pulls in OpenTelemetry; the rest of `core/` is stdlib-only.)
- **Server-rendered UI, hydrated in place.** Screens are Go: `Render` returns HTML and the server sends the full page. A small JS runtime attaches to it; in-page changes — sort, paginate, add a row — call the server and swap one region, and cross-page navigation swaps content client-side with a route cache, so there are no hard refreshes. No React or Vue on the client, and no router code for you to write.
- **The interactive layer keeps no server state.** Sessions are signed tokens, so any replica serves any request. Updates pull first — client signals, then RPC, then polling — and SSE push is reserved for presence and collaboration ([`reactivity.md`](framework/docs/content/reactivity.md)).
- **Security scopes live in the declaration, fail-closed.** `owner_field` makes auto-CRUD per-user (anonymous → 401, cross-user → 404), `access:` gates operations behind RBAC permissions (403), `multi_tenant` scopes by tenant — and `gofastr validate` flags PII-shaped fields (email, phone, address, …) exposed without any of them. The MCP tools respect the same scopes as the REST routes.
- **You own the output.** The generated code is normal Go you read, debug, commit, edit, and compose from your own `main`. Registration is ordinary Go in the generated files; no reflection discovers your entities, and no platform sits between your binary and your server.
- **Batteries are separate packages.** Auth, cache, email, queue, search, storage sit behind narrow interfaces — swap any one without forking.
- **A blueprint scaffolds the whole app when you want a head start.** A single `gofastr.yml` generates both halves — SQL + REST + OpenAPI + MCP *and* the screens — in one pass, consistent from the start. Then it's plain Go you own and edit, and the running app never needs the blueprint again. See [`examples/meridian`](examples/meridian/) for the whole pipeline — a SaaS console + marketing site — live and tested.

## The repo in 60 seconds

| Directory | What it is | Depend on it when… |
|---|---|---|
| `core/` | Stdlib-only primitives — router, query, schema, render, mcp, openapi, migrate. Each usable on its own. | you want plain Go building blocks, no framework. |
| `framework/` | The opinionated entity layer (`App`, `EntityConfig`, CRUD, hooks, migrations). A thin facade re-exporting its focused runtime subpackages. | you want one declaration → SQL + REST + OpenAPI + MCP. |
| `core-ui/` | Server-driven UI runtime — `html` primitives, `patterns`, `widget` islands, signals, the vanilla-JS runtime. Independently usable. | you're rendering HTML from Go. |
| `battery/` | Opt-in infrastructure — admin, auth, cache, email, embed, log, notify, print, queue, search, setup, storage, webhook. Each behind a small interface. | you need a real subsystem; import only the ones you use. |
| `cmd/gofastr` | The CLI — `init`, `generate`, `pack` (lossy app→blueprint snapshot), `migrate`, `build`, `dev`, `docs`, and more. | you're scaffolding or generating code. |
| `kiln` | Experimental agent build-mode runtime (mutate an in-memory IR over HTTP). | you're driving the app from an agent. |
| `examples/` | Runnable reference apps — the `meridian` blueprint flagship (a SaaS billing console + marketing site), the `ecommerce` blueprint pipeline, plus blog, api-tour, spa, and the docs site. | you want to see it wired end-to-end. |

You import `framework` and the batteries you opt into — not each of its subpackages. The
subpackage split is an internal seam (see `framework/ARCHITECTURE.md`); the
public API is `framework.X` plus the batteries you reach for.

## Built with GoFastr

In production:

- **[Barcode & QR Code Maker](https://barcode.donaldmurillo.com/)** — a live
  tool, no signup required, to generate and read barcodes and QR codes (QR, EAN-13, UPC-A,
  Code 128, Data Matrix, and more) as PNG, SVG, or PDF, with CSV/Excel batch
  export to a ZIP, a REST API, and an MCP server. Built and running on GoFastr.

The framework also runs on itself — GoFastr's own tooling and reference apps are
built on the same `framework`, `core-ui`, and batteries a user app imports:

- **`examples/site`**, the docs site and canonical component gallery, runs on
  `framework` + `framework/ui` + `framework/uihost` + the `core-ui` pattern
  presets + `battery/print`.
- **`examples/meridian`**, the declaration-first flagship, *is* generated from a
  `gofastr.yml` blueprint — a SaaS billing console (customers,
  subscriptions, invoices with status workflows, MRR + charts) *and* its public
  marketing site, auth, RBAC, and admin back-office, with writable app screens
  (add/edit/delete) and the generated end-to-end test suite green.
- **`examples/ecommerce`**, a second blueprint pipeline (five entities,
  owner-scoped orders), is generated the same way and exercised by its own
  end-to-end test.

Both blueprint apps are secure by default and carry a generated end-to-end
test suite — every screen, the full create→edit→delete lifecycle, and RBAC
asserted — the suite itself generated, not hand-written (each app is
scaffolded from its `gofastr.yml` and then extended in owned Go, e.g.
Meridian's `sdkdocs` mount and brand CSS).

The project uses these tools on itself. External production adopters are
the part still ahead of us — see [Project status](#project-status).

## Documentation

Every doc below is embedded into the `gofastr` binary — `gofastr docs` browses
them offline, and the `framework_docs_*` MCP tools expose them to agents
connected to a running app.

- [The gofastr CLI](framework/docs/content/cli.md) — every subcommand mapped to its doc: init, dev, migrate, generate, audit, upgrade
- [Blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md) — **generate a whole app from one file**: blueprint → generated UI + API → auth + owner scoping + RBAC → customize in plain Go → deploy
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
- [Agent-ready](framework/docs/content/agent-ready.md) — the discovery endpoints for AI agents (llms.txt, agent card, MCP)

The full, per-topic index lives in the docs site catalogue (`gofastr docs --list`, or `examples/site/docs_catalog.go`), which a parity test keeps in sync with every embedded page. The list above is a curated subset.

## Project status

GoFastr is pre-1.0 and explicitly not stable:

- The `core/` primitives are usable and tested in isolation.
- The `framework/` entity layer handles SQLite + Postgres CRUD apps today.
- `core-ui/` changes fastest — APIs may break between commits.
- The CLI binary blank-imports only `github.com/mattn/go-sqlite3`. To run migrations against Postgres, build a custom binary that imports your driver of choice.

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
- **Build for agents on both sides.** In production, the agents your
  users bring call the app's data over MCP — with the same login and
  permissions the users have. While you build, `gofastr dev` hands your
  coding agent the running app's routes, config, and logs over MCP. Both
  fall out of writing plain, readable Go.

## Contributing

This repo is a personal research tree at the moment. Issues and PRs are welcome but expect strong opinions about scope: the goal is a framework an AI agent can drive end-to-end, not a kitchen-sink CMS.

Before pushing, the `.githooks/pre-push` gate runs `go test -race -count=1 ./...` and `govulncheck`. Enable hooks once with:

```bash
git config core.hooksPath .githooks
```

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

## License

GoFastr is released under the [MIT License](LICENSE) — free to use, modify, and
distribute, including in commercial and closed-source projects, provided the
copyright notice and license text are preserved. The software is provided "as
is", without warranty; see [`LICENSE`](LICENSE) for the full terms.
