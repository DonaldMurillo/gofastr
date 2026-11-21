# On-disk logs and traces (`battery/telemetry`, experimental)

> **Experimental:** `battery/telemetry` is a zero-infrastructure observability **plugin** that writes structured request logs and OpenTelemetry traces to local daily files on disk — inspectable offline with `gofastr telemetry` even when the server is stopped. It does **not** configure OTLP or Prometheus export (see [Observability](observability.md) for external OTel/Prometheus export).

## Quickstart

Register `battery/telemetry` as a plugin with `app.RegisterPlugin(...)` alongside `framework.WithTracing()`:

```go
import (
	"time"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/framework"
)

app := framework.NewApp(
	framework.WithDB(db),
	framework.WithTracing(),
)

app.RegisterPlugin(telemetry.New(telemetry.Config{
	Retention:     14 * 24 * time.Hour, // default: 14 days
	SampleRate:    0.05,                // default: 5% of ordinary traces; failed/slow always kept
	SlowThreshold: 500 * time.Millisecond,
	Heartbeat:     os.Getenv("TELEMETRY_HEARTBEAT_URL"), // optional dead-man switch ping
}))
```

Default storage location: `telemetry/` under your app's local state directory (`$XDG_STATE_HOME/<app>` or `~/.local/state/<app>`, override with `Config.Dir`), bounded by `MaxDiskBytes` (default 5 GiB) and `MaxMemoryBytes` (default 32 MiB).

Inspect the files anytime with the CLI:

```bash
gofastr telemetry stats --since 24h
gofastr telemetry requests --failed --slow
gofastr telemetry logs --level error
gofastr telemetry request <trace-id>
```

## Alerts, MCP tools & external OTel

- **Built-in alerts:** checks `Config.Alerts` every 15s (default rule: 10 failed requests within 5 minutes) and delivers transitions to `Config.Notify` (disable with `NoAlerts: true`).
- **MCP debug tools:** set `EnableMCP: true` (auto-enabled on loopback under `gofastr dev`) to expose `telemetry_requests`, `telemetry_request`, and `telemetry_logs`, gated behind `framework.MCPRequireRole("admin")` by default (override with `Config.MCPGate`).
- **Combining with external OTLP export:** `battery/telemetry` installs its own OTel `TracerProvider` by default. If you also configure an external OTLP `TracerProvider` (e.g., Grafana/Honeycomb/Jaeger), set `ExternalTracerProvider: true` and attach the plugin's `SpanProcessor` to your provider.

## Common mistakes

- **Calling `app.RegisterBattery(telemetry.New(...))`:** `battery/telemetry` is a plugin — register it with `app.RegisterPlugin(...)`.
- **Expecting `battery/telemetry` to export to an OTLP collector or `/metrics`:** it writes local files on disk. Wire an OTel SDK exporter for OTLP and `framework.WithMetrics()` for Prometheus (see [Observability](observability.md)).
- **Registering `battery/telemetry` without `framework.WithTracing()`:** request spans won't be emitted unless tracing middleware is enabled.
- **Forgetting `ExternalTracerProvider: true` when using your own OTel provider:** without it, `battery/telemetry` overwrites the global OTel `TracerProvider`.
