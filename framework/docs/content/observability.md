# Observability (metrics & tracing)

GoFastr includes HTTP metrics, subsystem metrics (DB pool, queues, outbox,
webhooks, slow queries), an error-reporting seam, and OpenTelemetry tracing
middleware. HTTP metrics are opt-in via `WithMetrics()`; subsystem metrics
appear on the same `/metrics` endpoint automatically as the components that
produce them are wired. Tracing is opt-in via `WithTracing()`.

## Metrics (Prometheus)

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "database/sql"
var db *sql.DB
-->
```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithMetrics(),
)
```

`WithMetrics()`:

- adds the metrics middleware to the default chain; it records per-route
  request counts, status classes, and latency histograms;
- mounts a Prometheus text-format endpoint at **`/metrics`**.

The `/metrics` endpoint is **unauthenticated by design**. Scrape it from
inside the cluster and keep it off the public network; see the
[deploy checklist](deploy.md).

### What is recorded

HTTP metrics (always present with `WithMetrics()`):

| Metric | Type | Labels |
|---|---|---|
| `http_requests_total` | counter | `method`, `route`, `status` |
| `http_request_duration_ms` | histogram | `route` |

A request whose handler panicked is counted too: under `500` (the status
`Recovery` sends), or under the status already sent when the response had
started before the panic. With `Recovery` outside, the status is read off
its writer, which sees what reached the client, so a status `Compress`
held back is not counted as sent. A `Flush` before any `WriteHeader` counts as the
`200` it sends.

Subsystem metrics appear on the same `/metrics` endpoint as the components
that produce them are wired:

| Metric | Type | Labels | Source | Appears when |
|---|---|---|---|---|
| `db_pool_open_connections` | gauge | — | `sql.DBStats` | `WithDB` + `WithMetrics` |
| `db_pool_in_use_connections` | gauge | — | `sql.DBStats` | `WithDB` + `WithMetrics` |
| `db_pool_idle_connections` | gauge | — | `sql.DBStats` | `WithDB` + `WithMetrics` |
| `db_pool_wait_count_total` | counter | — | `sql.DBStats` | `WithDB` + `WithMetrics` |
| `db_pool_wait_duration_seconds_total` | counter | — | `sql.DBStats` | `WithDB` + `WithMetrics` |
| `outbox_pending` | gauge | `consumer` | transactional outbox | `WithOutbox` + `WithMetrics` |
| `outbox_dead_letter_total` | counter | `consumer` | transactional outbox | `WithOutbox` + `WithMetrics` |
| `queue_depth` | gauge | `lane` | `battery/queue` | queue registered (see below) |
| `queue_dead_letter_total` | counter | `lane` | `battery/queue` | queue registered (see below) |
| `webhook_deliveries_total` | counter | — | `battery/webhook` | manager registered (see below) |
| `webhook_failures_total` | counter | — | `battery/webhook` | manager registered (see below) |
| `slow_queries_total` | counter | — | `framework/slowquery` | logger registered (see below) |

The DB-pool and outbox metrics are zero-config: any code path that takes the
app's metrics handle (a battery's `Init`, or an explicit `app.Metrics()`
call) attaches them. The DB pool is sampled live at scrape time from
`sql.DBStats`; the outbox runs one `GROUP BY` per consumer.

The `route` label on HTTP metrics uses `r.Pattern` (the Go 1.22+ ServeMux
matched pattern, e.g. `GET /api/v1/posts/{id}`), which has bounded
cardinality. Unknown HTTP methods are collapsed to `other` to prevent
label-cardinality attacks from attacker-controlled
`X-HTTP-Method-Override` values. Histogram bucket boundaries (milliseconds):
1, 5, 10, 50, 100, 250, 500, 1000, +Inf.

### How subsystem metrics register

Every subsystem writes to the **same** `*middleware.Metrics` store that serves
`/metrics`, via a *collector*: a `func(io.Writer)` that emits Prometheus
text lines (HELP/TYPE/samples) and runs once per scrape, in name order, after
the HTTP metrics. Batteries and libraries reach the store through the App:

```go
m := app.Metrics()          // nil when WithMetrics() was not used
if m != nil {
    m.RegisterCollector("my_thing", func(w io.Writer) {
        fmt.Fprintln(w, "# HELP my_thing_total ...")
        fmt.Fprintln(w, "# TYPE my_thing_total counter")
        fmt.Fprintf(w, "my_thing_total %d\n", n)
    })
}
```

The framework-owned surfaces (`db_pool`, `outbox`) register themselves this
way inside `app.Metrics()`. Library-owned surfaces register when you wire
them, because the framework does not construct them for you:

```go
// battery/queue: DBQueue/MemoryQueue implement the Browsable interface.
app.Metrics().RegisterCollector("queue:ingest", queue.MetricsCollector(q, "ingest"))

// battery/webhook: the Manager counts deliveries and failures.
app.Metrics().RegisterCollector("webhook", mgr.MetricsCollector())

// framework/slowquery: a SlowQueryLogger you wrapped around *sql.DB.
app.Metrics().RegisterCollector("slow_queries", slowquery.MetricsCollector(slowDB))
```

A `SlowQueryLogger` line logs the statement's shape, masked by the same
code as a trace span's `db.query.text`: every string and number literal
(`'…'`, `E'…'`, `$tag$…$tag$`, `42`) printed as `?`, comments dropped,
whitespace collapsed, cut at 240 bytes, and bound arguments as a count
only. A value spelled inline, such as `ALTER ROLE … PASSWORD '…'`, never
reaches the log. Where a literal ends depends on the engine, so name it:

```go
slowDB := slowquery.NewSlowQueryLogger(db, 200*time.Millisecond, logger,
	slowquery.WithDialect("postgres")) // or "sqlite3", "mysql"
```

Without `WithDialect` (and with a name it does not know) the line masks
under every engine's rules at once: `"double-quoted"` text prints as `?`
(SQLite and MySQL can read it as a string), `#` starts a comment (MySQL),
block comments nest (PostgreSQL), and a string whose end depends on
whether a backslash escapes hides the rest of the statement. Naming
PostgreSQL keeps its `"Quoted"` identifiers.

`RegisterCollector` replaces by name, so re-registration never duplicates.
Collectors must be cheap and must not panic; a transient DB error should emit
nothing for that scrape rather than break `/metrics`.

### Custom middleware chain

If you use `WithoutDefaultMiddleware()` to build your own chain, wire the
primitives manually; `WithMetrics` panics when combined with
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

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
app := framework.NewApp(framework.WithTracing())
```

`WithTracing()` runs every request inside a span carrying method, route,
and status attributes, and propagates incoming trace context. Spans
**no-op until you install a TracerProvider**, so it's safe to leave on in
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

Responses with status `>= 500` set the span status to `codes.Error`. A
panicking handler's span ends with `codes.Error` and the status the client
got (`500`, or the one already sent), and the SDK records the panic as the
span's `exception` event.

Incoming trace context is extracted with the global propagator you set
with `otel.SetTextMapPropagator` (B3, Jaeger, a composite), the same one
that writes the response headers, so upstream spans in that format chain
correctly. When that propagator finds no upstream span on a request, the
W3C `traceparent` / `tracestate` headers are read as well, and W3C
`baggage` too when the propagator brought no baggage of its own. With no
propagator set (the otel no-op default), W3C `traceparent` / `tracestate`
and `baggage` are read.

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

### Spans for SQL, jobs and outbound calls

A request span shows how long the request took, not where the time went.
`core/tracing` adds child spans for the calls the app makes outward.

Open the database with `tracing.OpenDB` in place of `sql.Open`:

<!-- gofastr:compile
stmt: _ = app
-->
```go
import (
    "log"

    "github.com/DonaldMurillo/gofastr/core/tracing"
    "github.com/DonaldMurillo/gofastr/framework"
    _ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

db, err := tracing.OpenDB("sqlite3", "file:app.db")
if err != nil {
    log.Fatal(err)
}
app := framework.NewApp(framework.WithDB(db), framework.WithTracing())
```

Every Exec and Query, prepared or not, then gets a client span named by
the statement's first keyword (`SELECT`, `INSERT`), and each
transaction's `BEGIN`, `COMMIT` and `ROLLBACK` gets one too. A driver you
reach through its own connector (pgx's `stdlib.GetConnector`) wraps with
`tracing.WrapConnector(c, "postgresql")` and `sql.OpenDB`. A Query span
covers the driver call, not the reading of its rows.

For outbound HTTP, wrap the client's transport:

<!-- gofastr:compile
stmt: _ = client
-->
```go
import (
    "net/http"

    "github.com/DonaldMurillo/gofastr/core/tracing"
)

client := &http.Client{Transport: tracing.Transport(nil)}
```

Each request gets a client span named `HTTP <method>`, ending when the
response headers arrive. A transport error or a 4xx or 5xx status marks
it an error. No `traceparent` header goes out unless you pass
`tracing.PropagateTraceContext()`: the header hands your trace and span
ids to whatever host the request reaches, so turn it on only for calls
between your own services. The webhook battery's default delivery client
already uses this transport, without propagation.

Queue jobs need no wiring. Every handler call on the memory and database
queues runs under a consumer span named `process <job type>`, which the
handler's `ctx` carries, so SQL and HTTP spans inside the handler nest
under it. A job span is a root span: a job does not carry the trace
context of the request that enqueued it.

| Span | Attributes |
|---|---|
| SQL | `db.system.name`, `db.query.text` |
| Outbound HTTP | `http.request.method`, `url.full`, `server.address`, `http.response.status_code` |
| Queue job | `messaging.system` (`gofastr`), `messaging.destination.name` (job type), `messaging.message.id`, `gofastr.queue.lane`, `gofastr.queue.attempts` |

None of these spans carry data values. `db.query.text` has every string
and number literal replaced by `?` (placeholders such as `$1` stay). On
SQLite and MySQL, `"double-quoted"` text is replaced too: MySQL reads it
as a string, and SQLite does when no column has that name, so a quoted
identifier shows as `?` there. Comments are dropped (PostgreSQL's nest;
MySQL's `#` counts). For a `db.system.name` other than `postgresql`,
`sqlite` and `mysql`, every one of those rules applies at once, and a
string whose end depends on whether a backslash escapes hides the rest
of the statement. `url.full` drops the query string, fragment and user info. A failure sets
the span status to `codes.Error` with `error.type` holding the error's Go
type (or the HTTP status code, or `panic`), never its message, since
driver and handler errors routinely quote row values.

### Exporting traces to a collector (OTLP)

`WithTracing()` creates spans but drops them until you install an exporter.
The framework does not depend on an OTLP exporter (so your app stays free of
the OpenTelemetry exporter modules until you want them). Add the exporter to
**your own app** and wire it once at startup:

<!-- gofastr:compile
-->
```go
import (
    "context"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
    "go.opentelemetry.io/otel/propagation"
    sdkresource "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// go get go.opentelemetry.io/otel/sdk/trace \
//        go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp

func installTracer(ctx context.Context, endpoint string) (func(context.Context) error, error) {
    exporter, err := otlptracehttp.New(ctx,
        otlptracehttp.WithEndpoint(endpoint), // e.g. "localhost:4318"
        otlptracehttp.WithInsecure(),          // drop inside the cluster / behind TLS
    )
    if err != nil {
        return nil, err
    }
    res, err := sdkresource.New(ctx, sdkresource.WithAttributes(semconv.ServiceName("myapp")))
    if err != nil {
        return nil, err
    }
    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter), // async: flush on shutdown
        sdktrace.WithResource(res),
    )
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(propagation.TraceContext{}) // W3C traceparent
    return tp.Shutdown, nil // call on app shutdown
}
```

For gRPC collectors, swap `otlptracehttp` for
`go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc` and
`otlptracegrpc.New(...)`. Call the returned `Shutdown(ctx)` from your app's
graceful-shutdown path (`app.OnStop`) so the batcher flushes pending spans
before the process exits.

## Health & readiness

Separate from metrics: the framework auto-registers liveness/readiness
probes (and a DB readiness check when a DB is configured). See
[Health checks](health-checks.md).

## Goroutine leak detection

The Go runtime proves some goroutines leaked: blocked forever on a channel
or sync primitive that no runnable goroutine can still reach. The framework
exposes that verdict three ways, all backed by the runtime's `goroutineleak`
profile (a forced GC cycle runs first, so the report reflects now):

- `/.debug/stats` includes a `goroutineLeaks` count next to `goroutines`.
- `/.debug/goroutineleak` returns the leak report with stacks (text).
  Gated the same way as `/.debug/stats`: no user is a 401, and a signed-in
  user without the `admin` role is a 403. The role is read from the user's
  `GetRoles() []string` (battery/auth's `User` has it) or from the roles
  access middleware put on the context. A scope-restricted caller (an API
  token) counts the role only with the `role:admin` scope
  (`access.RoleScope`; `"role:*"` and `"*:*"` cover it), since a token
  carries its owner's roles. A request carrying an embed grant
  is always refused, and so is a caller an access `Decider` denies on
  `access.RoleGateCapability` (the zero `Ref`, as for a role gate). To use
  a different rule, pass
  `framework.WithDebugAuthorize(func(ctx context.Context) bool)`; it runs
  after the user check and replaces the role test, not the Decider or the
  scope check: a scope-restricted caller still needs `role:admin` before
  your function runs, since a token carries its owner's identity.
- The `app_goroutine_leaks` MCP tool (with `WithMCPIntrospection()`)
  returns `{count, stacks, truncated}` so a connected agent can check the
  running app without shell access.

Zero is healthy. A count that grows across calls means a worker, stream,
or handler is being abandoned — the stacks name the blocked function.
Detection is conservative: a goroutine blocked on a primitive that is
still reachable (a global, a live struct) is never reported, so a zero
count does not prove the absence of every leak, but a nonzero count is
never a false alarm.

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
  problem as `WithMetrics`: it panics. Use `framework.Tracing()` in your
  own chain.

## Deploying with observability

See [Deployment](deploy.md) for wiring `/metrics`, tracing exporters, and
graceful shutdown into a container/Kubernetes setup.
