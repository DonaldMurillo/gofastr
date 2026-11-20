# SQLite driver

GoFastr talks to SQLite through pure-Go `modernc.org/sqlite` — **zero CGO required**, so `CGO_ENABLED=0 go build` produces static binaries with no C toolchain dependency.

## Configuration

Import `github.com/DonaldMurillo/gofastr/sqlite/stdlib` for its side effect. It registers `modernc.org/sqlite` under the conventional `"sqlite3"` driver name and rewrites every DSN with production-safe defaults (`gofastr init` adds this import automatically):

```go
import (
	"database/sql"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

db, err := sql.Open("sqlite3", "app.db")
```

Common DSNs:
- `app.db` or `file:app.db` — persistent file on disk
- `file::memory:?cache=shared` — in-memory database for tests (shared cache keeps tables alive across pooled connections)

## Default pragmas added by the DSN rewrite

Four parameters are injected onto every opened DSN unless you explicitly set your own value:
- **`_pragma=foreign_keys(1)`** — enforces `FOREIGN KEY` constraints (SQLite leaves them off by default)
- **`_pragma=journal_mode(WAL)`** — lets concurrent readers run without blocking on writers
- **`_pragma=busy_timeout(5000)`** — waits up to 5 seconds on brief write contention instead of immediately returning `SQLITE_BUSY`
- **`_pragma=synchronous(NORMAL)`** — safe under WAL and substantially faster than `FULL`

## Single-writer concurrency tips

SQLite supports many concurrent readers under WAL and one writer at a time:
- Keep write transactions short; never make outbound HTTP calls inside a SQL transaction.
- Under heavy concurrent writes, keep `db.SetMaxOpenConns(1)` (or separate read/write pools) so Go serializes writers cleanly rather than racing on `busy_timeout`.
- Mount a persistent disk volume in production (never ephemeral container storage or NFS), and schedule WAL-safe backups (see [Backups](backups.md)).

## Moving to PostgreSQL later

Entities, migrations, hooks, query DSL, includes, and cursor pagination target the same SQL abstraction across SQLite and PostgreSQL. Switching later mostly means opening a Postgres `*sql.DB` and updating any raw dialect-specific SQL you wrote by hand.

## Common mistakes

- **Opening `"sqlite3"` without importing `_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`:** either the driver is unregistered or you miss the automatic `foreign_keys`, WAL, and `busy_timeout` pragmas.
- **Using `:memory:` without `?cache=shared` across multiple connections:** each pooled connection gets its own empty database, causing "no such table" errors in tests.
- **Running SQLite on an ephemeral container disk or NFS share:** ephemeral disks wipe data on redeploy, and network filesystems break SQLite file locking.
- **Copying `app.db` with `cp` while WAL is active:** copy misses `-wal`/`-shm` or produces a corrupt snapshot. Use `VACUUM INTO` or [Backups](backups.md).
