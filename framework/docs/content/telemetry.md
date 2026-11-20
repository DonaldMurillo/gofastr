# OpenTelemetry export (`battery/telemetry`)

`battery/telemetry` wires OpenTelemetry OTLP tracing and Prometheus/OTLP metrics into a `framework.App` with clean shutdown flushing and zero overhead when not registered.

## Quickstart

```go
import (
	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/framework"
)

app := framework.NewApp(
	framework.WithDB(db),
	framework.WithTracing(),
)

app.RegisterBattery(telemetry.New(telemetry.Config{
	ServiceName: "billing-api",
	Endpoint:    os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
}))
```

What `battery/telemetry` configures automatically:
- Initializes a batched OTLP exporter and registers the `TracerProvider` (honoring standard `OTEL_*` environment variables when fields are left blank).
- Records HTTP server spans and latency/status histograms keyed by matched route pattern (never raw high-cardinality URLs).
- Integrates with `framework.WithTracing()` so SQL queries, `battery/queue` jobs, `framework.Scheduler` cron runs, and `battery/log` `trace_id`/`span_id` fields all share one trace hierarchy (see [Observability](observability.md)).
- Flushes buffered spans and metrics during `App.Shutdown` via `BatteryLifecycle.OnStop`.

## Sampling and security

- **Sampling:** configure a head-sampling ratio for high-traffic production services rather than exporting 100% of traces blindly.
- **No sensitive payloads in spans:** authorization headers, cookies, request bodies, and SQL parameter values are never attached as span attributes.
- **TLS & headers:** pass exporter auth headers (`Authorization`, API keys) via `OTEL_EXPORTER_OTLP_HEADERS` or your OTLP exporter options over HTTPS; only allow insecure plaintext OTLP to local collectors (`localhost` / sidecars).

## Metrics endpoint

Expose Prometheus metrics on an internal ops route or listener rather than the public internet. Keep label cardinality bounded — never put user IDs, email addresses, or raw URL paths into metric labels.

## Common mistakes

- **Registering `battery/telemetry` without `framework.WithTracing()`:** the exporter initializes, but framework HTTP/SQL/cron instrumentation stays off. Enable both.
- **Putting high-cardinality values (user ID, raw path) in custom span or metric attributes:** explodes metric series cost in Prometheus/OTLP backends.
- **Forgetting to flush on shutdown:** if you wire an OTel `TracerProvider` manually instead of using `battery/telemetry`, always call `Shutdown(ctx)` inside `app.OnStop`.
- **Exposing `/metrics` publicly without auth:** protect metrics behind an internal port or admin middleware.
