# battery/log

Structured JSON server logs, panic-recovery middleware, HTTP access
logging, lifecycle events, and an optional MCP surface so a connected
agent can debug the running app via `log_recent` / `log_filter` /
`log_metrics` tools.

**Use this when** the prompt mentions: request log, access log, server
log, slog, structured logging, "why did that 500", panic recovery,
"log to a file", "ship logs to a webhook", debug the running server.

**Import:** `github.com/DonaldMurillo/gofastr/battery/log`

**Shape (zero config — file sink at OS state dir):**
```go
app.RegisterPlugin(log.New(log.Config{}))
```

**Shape (explicit sinks + MCP debug):**
```go
app.RegisterPlugin(log.New(log.Config{
    Level: slog.LevelInfo,
    Sinks: []log.Sink{
        log.MustFileSink("/var/log/app.log", log.FileOpts{
            MaxSize:    100 << 20, // 100 MiB
            MaxBackups: 5,
        }),
        log.WebhookSink("https://hooks.example.com/x", log.WebhookOpts{}),
    },
    EnableMCP:        true, // adds log_recent / log_filter / log_metrics
    AllowMCPMutation: false, // gate log_set_level — flipping log level
                              // remotely is a DoS-via-disk-fill risk.
}))
```

**What you get automatically:**
- HTTP access logging on every request (one JSON line per response).
- Panic-recovery middleware that converts panics into 500s and logs
  the stack trace through the same sinks.
- App lifecycle events (`app.start` / `app.stop`).
- Sink fan-out: file + webhook + any custom Sink, ordered by
  `Sinks` slice on shutdown so fast/local sinks outlive slow/remote
  ones.
- Logger swapped on `App.Logger` so framework middleware (Logging,
  slowquery, etc.) writes through the same surface.

**Don't reinvent** a `LoggingMiddleware` wrapper, a `defer recover()`
panic catcher, or per-handler `log.Println` calls. `log.New(Config{})`
gives you all three plus the MCP introspection surface for free.

**Security:** leave `AllowMCPMutation: false` on any host where `/mcp`
is reachable by untrusted callers. `log_set_level` lets a caller flip
the global log level — combined with a verbose access pattern, that's
both a DoS-via-disk-fill and an info-disclosure primitive.
