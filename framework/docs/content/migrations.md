# Migrations

GoFastr has two migration paths:

1. **Auto-migrate from declared entities.**
   `framework.AutoMigrate(db, app.Registry)` diffs the registered
   schema against the live database and applies `CREATE TABLE` /
   `ALTER TABLE` for every entity. Best for
   development and for apps where the entity declaration is the
   source of truth.
2. **SQL files with directives.** `core/migrate` runs versioned `.sql`
   files. Best when you need to express data backfills, complex
   constraints, or anything the entity declaration can't.

Both paths share the `_migrations` tracking table.

## SQL file format

```sql
-- +migrate Version 1
-- +migrate Name create_posts
-- +migrate Up
CREATE TABLE posts (
    id    TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    body  TEXT
);
CREATE INDEX posts_title_idx ON posts (title);
-- +migrate Down
DROP INDEX posts_title_idx;
DROP TABLE posts;
```

Required directives: `Version` (positive integer) and `Up`. `Name`
and `Down` are optional but `Down` is strongly recommended — without
it, `migrate down` fails for that version.

The runner reads `migrations/*.sql` in filename order. Convention:
zero-pad the version into the filename, e.g.
`0001_create_posts.sql`.

## CLI

```bash
gofastr migrate up                       # uses DATABASE_URL or .env
gofastr migrate status --db-url=file:app.db
gofastr migrate down 1
gofastr migrate diff                     # show schema drift vs entity registry
```

| Subcommand | Effect                                                          |
|------------|-----------------------------------------------------------------|
| `up`       | Apply all pending migrations in version order.                  |
| `status`   | Print applied count and pending versions.                       |
| `down N`   | Roll back the most recent `N` applied migrations in reverse.    |
| `diff`     | Compare the database schema to registered entities.             |

Flags & inputs:

- `--db-url=<dsn>` — required unless `DATABASE_URL` env var is set or
  a `.env` file in the working directory contains `DATABASE_URL=...`.
- `--driver=<name>` — defaults to `sqlite3`. Postgres or MySQL require
  building a `gofastr` binary that blank-imports the matching driver.

The migrations directory is hardcoded to `./migrations` relative to
the working directory. The tracking table name is `_migrations`. Both
are configurable via the programmatic API below if you embed the
runner in your own command.

## Programmatic API

```go
import "github.com/DonaldMurillo/gofastr/core/migrate"

m := migrate.New(db,
    migrate.WithDialect(migrate.DialectPostgres),
    migrate.WithTableName("_migrations"),
)
m.Register(migrate.Migration{
    Version: 1,
    Name:    "create_posts",
    Up:      "CREATE TABLE posts (...)",
    Down:    "DROP TABLE posts",
})
if err := m.Up(ctx); err != nil { … }
```

Use `RegisterFromReader` to load directive-formatted SQL from any
`io.Reader`, including embedded files.

## Dialects

- `DialectPostgres` (default) — uses `NOW()` and `$1, $2, …`
  placeholders.
- `DialectSQLite` — uses `CURRENT_TIMESTAMP` and `?` placeholders.

Dialect affects only the tracking-table queries and timestamp default.
Your migration `Up`/`Down` SQL is passed verbatim to the driver — keep
it dialect-portable, or split into two registrations.

## Tracking table

```sql
CREATE TABLE _migrations (
    version    BIGINT NOT NULL PRIMARY KEY,
    name       TEXT   NOT NULL DEFAULT '',
    applied_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

Created lazily on first `Up`/`Down`/`Status` call. Never edit it by
hand — if a migration is half-applied (e.g. partial DDL in a dialect
without transactional DDL), fix the schema manually and update the
row to match.

## Auto-migrate path

```go
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", framework.EntityConfig{ … })
if err := framework.AutoMigrate(db, app.Registry); err != nil { … }
```

`framework.AutoMigrate` is a package-level function, not a method on
`App`. It probes the connection to pick the SQL dialect (Postgres
first, SQLite on failure) and emits `CREATE TABLE IF NOT EXISTS` and
`CREATE INDEX IF NOT EXISTS` so it is safe to re-run.

This applies adds/alters required to make the database match the
registered entities. It will **not**:

- Drop columns or tables.
- Rename columns (it sees a rename as add+drop).
- Change a column type when data would be lost.

For destructive changes, write a numbered SQL file and stop using
auto-migrate for that table.

`AutoUUID` columns emit `DEFAULT gen_random_uuid()` on Postgres so raw
SQL `INSERT`s that omit the id column don't crash with a NOT NULL
violation. SQLite has no built-in UUID generator — the column stays
app-managed there (the framework's auto-CRUD layer and
`EntitySessionStore` already supply the value at insert time). If you
write raw `INSERT` statements against SQLite, you're responsible for
generating the id yourself.

## Portable SQL placeholders

Use `$N` (numbered) placeholders for all host-app SQL so the same
query string runs against SQLite (dev/tests) and Postgres (prod) with
no rebind step. Both drivers accept `$1`, `$2`, …:

```go
// Works on both engines.
db.QueryRowContext(ctx,
    `SELECT id, name FROM users WHERE email = $1 AND tenant_id = $2`,
    email, tenantID,
).Scan(&id, &name)
```

`?` (positional) also works on SQLite but NOT on Postgres, so a query
that compiles in tests will fail in prod. The framework's
`core/query.QueryBuilder` already emits `$N` and is the safer path.

## Common mistakes

- **Forgetting `-- +migrate Down`.** You can't roll back. Add it even
  if it's just a comment explaining why rollback is unsafe (then plan
  a forward fix instead).
- **Mixing auto-migrate with SQL files for the same table.** The
  diff calculator and the SQL file race. Pick one path per table.
- **Editing an applied migration.** The `_migrations` row already
  exists, so the change is silently skipped on the next `up`. Write
  a new migration instead.
- **Non-idempotent `Up`.** If `Up` fails partway through on a driver
  without transactional DDL (SQLite, some MySQL paths), the tracking
  row is not written but the partial schema remains. Use
  `CREATE TABLE IF NOT EXISTS` and similar guards.

## Concurrency model

GoFastr supports both SQLite and Postgres. Their concurrency characteristics differ significantly:

### SQLite

SQLite serialises writes — only one writer at a time. Under high concurrency (64+ goroutines), `CREATE` p99 can climb to 112 ms with only 10 writes completing out of 5000+ ops in mixed read/write workloads.

**Production guidance:**
- Set `MaxOpenConns(1)` on the `*sql.DB` for SQLite workloads (the framework does this automatically in test helpers).
- For write-heavy concurrent workloads, use Postgres instead.
- SQLite is fine for development, single-user tools, and read-heavy caches.

### Postgres

Postgres handles concurrent writes with MVCC. The same benchmarks show flat p99 latency at parallelism=64. Use Postgres for any deployment with concurrent write traffic.

## Pure-Go SQLite alternative

The default SQLite driver uses cgo, which adds ~4 MB to the binary and ~440 MB to build RAM. For environments where cgo is undesirable (CI, cross-compilation, minimal containers), use the pure-Go driver:

```go
import _ "modernc.org/sqlite"
```

Trade-offs:
- Binary is ~4 MB smaller, build uses ~440 MB less RAM.
- No cgo toolchain dependency — works in `GOOS=js` and scratch containers.
- Query performance is a few percent slower than cgo SQLite.
- Fully compatible with the GoFastr migration and query layers — no code changes required.
