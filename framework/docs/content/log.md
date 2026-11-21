# Server logs (`battery/log`)

`battery/log` wires structured JSON-line `slog` logging, access-log and panic-recovery middleware, lifecycle events (`app.start` / `app.stop`), optional HTTP error forwarding, and dev/admin MCP log-inspection tools into your `framework.App`.

## Quickstart

Zero-config (writes JSON lines to the OS state dir `$XDG_STATE_HOME/<app>/server.log` and calls `app.SetLogger` scoped to this app):

```go
app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "myapp"}))
app.RegisterPlugin(log.New(log.Config{}))
```

Container stdout JSON stream (recommended for Docker / Kubernetes / Cloud Run):

```go
app.RegisterPlugin(log.New(log.Config{
	Level: slog.LevelInfo,
	Sinks: []log.Sink{
		log.JSONSink(log.JSONOpts{}), // writes to os.Stdout
	},
}))
```

Other built-in sinks:
- `log.MustFileSink(path, log.FileOpts{MaxSize: 100 << 20, MaxBackups: 5})` — size-rotated file (`0600` default).
- `log.WebhookSink(url, log.WebhookOpts{BatchSize: 50, BatchInterval: time.Second, Headers: ...})` — async bounded queue (drop-oldest under backpressure, retries 5xx, drops 3xx/4xx).
- Custom `Sink` (`io.Closer` + `Write(entry []byte) error`):

<!-- gofastr:compile
import "io"
-->
```go
type Sink interface {
	io.Closer
	Write(entry []byte) error
}
```

Pull the logger anywhere in app code via `framework.PluginGetAs[*log.Plugin](app.Plugins, "log")`. When `framework.WithTracing()` is enabled, passing `r.Context()` (`logger.InfoContext(r.Context(), ...)`) stamps top-level `trace_id` and `span_id` on every entry.

## MCP debugging tools and metrics

- **MCP tools:** under `gofastr dev`, `log_recent`, `log_filter`, `log_metrics`, and `log_set_level` auto-enable for local agents (`GOFASTR_DEV_MCP=0` opts out). In production, opt in with `Config.EnableMCP: true` (and `AllowMCPMutation: true` for `log_set_level`), which requires the `admin` role (`role:admin` scope on API tokens) or a custom `Config.MCPGate`.
- **Metrics:** read drop/failure counters (`gofastr_log_post_stop_drops_total`, `gofastr_log_sink_write_failures_total`, `gofastr_log_webhook_dropped_total`, `gofastr_log_webhook_gave_up_total`) via `logp.Metrics()` or expose Prometheus text via `logp.MetricsHandler()`.
- **Error reporting:** set `Config.ErrorReporter` to `log.NewHTTPErrorReporter(url, opts)` to forward recovered `http.panic` events (and manual `logp.Reporter().Report(...)` calls) to an external collector with control bytes scrubbed.

## Common mistakes

- **Setting `TrustForwardedFor` without understanding hop order:** `forwarded_for` joins all `X-Forwarded-For` lines (capped at 2 KiB), and client IP selection picks the rightmost trusted hop, never the spoofable leftmost address.
- **Setting a tiny `MaxSize` in production:** synchronous rotation on every few requests turns into a rename storm.
- **Configuring a `WebhookSink` without auth headers:** always pass `Headers["Authorization"]` when shipping logs over HTTP.
- **Expecting access logs for handlers that hang forever:** access logs emit after the response completes; keep request timeouts enabled.
