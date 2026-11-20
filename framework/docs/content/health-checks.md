# Health and readiness

Every `framework.App` exposes two built-in probes during `Start` with `Cache-Control: no-store`:

| Endpoint | Kind | Behavior |
| --- | --- | --- |
| `/healthz` | Liveness | Always returns `200 ok` while the HTTP server is accepting connections (even during shutdown drain) |
| `/readyz` | Readiness | Runs registered dependency checks in parallel (default 5s timeout); returns `200` (`status: "ready"`) or `503` (`status: "not_ready"` / `"draining"`) |

Both endpoints run through the **root middleware chain** (default middleware plus every `app.Use` middleware) regardless of registration order. Scope authentication middleware to **route groups** (or explicitly exempt `/healthz` and `/readyz`) so orchestrator probes stay reachable.

## Registering readiness checks

When `WithDB` is set, GoFastr auto-registers a `db` ping check. Add custom checks with `App.RegisterReadiness` or by implementing `framework.ReadinessRegistrar` (`RegisterReadinessChecks(app *framework.App)`) on a plugin/battery:

```go
app.RegisterReadiness("redis", func(ctx context.Context) error {
	return redisClient.Ping(ctx).Err()
})
```

Each check runs in a panic-recovered goroutine under the shared deadline (`framework.WithReadinessTimeout`). By default, failing checks emit `"error": "check failed"` so public `/readyz` probes never leak internal IPs or DSNs; opt into full error messages on private networks with `framework.WithVerboseReadiness()`:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "time"
-->
```go
app := framework.NewApp(
	framework.WithReadinessTimeout(2*time.Second),
	framework.WithVerboseReadiness(),
)
```

## Graceful drain on shutdown

When `App.Shutdown` begins (or `SIGTERM` arrives), `/readyz` immediately answers `503 {"status":"draining","checks":[]}` while the server keeps accepting requests for the readiness drain grace period (default `5s` on exposed production listeners; `0s` in tests, `gofastr dev`, `battery/service`, and loopback/in-process listeners) so load balancers stop routing traffic before the listener closes:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "time"
-->
```go
app := framework.NewApp(
	framework.WithReadinessDrain(10*time.Second),
)
```

Keep `ReadinessDrain + ShutdownTimeout` within your orchestrator's `terminationGracePeriodSeconds`.

## Common mistakes

- **Putting slow or heavy queries in `/readyz`:** probes run every few seconds; keep checks fast pings.
- **Making `/healthz` fail when the database blips:** failing liveness restarts pods unnecessarily instead of letting `/readyz` drain traffic.
- **Putting auth middleware on the root middleware chain without exempting probes:** breaks Kubernetes/Fly/ELB health checks. Attach auth to route groups or exempt `/healthz` and `/readyz`.
- **Enabling `WithVerboseReadiness()` on a public endpoint:** exposes internal hostnames and connection errors.
