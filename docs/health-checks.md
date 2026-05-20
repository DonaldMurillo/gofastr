# Health and readiness

Every `framework.App` exposes two probes wired during `Start`:

| Endpoint   | Status   | Purpose                                  |
|------------|----------|------------------------------------------|
| `/healthz` | Liveness | "Is the process up?" — always returns 200 |
| `/readyz`  | Readiness | "Should it receive traffic?" — 200 / 503 |

Both endpoints set `Cache-Control: no-store`. They sit ahead of the
default middleware chain, so a hung downstream check or a recovered
panic won't make the process look healthy to its orchestrator.

## Liveness

```
GET /healthz
200 ok
```

Unconditional 200. The contract is: "the HTTP server is accepting
connections." Liveness probes that fail should restart the pod.

## Readiness

```
GET /readyz
200 OK
Content-Type: application/json

{
  "status": "ready",
  "checks": [
    { "name": "db", "status": "ok", "durationMs": 1 },
    { "name": "queue", "status": "ok", "durationMs": 0 }
  ]
}
```

If any check returns a non-nil error, status flips to `not_ready`
and the response code becomes `503 Service Unavailable`. Load
balancers should drain the instance — the process is still up but
its dependencies aren't.

All checks run in parallel under a 5-second overall deadline.
Per-check duration is reported in `durationMs` so a slow probe is
visible without a full timeout.

## Registering checks

```go
app := framework.NewApp(framework.WithDB(db))

app.RegisterReadiness("redis", func(ctx context.Context) error {
    return redisClient.Ping(ctx).Err()
})

app.RegisterReadiness("upstream-api", func(ctx context.Context) error {
    return apiClient.Health(ctx)
})
```

When `WithDB` is set, the framework auto-registers a `db` check that
calls `DB.PingContext(ctx)`. You don't need to add it yourself.

### From a plugin or battery

Implement the optional `framework.ReadinessRegistrar` interface:

```go
func (b *myBattery) RegisterReadinessChecks(app *framework.App) {
    app.RegisterReadiness("my-thing", b.checkReady)
}
```

The framework's plugin/battery init loop invokes
`RegisterReadinessChecks` automatically. The method name is plural and
distinct from `App.RegisterReadiness(name, fn)` so an embedded `*App`
inside your battery type does not produce an ambiguous selector.

## Verbose error reporting (opt-in)

By default, `/readyz` reports failures with `"error": "check failed"`
— it does **not** include the underlying `error.Error()` string. The
probe is typically reachable without authentication, and raw error
strings frequently leak internal IPs (`dial tcp 10.0.3.17:5432:
connect: connection refused`), connection strings, or other
infrastructure detail useful only to an attacker doing recon.

To turn on verbose reporting (e.g. behind an internal listener or with
auth in front of `/readyz`), pass the option at app construction:

```go
app := framework.NewApp(framework.WithVerboseReadiness())
```

The status field still distinguishes `"ok"`, `"error"`, and
`"timeout"` regardless of verbose mode — only the human-readable
`error` value differs.

## Timeouts and ctx-ignoring checks

`/readyz` applies an overall deadline (default 5 seconds; override
with `WithReadinessTimeout`). Each check runs in its own goroutine
under `recover()`, so a panicking check marks that row as
`"error"` without taking the process down. A check that ignores
`ctx.Done()` and runs past the deadline is reported as
`"timeout"` immediately — the response does not wait for the
straggler.

```go
app := framework.NewApp(
    framework.WithReadinessTimeout(2*time.Second),
    framework.WithVerboseReadiness(),
)
```

## Common mistakes

- **Don't put expensive work in `/readyz`.** It's polled often. A
  check should return in well under a second.
- **Don't make `/healthz` conditional.** Liveness should fail only
  when the process itself is wedged — file `/readyz` failures
  there and orchestrators will restart pods unnecessarily.
- **Don't gate `/readyz` behind auth.** Probes from k8s, Fly, ELB
  are anonymous. If you've installed broad auth middleware, mount
  these endpoints before it.
- **Don't enable `WithVerboseReadiness` on a public listener.** The
  default redacted form is the right choice when probes can be
  reached from outside the cluster.
