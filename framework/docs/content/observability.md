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

### What is recorded

| Metric | Type | Labels |
|---|---|---|
| `http_requests_total` | counter | `method`, `route`, `status` |
| `http_request_duration_ms` | histogram | `route` |

The `route` label uses `r.Pattern` (the Go 1.22+ ServeMux matched
pattern, e.g. `GET /api/v1/posts/{id}`), which has bounded cardinality.
Unknown HTTP methods are collapsed to `other` to prevent label-cardinality
attacks from attacker-controlled `X-HTTP-Method-Override` values.

Histogram bucket boundaries (milliseconds): 1, 5, 10, 50, 100, 250, 500,
1000, +Inf.

### Custom middleware chain

If you use `WithoutDefaultMiddleware()` to build your own chain, wire the
primitives manually — `WithMetrics` panics when combined with
`WithoutDefaultMiddleware`:

```go
m := framework.NewMetrics()
r.Use(framework.MetricsMiddleware(m))
r.Get("/metrics", framework.MetricsHandler(m))
```

`framework.NewMetrics`, `framework.MetricsMiddleware`, and
`framework.MetricsHandler` are re-exported from `core/middleware` for
convenience.

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

### Span shape

Each span is named `HTTP {method} {route}` (e.g. `HTTP GET /api/posts/{id}`)
and carries these attributes:

| Attribute | Value |
|---|---|
| `http.method` | request method |
| `http.route` | matched route pattern (or `"unmatched"`) |
| `http.status_code` | final response status |
| `http.target` | request URL path |

Responses with status `>= 500` set the span status to `codes.Error`.
Incoming W3C `traceparent` / `tracestate` headers are extracted so
upstream spans chain correctly.

### Reading the current span in a handler

```go
import "go.opentelemetry.io/otel/trace"

span := trace.SpanFromContext(r.Context())
span.AddEvent("queued background job")
```

`core/middleware` also exposes a convenience wrapper:

```go
import "github.com/DonaldMurillo/gofastr/core/middleware"

span := middleware.SpanFromRequest(r) // returns trace.Span
```

Both return a no-op span when `WithTracing()` is not installed, so they
are safe to call unconditionally.

### Custom middleware chain and tracing

Like `WithMetrics`, `WithTracing` panics when combined with
`WithoutDefaultMiddleware`. Wire it manually via the re-exported
`framework.Tracing`:

```go
r.Use(framework.Tracing())
```

## Health & readiness

Separate from metrics: the framework auto-registers liveness/readiness
probes (and a DB readiness check when a DB is configured). See
[Health checks](health-checks.md).

## Common mistakes

- **Exposing `/metrics` without network-level protection.** The endpoint
  is unauthenticated. Mount it on an internal listener or put it behind
  your ingress allow-list. A public `/metrics` leaks route names and
  traffic patterns to anyone who can reach the host.
- **Using `WithMetrics()` with `WithoutDefaultMiddleware()`.** This panics
  at startup. Use the `framework.MetricsMiddleware` + `framework.MetricsHandler`
  primitives directly and wire them into your custom chain.
- **Not installing a TracerProvider but expecting traces to appear.**
  `WithTracing()` is a no-op without a configured provider. Spans are
  created but dropped immediately. You must call `otel.SetTracerProvider(tp)`
  with an exporter-backed provider before traces land anywhere.
- **Adding `WithTracing()` after `WithoutDefaultMiddleware()`.** Same
  problem as `WithMetrics` — it panics. Use `framework.Tracing()` in your
  own chain.

## Deploying with observability

See [Deployment](deploy.md) for wiring `/metrics`, tracing exporters, and
graceful shutdown into a container/Kubernetes setup.
