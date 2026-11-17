<a id="top"></a>
<div align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/brand/gofastr-logo-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="assets/brand/gofastr-logo-light.svg">
    <img src="assets/brand/gofastr-logo-light.svg" alt="GoFastr" width="240">
  </picture>

  <h1>GoFastr</h1>
  <p>Go from idea to a full-stack app, fastr.</p>
  <p>
    <a href="https://donaldmurillo.github.io/gofastr/">Docs and component gallery</a>
    · <a href="https://barcode.donaldmurillo.com/">Try Barcode &amp; QR Code Maker</a>
    · <a href="https://github.com/DonaldMurillo/gofastr/issues">Report a bug</a>
  </p>

  <p>
    <a href="https://github.com/DonaldMurillo/gofastr/actions/workflows/ci.yml"><img src="https://github.com/DonaldMurillo/gofastr/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI"></a>
    <a href="https://github.com/DonaldMurillo/gofastr/releases"><img src="https://img.shields.io/github/v/release/DonaldMurillo/gofastr" alt="Release"></a>
    <a href="https://pkg.go.dev/github.com/DonaldMurillo/gofastr"><img src="https://pkg.go.dev/badge/github.com/DonaldMurillo/gofastr.svg" alt="Go Reference"></a>
    <a href="LICENSE"><img src="https://img.shields.io/github/license/DonaldMurillo/gofastr" alt="License: MIT"></a>
  </p>

</div>

[About](#about) · [Quickstart](#quickstart) · [Screenshots](#screenshots) · [Examples](#examples) · [Documentation](#documentation) · [Project status](#project-status) · [Contributing](#contributing)

## About

GoFastr is a full-stack Go framework. Write pages and data models in Go. Pages render on the server. Each model gets a SQL migration, REST routes, an OpenAPI spec, and optional MCP tools that agents can call.

Donald Murillo built GoFastr to make it easier to work on multiple
agent-driven projects in parallel. Read more about the [why here](https://donaldmurillo.com/blog/why-i-built-gofastr).

## Quickstart

### What you need

- **Go 1.27.4 or newer.** Run `go version` in a terminal to see which version you have. If Go is missing or older, download it from [go.dev/dl](https://go.dev/dl/), then follow the [install steps](https://go.dev/doc/install).
- **A terminal.** Terminal on macOS, PowerShell or Command Prompt on Windows, or any shell on Linux.
- **Git** (optional). `gofastr init` runs `git init` in the new project and warns if Git is missing. You also need Git to clone the [Meridian example](#run-meridian). Download it from [git-scm.com](https://git-scm.com/downloads).
- **A coding agent** (optional), such as Claude Code or Codex. `gofastr init` writes `CLAUDE.md` and `AGENTS.md` for it, and `gofastr dev` gives it MCP tools.

The default SQLite driver is written in Go and keeps data in a `my-app.db` file in your project. To use Postgres instead, run `gofastr init my-app --db=postgres` and supply your own server.

### Create an app

Copy these commands into your terminal:

```bash
go install github.com/DonaldMurillo/gofastr/cmd/gofastr@latest
gofastr init my-app
cd my-app
go mod tidy
gofastr dev
```

If your shell can't find `gofastr`, add the Go binary directory to your `PATH`. It defaults to `$(go env GOPATH)/bin` unless you set `GOBIN`.

Open <http://localhost:8080>. You'll see the starter home page. Register at `/register`, then sign in at `/login` to use the SQLite-backed posts API at `/api/posts`. Edit `screens.go` and save to see the dev server reload the page.

To describe a whole app in YAML, follow the [app spec tutorial](framework/docs/content/tutorial-app-spec.md). It takes a `gofastr.yml` with entities and screens and generates Go source you can edit.

<div align='right'>

[Back to Top](#top)

</div>

## Screenshots

Meridian is GoFastr's sample SaaS app. Its public site and signed-in billing
console run in the same Go app. [Run Meridian locally](#run-meridian)

| Meridian home page | Meridian billing console |
| --- | --- |
| <img src="assets/readme/meridian-home.webp" width="400" alt="Meridian home page with pricing links and feature cards"> | <img src="assets/readme/meridian-billing.webp" width="400" alt="Meridian billing dashboard showing revenue, customer status, and recent invoices"> |

<div align='right'>

[Back to Top](#top)

</div>

## How GoFastr works

- Screens render to HTML on the server. A small JavaScript runtime attaches to that HTML and updates individual page regions as users interact. [UI guide](framework/docs/content/ui-getting-started.md)
- Entity declarations drive schema migrations, record validation, CRUD routes, and OpenAPI. With MCP enabled, entity tools follow the same access rules as the REST API. [Entity declarations](framework/docs/content/entity-declarations.md) · [Access control](framework/docs/content/access-control.md)
- The router, SQL query builder, and renderer in `core/` work independently. You can also use plain `net/http` and `database/sql` in the same app. Auth, jobs, search, and storage are separate packages. [Core packages](framework/docs/content/core-packages.md)
- During development, `gofastr dev` gives a connected coding agent MCP tools to inspect routes and app state, plus recent logs when the logging package is enabled. `gofastr verify` runs 86 contract rules for things like routing, permissions, and accessibility. [Dev tools](framework/docs/content/dev-livereload.md) · [Contract checks](framework/docs/content/contracts.md)

<div align='right'>

[Back to Top](#top)

</div>

## Examples

### Run Meridian

To see the seeded billing console, set `ADMIN_SEED_PASSWORD` in your
environment to a local demo password before the first run.

```bash
git clone https://github.com/DonaldMurillo/gofastr.git
cd gofastr/examples/meridian
go run .
```

Open <http://localhost:8080> or the address printed in your terminal. Sign in
at `/login` as `admin@meridian.dev` with the password you set, then open `/app`.
[Meridian source](examples/meridian)

### Other apps built with GoFastr

- **Barcode & QR Code Maker.** Create or read barcodes and export batches from CSV or Excel. [Live app](https://barcode.donaldmurillo.com/)
- **Lotus Vegan Restaurant.** A restaurant site with a standalone menu. [Source](https://github.com/AlexVCS/lotus-vegan-restaurant)
- **St. Petersburg Library System recreation.** A public information site built as a single Go binary. [Source](https://github.com/AlexVCS/splibraries)

<details>
<summary>Show three runnable Go examples</summary>

Each is a complete `main.go`. To run one, save it in a new directory, run `go mod init example.com/demo` and `go mod tidy`, then `go run .`. The server listens on port 8080.

### Core only

A server-rendered home page at `/` and a typed JSON endpoint at `/api/ping`.

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

A SQLite-backed API at `/posts` with an OpenAPI spec at `/openapi.json` and MCP tools at `/mcp`. Run this demo locally. It allows anonymous reads and writes.

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

### Donald's Way

A full notes app with server-rendered screens, registration, and login. The notes REST API and MCP tools scope records to their owner.

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

</details>

<div align='right'>

[Back to Top](#top)

</div>

## Documentation

- [CLI reference](framework/docs/content/cli.md) for scaffolding, running, and building apps.
- [Authentication guide](framework/docs/content/auth.md) for login and session setup.
- [Agent integration](framework/docs/content/agent-ready.md) for exposing app capabilities to agents using MCP.
- [Deployment guide](framework/docs/content/deploy.md) for shipping an app.

Browse the [full docs index](framework/docs/content/README.md) or run `gofastr docs` to read the docs offline.

<div align='right'>

[Back to Top](#top)

</div>

## Project status

GoFastr is pre-1.0. Pin the Go module and CLI to the same release. Before updating, run `gofastr upgrade` to see which changes apply to your app. [Releases](https://github.com/DonaldMurillo/gofastr/releases) · [Upgrade policy](framework/docs/content/stability.md)

<div align='right'>

[Back to Top](#top)

</div>

## Contributing

See the [contributing guide](docs/contributing.md) for local checks and the review process. [Open an issue](https://github.com/DonaldMurillo/gofastr/issues) to report a bug or discuss a change. Thanks to [CodeRabbit](https://www.coderabbit.ai) for reviewing pull requests.

<div align='right'>

[Back to Top](#top)

</div>

## License

[MIT](LICENSE).

<div align='right'>

[Back to Top](#top)

</div>
