# Migrations

GoFastr supports two complementary migration paths that share the same DDL type mapping:

1. **Boot auto-migrate (`framework.AutoMigrate`):** Runs on `App.Start` to create missing tables, indexes, foreign keys, and columns (`ALTER TABLE ADD COLUMN`). Strictly additive—boot never drops, renames, or retypes columns. Best for development and entity-driven apps.
2. **Versioned SQL migrations (`core/migrate`):** Runs numbered `.sql` files with checksums, locks, dirty-state tracking, and `Down` rollbacks. Best for data backfills, renames, destructive changes, and controlled production rollouts.

Neither path migrates rows from another database; see [Data export & import](data-export.md#what-this-does-not-do) for one-off copy jobs.

## SQL file format

Put numbered SQL files in `./migrations` (e.g., `0001_create_posts.sql`):

```sql
-- +migrate Version 1
-- +migrate Name create_posts
-- +migrate Up
CREATE TABLE posts (
    id    TEXT PRIMARY KEY NOT NULL,
    title TEXT NOT NULL,
    body  TEXT
);
CREATE INDEX posts_title_idx ON posts (title);
-- +migrate Down
DROP INDEX posts_title_idx;
DROP TABLE posts;
```

- **Directives (case-sensitive):** `Version` (positive integer) and non-empty `Up` are required. `Name`, `Down`, `Group`, and `NoTransaction` are optional. Unknown or misspelled `-- +migrate` lines are rejected at parse time so a typo never silently merges `Down` into `Up`.
- **SQL sections:** Put SQL statements under `-- +migrate Up` (apply) or `-- +migrate Down` (rollback). A file with SQL before its first section is rejected even if its `Up` section is non-empty. Blank lines and SQL comments can precede the first section; block comments follow the selected SQL dialect.
- **`Down` safety:** `migrate down` fails with `ErrNoDown` before rolling anything back if any target version lacks `Down` SQL.
- **Non-transactional migrations:** Add `-- +migrate NoTransaction` for statements Postgres forbids inside a transaction (`CREATE INDEX CONCURRENTLY`, `VACUUM`, `CREATE DATABASE`). On Postgres, statements are split and executed individually. The version is marked `dirty` before running and cleared on success; if it fails mid-way, reconcile manually and clear it with `migrate force`.
- **Groups:** Add `-- +migrate Group <name>` to scope migrations to an optional feature or battery (`default` is reserved for ungrouped migrations). Versions are unique per `(group, version)`. Select groups with `--group=<name>` or `m.Up(ctx, "knowledge")`; keep groups schema-independent.

## CLI reference

```bash
gofastr migrate up                                        # apply all pending migrations
gofastr migrate status --db-url=file:app.db               # show applied, pending, dirty, drift
gofastr migrate down 1                                    # roll back the latest N migrations
gofastr migrate generate add_email --from=gofastr.yml     # diff app spec against snapshot
gofastr migrate force 7                                   # mark version 7 applied (baseline/recover)
gofastr migrate force 7 --not-applied                     # mark version 7 pending again
gofastr migrate repair --from=gofastr.yml --apply         # repair legacy SQLite owner FKs
```

Key flags (valued flags use `--name=<value>`):
- `--db-url=<dsn>`: Defaults to `DATABASE_URL` env var or `.env`.
- `--db=<sqlite|postgres>` (or `--driver=`): Inferred from `postgres://` / `postgresql://` URLs; defaults to SQLite otherwise.
- `--create-db` (`up` only): Creates the database first via `migrate.EnsureDatabase` if missing (Postgres role needs `CREATEDB`).
- `--group=<name>`: Repeatable filter for `up`/`down`/`status`/`force`/`generate`.
- `--from=<gofastr.yml>`, `--migrations=<dir>` (default `migrations`), `--snapshot=<path>` (default `migrations/schema.snapshot.json`).

## Declarative migration generation

`gofastr migrate generate <name> --from=gofastr.yml` diffs your app spec entities offline against `migrations/schema.snapshot.json`, writes the next numbered `.sql` file, and updates the snapshot. Commit both files, review the SQL, then run `gofastr migrate up`.

- **Adds & drops:** Emits `CREATE TABLE`, `ADD COLUMN`, `DROP COLUMN`, `DROP TABLE`, and index/unique changes. New `Required` columns without defaults are added nullable first (backfill rows, then tighten in the next migration). Table drops referenced by remaining foreign keys are refused.
- **Renames:** Undeclared renames look like drop + add and print a warning. Declare renames explicitly (`EntityConfig.Renames: {"old": "new"}` or `renames:` in `gofastr.yml`) to emit a non-destructive `RENAME COLUMN`.
- **Type changes:** Treated as destructive (refused by `ApplySchemaDiff` unless `AllowDestructive` is set). Type-change detection catches type classes (e.g., `TEXT → INTEGER`) and compares character widths on Postgres (`VARCHAR(50) → VARCHAR(100)` emits `ALTER COLUMN ... TYPE`; SQLite ignores widths). Decimal precision/scale changes (`DECIMAL(10,2) → DECIMAL(19,4)`) are normalized away and should be hand-written; `RawType` columns are skipped.
- **SQLite table rebuilds:** Because SQLite cannot alter column types, constraints, or drop unique/indexed/FK columns in place, `migrate generate` emits a full table rebuild (`CREATE` + `INSERT ... SELECT` + `DROP` + `RENAME`) with no `Down`. Rebuilds on tables referenced by other foreign keys are refused by `generate` (write a `-- +migrate NoTransaction` migration with `PRAGMA foreign_keys=OFF` / `ON` instead).
- **Identifiers:** Reserved SQL keywords (`order`, `user`, `group`, etc.) are quoted automatically (`core/query`). Index names past Postgres's 63-byte limit are deterministically truncated with an 8-hex hash (`query.FitIdent`); duplicate index names across tables are rejected.

### Primary-key changes

Changing an entity's `PrimaryKey` (for example moving a join table from `id` to `(student_id, course_id)`) is flagged as destructive:

- **Postgres:** Drops dependent declared foreign keys, drops the old primary key, clears old implicit `NOT NULL` flags (unless `Required`), adds the new `PRIMARY KEY`, and re-adds declared foreign keys. Hand-added undeclared foreign keys block the change until removed or run with `AllowDestructive`.
- **SQLite:** Rebuilds the table (forward-only, no `Down`). Refused by boot and `migrate generate` if other tables reference the old key.
- **Boot safety:** Auto-migrate only applies a primary-key change to an empty table; a populated table fails boot and directs you to `gofastr migrate generate`.

### Generating from a host binary (owned-Go path)

`gofastr migrate generate` reads only `gofastr.yml`. Once your app owns its schema in Go (`app.Entity`, `App.Table`, `App.View`, `App.Routine`), emit versioned migrations directly from your own binary via `migrate.GenerateMigrationFile` (using the exact same snapshot format and diff engine):

<!-- gofastr:compile
import "fmt"
import "log"
import "os"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/migrate"
func buildApp() *framework.App { return framework.NewApp() }
-->
```go
func main() {
    app := buildApp()

    if len(os.Args) >= 4 && os.Args[1] == "migrate" && os.Args[2] == "generate" {
        name := os.Args[3]
        plan := app.MigrationPlan() // call app.InitPlugins() first if batteries declare schema
        path, err := migrate.GenerateMigrationFile(plan, name, migrate.MigrationFileOptions{
            MigrationsDir: "migrations",
            SnapshotPath:  "migrations/schema.snapshot.json",
            Dialect:       migrate.DialectPostgres,
        })
        if err != nil { log.Fatal(err) }
        if path == "" { fmt.Println("schema is up to date"); return }
        fmt.Printf("generated %s\n", path)
        return
    }

    if err := app.Start(":8080"); err != nil { log.Fatal(err) }
}
```

For lower-level control, call `migrate.GeneratePlan`, `migrate.RenderMigrationFileChecked`, and `migrate.SaveSnapshot`.

## Raw tables, routines & views

### Raw tables (`app.Table`)

When you need a SQL table without auto-CRUD, HTTP routes, or injected columns (`id`, timestamps, `tenant_id`), declare a `migrate.Table`—it migrates and diffs alongside entities and participates in foreign keys:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import migrate "github.com/DonaldMurillo/gofastr/framework/migrate"
import "github.com/DonaldMurillo/gofastr/core/schema"
-->
```go
app.Table(migrate.Table{
    Name: "user_roles",
    Columns: []migrate.Column{
        {Name: "user_id", Type: schema.String, NotNull: true},
        {Name: "role",    Type: schema.String, NotNull: true},
        {Name: "amount",  RawType: "NUMERIC(12,2)"},
    },
    Indices:     []migrate.Index{{Name: "ux_user_roles", Columns: []string{"user_id", "role"}, Unique: true}},
    ForeignKeys: []migrate.ForeignKey{{Columns: []string{"user_id"}, RefTable: "users"}},
})
```

### Stored routines (`app.Routine` / `app.RoutinesFS`)

Routines (Postgres functions, procedures, triggers; SQLite triggers) apply idempotently on every boot after tables migrate and are recorded by SHA-256 checksum in `gofastr_routines` (for drift reporting via the `app_routines` MCP tool; orphaned rows log a warning rather than auto-dropping).

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
var app = framework.NewApp()
import migrate "github.com/DonaldMurillo/gofastr/framework/migrate"
-->
```go
app.Routine(migrate.Routine{
    Name: "double_it",
    Up:   `CREATE OR REPLACE FUNCTION double_it(x int) RETURNS int
            AS $$ BEGIN RETURN x * 2; END; $$ LANGUAGE plpgsql`,
    Down: "DROP FUNCTION IF EXISTS double_it(int)",
})
```

For multi-line SQL, embed a directory with `app.RoutinesFS(routinesFS, "db/routines")`. Filenames control dialect and direction (sorted alphabetically, so name functions before triggers that call them):
- `<name>.sql` / `<name>.down.sql` — all dialects
- `<name>.pg.sql` / `<name>.pg.down.sql` — Postgres only (`DialectPostgres`)
- `<name>.sqlite.sql` / `<name>.sqlite.down.sql` — SQLite only (`DialectSQLite`)

### Views (`app.View`)

Declare SQL views with `DependsOn` ordering and optional `Materialized: true` (Postgres). Adding `Columns` automatically registers an `Unmanaged` read-only entity (`GET /<view>`, `GET /<view>/{id}`, `GET /<view>/_events`—no write routes, no table DDL):

```go
app.View(migrate.View{
    Name:      "active_users",
    Select:    "SELECT id, name FROM users WHERE active",
    DependsOn: []string{"users"},
    Columns: []migrate.Column{
        {Name: "id", Type: schema.String, PrimaryKey: true},
        {Name: "name", Type: schema.String},
    },
})
```

## Auto-migrate, programmatic runner & placeholders

### Turning off boot DDL

For deployments that forbid schema changes on startup, disable boot auto-migrate and run versioned migrations in your deploy pipeline:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "database/sql"
var db *sql.DB
-->
```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithoutAutoMigrate(), // Start performs no entity DDL
)
```

Note: entity seeds still run once globally via `_gofastr_seeded` (plus `_gofastr_seed_lock` on SQLite and a Postgres advisory lock). `WithOutbox` ensures `event_outbox` at boot unless you pass `outbox.WithoutEnsureTable()`.

### Creating the DB & running migrations in Go

<!-- gofastr:compile
import migrate "github.com/DonaldMurillo/gofastr/core/migrate"
var dsn string
stmt: _ = created
stmt: _ = err
-->
```go
created, err := migrate.EnsureDatabase("postgres", dsn)
```

```go
m := migrate.New(db,
    migrate.WithDialect(migrate.DialectPostgres), // or DialectSQLite
    migrate.WithTableName("_migrations"),
)
m.Register(migrate.Migration{Version: 1, Name: "create_posts", Up: "...", Down: "..."})
if err := m.Up(ctx); err != nil { ... }
st, _ := m.Status(ctx)
```

Use `RegisterFromReader` to load directive `.sql` files from an `embed.FS`.

### Portable SQL placeholders & types

- Always use `$1`, `$2`, … in hand-written SQL—both SQLite and Postgres drivers accept `$N` (`?` breaks on Postgres).
- `schema.Bool` maps to `BOOLEAN` on Postgres; `schema.Int` maps to `BIGINT` (`BIGSERIAL` for auto-increment); `AutoUUID` emits `DEFAULT gen_random_uuid()` on Postgres (app-managed on SQLite).
- Tables may be schema-qualified (`Table: "archive.widgets"`). Multi-version entities merge the union of all versions' columns at auto-migrate time.

## Production safety & database concurrency

- **Cross-replica locking:** Postgres uses `pg_advisory_lock`; SQLite uses a leased lock row in `_gofastr_migrate_lock` (60s lease renewed per migration). Dialect detection fails closed if the DB is unreachable rather than silently guessing SQLite.
- **Atomicity & immutability:** Auto-migrate runs in one transaction; each versioned migration runs in its own transaction (unless `NoTransaction`). Applied migrations are SHA-256 checksummed in `_migrations`—editing an applied file fails with `ChecksumMismatchError`.
- **SQLite vs. Postgres:** SQLite serializes writes—set `MaxOpenConns(1)` for SQLite and switch to Postgres for concurrent write workloads. For pure-Go builds without CGO (smaller binaries, scratch containers), import `_ "modernc.org/sqlite"`.

## Common mistakes

- **Editing an already-applied migration.** The runner verifies SHA-256 checksums and aborts with `ChecksumMismatchError`. Write a new migration instead.
- **Omitting `-- +migrate Down`.** `migrate down` refuses with `ErrNoDown` before rolling anything back.
- **Mixing auto-migrate and hand-written SQL on the same table.** Pick one strategy per table (or disable boot DDL with `WithoutAutoMigrate()` and use `migrate generate` + `migrate up`).
- **Using `?` placeholders in raw SQL.** Works in SQLite tests, fails in Postgres production—use `$1`, `$2`, … everywhere.
- **Renaming a column without declaring `Renames`.** Without `EntityConfig.Renames` (or `renames:` in YAML), the diff sees a drop + add and destroys row data.
