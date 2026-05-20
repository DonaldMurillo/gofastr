# Plugins

A plugin is a small Go value that bundles related additions to an `App` —
routes, middleware, hooks, MCP tools, logger swap, anything an App can
host — and registers them in **one** call. Plugins exist so a feature
can be shipped as a single package and dropped into any GoFastr app
without touching `main`.

## Minimum plugin

```go
type MyPlugin struct{}

func (MyPlugin) Name() string                  { return "my-plugin" }
func (MyPlugin) Init(app *framework.App) error { return nil }

app.RegisterPlugin(MyPlugin{})
```

The `Init` method runs at `App.Start` (or `App.InitPlugins`) and is the
plugin's single integration point. From inside `Init` a plugin does
everything it needs by calling into the `App`:

```go
func (MyPlugin) Init(app *framework.App) error {
    app.Router.Get("/hello", helloHandler)
    app.Use(myMiddleware)
    app.MCP.RegisterTool("my_tool", "Does the thing", schema, handler)
    app.HookRegistry("users").RegisterHook(framework.AfterCreate, sendWelcomeEmail)
    return nil
}
```

There are no optional interfaces — no `HasRoutes`, `HasMiddleware`,
`HasHooks`, `HasTools`. The router resolves its middleware chain
late-bound per request, so middleware added from `Init` wraps routes
registered earlier (e.g. by `Mount`). There is no ordering footgun to
dodge.

## Init lifecycle

- `RegisterPlugin` queues the plugin; it does NOT init.
- `InitPlugins` runs every queued plugin's `Init` in registration
  order, then every battery's `Init` in dependency-resolved order.
- `App.Start` calls `InitPlugins` then binds the HTTP listener.
- Tests can call `InitPlugins` manually before driving the router
  in-memory; the call is idempotent. `framework.TestHarness(t, app)`
  invokes it automatically.

`RegisterPlugin` after `InitPlugins` panics — the new plugin would
never get to run, which would silently break the caller's expectations.

A plugin's `Init` panic (or an error return) is caught and attributed
to the plugin by name in the resulting error. Already-initialised
plugins are skipped on retry so a partial-init-then-fix retry doesn't
double-register routes / middleware.

## Lifecycle hooks

Plugins that need to run code at App start / stop call the App's hook
APIs from `Init`:

```go
func (MyPlugin) Init(app *framework.App) error {
    app.OnStart(func(ctx context.Context) error {
        // ... start a background worker ...
        return nil
    })
    app.OnStop(func() error {
        // ... stop the worker ...
        return nil
    })
    return nil
}
```

For shutdown hooks that must run AFTER every other plugin's OnStop —
e.g. the logging plugin's "close every sink" hook — use `OnStopFirst`:
it prepends to the hook list, so the reverse-order Shutdown iteration
runs it last. `battery/log` uses this so log sinks are still open while
other shutdown code emits its final entries.

## Plugin vs Battery

`Plugin` and `Battery` share the same single-Init contract. The
difference:

- `Battery` accepts dependency declarations at registration time
  (`app.RegisterBattery(b, "needs-this")`) and runs in dependency-resolved
  order. Use Battery when one module must initialise before another.
- `Battery` can also implement `BatteryLifecycle` (OnStart / OnStop)
  for structured per-battery start/stop, separate from the App-wide
  `OnStart` / `OnStop` hooks.

For everything else, prefer `Plugin`.

## Full example

```go
type Webhooks struct {
    deliveries chan webhook
}

func (w *Webhooks) Name() string { return "webhooks" }

func (w *Webhooks) Init(app *framework.App) error {
    w.deliveries = make(chan webhook, 128)
    app.OnStart(w.start)
    app.OnStop(w.stop)

    app.Router.Post("/__webhooks/test", w.testHandler)

    for _, name := range app.Registry.Names() {
        app.HookRegistry(name).RegisterHook(framework.AfterCreate, w.fanOut)
    }
    return nil
}

// In main:
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", entityConfig)
app.RegisterPlugin(&Webhooks{})
log.Fatal(app.Start(":8080"))
```

## Lookups

```go
p, err := app.Plugins.Get("webhooks")
all := app.Plugins.All()      // []Plugin, registration order
names := app.Plugins.Names()  // []string, registration order
```

`Get` returns an error rather than nil when the name is unknown so
callers can distinguish "not yet wired" from "wired but disabled".

## Common mistakes

- **Registering after `InitPlugins`**. The plugin queue is frozen at
  init time. The framework panics with an explicit message; register
  plugins before `Start` or before any manual `InitPlugins` call.
- **Returning early from `Init` after partial side effects**. Side
  effects (already-registered routes) don't roll back, but per-module
  init tracking ensures a retry won't re-run a successful plugin.
  Either return the error early before any side effects, or finish
  the wiring before returning.
- **Calling `app.Use` from `OnStart`**. The router IS race-safe for
  concurrent Use+ServeHTTP, but middleware added there only wraps
  requests that arrive AFTER the Use returns — operationally
  surprising. Stay in `Init` for chain composition.
- **Naming a plugin with whitespace / control chars / very long
  strings**. `RegisterPlugin` rejects these with a clear error; pick
  a stable, human-readable name (`"auth"`, `"telemetry"`, `"log"`).
