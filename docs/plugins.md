# Plugins

A plugin is a small Go value that bundles related additions to an
`App` — routes, middleware, hooks, MCP tools — and registers them in
one call. Plugins exist so a feature can be shipped as a single
package and dropped into any GoFastr app without touching `main`.

## Minimum plugin

```go
type MyPlugin struct{}

func (MyPlugin) Name() string                  { return "my-plugin" }
func (MyPlugin) Init(app *framework.App) error { return nil }

app.RegisterPlugin(MyPlugin{}) // fluent App helper
app.InitPlugins()              // initialises all registered plugins
```

`app.RegisterPlugin` and `app.InitPlugins` delegate to
`app.Plugins` (`*PluginManager`); use the manager directly if you
need lookups or named access.

`Plugin` is the only mandatory interface. `Init` runs once per
`InitPlugins` / `InitAll` call — typically at startup.

## Optional capabilities

Implement any of these to add to the App:

```go
type HasRoutes interface {
    RegisterRoutes(r *router.Router)
}

type HasMiddleware interface {
    RegisterMiddleware(app *App)
}

type HasHooks interface {
    RegisterHooks(app *App)
}

type HasTools interface {
    RegisterTools(server *mcp.Server)
}
```

The plugin manager calls each capability's method in **registration
order**, only on plugins that implement that interface. A plugin may
implement zero, one, or all four.

## Full example

```go
type Webhooks struct {
    deliveries chan webhook
}

func (w *Webhooks) Name() string { return "webhooks" }

func (w *Webhooks) Init(app *framework.App) error {
    w.deliveries = make(chan webhook, 128)
    go w.worker(app)
    return nil
}

func (w *Webhooks) RegisterRoutes(r *router.Router) {
    r.Post("/__webhooks/test", w.testHandler)
}

func (w *Webhooks) RegisterHooks(app *framework.App) {
    for _, name := range app.Registry.Names() {
        app.HookRegistry(name).RegisterHook(framework.AfterCreate, w.fanOut)
    }
}

// In main:
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", …)
app.RegisterPlugin(&Webhooks{})
app.InitPlugins()
app.Plugins.RegisterRoutes(app.Router)
app.Plugins.RegisterHooks(app)
```

## Registration order

`Register` returns an error if a plugin with the same `Name()` is
already registered — names are the unique identifier across the
plugin set. The manager remembers insertion order; every
`Register*` call iterates in that order, so plugins added later see
state set up by plugins added earlier.

## Lookups

```go
p, err := app.Plugins.Get("webhooks")
all := app.Plugins.All()      // []Plugin, registration order
names := app.Plugins.Names()  // []string, registration order
```

`Get` returns an error rather than nil when the name is unknown so
callers can distinguish "not yet wired" from "wired but disabled".

## Where to wire plugins

Standard order in `main`:

```go
app := framework.NewApp(framework.WithDB(db))
// … entity registrations …
app.RegisterPlugin(pluginA)
app.RegisterPlugin(pluginB)
app.InitPlugins()
app.Plugins.RegisterMiddleware(app)
app.Plugins.RegisterRoutes(app.Router)
app.Plugins.RegisterHooks(app)
app.Plugins.RegisterTools(app.MCP)
log.Fatal(app.Start(":8080"))
```

You don't have to call every `Register*` — only the capabilities you
actually use. Calling them all is the safe default.

## Common mistakes

- **Registering routes before `InitAll`.** `Init` may set up state
  (channels, workers, DB connections) the route handler depends on.
  Always `InitAll` first.
- **Mutating shared state without coordination.** Plugins run in
  registration order at wiring time but in arbitrary goroutines at
  request time. Use locks or channels.
- **Same `Name()` from two plugins.** `Register` errors; check the
  return value.
- **Calling `RegisterRoutes` after the server has started.** The
  router accepts late registrations but middleware applied earlier
  in the default chain does not wrap them automatically.
