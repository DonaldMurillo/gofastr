# Overview

GoFastr is a full-stack Go framework for prototypes and small-to-medium apps: **go from idea to a full-stack app, fastr.** Simple for humans to understand. Optimized for agents and the agentic ecosystem.

You decide how barebones or batteries-included you want to be. Start with a stdlib-first router and HTML builder, or turn on server-rendered screens with interactive islands (no React or Node), auth, SQLite or Postgres, background jobs, search, and MCP—all in plain Go you own, compiling to one binary.

## Barebones or full stack

GoFastr is split into clean layers, and you can mix them in the same app.

### 1. Barebones (`core/` + `core-ui/`)

Stdlib-first building blocks—router, typed handlers, HTML rendering, SQL query builder, schema, migrations, OpenAPI, MCP, signals, and the client runtime. Each package works on its own with no framework required:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/router"
import "github.com/DonaldMurillo/gofastr/core/render"
import "net/http"
-->
```go
r := router.New()
r.Get("/", render.HTMLHandler(func(req *http.Request) render.HTML {
	return render.Tag("h1", nil, render.Text("Hello from core."))
}))
http.ListenAndServe(":8080", r)
```

### 2. Managed server + UI + MCP (`framework/` + `framework/ui/`)

Step up to `framework` when you want a managed server lifecycle, styled UI components, middleware, health checks, and MCP tools—without needing a database or entities yet:

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core-ui/html"
import "github.com/DonaldMurillo/gofastr/core/mcp"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/ui"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
type HomeScreen struct{}
func (s *HomeScreen) ScreenTitle(context.Context) string { return "Hello" }
func (s *HomeScreen) Render() render.HTML {
	return ui.Card(ui.CardConfig{Heading: "Ship faster"},
		html.Paragraph(html.TextConfig{}, render.Text("Server-rendered HTML in Go.")),
	)
}
-->
```go
pages := app.NewApp("Demo")
pages.Register("/", &HomeScreen{}, nil)

fw := framework.NewUIHostApp(uihost.New(pages),
	framework.WithMCP(),
	framework.WithMCPTools(func(s *mcp.Server) error {
		return s.RegisterTool("ping", "Health ping", nil, func(context.Context, map[string]any) (any, error) {
			return map[string]string{"status": "ok"}, nil
		})
	}),
)
fw.Start(":8080")
```

### 3. Full stack (`gofastr init` + `battery/`)

When you want to move as fast as possible, `gofastr init myapp` scaffolds a working app on disk—plain Go you read, edit, and commit—with SQLite or Postgres, auth screens, hot reload (`gofastr dev`), and onboarding files for your coding agent. Add optional `app.Entity(...)` declarations when you want auto-wired tables, REST CRUD, OpenAPI, and MCP tools, or drop to `net/http` and `database/sql` whenever a route needs custom logic.

You can also scaffold a whole multi-screen app from a single `gofastr.yml` file ([app spec tutorial](/docs/tutorial-app-spec)), then delete the YAML and own the Go.

## The major areas

### UI without a JS build step

Every page renders full HTML on the server. A small vanilla-JS runtime hydrates it in place: cross-page links navigate client-side with a route cache, and in-page updates happen via server islands, client signals, polling, or SSE push.

- **[UI getting started](/docs/ui-getting-started)** and **[UI capability map](/docs/ui-capability-map)**: build your first screens and pick the right pattern.
- **[Reactivity model](/docs/reactivity)** and **[Runtime contract](/docs/runtime-contract)**: signals → RPC → polling → SSE.
- **[Theming](/docs/theming)**, **[Forms](/docs/form-module)**, **[Widgets](/docs/widgets)**, and the live **[component gallery](/components/)**.
- **[Static export](/docs/static-export)** and **[PWA](/docs/pwa)**: ship static sites or installable apps from the same screens.

### Backend, data & batteries

SQLite (pure Go, `CGO_ENABLED=0`) and Postgres out of the box, plus opt-in `battery/` packages behind small interfaces.

- **[Backend capability map](/docs/backend-capability-map)**: find the exact package for any backend job.
- **[Entity declarations](/docs/entity-declarations)**, **[Migrations](/docs/migrations)**, **[Hooks & transactions](/docs/hooks-and-transactions)**, and **[Query DSL](/docs/query-dsl)**: opt-in data layer when CRUD saves you time.
- **[Auth](/docs/auth)**, **[Access control](/docs/access-control)**, **[Security defaults](/docs/security)**, and **[Multi-tenant scoping](/docs/multi-tenant)**: fail-closed by default.
- **[Queue](/docs/queue)**, **[Cron](/docs/cron)**, **[Email](/docs/email)**, **[Search](/docs/search)**, **[Storage](/docs/storage)**, **[Webhooks](/docs/webhooks)**, and **[Admin UI](/docs/admin)**: import only what you use.

### Built for coding agents and the agentic web

Agents work with GoFastr on both sides of deployment:

- **While building:** `gofastr dev` hands Claude Code or Codex your app's routes, config, docs, and logs over MCP; `gofastr verify` runs static and semantic contract checks ([contracts](/docs/contracts)) to catch security, routing, and UI bugs before they ship.
- **In production:** opt-in options serve `/mcp` tools behind the same auth and permissions as your HTTP API, along with per-screen `llm.md` (via `uihost.WithPublicLLMMD()`), `/llms.txt`, and discovery endpoints ([agent-ready](/docs/agent-ready), [MCP server](/docs/mcp)).

### Shipping & growing

- **[Project structure](/docs/project-structure)**: start flat, extract domain packages only when boundaries appear.
- **[Deployment](/docs/deploy)** and **[Horizontal scaling](/docs/scaling)**: one static binary on a VPS, or multiple replicas with shared sessions and fanout.
- **[Client SDKs](/docs/sdk)**, **[Customer CLI](/docs/app-cli)**, and **[Application hosts](/docs/application-hosts)**: generate Go/TS SDKs or a branded CLI, or run your app headless in-process.

## Where to go next

1. **[Get started](/get-started)**: from zero to a running app in minutes.
2. **[Project structure](/docs/project-structure)**: how a GoFastr app is laid out and grows.
3. **[Examples](/examples)**: runnable reference apps, including the Meridian SaaS console.
4. **[Full docs index](/docs/)**: every topic, also available offline with `gofastr docs`.
