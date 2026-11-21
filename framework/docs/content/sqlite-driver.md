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

## Default parameters added by the DSN rewrite

Four parameters are injected onto every opened DSN unless you explicitly set your own value:
- **`_time_format=sqlite`** — writes `time.Time` values in SQLite's `2006-01-02 15:04:05.999999999-07:00` layout (byte-compatible with `mattn/go-sqlite3` and GoFastr's timestamp scanners)
- **`_pragma=busy_timeout(5000)`** — waits up to 5 seconds on brief write contention instead of immediately returning `SQLITE_BUSY`
- **`_pragma=foreign_keys(1)`** — enforces `FOREIGN KEY` constraints on every connection (SQLite leaves them off by default; pass `_pragma=foreign_keys(0)` if you explicitly need to opt out)
- **`_txlock=immediate`** — begins write transactions with `BEGIN IMMEDIATE` so `busy_timeout` applies up front instead of failing with `SQLITE_BUSY_SNAPSHOT` on read-then-write upgrades (read-only transactions stay deferred)

The driver does **not** inject `journal_mode(WAL)` or `synchronous(NORMAL)` automatically. For file-backed production databases with concurrent readers, add them to your DSN explicitly (e.g., `file:app.db?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)`).

## Single-writer concurrency tips

SQLite allows one writer at a time (and many concurrent readers once WAL mode is enabled):
- Keep write transactions short; never make outbound HTTP calls inside a SQL transaction.
- Under heavy concurrent writes, keep `db.SetMaxOpenConns(1)` (or separate read/write pools) so Go serializes writers cleanly rather than racing on `busy_timeout`.
- Mount a persistent disk volume in production (never ephemeral container storage or NFS), and use online backups instead of copying files directly (see [Backups](backups.md)).

## Moving to PostgreSQL later

Entities, migrations, hooks, query DSL, includes, and cursor pagination target the same SQL abstraction across SQLite and PostgreSQL. Switching later mostly means opening a Postgres `*sql.DB` and updating any raw dialect-specific SQL you wrote by hand.

## Common mistakes

- **Opening `"sqlite3"` without importing `_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`:** either the driver is unregistered or you miss the automatic `_time_format=sqlite`, `busy_timeout(5000)`, `foreign_keys(1)`, and `_txlock=immediate` defaults.
- **Using `:memory:` without `?cache=shared` across multiple connections:** each pooled connection gets its own empty database, causing "no such table" errors in tests.
- **Running SQLite on an ephemeral container disk or NFS share:** ephemeral disks wipe data on redeploy, and network filesystems break SQLite file locking.
- **Copying `app.db` with `cp` while the server is running:** a raw file copy can miss `-wal`/`-shm` or capture a torn snapshot. Use `VACUUM INTO` or [Backups](backups.md).
