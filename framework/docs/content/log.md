# Server logs (`battery/log`)

The `battery/log` plugin wires structured JSON-line server logs into the
app. A single `*slog.Logger` fans out to one or more **sinks**; built-in
sinks cover three destinations:

1. A **default file** under the OS state dir (`$XDG_STATE_HOME/<app>/server.log`).
2. A **chosen file** at an explicit path, with size+count rotation.
3. A **webhook** URL that receives batched JSON.

The plugin also installs:

- An HTTP **access-log middleware** that emits `http.access` entries with
  method, path, status, response bytes, latency, request ID, and remote.
- A **panic-recovery middleware** that logs the stack with request
  context and returns 500.
- App **lifecycle events** (`app.start` / `app.stop`).

During `Init` the plugin calls `app.SetLogger(p.Logger())`. The framework's
default `middleware.Logging` (which uses `App.Logger` per request) and any
code calling `app.Logger()` directly then flow through this plugin's sinks
without rewiring `slog.Default` and without touching the stdlib `log`
package. The swap is scoped to one App — multiple Apps in the same
process don't collide.

## Quickstart

Zero-config — default file sink, all middleware on:

```go
app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))
app.RegisterPlugin(log.New(log.Config{}))
```

Custom file + Slack webhook:

```go
app.RegisterPlugin(log.New(log.Config{
    Level: slog.LevelInfo,
    Sinks: []log.Sink{
        log.MustFileSink("/var/log/myapp.log", log.FileOpts{
            MaxSize:    100 << 20, // 100 MiB
            MaxBackups: 5,
        }),
        log.WebhookSink("https://hooks.slack.com/services/...", log.WebhookOpts{
            BatchSize:     50,
            BatchInterval: time.Second,
            Headers:       map[string]string{"X-Source": "myapp"},
        }),
    },
}))
```

Pulling the logger out for app code:

```go
logp, err := framework.PluginGetAs[*log.Plugin](app.Plugins, "log")
if err != nil {
    return err // not registered, or registered under a different type
}
logp.Logger().Info("worker.tick", "queue", "ingest")
```

`PluginGetAs` does the lookup and the type assertion in one call and
returns an error (never a usable zero value) when the plugin is absent
or isn't a `*log.Plugin` — see [plugins](plugins.md) → Typed lookups.

## Real-time debugging via MCP

When `Config.EnableMCP` is set, the plugin installs an in-memory
RingSink (capacity `MCPRingSize`, default 1000) and registers four
tools on the App's MCP server. Connected agents (Claude Code, Cursor,
etc.) can call these to inspect the running app live:

| Tool            | Use                                                                                                  |
|-----------------|------------------------------------------------------------------------------------------------------|
| `log_recent`    | Last N entries from the ring, optional level filter.                                                 |
| `log_filter`    | Match by `msg`/`path`/`request_id`/`since_ts`/`until_ts`/`level`. `historical=true` tails the file sink for entries evicted from the ring. |
| `log_metrics`   | Current counter snapshot — same data as `Plugin.Metrics()`.                                          |
| `log_set_level` | Mutate the runtime log level (e.g. flip to DEBUG for an investigation, back to INFO afterwards).     |

Opt-in because the surface reveals a lot about the running app —
weigh the disclosure before enabling on a production MCP server
exposed to untrusted callers.

**In the dev loop the tools auto-enable.** Under `gofastr dev`
(`GOFASTR_DEV`; opt-out `GOFASTR_DEV_MCP=0`) the plugin turns on
`EnableMCP` and `AllowMCPMutation` regardless of Config — the local
dev `/mcp` is trusted, and an agent driving the dev loop needs the
debug tools without wiring. The explicit fields below remain the only
path for production processes:

```go
app.RegisterPlugin(log.New(log.Config{
    EnableMCP:   true,
    MCPRingSize: 5000,
}))
```

Pair with `framework.WithMCPIntrospection()` for parallel introspection
of the app's routes / plugins / batteries / config / readiness — see
`framework/mcp_introspection.go`.

## Metrics

The plugin exposes four counters covering the silent-loss scenarios
operators care about:

| Counter                                      | Meaning                                                            |
|----------------------------------------------|--------------------------------------------------------------------|
| `gofastr_log_post_stop_drops_total`          | Entries dropped because sinks were closed (post-Shutdown writes).  |
| `gofastr_log_sink_write_failures_total`      | Entries dropped because a sink's Write returned an error.          |
| `gofastr_log_webhook_dropped_total`          | Entries dropped from webhook queues under backpressure.            |
| `gofastr_log_webhook_gave_up_total`          | Webhook batches given up after exhausting MaxRetries.              |

Read them programmatically:

```go
logp, _ := framework.PluginGetAs[*log.Plugin](app.Plugins, "log")
m := logp.Metrics()
// m.PostStopDrops, m.SinkWriteFailures, m.WebhookDropped, m.WebhookGaveUp
```

Or expose them over HTTP in Prometheus text exposition format:

```go
logp, _ := framework.PluginGetAs[*log.Plugin](app.Plugins, "log")
app.Router().Handle("GET", "/metrics", logp.MetricsHandler())
```

The handler is stateless and safe to mount under any access-controlled
prefix you use for ops endpoints.

## Configuration

`log.Config` fields (all optional):

| Field                    | Default             | Notes                                                          |
|--------------------------|---------------------|----------------------------------------------------------------|
| `Level`                  | `slog.LevelInfo`    | Minimum level emitted by the fan-out handler.                  |
| `Sinks`                  | `[DefaultFileSink]` | If empty, resolves a per-app file under the OS state dir.      |
| `DisableLifecycleEvents` | `false`             | Set true to skip `app.start` / `app.stop` entries.             |
| `AddSource`              | `false`             | Adds `source` (file:line) attribute to every entry.            |

> **Zero-config = full plugin.** `Config{}` gives you everything: file
> sink, structured `http.access` + `http.panic` middleware, lifecycle
> events, and the App's logger swapped so framework middleware writes
> through the same sinks. The framework's router late-binds its
> middleware chain, so this plugin's contributions wrap routes
> registered before it loaded — no ordering footguns.

## Sinks

### `FileSink(path, opts)` and `DefaultFileSink(appName, opts)`

- Buffered writes flushed after every entry (durability over throughput
  — server logs are read live during debugging).
- Size-based rotation: when an entry would push the file past
  `MaxSize`, the active file is renamed to `<path>.1`, existing
  backups shift up (`.1` → `.2`, …), and anything past `MaxBackups`
  is removed.
- Default rotation: 100 MiB cap, 5 backups.

### `WebhookSink(url, opts)`

POSTs a JSON envelope `{"entries":[<entry>, <entry>, ...]}` to `url`.

- Batching: flush on `BatchSize` (default 50) **or** `BatchInterval`
  (default 1s), whichever first.
- Bounded queue (default 1000 entries). When full, drop-oldest — the
  webhook sink will **never** block the request path.
- Retry with exponential backoff on 5xx / transport errors, up to
  `MaxRetries` (default 3). 4xx is treated as a hard failure (the
  receiver said "no", retrying won't help).
- `Headers` lets you inject auth (`Authorization: Bearer …`) or routing
  hints. `Content-Type` is forced to `application/json`.
- `Close` flushes pending entries before returning (App.Stop awaits it).

### Custom sinks

Implement the `Sink` interface:

```go
type Sink interface {
    io.Closer
    Write(entry []byte) error
}
```

`entry` is one JSON object without a trailing newline. Sinks that
produce line-oriented output should append `'\n'` themselves.

## Log entry shape

Standard slog JSON: `time`, `level`, `msg`, plus per-entry attrs.

**HTTP access:**
```json
{"time":"...","level":"INFO","msg":"http.access","method":"GET","path":"/users/1","status":200,"bytes":421,"dur_ms":12,"request_id":"...","remote":"10.0.0.4"}
```

**Panic:**
```json
{"time":"...","level":"ERROR","msg":"http.panic","panic":"nil pointer dereference","method":"POST","path":"/things","request_id":"...","stack":"goroutine 18 [running]:\n..."}
```

**Lifecycle:**
```json
{"time":"...","level":"INFO","msg":"app.start","app":"myapp","go":"go1.26.0"}
{"time":"...","level":"INFO","msg":"app.stop","app":"myapp"}
```

## Common mistakes

- **Pointing a sink at a high-volume webhook synchronously.** The
  webhook sink is already async and bounded — never wrap it in your own
  blocking adapter, and don't set `QueueSize` so high that a downstream
  outage causes unbounded memory growth.
- **Setting `DisableReplaceDefault` and then wondering where slow-query
  logs went.** `framework/slowquery` writes to `slog.Default`. If you
  want those entries in your sinks, leave the default behavior on or
  pass your `Plugin.Logger()` to `slowquery.NewSlowQueryLogger`
  explicitly.
- **Treating the access middleware as a request log of last resort.**
  It logs once per request *after* the response is sent. Handlers that
  hang forever never emit an access entry — pair with a timeout
  middleware (`middleware.Timeout`) if you need bounded coverage.
- **Setting `MaxSize` very low to "test rotation in production."**
  Rotation occurs synchronously on the calling write; tiny caps turn
  every request into a rename storm.
- **Adding a webhook sink during a debugging session without auth
  headers.** The sink will POST your prod request log to whatever is
  on the other side. Always set `Headers["Authorization"]` (or the
  receiver's equivalent) before pointing the URL at a real endpoint.
- **Expecting one access entry per request.** With default middleware
  on, the framework emits a minimal `request` entry from
  `middleware.LoggingFn(app.Logger)` and this plugin emits a richer
  `http.access` entry from its own access middleware — both flow
  through the same sinks. Either pass `framework.WithoutDefaultMiddleware()`
  and wire the rest of the chain manually, or filter on `msg` in the
  consumer.
