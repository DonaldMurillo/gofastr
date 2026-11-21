# Modules

A **Module** is a `Battery` with a manifest and a **live enable/disable gate**. Everything a module registers during `Init` — HTTP routes, entities, MCP tools, cron schedulers (`app.AddCron`), and queues (`app.AddQueue`) — is attributed to it so operators (or agents via `WithMCPControl`) can turn the feature on or off without restarting.

- **Plugin:** lightweight bundle of routes/hooks/tools, no dependencies.
- **Battery:** dependency-ordered `Init` + `OnStart`/`OnStop` lifecycle.
- **Module:** a Battery that also supports runtime enable/disable.

## Registration

```go
type FeatureModule struct{}

func (FeatureModule) Name() string { return "feature" }
func (FeatureModule) Init(app *framework.App) error {
	app.Router().Get("/feature/status", statusHandler)
	app.Entity("widgets", widgetConfig)
	return nil
}
func (FeatureModule) Manifest() framework.ModuleManifest {
	return framework.ModuleManifest{
		Version:        "1.0.0",
		Description:    "Widget management",
		DependsOn:      []string{"auth"},
		MigrationGroup: "feature",
	}
}

app.RegisterModule(FeatureModule{})
```

`App.Start` runs a second migrate-and-seed pass after every `Init` finishes so entities registered inside a module have their tables ready before listening.

## Toggling at runtime

<!-- gofastr:compile
stmt: _ = err
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import "context"
var ctx = context.Background()
stmt: _ = enabled
stmt: _ = list
-->
```go
err := app.Modules().Disable(ctx, "feature")
err = app.Modules().Enable(ctx, "feature")

enabled := app.Modules().Enabled("feature")
list := app.Modules().List()
```

What disabling a module does:
- **Routes:** return plain `404` before middleware runs (and omit disabled methods from `Allow` headers).
- **MCP tools:** hidden from `tools/list` and return `-32601` (`not found`) on `tools/call`.
- **Cron & queues:** cron ticks skip; queue jobs defer back to pending without burning retries until re-enabled.
- **Fail-closed dependencies:** `Disable` refuses if an enabled module depends on it; `Enable` refuses if any dependency is disabled.
- **Persistence & fanout:** with `WithDB`, enabled state persists in `gofastr_modules` (fails closed at boot if unwritable). With `WithFanout` + shared DB, toggles propagate across replicas by sending a refresh signal on `gofastr.modules` so peers re-read the authoritative table. With `WithAuditLog`, toggles write `module_enable`/`module_disable` audit entries.

## Common mistakes

- **Registering routes or tools outside `Init`:** only registrations made during `Init` are attributed to the module and gated.
- **Trying to disable a dependency while dependents are still enabled:** disable dependent modules first; GoFastr never cascades disables silently.
- **Expecting queued jobs to be deleted on disable:** jobs defer until re-enabled; drain explicitly if you want to purge them.
- **Expecting OS process isolation from an in-process Module:** disabled modules still live in the Go binary; use [Process modules](process-modules.md) for crash/memory isolation.
