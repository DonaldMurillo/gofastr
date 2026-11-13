# battery/telemetry

Experimental. Keeps an app's logs and traces in plain files on the
machine it runs on: one JSON-lines file per kind per UTC day, held to a
retention period, a disk cap and a memory cap. No collector, agent or
database runs beside the app.

**The goal** is most of what a hosted observability product gives a
small team, at a tenth of the code and of the machine: one process, no
new dependency, a 1 vCPU and 1 GB host. A change that needs a second
process, a database or a dependency has to say what it buys that the
files cannot.

**Use this when** the prompt mentions: traces, tracing on a small
server, "see what a request did", keep logs for N days, log retention,
disk cap for logs, observability without Datadog or a collector,
single-VPS or droplet deployment.

**Import:** `github.com/DonaldMurillo/gofastr/battery/telemetry`

**Shape:**
```go
tel := telemetry.New(telemetry.Config{})
app := framework.NewApp(
    framework.WithConfig(framework.AppConfig{Name: "shop"}),
    framework.WithTracing(), // request spans; without it, no request traces
)
app.RegisterPlugin(tel) // before the log plugin
app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{tel.LogSink()}}))
```

**What you get automatically:**
- `logs-YYYYMMDD.jsonl` and `traces-YYYYMMDD.jsonl` under
  `<state dir>/<app>/telemetry` (or `Config.Dir`).
- `trace_id` and `span_id` on every log line inside a traced request.
- Every failed request and every one at or over `SlowThreshold` kept,
  plus `SampleRate` (default 5%) of the rest. Framework assets and
  `Config.QuietPaths` are kept only when failed or slow.
- Old days removed by `Retention` (14 days) and `MaxDiskBytes` (5 GiB).
- Finished days sealed into `*.jsonl.gz` blocks with an `.idx` beside
  them, about a seventh of the size. Read them with `zcat` or
  `gofastr telemetry`; never write to the directory yourself.

**Screens, shell and agent access** (all read the same files):
```go
// in the admin's sidebar, behind the admin's gate
admin.New(admin.Config{Pages: telemetryadmin.Pages(tel, "/admin")})
// or on their own; Authorize is required
app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Path: "/telemetry", Authorize: isOperator}))
```
- Shell: `gofastr telemetry stats|logs|requests|issues|request <trace-id> --app <name>`.
- MCP: `telemetry.Config{EnableMCP: true}` adds `telemetry_stats`,
  `telemetry_issues`, `telemetry_requests`, `telemetry_request`, `telemetry_logs` (admin
  role by default). Asked what is breaking, call `telemetry_issues`
  first.

**Request counts:**
- Every request is counted by minute and route in `stats-YYYYMMDD.jsonl`,
  kept or not. Rates and percentiles come from there (the Overview
  screen, `gofastr telemetry stats`, `telemetry_stats`), never from the
  traces, which are a sample.
- Counts are about a minute behind, percentiles are right to within a
  bucket, and quiet paths are left out.

**Issues and alerts:**
- Failed requests are grouped by route, status and error (numbers and
  IDs set aside). The error is the first failed step's, else the first
  ERROR line logged with the request's context, else the status text.
  So log `slog.ErrorContext(ctx, "payment failed", "err", err)` before
  answering 500.
- One alert rule is on by default: ten failed requests in five minutes.
  It is logged as `telemetry.alert` and shown on Requests and Issues. To be
  told, set `Notify`:
```go
telemetry.Config{
    Alerts: []telemetry.AlertRule{{Name: "checkout failing", Kind: telemetry.AlertFailed, AtLeast: 5, Route: "POST /checkout"}},
    Notify: telemetry.WebhookNotifier(os.Getenv("ALERT_WEBHOOK_URL")), // Slack, Discord, Teams, Mattermost
}
```

**Do not:**
- Register it after the log plugin: lines logged before its `Init` are
  dropped (and counted in `Stats().LinesDropped`).
- Call `otel.SetTracerProvider` yourself as well. Set
  `Config.ExternalTracerProvider` and add `tel.SpanProcessor()` to your
  provider instead.
- Mount `telemetryui` with an `Authorize` that always returns true: the
  pages show request paths, client addresses and log lines.
- Draw your own request timeline: the screens use `ui.Waterfall`.

Docs: `framework/docs/content/telemetry.md`.
