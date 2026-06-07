# Observability (metrics & tracing)

GoFastr ships production-grade HTTP metrics and OpenTelemetry tracing
middleware. Both are **opt-in** so a minimal app stays minimal, but they
are one option each to turn on.

## Metrics (Prometheus)

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithMetrics(),
)
```

`WithMetrics()`:

- adds the metrics middleware to the default chain — it records per-route
  request counts, status classes, and latency histograms;
- mounts a Prometheus text-format endpoint at **`/metrics`**.

The `/metrics` endpoint is **unauthenticated by design** — scrape it from
inside your network / behind your ingress, and don't expose it publicly.
Point a Prometheus scrape config at `http://<host>/metrics`.

Custom chain (`WithoutDefaultMiddleware`)? Wire the primitives yourself:

```go
m := framework.NewMetrics()
r.Use(framework.MetricsMiddleware(m))
r.Get("/metrics", framework.MetricsHandler(m))
```

## Tracing (OpenTelemetry)

```go
app := framework.NewApp(framework.WithTracing())
```

`WithTracing()` runs every request inside a span carrying method, route,
and status attributes, and propagates incoming trace context. Spans
**no-op until you install a TracerProvider** — so it's safe to leave on in
all environments:

```go
import "go.opentelemetry.io/otel"
// e.g. an OTLP exporter wired into a TracerProvider:
otel.SetTracerProvider(tp)
```

Without a provider, the otel default no-op tracer is used and tracing adds
negligible overhead.

## Health & readiness

Separate from metrics: the framework auto-registers liveness/readiness
probes (and a DB readiness check when a DB is configured). See
[Health checks](health-checks.md).

## Deploying with observability

See [Deployment](deploy.md) for wiring `/metrics`, tracing exporters, and
graceful shutdown into a container/Kubernetes setup.
