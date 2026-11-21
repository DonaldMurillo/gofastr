# Deployment

GoFastr apps compile to a single static binary with templates, runtime JS, and optional embedded migrations baked in. Build one binary, pass your environment variables, and run it.

## Building the binary

`go build` compiles your `main` package into one executable—no Node, no asset pipeline, no sidecar:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o app ./
./app                 # listens on :8080 (or $PORT)
```

`gofastr build` runs code generation, project-wide `go vet`, and the accessibility check before building. Pass `--pkg` when `main` lives under `cmd/`:

```bash
gofastr build
gofastr build --pkg ./cmd/server
```

- **Go toolchain:** GoFastr targets `go 1.27.4` (minimum security floor for `crypto/x509` and `net/http` fixes, plus 1.27 stdlib features). With default `GOTOOLCHAIN=auto`, Go downloads it automatically.
- **SQLite vs. Postgres:** `CGO_ENABLED=0` works with pure-Go Postgres (`jackc/pgx` stdlib driver) and `modernc.org/sqlite`. If you use CGO `mattn/go-sqlite3`, build with `CGO_ENABLED=1` and run on a base image with libc.

## One server over SSH: `gofastr deploy`

For a single Linux server (with `sshd`, `systemd`, `bash`, and root or passwordless sudo), `gofastr deploy` cross-compiles the binary, uploads it, and manages it as a systemd service—no agent or container runtime required:

```bash
gofastr deploy deploy@203.0.113.7 --name shop --env-file prod.env --port 8080
gofastr deploy status deploy@203.0.113.7 --name shop
gofastr deploy rollback deploy@203.0.113.7 --name shop
```

Useful flags: `--keep` (releases retained, default 5), `--bin <file>` (deploy a prebuilt Linux binary), `--pkg` (main package if not `.`), `--ssh-port`, and `-i`.

What it configures on the host:

- `/opt/<name>/releases/<UTC-time>/<name>` and `/opt/<name>/current` symlink, plus per-release unit snapshots under `units/`.
- A non-login system user `<name>` and writable data directory `/var/lib/<name>` (`HOME` and `XDG_STATE_HOME` point here, so relative SQLite paths and [telemetry](telemetry.md) stay contained).
- `/etc/<name>/env` from `--env-file` (root + app readable only; omitted on later deploys to keep existing env).
- `/etc/systemd/system/<name>.service` (auto-restarts on crash, starts on boot, forces `GOFASTR_ENV=production`) and `<name>.socket`.

After starting a release, `gofastr deploy` waits up to 30 seconds for `/healthz` to return 2xx. On failure, it rolls back to the previous release, prints the failing app logs, and exits non-zero.

### Zero-refusal restarts via socket activation

systemd owns the listening socket (`--port` on all interfaces) and hands it to `App.Start`. During a deploy, rollback, or crash restart, incoming connections queue in the kernel socket instead of being refused, then get served once the new process starts.

`gofastr deploy` does not set up TLS (put a reverse proxy in front or use [Serving HTTPS from the app](#serving-https-from-the-app)), a firewall, or multi-node orchestration.

## Production Dockerfile

Multi-stage, distroless, non-root, pure-Go build (Postgres):

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

Using CGO `mattn/go-sqlite3`? Build with `CGO_ENABLED=1` and run on `gcr.io/distroless/base-debian12` instead of `static`.

## Environment & secrets

GoFastr reads config from real process environment variables. `NewApp` also loads `.env` files at boot in every environment; real env vars always win, and you can disable file loading with `GOFASTR_DOTENV=off` (see [dotenv](dotenv.md)). Never commit `.env` files or bake them into container images—inject production secrets from your platform's secret store (Kubernetes Secrets, AWS Secrets Manager/SSM, Vault).

| Variable | Purpose |
|---|---|
| `PORT` | Listen port. Bare values like `8080` normalize to `:8080`. |
| `DATABASE_URL` | Read by your app and passed to `framework.WithDB`. |
| `APP_ENV` | Adds `.env.<APP_ENV>` to the dotenv load order whenever set. |
| `GOFASTR_SECRET` | HMAC signing key (≥32 chars) for `uihost` session tokens. Set it (or `framework.WithSecret`) before running multiple replicas; required with `WithFanout`. |
| `GOFASTR_SECRET_PREVIOUS` | Comma-separated previous `GOFASTR_SECRET` values for zero-downtime key rotation. |
| `AuthConfig.JWTSecret` | Required in production (`DevMode=false`) when using `battery/auth`; `Init` fails closed if empty. |

### Rotating secrets without logging everyone out

Both session and JWT keys support dual verification during a rotation window—sign with the new secret, verify against new **or** previous, wait one token lifetime, then drop the old secret:

- **`GOFASTR_SECRET`:** Set `GOFASTR_SECRET` to the new key and `GOFASTR_SECRET_PREVIOUS` to the old key (or pass `framework.WithSecretRotation(newSecret, oldSecret)` in Go). Deploy, wait one session TTL, then unset `GOFASTR_SECRET_PREVIOUS`. Note: `GOFASTR_SECRET` is used verbatim including whitespace; a single entry in `GOFASTR_SECRET_PREVIOUS` tolerates accidental trailing whitespace/newlines.
- **`AuthConfig.JWTSecret`:** Set `JWTSecret` to the new key and put the old one in `JWTPreviousSecrets`. Deploy, wait one `JWTExpiry` (default 1h), then remove `JWTPreviousSecrets`.

## Migrations

`App.Start` runs additive auto-migrations on boot (creating missing tables and adding missing columns—never drops, renames, or retypes). For controlled rollouts or destructive changes, generate and run versioned migrations as a separate deploy step:

```bash
gofastr migrate up --db-url="$DATABASE_URL"
gofastr migrate status --db-url="$DATABASE_URL"
```

See [Migrations](migrations.md) for locking, checksums, and destructive-change rules.

## TLS & graceful shutdown

Most deployments terminate TLS at an ingress or reverse proxy and speak plain HTTP to the app.

On `SIGINT`/`SIGTERM`, `App.Start` drains cleanly via `App.Shutdown`:

1. `/readyz` immediately flips to `503` while the server keeps serving for the readiness grace (`framework.WithReadinessDrain`, default 5s, capped at half the shutdown deadline; skipped on loopback binds and `battery/service`). `/healthz` stays `200` until exit.
2. The listener stops accepting new connections (dropping idle speculative preconnects) and drains active requests up to `AppConfig.ShutdownTimeout` (default 15s on top of grace).
3. Long-lived connections end immediately so they don't stall shutdown: GET SSE streams (`text/event-stream`) have their context cancelled, `core/stream` WebSockets receive close code `1001` (with up to 2s for close handshakes), manually hijacked connections are closed, and in-flight cron jobs have their contexts cancelled.

If your process manages OS signals itself, set `AppConfig.DisableSignalHandling` and call `App.Shutdown` (idempotent) or `App.RunWithSignals`.

### Serving HTTPS from the app

When nothing sits in front of the app, `framework.WithTLS` (or `AppConfig.TLS`) terminates TLS 1.2+ with HTTP/2 directly. Pick exactly one certificate source:

```go
// Disk files (e.g., certbot); renewals are picked up automatically on the next handshake.
framework.WithTLS(framework.TLSConfig{
    CertFile: "/etc/letsencrypt/live/example.com/fullchain.pem",
    KeyFile:  "/etc/letsencrypt/live/example.com/privkey.pem",
})

// Automatic Let's Encrypt via TLS-ALPN-01 on :443 (no :80 listener required).
framework.WithTLS(framework.TLSConfig{AutoCert: &framework.AutoCertConfig{
    Domains:  []string{"example.com", "www.example.com"},
    CacheDir: "/var/lib/myapp/certs", // required to avoid hitting ACME rate limits on restart
    Email:    "ops@example.com",
}})
```

You can also pass a custom `*tls.Config` via `TLSConfig.Config` (TLS 1.2 minimum enforced). `AutoCert` refuses handshakes for unlisted hostnames; set `DirectoryURL` to Let's Encrypt staging while testing. Plain HTTP on the TLS port returns 400—run a separate `:80` listener or proxy if you need HTTP→HTTPS redirects.

### HTTP versions

By default the server speaks HTTP/1.1, plus HTTP/2 over TLS. To accept unencrypted HTTP/2 (`h2c` prior knowledge) from an internal proxy, pass `framework.WithHTTPProtocols`:

```go
var p http.Protocols
p.SetHTTP1(true)
p.SetUnencryptedHTTP2(true)
app := framework.NewApp(framework.WithHTTPProtocols(p))
```

`Upgrade: h2c` and HTTP/3 are not supported.

## HTTP server timeouts

`App.Start` sets four `http.Server` connection deadlines by default:

| Field | Default | Purpose |
|---|---|---|
| `ReadHeaderTimeout` | 10s | Time to receive request headers (slowloris defense). Keep set. |
| `ReadTimeout` | 60s | Whole request read (headers + body). Caps upload time. |
| `WriteTimeout` | 60s | Whole response write. Caps every handler response. |
| `IdleTimeout` | 120s | Max idle time on keep-alive connections. |

Override any field via `framework.WithHTTPServerTimeouts` (or `AppConfig.HTTPServerTimeouts`). Fields are `*time.Duration`: `nil` keeps the default, `0` disables that deadline:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "time"
-->
```go
app := framework.NewApp(framework.WithHTTPServerTimeouts(framework.HTTPServerTimeoutsConfig{
    WriteTimeout: new(2 * time.Minute),    // slow report handler
    ReadTimeout:  new(time.Duration(0)),   // disable for large uploads
}))
```

### Long-running requests & per-route budgets

Prefer raising `WriteTimeout` just enough for your slowest real endpoint instead of disabling it globally. If `AppConfig.RequestTimeout` (per-request middleware deadline, default 30s) exceeds an unset `WriteTimeout`, `WriteTimeout` automatically raises to `RequestTimeout + 10s`. Setting an explicit non-zero `WriteTimeout <= RequestTimeout` fails `App.Start` so buffered 504 responses aren't cut off.

You can override the middleware timeout per route or group:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/core/router"
import "time"
-->
```go
app := framework.NewApp()
app.Router().SetRouteTimeout("GET", "/reports/{id}", 2*time.Minute)

admin := app.Router().Group("/admin")
admin.SetTimeout(5 * time.Minute)

app.Router().SetRouteTimeout("POST", "/exports", router.NoTimeout)
```

Note that `WriteTimeout` still caps the whole response, so a route budget above 60s also needs `WithHTTPServerTimeouts` raised. `AppConfig.DisableRequestTimeout` removes the timeout middleware (including per-route budgets) and zeroes unset `ReadTimeout`/`WriteTimeout`.

### SSE and streaming

Because `WriteTimeout` bounds an entire response, SSE streams reconnect when it lapses. On `/__gofastr/sse`, `EventSource` reconnects automatically, so the 60s default works out of the box. For long-lived presence streams where you want fewer reconnects, raise `WriteTimeout` or set it to `0`. Flushing responses already bypass the per-request `Timeout` middleware (see [Reactivity](reactivity.md)).

## Multi-replica deployments, health & metrics

- **Scaling out:** Sessions, rate limits, cron, in-memory queues, and SSE push are process-local by default. See [Horizontal scaling](scaling.md) before running more than one replica.
- **Health probes:** `/healthz` and `/readyz` (including DB checks when configured) are registered automatically. See [Health checks](health-checks.md).
- **Observability:** Enable `framework.WithMetrics()` for Prometheus `/metrics` (scrape internally—it is unauthenticated by design) and `framework.WithTracing()` for OpenTelemetry. See [Observability](observability.md).

## Pre-launch checklist

- [ ] `CGO_ENABLED` matches your DB driver (`0` for `pgx`/`modernc`, `1` for `go-sqlite3`).
- [ ] `AuthConfig.JWTSecret` set from env (not dev fallback).
- [ ] `GOFASTR_SECRET` set before running multiple replicas (required with `WithFanout`).
- [ ] Migrations run as a deploy step (or additive auto-migrate accepted for single replica).
- [ ] `/healthz` and `/readyz` wired in your orchestrator; `/metrics` restricted to internal network.
- [ ] TLS terminated at ingress or configured via `framework.WithTLS`.
- [ ] Backups scheduled and restore tested (see [Backups and restore](backups.md)).
- [ ] `WriteTimeout` / `ReadTimeout` fit your slowest report, export, AI, or upload route.

## Common mistakes

- **Shipping `.env` files in container images.** `.env` is for local dev only—inject production secrets via real environment variables and set `GOFASTR_DOTENV=off` if you want file loading disabled entirely.
- **Expecting boot auto-migrate to drop or rename columns.** Auto-migrate is additive only. Destructive changes require `gofastr migrate generate` and `gofastr migrate up`.
- **CGO flag and base image mismatch.** `CGO_ENABLED=0` fails with `mattn/go-sqlite3`, and a `CGO_ENABLED=1` binary crashes on `distroless/static` (missing libc—use `base-debian12`).
- **Booting production auth without `JWTSecret`.** With `DevMode=false`, `auth.Init` fails closed instead of minting an ephemeral key.
- **Slow endpoints dying at 60s.** The server `WriteTimeout` defaults to 60s regardless of handler `ctx` deadlines. Raise it via `WithHTTPServerTimeouts` for reports, exports, or LLM endpoints.
