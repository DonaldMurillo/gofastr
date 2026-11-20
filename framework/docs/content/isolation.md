# Worktree isolation (`framework/isolation`)

GoFastr automatically isolates listen ports and databases in **linked Git worktrees** so multiple feature branches or coding-agent worktrees can run side by side without port or database collisions.

Configure isolation in the `isolation:` section of [`gofastr.settings.yml`](settings.md):

```yaml
version: 1
isolation:
  enabled: true
  mode: worktree
  port:
    offset: 1000
    range: 1000
    scan: 20
  services:
    redis: 6379
  env:
    REDIS_URL: "redis://localhost:{port:redis}/0"
```

## How auto-isolation works

- **Linked worktrees only:** the main checkout keeps its normal port and database. Inside any linked git worktree, `mode: worktree` auto-activates, deterministically remapping the listen port (logging a warning naming both ports) and isolating database paths.
- **SQLite & Postgres:** SQLite DSNs move into owner-only `.gofastr/isolation/{id}/` (`0700`); Postgres database names gain a stable `_{id}` suffix within Postgres's 63-byte identifier limit.
- **Safe `isolation.env`:** only template values (`{id}`, `{project_dir}`, `{port}`, `{port:name}`) are allowed; static secrets and process-shaping vars (`PATH`, `HOME`, proxies, `GOFASTR_ISOLATION*`) are rejected, and exported shell env vars always win.
- **Overrides:** set `GOFASTR_ISOLATION=off` to disable all isolation for a process, or `GOFASTR_ISOLATION_REWRITE=0` to keep DB/worktree isolation while honoring your explicit `PORT` and `DATABASE_URL`.

## Public Go API

When opening a database or picking a listen address before `App.Start`, use `framework/isolation` so `go run .` and `gofastr dev` agree on the exact same port and DB:

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

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/isolation"
import "log"
stmt: _ = addr
-->
```go
addr, err := isolation.ListenAddr(".", ":8080")
if err != nil {
	log.Fatal(err)
}
```

## Common mistakes

- **Wondering why `PORT=8080 ./app` inside a worktree listens on another port:** worktree isolation auto-remaps ports; check the startup log or pass `GOFASTR_ISOLATION_REWRITE=0` / `GOFASTR_ISOLATION=off`.
- **Opening SQLite directly in `main.go` before `App.Start` without `runtimeIsolation.Database`:** bypasses DB isolation so two worktrees write the same file.
- **Expecting isolation in the main checkout:** only linked worktrees remap resources.
