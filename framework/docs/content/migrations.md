# Migrations

GoFastr has two migration paths:

1. **Auto-migrate from declared entities.**
   `framework.AutoMigrate(db, app.Registry)` creates every registered
   entity's table (and indexes, and foreign keys) so the database
   matches the entity declarations. Runs on `App.Start`. Best for
   development and for apps where the entity declaration is the
   source of truth.
2. **SQL files with directives.** `core/migrate` runs versioned `.sql`
   files. Best when you need to express data backfills, complex
   constraints, or anything the entity declaration can't.

Both are production-hardened (see [Production safety](#production-safety)):
auto-migrate runs under an advisory lock inside a single transaction; the
versioned runner adds checksums, dirty-state tracking, and a
no-transaction escape hatch. The two paths are kept coherent — the
entity schema is the single source of DDL type mapping, so a table
auto-migrate creates diffs clean against the same entity declaration.

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

### Non-transactional migrations

By default each migration runs inside a transaction. Statements that
cannot run in a transaction — `CREATE INDEX CONCURRENTLY`, `VACUUM`,
`CREATE DATABASE` on Postgres — need the `NoTransaction` directive:

```sql
-- +migrate Version 7
-- +migrate Name concurrent_email_index
-- +migrate NoTransaction
-- +migrate Up
CREATE INDEX CONCURRENTLY posts_email_idx ON posts (email);
-- +migrate Down
DROP INDEX posts_email_idx;
```

A non-transactional migration is recorded **dirty** before its SQL
runs and only marked clean on success. If it fails partway, the dirty
flag stays set and every later `up`/`down` refuses with an error until
you reconcile the schema by hand and run `migrate force` (below).
Reserve `NoTransaction` for statements that genuinely require it.

The runner reads `migrations/*.sql` in filename order. Convention:
zero-pad the version into the filename, e.g.
`0001_create_posts.sql`.

## CLI

```bash
gofastr migrate up                       # uses DATABASE_URL or .env
gofastr migrate status --db-url=file:app.db
gofastr migrate down 1
gofastr migrate diff                     # show schema drift vs entity registry
gofastr migrate diff --apply             # apply non-destructive changes
gofastr migrate diff --apply --allow-destructive   # also run DROP COLUMN
gofastr migrate generate add_email       # write a versioned migration file
gofastr migrate force 7                  # mark version 7 cleanly applied
gofastr migrate force 7 --not-applied    # treat version 7 as pending again
```

| Subcommand   | Effect                                                        |
|--------------|---------------------------------------------------------------|
| `up`         | Apply all pending migrations in version order.                |
| `status`     | Print applied count, pending versions, and any dirty version. |
| `down N`     | Roll back the most recent `N` applied migrations in reverse.  |
| `diff`       | Compare the database schema to registered entities.           |
| `generate N` | Write a versioned, reversible migration file from entity changes. |
| `force V`    | Reconcile the tracking table for version `V` (recover/baseline).|

Flags & inputs:

- `--db-url=<dsn>` — required unless `DATABASE_URL` env var is set or
  a `.env` file in the working directory contains `DATABASE_URL=...`.
- `--driver=<name>` — defaults to `sqlite3`. Postgres or MySQL require
  building a `gofastr` binary that blank-imports the matching driver.
- `--apply` (`diff` only) — execute the changes in a single
  transaction instead of just printing them.
- `--allow-destructive` (`diff --apply` only) — permit `DROP COLUMN`.
  Without it, a change set containing a drop is refused outright (no
  partial apply). Destructive changes are flagged with `⚠` in the diff
  output.
- `--not-applied` (`force` only) — remove the version from the tracking
  table (treat as pending) instead of marking it applied.

`migrate force <V>` is the recovery path out of a dirty state and the
way to **baseline** an existing database: it marks a version applied
*without running its Up SQL*, recording the migration's checksum so
later drift checks line up. Use `--not-applied` to drop a version's row
so it re-runs.

The migrations directory is hardcoded to `./migrations` relative to
the working directory. The tracking table name is `_migrations`. Both
are configurable via the programmatic API below if you embed the
runner in your own command.

## Generating migrations from entity changes (declarative workflow)

`migrate diff --apply` is fine for development, but production change
management wants **reviewable, version-controlled, reversible**
increments — not direct applies. `migrate generate` produces them
*offline* (no database needed):

```bash
gofastr migrate generate add_published --driver=postgres
```

It diffs the **entity declarations** in `entities/*.json` against a
committed **schema snapshot** (`migrations/schema.snapshot.json`) and
writes the next numbered file, e.g. `migrations/0002_add_published.sql`,
with both `Up` and a computed `Down`:

```sql
-- +migrate Version 2
-- +migrate Name add_published
-- +migrate Up
ALTER TABLE "posts" ADD COLUMN "published" BOOLEAN;
-- +migrate Down
ALTER TABLE "posts" DROP COLUMN "published";
```

> **Scope.** The `migrate generate` / `migrate diff` CLI commands read schema
> **only from `entities/*.json`** declarations (see
> [entity-declarations](entity-declarations.md)). They do **not** see anything
> registered in Go — neither `app.Entity(...)` entities nor `App.View` /
> `App.Routine` / `App.Table`. For Go-defined schema, use auto-migrate
> (`App.Start` applies it on boot) or `migrate diff` against a live DB; to emit
> a versioned migration that includes Go-registered views/routines/tables, call
> the programmatic `migrate.GeneratePlan(plan, snapshot, dialect)` from your own
> code (it returns the Up/Down SQL and next snapshot; write them with
> `migrate.RenderMigrationFile` / `SaveSnapshot`).

It then updates the snapshot. The typical loop:

1. Edit an entity declaration (add a field, add an entity, …).
2. `gofastr migrate generate <name>` → review the generated `.sql`.
3. Commit the migration **and** the updated `schema.snapshot.json`.
4. `gofastr migrate up` applies it through the tracked, locked,
   checksummed runner.

What it generates: `CREATE TABLE` (new entity), `ADD COLUMN` (new
field), `DROP COLUMN` (removed field, marked reversible — re-adds the
column on `Down` but does not restore row data), and `DROP TABLE`
(removed entity). The forward DDL is built by the same code path as
auto-migrate, so a generated migration matches what auto-migrate would
have applied. A new **required** field with **no default** is added
*nullable* — a `NOT NULL` `ADD COLUMN` with nothing to fill existing
rows fails on a populated table, so the constraint is deferred (the
change summary notes this); backfill the rows and tighten it in a later
migration. A required field that has a default keeps `NOT NULL`, since
every existing row gets the default. Type changes are out of scope (same as `diff`); express
those as a hand-written migration. The snapshot is offline state — pick
`--driver` to match your production engine so the emitted types are
right.

Flags: `--entities=<dir>` (default `entities`), `--migrations=<dir>`
(default `migrations`), `--snapshot=<path>` (default
`<migrations>/schema.snapshot.json`), `--driver=<name>`.

## Non-entity tables (raw tables)

Not every table wants the entity machinery — no auto-CRUD, no HTTP routes, no
validation, no auto-injected `id`/timestamps/`tenant_id`. Join tables,
analytics roll-ups, tables owned by a stored procedure, or just a preference
not to declare entities. `migrate.Table` is a raw schema declaration that
migrates, diffs, and generates **alongside** entities in the same registry —
including foreign keys that cross between an entity and a raw table.

```go
app.Table(migrate.Table{
    Name: "user_roles", // also the FK reference name
    Columns: []migrate.Column{
        {Name: "user_id", Type: schema.String, NotNull: true},
        {Name: "role",    Type: schema.String, NotNull: true},
        {Name: "amount",  RawType: "NUMERIC(12,2)"}, // RawType escape hatch
    },
    Indices:     []migrate.Index{{Name: "ux_user_roles", Columns: []string{"user_id", "role"}, Unique: true}},
    ForeignKeys: []migrate.ForeignKey{{Column: "user_id", RefTable: "users"}},
})
```

Only the columns you declare are emitted — nothing is injected. A foreign key
references the target table's primary key (the target may be an entity or
another raw table). Single-column or no primary key are supported; composite
primary keys are not yet (use a unique index). `RawType` on a column overrides
the portable type with an exact SQL type (`NUMERIC(10,2)`, `INET`, …) and works
on entity fields too.

## Stored routines (functions, procedures, triggers, views)

Routines are first-class migration objects. A `migrate.Routine` runs on every
boot (idempotent `CREATE OR REPLACE`) after tables migrate, and `migrate
generate` tracks it so a changed body produces a reversible versioned
migration (its `Down` restores the previous definition).

```go
app.Routine(migrate.Routine{
    Name: "double_it",
    Up:   `CREATE OR REPLACE FUNCTION double_it(x int) RETURNS int
            AS $$ BEGIN RETURN x * 2; END; $$ LANGUAGE plpgsql`,
    Down: "DROP FUNCTION IF EXISTS double_it(int)",
})
```

The SQL is run verbatim and is dialect-specific (functions/procedures are a
Postgres feature; SQLite has triggers and views). On SQLite, which has no
`CREATE OR REPLACE` for triggers/views, make the `Up` re-runnable with
`DROP … IF EXISTS;\nCREATE …` so every boot is idempotent.

`App.Start` runs every routine's `Up` on boot (after tables) via the plan it
builds from `App.Table` / `App.Routine` / `App.View`. To capture routine/view
changes as **versioned** migrations instead, use the programmatic generator —
the file-based `migrate generate` CLI does not see Go-registered routines/views
(see the Scope note above):

```go
plan := migrate.Plan{Registry: reg, Routines: routines, Views: views}
up, down, next, _ := migrate.GeneratePlan(plan, prevSnapshot, migrate.DialectPostgres)
// then migrate.RenderMigrationFile(version, name, up, down) + migrate.SaveSnapshot(...)
```

`GeneratePlan` emits each new/changed routine's body forward and restores the
previous body on rollback; a removed routine is dropped (and recreated on
`Down`). Tables, then views, then routines generate into one migration with
correct rollback ordering.

## Views (virtual tables built from entities)

A `migrate.View` is a read model defined by a SELECT over your entity
tables — a "virtual table constructed from other entities". It belongs to
both stories: it's created on boot after its source tables (and tracked
reversibly by `migrate generate`), and when it declares `Columns` it's
also exposed through the ORM as a **read-only** entity.

```go
app.Table( /* ...entities... */ )
app.View(migrate.View{
    Name:      "active_users",
    Select:    "SELECT id, name FROM users WHERE active",
    DependsOn: []string{"users"}, // created after this table
    Columns: []migrate.Column{    // declare to expose via the ORM (read-only)
        {Name: "id", Type: schema.String, PrimaryKey: true},
        {Name: "name", Type: schema.String},
    },
    // Materialized: true, // Postgres materialized view (plain view otherwise)
})
```

- **Migration**: emitted after all tables (and ordered among views by
  `DependsOn`). Idempotent — `CREATE OR REPLACE VIEW` on Postgres, `DROP …
  IF EXISTS` + `CREATE` on SQLite/materialized. `migrate generate` tracks
  the definition by checksum and writes a reversible migration when it
  changes (the `Down` restores the previous definition / drops a new one).
- **ORM**: with `Columns` declared, `GET /active_users` and
  `GET /active_users/{id}` are mounted (plus the query layer); no write
  routes. Without `Columns`, the view is migration-only — query it with
  raw SQL.
- The view's ORM entity is `Unmanaged`: the migration system emits no
  table DDL for it (the view DDL handles its existence). `Unmanaged` is a
  general `EntityConfig` flag — use it for any externally-created table
  (FTS virtual tables, legacy tables) you want to query through the ORM
  without auto-migrate touching its schema.

## Creating the database

By default the database must already exist. To create it on first run:

```bash
gofastr migrate up --create-db --driver=postgres --db-url=$DATABASE_URL
```

Or programmatically before `App.Start` / `migrate up`:

```go
created, err := migrate.EnsureDatabase("postgres", dsn)
```

On Postgres it connects to the maintenance `postgres` database and issues
`CREATE DATABASE` when the target is absent (the role needs `CREATEDB`). On
SQLite it's a no-op — the file is created when the runner opens it. It tolerates
a still-starting database with a short connection retry.

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

// Recovery / baseline: mark a version applied (true) or pending (false).
if err := m.Force(ctx, 1, true); err != nil { … }

// State.
st, _ := m.Status(ctx)   // st.Applied[i].Checksum, .Dirty are populated
```

Use `RegisterFromReader` to load directive-formatted SQL from any
`io.Reader`, including embedded files. Set `Migration.NoTransaction`
(or the `-- +migrate NoTransaction` directive) to run a migration
outside a transaction. Checksums and dirty-state tracking are automatic
— no API to opt in.

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
    version    BIGINT  NOT NULL PRIMARY KEY,
    name       TEXT    NOT NULL DEFAULT '',
    applied_at TIMESTAMP NOT NULL DEFAULT NOW(),
    checksum   TEXT    NOT NULL DEFAULT '',   -- SHA-256 of the Up SQL
    dirty      BOOLEAN NOT NULL DEFAULT FALSE -- failed no-transaction migration
);
```

Created lazily on first `Up`/`Down`/`Status` call. The `checksum` and
`dirty` columns are backfilled automatically onto tables created by an
older GoFastr, so upgrading is seamless. Never edit the table by hand —
use `migrate force` to reconcile state instead.

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

It creates tables, indexes, and foreign keys to make the database
match the registered entities, **inside one transaction and under an
advisory lock** (see [Production safety](#production-safety)). It will
**not**:

- Drop columns or tables.
- Rename columns (it sees a rename as add+drop).
- Change a column type when data would be lost.

Framework-managed columns are created for you: `created_at` /
`updated_at` (when `Timestamps` is on), `deleted_at` (when `SoftDelete`
is on), and `tenant_id` (when `MultiTenant` is on). You do not declare
these as fields — the framework injects them and auto-migrate creates
them, so a multi-tenant entity's table always has the `tenant_id`
column its writes scope by.

For destructive changes, use `gofastr migrate diff` (which reports and,
with `--apply --allow-destructive`, runs `DROP COLUMN`) or write a
numbered SQL file and stop using auto-migrate for that table.

`AutoMigrateContext(ctx, db, registry)` is the context-aware variant —
boot uses it so a shutdown signal cancels a migration that's waiting on
the advisory lock instead of hanging.

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
- **Editing an applied migration.** The runner records each
  migration's checksum, so an edited `Up` is **caught** on the next
  `up` with a `ChecksumMismatchError` rather than silently skipped.
  Write a new migration instead.
- **Non-idempotent `Up`.** Transactional migrations roll back cleanly
  on failure (both SQLite and Postgres have transactional DDL). Only
  `NoTransaction` migrations can leave a partial schema — and those are
  recorded dirty so the next run refuses until you run `migrate force`.

## Production safety

The migration system is built to be relied on for critical infrastructure,
not just dev convenience. The guarantees:

- **Advisory locking.** Every migration run — auto-migrate on boot and
  the versioned `Up`/`Down`/`Force` — is serialized by a Postgres
  advisory lock (`pg_advisory_lock`, keyed on a fixed constant). Boot N
  replicas at once and only one migrates at a time; the others wait.
  The lock is acquired by polling `pg_try_advisory_lock` so a cancelled
  context (shutdown) returns promptly instead of hanging on a stuck
  holder. SQLite takes no lock — it serializes writers at the file
  level — but the same code path runs so behavior is uniform.
- **Atomicity.** Auto-migrate runs all of its DDL in a single
  transaction; if entity *K* fails, entities *1..K-1* roll back too, so
  a botched boot never leaves a half-created schema. Versioned
  migrations each run in their own transaction (unless `NoTransaction`).
- **Checksum drift detection.** Each migration's `Up` SQL is hashed
  (SHA-256) at apply time. On every later run the runner recomputes and
  compares; a mismatch means the file was edited after it was applied,
  and the run aborts with `ChecksumMismatchError`. Applied migrations
  are immutable.
- **Dirty-state tracking.** A failed `NoTransaction` migration leaves a
  `dirty` marker; `up`/`down` refuse to proceed (`ErrDirty`) until an
  operator reconciles the schema and runs `migrate force`. This is the
  same posture as golang-migrate's dirty flag.
- **Destructive-change gate.** `DiffSchema` flags `DROP COLUMN` as
  destructive and `ApplySchemaDiff` refuses to run destructive changes
  unless the caller opts in (`ApplySchemaDiffWithOptions` /
  `--allow-destructive`). The default never drops data.
- **Baseline / recovery.** `migrate force <V>` (programmatically
  `Migrator.Force(ctx, version, applied)`) marks a version applied
  without running it (adopt an existing database) or removes it (treat
  as pending) — and clears any dirty flag either way.

These features map onto the production-grade feature set of mature
migrators (golang-migrate, goose, Atlas): versioned up/down, advisory
locking, transactional runs with a no-transaction escape hatch, dirty
state, checksums/drift detection, baseline, and a destructive-change
safety gate.

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
