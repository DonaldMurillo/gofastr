# GoFastr

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="assets/brand/gofastr-logo-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="assets/brand/gofastr-logo-light.svg">
  <img src="assets/brand/gofastr-logo-light.svg" alt="GoFastr" width="240">
</picture>

**Go from idea to a full-stack app, fastr.**

Simple for humans to understand. Optimized for agents and the agentic ecosystem.

[![CI](https://github.com/DonaldMurillo/gofastr/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/DonaldMurillo/gofastr/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/DonaldMurillo/gofastr)](https://github.com/DonaldMurillo/gofastr/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/DonaldMurillo/gofastr)](go.mod)
[![Go Reference](https://pkg.go.dev/badge/github.com/DonaldMurillo/gofastr.svg)](https://pkg.go.dev/github.com/DonaldMurillo/gofastr)
[![License: MIT](https://img.shields.io/github/license/DonaldMurillo/gofastr)](LICENSE)

**[Docs & live component gallery →](https://donaldmurillo.github.io/gofastr/)**
&nbsp;·&nbsp; [Quickstart](#quickstart)
&nbsp;·&nbsp; [Live app in production](https://barcode.donaldmurillo.com/)

---

GoFastr is a full-stack Go framework for prototypes and small-to-medium apps. Stay as barebones as a stdlib router and HTML helper, or turn on the whole stack—server-rendered UI with interactive islands (no React or Node), auth, SQLite or Postgres, background jobs, search, and MCP—in one binary of plain Go you own.

It is designed so both you and your coding agents can move fast without creating a mess: plain Go on disk, live app introspection during development, automated contract checks before you ship, and built-in MCP and `llm.md` support for the agents your users bring.

## Quickstart

Requires Go 1.27.4+. Install the CLI:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
```

Prebuilt binaries and install scripts are also attached to [GitHub Releases](https://github.com/DonaldMurillo/gofastr/releases) (see [cli](framework/docs/content/cli.md)).

### Start a full-stack app in seconds

```bash
gofastr init myapp
cd myapp
gofastr dev
```

`gofastr init` scaffolds plain Go you own—server-rendered screens, auth, SQLite, hot reload, and onboarding files for coding agents. Prefer declaring an entire app in one file first? Scaffold it with `gofastr generate` ([app spec tutorial](framework/docs/content/tutorial-app-spec.md)), then keep and edit the generated Go.

## As barebones or as productive as you want

GoFastr is layered so you never have to buy the whole framework just to start—and you can always drop back to `net/http` or `database/sql` in the same app.

### Core only

Want to stay barebones? `core/` is a set of stdlib-first building blocks (router, typed handlers, HTML rendering, SQL query builder, migrations, MCP) that work without the framework:

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

Step up to `framework` for a managed server, styled UI components, typed routes, and MCP tools—no database or entities needed until you want them:

```go
package main

import (
	"context"
	"log"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle(context.Context) string { return "Hello" }
func (s *HomeScreen) Render() render.HTML {
	return ui.Card(ui.CardConfig{Heading: "Ship faster"},
		html.Paragraph(html.TextConfig{}, render.Text("Server-rendered HTML in Go, zero JS build step.")),
	)
}

type Greeting struct {
	Message string `json:"message"`
}

func main() {
	pages := app.NewApp("Demo")
	pages.Register("/", &HomeScreen{}, nil)

	fw := framework.NewUIHostApp(uihost.New(pages),
		framework.WithMCP(),
		framework.WithMCPTools(func(s *mcp.Server) error {
			return s.RegisterTool("greet", "Return a greeting", nil, func(ctx context.Context, _ map[string]any) (any, error) {
				return Greeting{Message: "hello from mcp"}, nil
			})
		}),
	)

	fw.Router().Get("/api/hello", handler.HandlerAdapter(func(ctx context.Context, _ struct{}) (Greeting, error) {
		return Greeting{Message: "hello from go"}, nil
	}))

	log.Fatal(fw.Start(":8080")) // HTML at /, JSON at /api/hello, MCP at /mcp
}
```

### Donald's Way

Turn on the full stack in one binary: server-rendered screens with SEO and `llm.md`, an owner-scoped API, MCP tools for user agents, and login + sessions:

```go
package main

import (
	"context"
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

func (s *HomeScreen) ScreenTitle(ctx context.Context) string { return "Notes" }
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

`GET /` renders HTML, anonymous `GET /api/notes` returns `401`, `/auth/register` and `/auth/login` work out of the box, and `/mcp` enforces the exact same per-user scope as the HTTP API.

## Why GoFastr

- **Simple to read, easy to drop down.** Everything stays plain Go on disk—no reflection discovery, no black-box runtime, no split frontend/backend repo. Start flat and grow into `internal/<domain>/` when boundaries appear ([project structure](framework/docs/content/project-structure.md)).
- **Interactive UI without a JS framework.** Write screens and components in Go (`core-ui/` and `framework/ui`). Pages server-render on first load and hydrate with a small JS runtime for client-side navigation, server islands, signals, polling, and SSE ([reactivity](framework/docs/content/reactivity.md)). No React, Vue, or bundler required.
- **Optimized for coding agents.** `gofastr dev` gives Claude Code or Codex live access to your app's routes, config, docs, and logs over MCP. `gofastr verify` checks your app against 86 contract rules covering routing, permissions, security, rendering, and accessibility ([contracts](framework/docs/content/contracts.md)), catching agent mistakes early.
- **Ready for the agentic web.** Ship `/mcp` tools behind the same sessions, RBAC, and owner/tenant scopes as your HTTP API, along with automatic `llm.md`, `llms.txt`, and discovery endpoints ([agent-ready](framework/docs/content/agent-ready.md)).
- **Opt-in batteries, one binary.** Pull in `battery/` packages only as you need them—auth, queues, search, email, storage, admin UI, webhooks, notifications, telemetry—backed by SQLite (pure Go, `CGO_ENABLED=0`) or Postgres.
- **Grow beyond a web server when needed.** Generate typed Go/TS SDKs or a customer CLI from your app, run headless in-process with `framework.ServicesHost()`, or package as a local OS service or pure-Go desktop app (experimental).

## What's in the repo

| Directory | Role |
|---|---|
| `core/` | Stdlib-first primitives: router, typed handlers, HTML render, SQL query builder, schema, migrate, OpenAPI, MCP. Usable standalone. |
| `core-ui/` | Server-driven UI kernel: HTML primitives, islands, widgets, signals, browser-local IndexedDB, and the vanilla JS runtime. |
| `framework/` | Opinionated full-stack layer (`App`, UI host, component kit, CRUD/hooks/migrations, access control, contracts). |
| `battery/` | Opt-in infrastructure: auth, admin, cache, email, log, notify, print, queue, rtc, search, semantic, storage, webhook, plus experimental desktop/service/telemetry hosts. |
| `cmd/gofastr` | CLI for scaffolding (`init`, `generate`), dev server (`dev`), verification (`verify`), migrations, SDK/CLI generation, releases, and offline docs (`docs`). |
| `examples/` | Reference apps: [`meridian`](examples/meridian) (flagship SaaS app) and [`site`](examples/site) (docs site and component gallery). |

## Live examples

- **[Barcode & QR Code Maker](https://barcode.donaldmurillo.com/)** — production app built on GoFastr with web UI, batch exports, REST API, and an MCP server.
- **[Docs & component gallery](https://donaldmurillo.github.io/gofastr/)** (`examples/site`) — live docs and interactive UI gallery running on GoFastr.
- **[Meridian](examples/meridian)** — SaaS billing console and marketing site built end-to-end with GoFastr. Run it locally with `go run ./examples/meridian`.

## Documentation

Browse online at **[donaldmurillo.github.io/gofastr](https://donaldmurillo.github.io/gofastr/)**, offline in your terminal with `gofastr docs`, or start here in the repo ([full index](framework/docs/content/README.md)):

- [CLI reference](framework/docs/content/cli.md)
- [UI getting started](framework/docs/content/ui-getting-started.md) and [components](framework/docs/content/ui-new-components.md)
- [Entity declarations](framework/docs/content/entity-declarations.md) and [app spec tutorial](framework/docs/content/tutorial-app-spec.md)
- [Auth](framework/docs/content/auth.md) and [access control](framework/docs/content/access-control.md)
- [Agent-ready apps & MCP](framework/docs/content/agent-ready.md)
- [Contracts & `gofastr verify`](framework/docs/content/contracts.md)
- [Deployment & scaling](framework/docs/content/deploy.md)

## Status & contributing

GoFastr is **pre-1.0**: APIs can still change between minor releases. Pin a release version and use `gofastr upgrade` to see migration notes and affected lines when upgrading ([stability](framework/docs/content/stability.md), [upgrading](framework/docs/content/upgrading.md)).

To run tests or examples from a clone:

```bash
git clone https://github.com/DonaldMurillo/gofastr.git
cd gofastr
go test ./...              # SQLite suite (fast)
go run ./examples/meridian # run the flagship SaaS app locally
```

Postgres tests (`make test-pg`), race detection (`make test-race`), git hooks, and architecture guidelines live in [docs/contributing.md](docs/contributing.md).

## License

[MIT](LICENSE)
