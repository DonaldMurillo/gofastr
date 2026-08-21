# GoFastr

> The full-stack Go framework that doesn't get in the way of you or your agents.

**[Docs, component gallery, and live demos →](https://donaldmurillo.github.io/gofastr/)**
&nbsp;·&nbsp; [Quickstart](#quickstart)
&nbsp;·&nbsp; [A GoFastr app in production](https://barcode.donaldmurillo.com/)

GoFastr is a pre-v1 full-stack Go framework. The API can still change between releases, under the [deprecation policy](framework/docs/content/stability.md). Declare your domain in Go and get server-rendered screens, REST endpoints, MCP tools, an OpenAPI spec, SQL migrations, and a typed query builder. The output is plain Go you can read, edit, and own: no reflection discovers your entities, no generated code you can't open. Auth, background jobs, search, and storage are opt-in packages, and you can drop to `net/http` or `database/sql` at any point.

It is built for both the agentic web and AI-assisted development. The app you ship joins the agentic web: the agents your users bring call your data over MCP, with the same login and permissions your users have. While you build, `gofastr dev` hands your coding agent, Claude Code or Codex, the app's routes, config, and logs over MCP, to help build and debug it.

Start with [the quickstart](#quickstart). Or scaffold a whole app in one command: screens, API, and auth from `gofastr init <name>`, or `gofastr generate` from a one-file declaration ([blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md)).

> **Status:** early / `v0.x`. MIT-licensed and usable, but the API may change
> between releases, so pin a version (`go get …@v0.x.y`). A `v1.0.0` tag will
> mark the stability promise. Ship at your own risk until then.

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
go test ./...                        # SQLite + everything but the Postgres halves and chromedp e2e

# The Postgres suites read TEST_POSTGRES_DSN and skip without it. `make
# postgres-up` starts the service but cannot export into your shell:
make postgres-up
export TEST_POSTGRES_DSN='postgres://test:test@localhost:5432/framework_test?sslmode=disable'
go test ./...                        # now both dialects (Chrome still needed for chromedp e2e)
go run ./cmd/gofastr --help          # CLI overview
go run ./examples/blog               # minimal blog with auto-CRUD on SQLite
```

Two Git worktrees of the same app, one per coding agent for example,
can run side by side: with isolation on, each linked worktree gets its own
local port and database path, so nothing collides
([`framework/docs/content/isolation.md`](framework/docs/content/isolation.md)).

With the blog example running (`go run ./examples/blog`), open
<http://localhost:8080> and try:

```bash
curl http://localhost:8080/posts
curl 'http://localhost:8080/posts/search?q=gofastr'   # custom route the example adds
# /openapi.json is auth-gated by default (it enumerates every route).
# /api/docs/ serves a page linking the spec (load it in Swagger UI,
# Insomnia, …), or expose the raw spec with
# framework.WithPublicOpenAPI() and then:
curl http://localhost:8080/openapi.json | jq .info     # auto-generated spec
```

### Updating GoFastr

The module dependency and the installed CLI are versioned
independently. Keep them on the same release. Read the [release
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
| Cursor paging    | `?cursor=&limit=50`: keyset by `EntityConfig.Pagination.CursorField` (defaults to PK); composite `Pagination.CursorFields` takes precedence     |
| Multipart upload | `multipart/form-data` on `Image`/`File` fields → streamed through `WithFileStorage` |
| Validation       | Required, unique, enum, min/max, regex pattern, multi-tenant scope              |
| Migrations       | Versioned runner: advisory-lock serialization, checksum-drift + dirty-state guards, `NoTransaction` escape hatch, a down section when a safe inverse exists; declarative incremental generation; real-Postgres tested |
| FK constraints   | BelongsTo relations emit `FOREIGN KEY` clauses; `AutoMigrate` topo-sorts tables |
| Transactions     | `Create/Update/Delete` + hooks share one tx; `TxFromContext(ctx)` exposes it    |
| OpenAPI 3        | `/openapi.json` plus a spec-viewer page at `/api/docs/`                         |
| MCP              | `posts_list`, `posts_get`, `posts_create`, `posts_update`, `posts_delete`       |
| Soft delete      | `deleted_at` column + automatic filter                                          |
| Multi-tenant     | `tenant_id` column + automatic scope from request context                       |
| Hooks            | `BeforeCreate`, `AfterUpdate`, etc. for custom behaviour                        |
| Custom routes    | `EntityConfig.Endpoints` with optional MCP exposure                             |
| Client SDKs      | `gofastr generate sdk`: Go module + JS/TS client artifacts an app can serve via `sdkdocs.Mount`, with a live docs site (`framework/sdkdocs`) |
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
- **The framework checks whether you're still using it well.** `gofastr verify` runs the contract catalog over your code, covering routing, permissions, security, architecture, rendering, accessibility, and more. It reports what drifted, and each finding carries the reason, the fix, and a worked example. It also measures *semantic* coverage: not "did this line run" but "did a request ever reach this route through the real router, did this permission ever get evaluated, did this lifecycle hook ever fire". Strict by default; relax a rule in `gofastr.contracts.yml` or waive one line with `//gofastr:allow(RULE) reason`, both visible in review. An existing codebase adopts it with `--baseline-write`, which accepts today's debt and fails on anything added. See [`contracts.md`](framework/docs/content/contracts.md).
- **A blueprint scaffolds the whole app when you want a head start.** A single `gofastr.yml` generates both halves, SQL + REST + OpenAPI + MCP *and* the screens, in one pass, consistent from the start. Then it's plain Go you own and edit, and the running app never needs the blueprint again. See [`examples/meridian`](examples/meridian/) for the whole pipeline, a SaaS console + marketing site, live and tested.

## The repo in 60 seconds

| Directory | What it is | Depend on it when… |
|---|---|---|
| `core/` | Stdlib-only primitives: router, query, schema, render, mcp, openapi, migrate. Each usable on its own. | you want plain Go building blocks, no framework. |
| `framework/` | The opinionated entity layer (`App`, `EntityConfig`, CRUD, hooks, migrations). A thin facade re-exporting its focused runtime subpackages. | you want one declaration → SQL + REST + OpenAPI + MCP. |
| `core-ui/` | Server-driven UI runtime: `html` primitives, `patterns`, `widget` islands, signals, the vanilla-JS runtime. Independently usable. | you're rendering HTML from Go. |
| `battery/` | Opt-in infrastructure: admin, auth, cache, email, semantic, log, notify, print, queue, search, setup, storage, webhook. Each behind a small interface. | you need a real subsystem; import only the ones you use. |
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

The framework also runs on itself. GoFastr's own tooling and reference apps are
built on the same `framework`, `core-ui`, and batteries a user app imports:

- **`examples/site`**, the docs site and canonical component gallery, runs on
  `framework` + `framework/ui` + `framework/uihost` + the `core-ui` pattern
  presets + `battery/print`.
- **`examples/meridian`** started from a `gofastr.yml` blueprint and has been
  edited by hand ever since: a SaaS billing console (customers,
  subscriptions, invoices with status workflows, MRR + charts) *and* its public
  marketing site, auth, RBAC, and admin back-office, with writable app screens
  (add/edit/delete). Read it to see what a blueprint grows into once you own
  the code; see `examples/meridian/doc.go` for what is still checked against
  the blueprint.
- **`examples/ecommerce`** is the app the generator still owns outright. Its
  blueprint sets `output_dir: app`, and `flagship_test.go` regenerates `app/`
  with `--force` on every run, so a generator regression fails a test.

Both apps are secure by default and carry an end-to-end test suite: every
screen, the full create→edit→delete lifecycle, and RBAC asserted. Ecommerce's
suite is regenerated with the app; Meridian's has been extended by hand
alongside its owned Go (the `sdkdocs` mount, the brand theme, the island
table).

The project uses these tools on itself. External production adopters are
the part still ahead of us. See [Project status](#project-status).

## Documentation

Every doc below is embedded into the `gofastr` binary. `gofastr docs` browses
them offline, and the `framework_docs_*` MCP tools expose them to agents
connected to a running app.

- [The gofastr CLI](framework/docs/content/cli.md): init, dev, migrate, generate, verify, audit, and upgrade, each subcommand mapped to its doc
- [Contracts](framework/docs/content/contracts.md): **`gofastr verify`**, the 51 rules that say whether an app is still an idiomatic GoFastr app, semantic coverage beyond line coverage, and how an existing codebase adopts the gate with a baseline
- [Blueprint tutorial](framework/docs/content/tutorial-blueprint-app.md): **generate a whole app from one file**. Blueprint → generated UI + API → auth + owner scoping + RBAC → customize in plain Go → deploy
- [Kiln (experimental)](framework/docs/content/kiln.md): agent-driven build mode
- [UI capability map](framework/docs/content/ui-capability-map.md): **start from the job**. Architecture, state ownership, delivery/scaling semantics, runnable proof, and explicit non-goals
- [UI getting started](framework/docs/content/ui-getting-started.md): **the 15-minute path**. Scaffold → design direction → theme → framework-native composition
- [UI composition recipes](framework/docs/content/ui-composition-recipes.md): product-shaped page structures built entirely from `framework/ui` primitives
- [UI components index](framework/docs/content/ui-new-components.md): **the catalog**. Every component the framework ships, with its `go doc` path and live demo at `/components/<slug>` in `examples/site`
- [core-ui architecture](core-ui/ARCHITECTURE.md): **deeper UI/runtime reference** (SSR, hydration, islands, component CSS, data-fui-* primitives)
- [Runtime contract](framework/docs/content/runtime-contract.md): the SSR/hydration/island/SSE model and the full `data-fui-*` attribute reference as an embedded page (extract of `core-ui/ARCHITECTURE.md`, kept in sync by test)
- [Interactive patterns](framework/docs/content/interactive-patterns.md): every `data-fui-*` behavior, plus **"Writing a hand-written island, end to end"** (no-reload updates on your own screens) and themed confirms
- [framework architecture](framework/ARCHITECTURE.md): package layout, layering rules, cycle-breaking interfaces
- [Entity declarations](framework/docs/content/entity-declarations.md): JSON schema reference
- [Migrations](framework/docs/content/migrations.md): versioned migrations and the CLI
- [Query DSL](framework/docs/content/query-dsl.md): `Entity.where(...).order(...).limit(N)`
- [Search](framework/docs/content/search.md): the `battery/search` interface
- [Semantic search](framework/docs/content/semantic-search.md): local semantic search via `battery/semantic`
- [Embeddable surfaces](framework/docs/content/embed.md): hand a screen to a website you don't control. Single-use handshake nonce, exact origin allowlist, a frame runtime with no SPA navigation
- [Authentication](framework/docs/content/auth.md): the `battery/auth` reference. Password login, magic links, OAuth (Google + GitHub built in, any OIDC IdP), 2FA, password reset, sessions, scoped API tokens. Each method is an opt-in plugin
- [Access control](framework/docs/content/access-control.md): permission-based RBAC. `RequirePermission` on your own routes, `AccessMiddleware`, gating auto-CRUD per entity, and where owner scoping fits alongside it
- [Security](framework/docs/content/security.md): defaults, headers, and limits
- [Deployment](framework/docs/content/deploy.md): single-binary build, graceful shutdown, production checklist
- [Horizontal scaling](framework/docs/content/scaling.md): what's process-local by default and the replica-safe alternative for each
- [Observability](framework/docs/content/observability.md): metrics and tracing
- [PWA](framework/docs/content/pwa.md): installable app manifest + versioned offline shell via `uihost.WithPWA`
- [Agent-ready](framework/docs/content/agent-ready.md): the discovery endpoints for AI agents (llms.txt, agent card, MCP)
- [Strict mode](framework/docs/content/strict-mode.md): `uihost.WithStrict` turns missing SEO and missing per-screen axe tests into boot failures

The list above is a curated subset. The [docs index](framework/docs/content/README.md) is the browsable entry point to the rest; the full, per-topic catalogue lives in the docs site (`gofastr docs --list`, or `examples/site/docs_catalog.go`), which a parity test keeps in sync with every embedded page.

## Project status

GoFastr is pre-1.0 and explicitly not stable:

- The `core/` primitives are usable and tested in isolation.
- The `framework/` entity layer handles SQLite + Postgres CRUD apps today.
- `core-ui/` changes fastest. Its exported APIs follow the same [deprecation policy](framework/docs/content/stability.md) as the rest of the tree: deprecate first, keep the old shape for at least one minor release. Expect that window to be exercised more often here than anywhere else before v1.
- The CLI binary bundles a pure-Go SQLite driver (`modernc.org/sqlite`, registered as `sqlite3` by `gofastr/sqlite/stdlib`), so it builds with `CGO_ENABLED=0`. To run migrations against Postgres, build a custom binary that imports your driver of choice.

## Why this exists

This is a personal project first, a way to practice building something
large alongside AI. A few things I wanted to dig into:

- **Solidify my web-tech foundations.** Rebuild the stack from the
  socket up so the fundamentals stop feeling like magic.
- **Attack UI generation from a different angle.** My background is in
  Node, so I wanted to see what server-rendered, server-driven UI looks
  like when you take the heavy client framework off the table and
  generate the markup in a compiled language instead.
- **Work in a compiled language.** Most of what I've built is in Node,
  where mistakes surface at runtime, in production. I wanted to know what
  it's like when a compiler catches them first, when you ship one binary
  and types actually hold up under a refactor.
- **Skip the convention-vs-configuration false choice.** When it's your
  own framework you don't have to pick a side: you get opinionated
  defaults *and* a hatch down to plain stdlib code in the same app.
- **Build something large, fun, and open source with AI.** Most of this
  repo was written alongside coding agents, so the workflow itself is
  part of the experiment.
- **Build for agents on both sides.** In production, the agents your
  users bring call the app's data over MCP, with the same login and
  permissions the users have. While you build, `gofastr dev` hands your
  coding agent the running app's routes, config, and logs over MCP. Both
  fall out of writing plain, readable Go.

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

GoFastr is released under the [MIT License](LICENSE): free to use, modify, and
distribute, including in commercial and closed-source projects, provided the
copyright notice and license text are preserved. The software is provided "as
is", without warranty; see [`LICENSE`](LICENSE) for the full terms.
