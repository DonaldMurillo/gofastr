# Plugin platform (sandboxed UI plugins)

GoFastr keeps its core browser runtime tiny (~14 KB gzip). Heavy client-side features — rich text editors, diagram canvases, code editors, maps, or third-party UI extensions — live outside the core as sandboxed UI plugins running inside opaque-origin `<iframe>` containers over a versioned `postMessage` bridge.

## Why sandboxed iframes

- **Zero runtime bloat:** core pages never pay for megabytes of JS used on one screen.
- **Hard security boundary:** an opaque-origin sandboxed iframe (`sandbox="allow-scripts"`) has no access to your app's cookies, `localStorage`, DOM, or CSRF tokens, and your main app's strict `default-src 'self'` CSP stays intact.
- **Typed host bridge:** the plugin talks to your GoFastr host only through explicitly declared capabilities and actions over `postMessage`, validated on both sides.

## Mounting a plugin

Official plugins live in the sibling `gofastr-plugins` repository as ordinary Go modules (`go get` + `app.RegisterPlugin(...)` — no runtime plugin marketplace downloads). Register the plugin on your `framework.App` and render its sandboxed frame marker in a screen using `framework/pluginhost`:

```go
// Register the plugin at startup:
app.RegisterPlugin(myplugin.New(myplugin.Config{}))

// Render the sandboxed plugin mount inside a screen:
pluginhost.MountMarker(pluginhost.MountConfig{
	Plugin:    "myplugin",
	DocID:     "doc-42",
	MinHeight: "420px",
})
```

Every plugin mounted through `framework/pluginhost` runs inside an opaque-origin sandboxed iframe (`sandbox="allow-scripts"`). `pluginhost` **rejects `allow-same-origin` in manifests** — it is not a manifest opt-out from opaque isolation.

## Building a custom UI plugin

1. **Bundle static assets** into your Go package with `//go:embed` and serve them under a dedicated plugin prefix with a plugin-scoped CSP.
2. **Mount the iframe** through the framework's plugin container component, passing only initial props and allowed action names.
3. **Handle host actions in Go:** when the iframe posts an action message, the host runtime routes it through an authenticated GoFastr endpoint or island RPC so auth, owner scoping, and validation run on the server.
4. **Keep state durable:** persist edits on the server (or `localdb`), never solely inside transient iframe memory.

This is also the sanctioned boundary for runtime generative UI plugins (see [Generative UI](generative-ui.md)).

## Common mistakes

- **Adding `allow-same-origin` alongside `allow-scripts` on an untrusted plugin iframe:** lets the iframe escape its own sandbox by removing the `sandbox` attribute and reading your app's cookies/DOM.
- **Trusting `postMessage` payloads without checking `event.source`, `event.origin`, and schema:** always validate message source and structure before acting on host commands.
- **Pulling heavy npm bundles directly into the core `uihost` page:** defeats the runtime size budget and CSP. Isolate heavy JS widgets inside a plugin iframe.
- **Letting a plugin call arbitrary backend endpoints:** expose only the explicit actions the plugin needs.
