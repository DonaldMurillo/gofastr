# Worktree isolation

GoFastr can isolate local runtime resources for linked Git worktrees so a
feature worktree can run beside the main checkout without port or database
collisions.

Isolation is configured in `gofastr.isolation.yml`:

```yaml
version: 1
isolation:
  enabled: true
  mode: worktree
  port:
    offset: 1000
    range: 1000
    scan: 20      # capped server-side at 64
  services:
    redis: 6379
  env:
    REDIS_URL: "redis://localhost:{port:redis}/0"
```

With `mode: worktree`, the main checkout keeps normal resources and linked
worktrees get deterministic replacements. `GOFASTR_ISOLATION=off` disables
isolation for a process.

**Worktree isolation auto-activates with no opt-in.** On any linked git
worktree checkout, `mode: worktree` turns on by default and deterministically
remaps the listen port — so `PORT=8080 ./app` run from a worktree serves on a
*different* port, and anything polling `8080` (a test, a reverse proxy, you)
hangs to its deadline. Two things make this visible: `App.Start` logs a
`slog.Warn` ("isolation remapped the listen address") naming both ports and the
kill switches whenever it remaps an explicitly-assigned address, and you can
turn the remap off with `GOFASTR_ISOLATION=off` (all isolation) or
`GOFASTR_ISOLATION_REWRITE=0` (keep DB/worktree isolation, honor explicit
`PORT`/`DATABASE_URL`). This is the checkout style agents use most, so a boot
that "hangs" from a worktree is almost always this.

## What is isolated

- `framework.App.Start(addr)` remaps the requested listen port in linked
  worktrees.
- `gofastr dev` resolves isolation before launching the app and passes
  isolated child env values.
- Generated apps from `gofastr init` and blueprint output use
  `framework/isolation` to resolve `PORT` and database DSNs.
- SQLite DSNs move under `.gofastr/isolation/{id}/`.
- Postgres URL database names get a stable `_{id}` suffix while query params
  are preserved.
- Env templates support `{id}`, `{project_dir}`, `{port}`, and
  `{port:name}` for named services.

Explicit `PORT`, `DATABASE_URL`, and configured env values are rewritten by
default inside an isolated worktree. Set `GOFASTR_ISOLATION_REWRITE=0` to keep
explicit env overrides untouched.

## Public API

Use `framework/isolation` when app code opens resources before calling
`App.Start`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/isolation"
import "log"
stmt: _ = db
import "database/sql"
-->
```go
runtimeIsolation, err := isolation.Resolve(".")
if err != nil {
    log.Fatal(err)
}

driver, dsn, err := runtimeIsolation.Database("sqlite3", "file:app.db")
if err != nil {
    log.Fatal(err)
}
db, err := sql.Open(driver, dsn)
```

Hand-written apps still get automatic port isolation through `App.Start`.
Database isolation is automatic when the app either runs through `gofastr dev`
and reads env, or opens the database through `Runtime.Database`.

## Common mistakes

- **Opening the database before `App.Start` without resolving
  isolation.** `App.Start` remaps the port, but a DSN you opened
  yourself is untouched, so two worktrees end up writing the same
  database. Open it through `isolation.Resolve(".")` →
  `Runtime.Database(driver, dsn)`, or run under `gofastr dev` and read
  the env it injects.
- **Expecting an explicit `PORT` / `DATABASE_URL` to win inside a
  linked worktree.** Explicit env values are rewritten by default so
  worktrees never collide. Set `GOFASTR_ISOLATION_REWRITE=0` if you
  really want your override honored.
- **Looking for isolation effects in the main checkout.** With
  `mode: worktree`, only *linked* worktrees get remapped resources.
  The main checkout keeps its normal port and database on purpose.
- **Fighting isolation instead of switching it off.** For a one-off
  un-isolated run (e.g. comparing against the main checkout's DB),
  `GOFASTR_ISOLATION=off` disables it for that process.
