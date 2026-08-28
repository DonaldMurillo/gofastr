# Deployment

GoFastr apps compile to a **single static binary** with templates, runtime
JS, and (optionally) embedded migrations baked in. That makes deployment
boring in the good way: build one binary, run it with a few env vars.

## The single-binary model

`go build` your `main` package → one executable. It serves HTTP, runs
auto-migrations on `Start`, and embeds the UI runtime. No Node, no asset
pipeline, no sidecar.

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o app ./
./app                 # listens on :8080 (or $PORT)
```

The CLI runs generation, project-wide vet, and the accessibility gate before
building. It targets the project root by default; when the executable lives
under `cmd/`, select only the final main package with `--pkg`. Vet and
accessibility checks still run from the project root, matching `gofastr dev`:

```bash
gofastr build
gofastr build --pkg ./cmd/server
```

> **Go version.** `go.mod` declares `go 1.27.0`. The framework uses the 1.27
> standard library directly: `uuid` for request ids and entity keys,
> `strings.CutLast`, and the runtime's `goroutineleak` profile behind
> `/.debug/goroutineleak`. Go 1.21+ refuses to build a module whose `go`
> directive is below a dependency's, so the floor can only rise: `chromedp`
> (v0.15.1, `go 1.26`) is a direct dependency of the browser-driven tooling in
> `framework/testkit/axetest`, the CLI accessibility audit, and the eval
> harness. `battery/print/chromepdf` is a nested module that isolates the print
> feature's own chromedp usage and does not set the floor.

> **SQLite vs Postgres.** The bundled `gofastr` CLI uses SQLite. For a
> Postgres deployment, import a Postgres driver in your app and pass a
> `*sql.DB` via `framework.WithDB`. `CGO_ENABLED=0` works with the pure-Go
> `jackc/pgx` stdlib driver; the `mattn/go-sqlite3` driver needs CGO, so
> choose your base image accordingly (see the Dockerfile note below).

## Production Dockerfile

Multi-stage, distroless runtime, non-root, pure-Go build (Postgres):

```dockerfile
# ---- build ----
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app ./

# ---- runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
ENV PORT=8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app"]
```

> Using the CGO SQLite driver (`mattn/go-sqlite3`) instead? Build with
> `CGO_ENABLED=1` on `golang:1.27` and run on `gcr.io/distroless/base-debian12`
> (has libc) rather than `static`.

## Configuration (env)

GoFastr reads config from the environment. `NewApp` also loads `.env`
files at boot in every environment, not just development; set
`GOFASTR_DOTENV=off` to suppress it, see [dotenv](dotenv.md); real env
always wins. Common vars:

| Var | Purpose |
|-----|---------|
| `PORT` | Listen port. A bare value like `8080` is normalized to `:8080`, so PaaS-injected `$PORT` works. |
| `DATABASE_URL` | Your app reads this and passes the connection to `WithDB`. |
| `APP_ENV` | Adds `.env.<APP_ENV>` to the dotenv load order whenever it is set. |
| auth secrets | If you use `battery/auth`, set its JWT/session secret explicitly in production. Do not rely on the dev auto-generated secret (it rotates per process and silently invalidates sessions). See [auth](auth.md). |
| `GOFASTR_SECRET` | HMAC signing key for the uihost session token. Set it (or use `framework.WithSecret` in code) so a session token issued by one replica verifies on every other replica. With one replica and no secret, an ephemeral boot secret is minted (sessions roll over on restart). With `WithFanout` and no secret, the app refuses to boot. See [Reactivity model](reactivity.md) and [Horizontal scaling](scaling.md). |

## Secrets

GoFastr reads secrets from the process environment. It does not bundle a
secrets manager, and `.env` files are a **development** convenience only
(never commit them, never ship them in the image). In production, inject
secrets as env vars from your platform's secret store:

- **Kubernetes:** a `Secret` mounted as env vars (or via the CSI secrets
  store driver).
- **AWS:** Secrets Manager / SSM Parameter Store → env at task start
  (ECS task definition `secrets:`, or fetch-on-boot).
- **Vault:** the Vault Agent injector or `vault kv get` in an init step.

### Rotating secrets without a mass logout

Both the uihost session key (`GOFASTR_SECRET`) and the auth JWT key
(`AuthConfig.JWTSecret`) rotate gracefully: new tokens are signed with the
new secret, and verification accepts the new **or** the previous secret for
one token lifetime, after which you drop the old one.

- **`GOFASTR_SECRET`**: set `GOFASTR_SECRET` to the new value and
  `GOFASTR_SECRET_PREVIOUS` to the old (comma-separated to roll through
  several; each ≥32 chars). In code: `framework.WithSecretRotation(newSecret,
  oldSecret)`. Deploy to every replica, wait one session TTL, then unset
  `GOFASTR_SECRET_PREVIOUS` and redeploy.
- **`AuthConfig.JWTSecret`**: set `JWTSecret` to the new value and move the
  old into `JWTPreviousSecrets`. Wait one `JWTExpiry` (default 1h), then
  remove it and redeploy.

Omitting the previous key, the pre-rotation behavior, still invalidates
every outstanding token at once. Use the previous-key form only during an
active rotation. See [security](security.md) for the underlying key rules.

The one secret every auth-enabled app must set is **`AuthConfig.JWTSecret`**
(typically from env). With `DevMode=false` and no `JWTSecret`, the auth
battery **fails closed**: `Init` returns an error and the app refuses to
start, because an empty signing key yields forgeable, restart-unstable
sessions. In dev, a per-process secret is auto-minted (with a WARN log)
so the boilerplate never ships a literal `change-me`.

## Migrations

`App.Start` auto-migrates on boot: it creates missing tables and adds
missing columns (additive only: it never drops, renames, or retypes;
those need a reviewed versioned migration from `gofastr migrate generate`,
applied with `gofastr migrate up`). For controlled rollouts, run migrations
as a separate step with the CLI instead of on every replica's boot:

```bash
gofastr migrate up --db-url="$DATABASE_URL"
gofastr migrate status --db-url="$DATABASE_URL"
```

See [Migrations](migrations.md) for the production-hardening details
(locking, checksums, dirty-state, destructive-change gating).

## TLS & graceful shutdown

Terminate TLS at your ingress/load balancer (the common setup) and run the
app over plain HTTP behind it. `App.Start` installs signal handling and
drains in-flight requests on `SIGINT`/`SIGTERM` via `App.Shutdown`, so
rolling deploys don't cut active requests.

The drain is **bounded**: `AppConfig.ShutdownTimeout` (default 15s) caps
it, and anything still open at the deadline, say an SSE stream that
never goes idle, is force-closed so the process exits well inside Kubernetes'
30s SIGTERM→SIGKILL window. In-flight cron jobs are joined under the
same deadline (their contexts are cancelled when the drain starts).
If your process owns signal handling itself, set
`AppConfig.DisableSignalHandling` and call `App.Shutdown` (or
`App.RunWithSignals`) from your own handler. `Shutdown` is idempotent,
so double-wiring is harmless.

## HTTP server timeouts

`App.Start` builds the embedded `http.Server` with four connection deadlines:

| Field | Default | Bounds |
|-------|---------|--------|
| `ReadHeaderTimeout` | 10s | Time to receive the request headers (slowloris defence). Keep this set. |
| `ReadTimeout` | 60s | Whole request read: headers + body. Caps upload time. |
| `WriteTimeout` | 60s | Whole response write, from first header byte to the last. Caps every response. |
| `IdleTimeout` | 120s | How long a keep-alive connection may sit idle between requests. |

These used to be hardcoded; a handler that needed to run longer than 60s was
out of luck regardless of its own `ctx` deadline. Override any of them through
`framework.WithHTTPServerTimeouts` (or the `AppConfig.HTTPServerTimeouts`
field). Each field is a `*time.Duration`: leave it `nil` to keep the default,
or point at a value, including `0`, which **disables** that one deadline
(matching `net/http`, where a zero duration means "no timeout"). Build the
pointers with the `new(expr)` builtin:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "time"
-->
```go
app := framework.NewApp(framework.WithHTTPServerTimeouts(framework.HTTPServerTimeoutsConfig{
    WriteTimeout: new(2 * time.Minute), // slow report handler
    ReadTimeout:  new(time.Duration(0)), // disable: accept arbitrarily large uploads
}))
```

`AppConfig.DisableRequestTimeout` is the coarser, older knob: it removes the
per-request `Timeout` middleware from the chain **and** zeroes the server
`ReadTimeout`/`WriteTimeout`. An explicit `HTTPServerTimeouts` value wins over
that zeroing, so you can drop the middleware yet keep a custom deadline: set
the field you want to keep and leave `DisableRequestTimeout` for the rest.

### Long-running requests

A request that legitimately outlives the default `WriteTimeout`, a slow
report, a large export, an AI completion, needs the deadline raised or
removed for that path:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "time"
-->
```go
// Raise the server write deadline app-wide for a report-heavy service.
app := framework.NewApp(framework.WithHTTPServerTimeouts(framework.HTTPServerTimeoutsConfig{
    WriteTimeout: new(2 * time.Minute),
}))
```

Prefer the smallest raise that covers your slowest real request over disabling
the deadline entirely. `WriteTimeout` is a whole-response backstop: without
it, a handler that hangs holds its connection (and a goroutine) forever.

The per-request middleware deadline (`AppConfig.RequestTimeout`, default
30s) can be overridden per route or per group instead of app-wide:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/core/router"
import "time"
-->
```go
app := framework.NewApp()
// One slow dashboard keeps its own budget; every other route stays at 30s.
app.Router().SetRouteTimeout("GET", "/reports/{id}", 2*time.Minute)
// Or budget a whole group; the nearest enclosing group wins, and an
// exact SetRouteTimeout beats any group.
admin := app.Router().Group("/admin")
admin.SetTimeout(5 * time.Minute)
// router.NoTimeout exempts a route from the middleware deadline entirely.
app.Router().SetRouteTimeout("POST", "/exports", router.NoTimeout)
```

When the deadline fires, the 504 logs a structured `request timeout`
line naming the method, path, matched pattern, and budget. Two caveats:
the server-level `WriteTimeout` above still bounds the whole response,
so a route budget beyond it needs `WithHTTPServerTimeouts` raised too;
and `DisableRequestTimeout` removes the middleware entirely, taking the
per-route budgets with it.

### SSE and streaming

`WriteTimeout` bounds the whole response, and a Server-Sent Events stream is a
single response, so the stream is cut when the deadline lapses. That does not
break the framework's `/__gofastr/sse` bus: the browser's `EventSource`
reconnects automatically, so live updates keep flowing at any
`WriteTimeout`. The default 60s simply means an idle-enough stream reconnects
at most once a minute. For a presence/collaboration surface where you want
fewer reconnects on a single long-lived stream, raise `WriteTimeout` or set it
to `0`. The per-request middleware deadline is a separate concern: a handler
that flushes (as every SSE subscription does) already sheds it at first flush;
see [Reactivity](reactivity.md).

## Running more than one replica

Everything on this page assumes one replica. Sessions, rate limits,
cron, in-memory queues, and SSE push are **process-local by default**
and need a shared backend (or a single-runner strategy) before you
scale out. See [Horizontal scaling](scaling.md) for the complete
what-breaks/what-fixes-it list.

## Health & metrics

- **Readiness/liveness:** auto-registered probes (plus a DB readiness check
  when a DB is configured). Point your orchestrator's probes at them. See
  [Health checks](health-checks.md).
- **Metrics:** enable `framework.WithMetrics()` to expose Prometheus
  `/metrics`; enable `framework.WithTracing()` for OpenTelemetry. See
  [Observability](observability.md). Scrape `/metrics` from inside the
  cluster. It is unauthenticated by design.

## Checklist

- [ ] `CGO_ENABLED` matches your DB driver (0 for pgx, 1 for go-sqlite3).
- [ ] Auth/session secret set explicitly (not the dev default).
- [ ] `GOFASTR_SECRET` set (or `framework.WithSecret` in code) before running a second replica. Required with `WithFanout`.
- [ ] Migrations run as a deploy step (or accepted on-boot for single-replica).
- [ ] Readiness/liveness probes wired.
- [ ] `/metrics` scraped from inside the network only.
- [ ] TLS terminated at ingress.
- [ ] Database backup scheduled and a restore actually tested. See [Backups and restore](backups.md).
- [ ] Server timeouts fit your slowest real request. Raise `WriteTimeout` via `WithHTTPServerTimeouts` for report/export/AI endpoints instead of letting the 60s default cut them.

## Common mistakes

- **Shipping `.env` files in the image.** They are a development
  convenience only. In production, inject secrets as real env vars
  from your platform's secret store. Real env always wins over file
  values anyway, so a stowaway `.env` is at best dead weight and at
  worst a leaked secret.
- **Expecting boot auto-migrate to handle every schema change.** It
  creates tables and adds columns, additive only. Drops, renames, and
  type changes need a reviewed versioned migration (`gofastr migrate
  generate <name>` then `gofastr migrate up`); booting won't do them.
- **CGO flag and base image out of sync.** `CGO_ENABLED=0` with
  `mattn/go-sqlite3` fails at build; a CGO build on
  `distroless/static` fails at runtime (no libc, so use
  `base-debian12`). Pure-Go pgx is what makes the static image work.
- **Booting production auth without a `JWTSecret`.** With
  `DevMode=false` and no `JWTSecret`, the battery refuses to start
  (`Init` errors, `App.Start` fails). There is no auto-minted fallback
  in production: the dev fallback rotates per process, which would
  silently invalidate sessions on every deploy. Set the secret
  explicitly from env.
- **A slow handler dies at exactly 60s.** The server's `WriteTimeout` is a
  60s default; a report, export, or AI-completion handler that runs longer is
  cut mid-response no matter what `ctx` deadline it sets. Raise
  `WriteTimeout` (or `ReadTimeout` for uploads) via `WithHTTPServerTimeouts`
  to fit your slowest real request, see "HTTP server timeouts" above. SSE is
  not affected the same way: `EventSource` reconnects when a stream is cut.
