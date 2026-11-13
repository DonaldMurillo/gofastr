# battery/telemetry

Experimental. Keeps an app's logs and traces in plain files on the
machine it runs on: one JSON-lines file per kind per UTC day, held to a
retention period, a disk cap and a memory cap. No collector, agent or
database runs beside the app.

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
- Every failed trace and every trace at or over `SlowThreshold` kept,
  plus `SampleRate` (default 5%) of the rest.
- Old days removed by `Retention` (14 days) and `MaxDiskBytes` (5 GiB).

**Do not:**
- Register it after the log plugin: lines logged before its `Init` are
  dropped (and counted in `Stats().LinesDropped`).
- Call `otel.SetTracerProvider` yourself as well. Set
  `Config.ExternalTracerProvider` and add `tel.SpanProcessor()` to your
  provider instead.
- Expect a UI or MCP tools for the files yet; read them with `grep` and
  `jq`.

Docs: `framework/docs/content/telemetry.md`.
