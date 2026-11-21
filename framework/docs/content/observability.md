# Observability (tracing, metrics, and log correlation)

GoFastr integrates with OpenTelemetry and structured logging through a thin opt-in layer: `framework.WithTracing()`, `battery/telemetry`, and `battery/log`. If you don't enable tracing, overhead is zero and no telemetry exporter dependencies are pulled in.

## Quickstart

Enable request, SQL, cron, and job tracing on your app:

```go
app := framework.NewApp(
	framework.WithDB(db),
	framework.WithTracing(),
)
```

Configure an OpenTelemetry `TracerProvider` for external OTLP export (or register the [`battery/telemetry`](telemetry.md) plugin for zero-infra local files on disk). Every incoming HTTP request starts a root span named by method and matched route pattern (never raw paths with high-cardinality IDs).

## Trace-correlated logs

When `framework.WithTracing()` is enabled alongside [`battery/log`](log.md), every log line written with a traced request context (`logger.InfoContext(r.Context(), ...)`) automatically carries top-level `trace_id` and `span_id` fields — including `http.access` entries — so you can jump straight from a log line to its full distributed trace.

## Spans for SQL, jobs, and outbound calls

Passing `ctx` through GoFastr's primitives creates child spans under the active trace:

- **HTTP requests:** root server span with method, route template, status code, and error recording on 5xx.
- **SQL queries:** open your DB with `tracing.OpenDB` or wrap its driver with `tracing.WrapConnector` (`core/tracing`) to record operation, table/entity context, and latency (SQL literals/parameters are never put in span attributes to avoid leaking PII or secrets). Pair with `framework/slowquery` to log slow queries above a threshold.
- **Background jobs (`battery/queue`):** `Enqueue` injects trace context into job metadata and workers extract it as a linked/child span when executing, preserving end-to-end causality from HTTP request to worker execution.
- **Cron jobs (`framework.Scheduler`):** each firing starts a root trace named `cron <job name>` and marks the span failed on returned error or panic.
- **Outbound HTTP / webhooks:** propagate `traceparent` headers on outbound calls using `otelhttp` or your configured propagator.

## Health, metrics, and runtime stats

- **Probes:** `/healthz` (liveness) and `/readyz` (parallel dependency checks with 5s deadline; see [Health checks](health-checks.md)).
- **Prometheus metrics:** enable app HTTP RED metrics with `framework.WithMetrics()` (and expose `battery/log` drop/delivery counters via `logp.MetricsHandler()`) on an internal ops route. (`battery/telemetry` writes local log/trace files on disk and does not mount Prometheus or OTLP exporters.)
- **Dev & admin introspection:** `AppConfig.DebugEndpoints` mounts `/.debug/stats` and `/.debug/goroutineleak` (admin-gated), and `framework.WithMCPIntrospection()` lets connected agents inspect routes, readiness, and recent logs via `/mcp`.

## Common mistakes

- **Using `context.Background()` inside handlers or hooks:** breaks the trace chain so SQL queries, jobs, and logs lose their `trace_id`. Always thread `r.Context()`.
- **Using raw request URLs as span or metric names:** `/users/123` and `/users/456` create unbounded cardinality. Stick to matched route templates (`/users/{id}`).
- **Exposing `/metrics` or `/.debug/*` publicly without auth:** mount metrics on an internal listener or behind admin authentication.
- **Enabling tracing without calling `TracerProvider.Shutdown(ctx)` on exit:** buffered spans are lost on shutdown. Register shutdown via `app.OnStop` (handled automatically by `battery/telemetry`).
