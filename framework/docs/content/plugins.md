# Plugins and batteries

A **Plugin** bundles related additions to a `framework.App` — routes, middleware, hooks, MCP tools, or a logger swap — behind one `Init(app *framework.App) error` method so features stay modular and drop cleanly into any app.

## Minimum plugin

```go
type MyPlugin struct{}

func (MyPlugin) Name() string { return "my-plugin" }
func (MyPlugin) Init(app *framework.App) error {
	app.Router().Get("/hello", helloHandler)
	app.Use(myMiddleware)
	app.MCP.RegisterTool("my_tool", "Does the thing", schema, handler,
		mcp.WithToolGate(auth.MCPRole("admin")))
	app.HookRegistry("users").RegisterHook(framework.AfterCreate, sendWelcomeEmail)
	return nil
}

app.RegisterPlugin(MyPlugin{})
```

Key behaviors:
- **Late-bound middleware:** middleware registered in `Init` wraps routes registered earlier (no ordering footgun).
- **MCP tool gating:** directly registered MCP tools do not pass through HTTP route middleware — gate them with `mcp.WithToolGate(...)` (which also hides the tool from `tools/list` for unauthorized callers). Entity CRUD tools already inherit HTTP auth, owner scoping, and RBAC automatically.
- **Panic isolation:** `Init`, `OnStart`, `OnReady`, `WithSeed`, and battery hooks recover panics into attributed errors rather than crashing the process (scrubbing panic values from user-facing errors and logging stacks).

## Lifecycle hooks and seeding

- `app.OnStart(fn)` — runs before the listener binds.
- `app.OnReady(func(addr string))` — runs only after boot succeeds and the HTTP listener binds (for banners or service registration; wait on `app.ServicesReady()` on listener-less hosts like `ServicesHost`).
- `app.OnStop(fn)` / `app.OnStopFirst(fn)` — graceful shutdown hooks (`OnStopFirst` runs last so log sinks stay open until other shutdown hooks finish).
- `app.WithSeed(fn)` — runs after auto-migrate and per-entity `EntityConfig.Seed` under a Postgres advisory lock (keep hooks idempotent).

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "fmt"
-->
```go
app.OnReady(func(addr string) {
	fmt.Printf("listening on http://%s\n", addr)
})
```

## Plugin vs. Battery vs. Module

- **Plugin:** lightweight, runs in registration order. Use by default.
- **Battery:** adds dependency-ordered `Init` (`app.RegisterBattery(b, "cache")`) and `BatteryLifecycle` (`OnStart`/`OnStop`). Auth/identity batteries call `app.NoteAuthenticator()` so startup banners know auth is wired.
- **Module:** a Battery with live runtime enable/disable gating (see [Modules](modules.md)).

## Typed lookups

Prefer generic `framework.PluginGetAs` / `framework.GetAs` over manual type assertions:

```go
logp, err := framework.PluginGetAs[*logplugin.Plugin](app.Plugins, "log")
searcher, err := framework.GetAs[search.Backend](app.Batteries, "search")
```

<!-- gofastr:compile
stmt: _ = p
stmt: _ = err
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
stmt: _ = all
stmt: _ = names
-->
```go
p, err := app.Plugins.Get("webhooks")
all := app.Plugins.All()
names := app.Plugins.Names()
```

## Common mistakes

- **Calling `RegisterPlugin` after `InitPlugins` / `Start`:** panics because init has already run.
- **Registering direct MCP tools without `mcp.WithToolGate`:** leaves tools open to anyone who can reach `/mcp`.
- **Adding `app.Use` inside `OnStart` instead of `Init`:** keep middleware composition inside `Init`.
- **Running `db.Exec("INSERT ...")` in `main()` before `App.Start`:** tables don't exist until auto-migrate runs; use `app.WithSeed`.
