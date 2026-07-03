# Deployment

GoFastr apps compile to a **single static binary** with templates, runtime
JS, and (optionally) embedded migrations baked in. That makes deployment
boring in the good way: build one binary, run it with a few env vars.

## The single-binary model

`go build` your `main` package → one executable. It serves HTTP, runs
auto-migrations on `Start`, and embeds the UI runtime — no Node, no asset
pipeline, no sidecar.

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o app ./
./app                 # listens on :8080 (or $PORT)
```

> **Go version.** `go.mod` declares `go 1.26`. The framework core only needs
> Go 1.24 (generic type aliases); the 1.26 floor comes from the optional
> `battery/print/chromepdf` PDF dependency (`chromedp/cdproto`). If you don't
> use that battery, an older Go works in practice — but the declared floor is
> what the toolchain enforces.

> **SQLite vs Postgres.** The bundled `gofastr` CLI uses SQLite. For a
> Postgres deployment, import a Postgres driver in your app and pass a
> `*sql.DB` via `framework.WithDB`. `CGO_ENABLED=0` works with the pure-Go
> `jackc/pgx` stdlib driver; the `mattn/go-sqlite3` driver needs CGO, so
> choose your base image accordingly (see the Dockerfile note below).

## Production Dockerfile

Multi-stage, distroless runtime, non-root, pure-Go build (Postgres):

```dockerfile
# ---- build ----
FROM golang:1.26 AS build
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
> `CGO_ENABLED=1` on `golang:1.26` and run on `gcr.io/distroless/base-debian12`
> (has libc) rather than `static`.

## Configuration (env)

GoFastr reads config from the environment (with `.env` auto-loaded in
development — see [dotenv](dotenv.md); real env always wins). Common vars:

| Var | Purpose |
|-----|---------|
| `PORT` | Listen port. A bare value like `8080` is normalized to `:8080`, so PaaS-injected `$PORT` works. |
| `DATABASE_URL` | Your app reads this and passes the connection to `WithDB`. |
| `APP_ENV` | Selects `.env.<APP_ENV>` in development. |
| auth secrets | If you use `battery/auth`, set its JWT/session secret explicitly in production — do not rely on the dev auto-generated secret (it rotates per process and silently invalidates sessions). See [auth](auth.md). |

## Secrets

GoFastr reads secrets from the process environment — it does not bundle a
secrets manager, and `.env` files are a **development** convenience only
(never commit them, never ship them in the image). In production, inject
secrets as env vars from your platform's secret store:

- **Kubernetes:** a `Secret` mounted as env vars (or via the CSI secrets
  store driver).
- **AWS:** Secrets Manager / SSM Parameter Store → env at task start
  (ECS task definition `secrets:`, or fetch-on-boot).
- **Vault:** the Vault Agent injector or `vault kv get` in an init step.

The one secret every auth-enabled app must set is **`AuthConfig.JWTSecret`**
(typically from env). With `DevMode=false` and no `JWTSecret`, the auth
battery **fails closed**: `Init` returns an error and the app refuses to
start — an empty signing key yields forgeable, restart-unstable
sessions. In dev, a per-process secret is auto-minted (with a WARN log)
so the boilerplate never ships a literal `change-me`.

## Migrations

`App.Start` auto-migrates on boot: it creates missing tables and adds
missing columns (additive only — it never drops, renames, or retypes;
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
it, and anything still open at the deadline — an SSE stream never goes
idle — is force-closed so the process exits well inside Kubernetes'
30s SIGTERM→SIGKILL window. In-flight cron jobs are joined under the
same deadline (their contexts are cancelled when the drain starts).
If your process owns signal handling itself, set
`AppConfig.DisableSignalHandling` and call `App.Shutdown` (or
`App.RunWithSignals`) from your own handler — `Shutdown` is idempotent,
so double-wiring is harmless.

## Running more than one replica

Everything on this page assumes one replica. Sessions, rate limits,
cron, in-memory queues, and SSE push are **process-local by default**
and need a shared backend (or a single-runner strategy) before you
scale out — see [Horizontal scaling](scaling.md) for the complete
what-breaks/what-fixes-it list.

## Health & metrics

- **Readiness/liveness:** auto-registered probes (plus a DB readiness check
  when a DB is configured) — point your orchestrator's probes at them. See
  [Health checks](health-checks.md).
- **Metrics:** enable `framework.WithMetrics()` to expose Prometheus
  `/metrics`; enable `framework.WithTracing()` for OpenTelemetry. See
  [Observability](observability.md). Scrape `/metrics` from inside the
  cluster — it is unauthenticated by design.

## Checklist

- [ ] `CGO_ENABLED` matches your DB driver (0 for pgx, 1 for go-sqlite3).
- [ ] Auth/session secret set explicitly (not the dev default).
- [ ] Migrations run as a deploy step (or accepted on-boot for single-replica).
- [ ] Readiness/liveness probes wired.
- [ ] `/metrics` scraped from inside the network only.
- [ ] TLS terminated at ingress.

## Common mistakes

- **Shipping `.env` files in the image.** They are a development
  convenience only. In production, inject secrets as real env vars
  from your platform's secret store — real env always wins over file
  values anyway, so a stowaway `.env` is at best dead weight and at
  worst a leaked secret.
- **Expecting boot auto-migrate to handle every schema change.** It
  creates tables and adds columns — additive only. Drops, renames, and
  type changes need a reviewed versioned migration (`gofastr migrate
  generate <name>` then `gofastr migrate up`); booting won't do them.
- **CGO flag and base image out of sync.** `CGO_ENABLED=0` with
  `mattn/go-sqlite3` fails at build; a CGO build on
  `distroless/static` fails at runtime (no libc — use
  `base-debian12`). Pure-Go pgx is what makes the static image work.
- **Booting production auth without a `JWTSecret`.** With
  `DevMode=false` and no `JWTSecret`, the battery refuses to start
  (`Init` errors, `App.Start` fails). There is no auto-minted fallback
  in production — the dev fallback rotates per process, which would
  silently invalidate sessions on every deploy. Set the secret
  explicitly from env.
