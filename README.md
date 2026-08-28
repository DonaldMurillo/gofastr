# GoFastr

[![CI](https://github.com/DonaldMurillo/gofastr/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/DonaldMurillo/gofastr/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/DonaldMurillo/gofastr)](https://github.com/DonaldMurillo/gofastr/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/DonaldMurillo/gofastr)](go.mod)
[![Go Reference](https://pkg.go.dev/badge/github.com/DonaldMurillo/gofastr.svg)](https://pkg.go.dev/github.com/DonaldMurillo/gofastr)
[![License: MIT](https://img.shields.io/github/license/DonaldMurillo/gofastr)](LICENSE)
[![CodeRabbit Pull Request Reviews](https://img.shields.io/coderabbit/prs/github/DonaldMurillo/gofastr?utm_source=oss&utm_medium=github&utm_campaign=DonaldMurillo%2Fgofastr&labelColor=171717&color=FF570A&link=https%3A%2F%2Fcoderabbit.ai&label=CodeRabbit+Reviews)](https://www.coderabbit.ai)

> The full-stack Go framework that doesn't get in the way of you or your agents.

**[Docs, component gallery, and live demos →](https://donaldmurillo.github.io/gofastr/)**
&nbsp;·&nbsp; [Quickstart](#quickstart)
&nbsp;·&nbsp; [A GoFastr app in production](https://barcode.donaldmurillo.com/)

GoFastr is a pre-v1 full-stack Go framework. The API can still change between releases, under the [deprecation policy](framework/docs/content/stability.md). Declare your domain in Go and get server-rendered screens, REST endpoints, MCP tools, an OpenAPI spec, SQL migrations, and a typed query builder. The output is plain Go you can read, edit, and own: no reflection discovers your entities, no generated code you can't open. Auth, background jobs, search, and storage are opt-in packages, and you can drop to `net/http` or `database/sql` at any point.

It is built for both the agentic web and AI-assisted development. The app you ship joins the agentic web: the agents your users bring call your data over MCP, with the same login and permissions your users have. While you build, `gofastr dev` hands your coding agent, Claude Code or Codex, the app's routes, config, and logs over MCP, to help build and debug it.

Start with [the quickstart](#quickstart). Or scaffold a whole app in one command: screens, API, and auth from `gofastr init <name>`, or `gofastr generate` from a one-file declaration ([blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md)).

**A shoutout to [CodeRabbit](https://www.coderabbit.ai):** it reviews every
PR in this repo and keeps catching what everyone else missed, like six Major
findings on [#198](https://github.com/DonaldMurillo/gofastr/pull/198) while
the checks list showed `pass`. Every finding is triaged on the PR before merge.

## Quickstart

Requires Go 1.27+. Install the CLI:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
```

Three complete programs, smallest to fullest, the same three the
site's homepage shows (`examples/site`): plain `core/`, one `framework`
entity, and the full app shape. CI extracts all three from this README,
compiles them, boots them, and curls them
(`cmd/gofastr/readme_quickstart_test.go`).

### Core only

`core/` is stdlib-first building blocks: router, typed handlers,
render, a SQL query builder, schema, migrate, mcp. Each is usable without
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

	// A typed JSON route: the adapter binds input and serializes output.
	r.Get("/api/ping", handler.HandlerAdapter(func(ctx context.Context, _ struct{}) (Pong, error) {
		return Pong{Status: "ok"}, nil
	}))

	http.ListenAndServe(":8080", r)
}
```

### Framework

One `framework` entity is a complete server: a migrated table, REST
CRUD, an OpenAPI spec, and MCP tools. Add only what you need from there.

```go
package main

import (
	"database/sql"
	"log"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func main() {
	db, _ := sql.Open("sqlite3", "app.db")
	app := framework.NewApp(framework.WithDB(db), framework.WithMCP()) // WithMCP serves the tools at /mcp

	// CRUD is auto-on when a DB is set (CRUD *bool: nil = auto).
	app.Entity("posts", framework.EntityConfig{
		Exposure: &framework.ExposureConfig{
			Public: true, // anonymous read AND write; omit it and CRUD requires a session (secure by default)
			MCP:    true, // emit posts_list/get/create/update/delete MCP tools
		},
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	})

	log.Fatal(app.Start(":8080")) // GET/POST /posts, /openapi.json, MCP: all live
}
```

That's the whole program. No config files, no codegen step, no registration
boilerplate. Add entities-as-JSON, batteries, the UI runtime, or the
generator when you need them. For how a flat app grows into
`internal/<domain>/` as boundaries appear, see
[project structure](framework/docs/content/project-structure.md).

### Donald's Way

The full app shape: server-rendered screens with SEO, an owner-scoped
entity API, MCP for agents, and login + sessions, in one binary. A screen
is Go too: `Render` returns HTML, and a small JS runtime hydrates it in
place, with no React or Vue on the client:

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
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
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

	// Scope.OwnerField scopes rows per user: anonymous → 401, cross-user → 404.
	fwApp.Entity("notes", framework.EntityConfig{
		Scope:    &framework.ScopeConfig{OwnerField: "user_id"},
		Exposure: &framework.ExposureConfig{MCP: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
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

`GET /` is the rendered screen. Anonymous `GET /api/notes` answers 401:
`OwnerField` scopes rows per user, and auto-CRUD requires a session
unless the entity is `Public`. `/auth/register` and `/auth/login` come
from the auth battery, and the MCP tools at `/mcp` respect the same
owner scope as the REST API.

### Run it from a clone

To work on the framework itself, or run the examples:

```bash
git clone https://github.com/DonaldMurillo/gofastr.git
cd gofastr
go test ./...                        # SQLite; the Postgres halves skip without TEST_POSTGRES_DSN
go run ./cmd/gofastr --help          # CLI overview
go run ./examples/blog               # minimal blog with auto-CRUD on SQLite
```

Postgres setup and the race pass are under [Contributing](#contributing).
Linked Git worktrees of the same app each get their own port and database
path, so two coding agents can run side by side
([isolation](framework/docs/content/isolation.md)).

### Updating GoFastr

The module dependency and the installed CLI are versioned independently;
keep them on the same release. `gofastr upgrade` reads your `go.mod`, lists
every migration note between your version and the target (breaking changes
are marked in the [release notes](https://github.com/DonaldMurillo/gofastr/releases)),
and points at the affected lines in your code (`--apply` runs the steps).
Manual steps and the full guide:
[upgrading](framework/docs/content/upgrading.md), or `gofastr docs upgrading`.

## The code you don't write

The framework emits routes, validation, migrations, pagination, uploads,
the spec, and agent tools from one declaration
(`app.Entity` in Go, or an `entities:` entry in a blueprint).
Declarations are optional: `core/` routes and hand-written screens run
without them. A declaration grows the same way it starts, with fields,
enums, relations, and soft delete:

```go
app.Entity("posts", framework.EntityConfig{
    Scope: &framework.ScopeConfig{SoftDelete: true},
    Fields: []schema.Field{
        {Name: "title", Type: schema.String, Required: true},
        {Name: "body", Type: schema.Text},
        {Name: "status", Type: schema.Enum,
            Values: []string{"draft", "published"}, Default: "draft"},
        {Name: "author_id", Type: schema.Relation, To: "users"},
    },
    Exposure: &framework.ExposureConfig{MCP: true},
})
```

| Output           | Auto-generated                                                                  |
|------------------|---------------------------------------------------------------------------------|
| HTTP             | `GET / POST /posts`, `GET / PUT / PATCH / DELETE /posts/{id}`                   |
| Batch endpoints  | `POST / PATCH / DELETE /posts/_batch`: atomic; one tx for all items            |
| SSE stream       | `GET /posts/_events`: entity.created/updated/deleted, scoped per tenant        |
| Filtering        | `?status=published&views_gte=10&sort=-created_at&page=2`                        |
| Eager loading    | `?include=author.profile,comments`: flat or nested, validated against the registry |
| Cursor paging    | `?cursor=&limit=50`: keyset paging ([cursor-pagination](framework/docs/content/cursor-pagination.md)) |
| Multipart upload | `multipart/form-data` on `Image`/`File` fields → streamed through `WithFileStorage` |
| Validation       | Required, unique, enum, min/max, regex pattern, multi-tenant scope              |
| Migrations       | Versioned runner with drift + dirty-state guards; declarative incremental generation ([migrations](framework/docs/content/migrations.md)) |
| FK constraints   | BelongsTo relations emit `FOREIGN KEY` clauses; `AutoMigrate` topo-sorts tables |
| Transactions     | `Create/Update/Delete` + hooks share one tx; `TxFromContext(ctx)` exposes it    |
| OpenAPI 3        | `/openapi.json` plus a spec-viewer page at `/api/docs/`                         |
| MCP              | `posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete`       |
| Soft delete      | `deleted_at` column + automatic filter                                          |
| Multi-tenant     | `tenant_id` column + automatic scope from request context                       |
| Hooks            | `BeforeCreate`, `AfterUpdate`, etc. for custom behaviour                        |
| Custom routes    | `EntityConfig.Endpoints` with optional MCP exposure                             |
| Client SDKs      | `gofastr generate sdk`: a Go module + JS/TS clients, with a live docs site ([sdk](framework/docs/content/sdk.md)) |
| Customer CLI     | `gofastr generate cli`: a branded terminal client for your customers, scoped API-token auth |

Try all of it against a running server: [`examples/api-tour`](examples/api-tour/README.md)
is the curl tour, covering eager loading, cursor paging, atomic batch, SSE,
uploads, and sparse updates. Hooks run inside the write's transaction
([hooks-and-transactions](framework/docs/content/hooks-and-transactions.md)).

## The design bets

- **Two layers.** A small `core/` of stdlib-first primitives sits under an opinionated `framework/`. Use the framework for the common path; drop to core and write plain `net/http` when it's in your way. (The one external touchpoint is `core/middleware/tracing.go`, which pulls in OpenTelemetry; the rest of `core/` is stdlib-only.)
- **Server-rendered UI, hydrated in place.** Screens are Go: `Render` returns HTML and the server sends the full page. A small JS runtime attaches to it. In-page changes like sort, paginate, or add-a-row call the server and swap one region, and cross-page navigation swaps content client-side with a route cache, so there are no hard refreshes. No React or Vue on the client, and no router code for you to write.
- **The interactive layer keeps no server state.** Sessions are signed tokens, so any replica serves any request. Updates pull first: client signals, then RPC, then polling. SSE push is reserved for presence and collaboration ([`reactivity.md`](framework/docs/content/reactivity.md)).
- **Security scopes live in the declaration, fail-closed.** `owner_field` makes auto-CRUD per-user (anonymous → 401, cross-user → 404), `access:` gates operations behind RBAC permissions (403), `multi_tenant` scopes by tenant, and `gofastr validate` flags PII-shaped fields (email, phone, address, …) exposed without any of them. The MCP tools respect the same scopes as the REST routes.
- **You own the output.** The generated code is normal Go you read, debug, commit, edit, and compose from your own `main`. Registration is ordinary Go in the generated files; no reflection discovers your entities, and no platform sits between your binary and your server.
- **Batteries are separate packages.** Auth, cache, email, queue, search, storage sit behind narrow interfaces; swap any one without forking.
- **The framework checks whether you're still using it well.** `gofastr verify` runs the 51 rules of the contract catalog (routing, permissions, security, rendering, accessibility) and measures semantic coverage: not "did this line run" but "did a request ever reach this route, did this permission ever get evaluated". Error-severity findings fail the run (`--strict` makes warnings fail too), with per-line waivers and a baseline mode for existing codebases. See [contracts](framework/docs/content/contracts.md).
- **A blueprint scaffolds the whole app when you want a head start.** One `gofastr.yml` generates the screens and the API in one pass; then it's plain Go you own and edit, and the running app never needs the blueprint again ([blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md)).

## The repo in 60 seconds

| Directory | What it is | Depend on it when… |
|---|---|---|
| `core/` | Stdlib-only primitives: router, query, schema, render, mcp, openapi, migrate. Each usable on its own. | you want plain Go building blocks, no framework. |
| `framework/` | The opinionated entity layer (`App`, `EntityConfig`, CRUD, hooks, migrations). A thin facade re-exporting its focused runtime subpackages. | you want one declaration → SQL + REST + OpenAPI + MCP. |
| `core-ui/` | Server-driven UI runtime: `html` primitives, `patterns`, `widget` islands, signals, the vanilla-JS runtime. Independently usable. | you're rendering HTML from Go. |
| `battery/` | Opt-in infrastructure: admin, auth, cache, email, semantic, log, notify, print, queue, relay, search, setup, storage, webhook. Each behind a small interface. | you need a real subsystem; import only the ones you use. |
| `cmd/gofastr` | The CLI: `init`, `generate`, `pack` (lossy app→blueprint snapshot), `migrate`, `build`, `dev`, `verify`, `docs`, and more. | you're scaffolding, generating, or checking code. |
| `kiln` | Experimental agent build-mode runtime (mutate an in-memory IR over HTTP). | you're driving the app from an agent. |
| `examples/` | Runnable reference apps: the `meridian` blueprint flagship (a SaaS billing console + marketing site), the `ecommerce` blueprint pipeline, plus blog, api-tour, spa, and the docs site. | you want to see it wired end-to-end. |

You import `framework` and the batteries you opt into, not each of its subpackages. The
subpackage split is an internal seam (see `framework/ARCHITECTURE.md`); the
public API is `framework.X` plus the batteries you reach for.

## Built with GoFastr

In production:

- **[Barcode & QR Code Maker](https://barcode.donaldmurillo.com/)**: a live
  tool, no signup required, to generate and read barcodes and QR codes (QR, EAN-13, UPC-A,
  Code 128, Data Matrix, and more) as PNG, SVG, or PDF, with CSV/Excel batch
  export to a ZIP, a REST API, and an MCP server. Built and running on GoFastr.

The framework also runs on itself: [`examples/site`](examples/site) is the
docs site and component gallery, [`examples/meridian`](examples/meridian) is a
blueprint-generated SaaS console + marketing site edited by hand ever since,
and [`examples/ecommerce`](examples/ecommerce) is still owned by the generator
and regenerated on every test run. All three import the same `framework`,
`core-ui`, and batteries a user app does, and each carries an end-to-end
suite. External production adopters are the part still ahead of us; see
[Project status](#project-status).

## Documentation

Every doc is embedded into the `gofastr` binary: `gofastr docs` browses them
offline, the [docs site](https://donaldmurillo.github.io/gofastr/) serves them
rendered with live component demos, and the `framework_docs_*` MCP tools
expose them to agents connected to a running app. The
[docs index](framework/docs/content/README.md) is the browsable entry point.
Start with:

- [The gofastr CLI](framework/docs/content/cli.md): every subcommand mapped to its doc
- [Blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md): generate a whole app from one file, then own the code
- [Entity declarations](framework/docs/content/entity-declarations.md): the declaration schema reference
- [UI getting started](framework/docs/content/ui-getting-started.md), the [UI components index](framework/docs/content/ui-new-components.md), and the [runtime contract](framework/docs/content/runtime-contract.md)
- [Authentication](framework/docs/content/auth.md) and [access control](framework/docs/content/access-control.md)
- [Contracts](framework/docs/content/contracts.md): the `gofastr verify` rule catalog and semantic coverage
- [Deployment](framework/docs/content/deploy.md) and [horizontal scaling](framework/docs/content/scaling.md)
- [Agent-ready](framework/docs/content/agent-ready.md): llms.txt, the agent card, and MCP discovery

## Project status

GoFastr is pre-1.0 and explicitly not stable. Pin a version
(`go get …@v0.x.y`); a `v1.0.0` tag will mark the stability promise.

- The `core/` primitives are usable and tested in isolation.
- The `framework/` entity layer handles SQLite + Postgres CRUD apps today.
- `core-ui/` changes fastest. Its exported APIs follow the same [deprecation policy](framework/docs/content/stability.md) as the rest of the tree: deprecate first, keep the old shape for at least one minor release. Expect that window to be exercised more often here than anywhere else before v1.
- The CLI binary bundles a pure-Go SQLite driver (`modernc.org/sqlite`, registered as `sqlite3` by `gofastr/sqlite/stdlib`), so it builds with `CGO_ENABLED=0`. To run migrations against Postgres, build a custom binary that imports your driver of choice.

## Why this exists

This is a personal project first, a way to practice building something
large alongside AI. A few things I wanted to dig into:

- **Solidify my web-tech foundations** by rebuilding the stack from the
  socket up, so the fundamentals stop feeling like magic.
- **Attack UI generation from a different angle.** My background is in
  Node; I wanted to see what server-rendered, server-driven UI looks like
  with the heavy client framework off the table and the markup generated
  in a compiled language instead.
- **Work in a compiled language**, where a compiler catches the mistakes
  Node surfaces at runtime, in production.
- **Skip the convention-vs-configuration false choice.** Your own
  framework can have opinionated defaults *and* a hatch down to plain
  stdlib code in the same app.
- **Build something large, fun, and open source with AI.** Most of this
  repo was written alongside coding agents; the workflow itself is part
  of the experiment.

## Contributing

This repo is a personal research tree at the moment. Issues and PRs are welcome but expect strong opinions about scope: the goal is a framework an AI agent can drive end-to-end, not a kitchen-sink CMS.

Before pushing, the `.githooks/pre-push` gate re-runs the deterministic CI test sweep (when a commit skipped it), module-integrity checks, and `govulncheck`. The race pass is separate. Run `make test-race` for that. Enable hooks once with:

```bash
git config core.hooksPath .githooks
```

### Testing against Postgres

The framework's tests fan over both SQLite and Postgres. With Docker
running, every dialect-aware test runs on both engines automatically:

```bash
make test            # SQLite only, fast
make test-pg         # both dialects against the docker-compose Postgres
make test-pg-env     # both dialects, points at TEST_POSTGRES_DSN
make test-race       # race detector across the whole repo
```

Each Postgres test gets its own schema for isolation; the container is
shared across the whole `go test` invocation so cold-start is amortised.

## License

[MIT](LICENSE): free to use, modify, and distribute, including commercially,
provided the copyright notice and license text are preserved.
