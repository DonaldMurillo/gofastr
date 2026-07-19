# Migrations

GoFastr has two migration paths:

1. **Auto-migrate from declared entities.**
   `framework.AutoMigrate(db, app.Registry)` converges the database
   with the entity declarations: it creates missing tables (with
   indexes and foreign keys) **and adds missing columns to existing
   tables** (`ALTER TABLE ADD COLUMN` — additive only, never a drop,
   rename, or type change). Runs on `App.Start`. Best for development
   and for apps that keep their schema in entity declarations rather
   than hand-written SQL.
2. **SQL files with directives.** `core/migrate` runs versioned `.sql`
   files. Best when you need to express data backfills, complex
   constraints, or anything the entity declaration can't.

Both are production-hardened (see [Production safety](#production-safety)):
auto-migrate runs under an advisory lock inside a single transaction; the
versioned runner adds checksums, dirty-state tracking, and a
no-transaction escape hatch. The two paths are kept coherent — the
entity schema is the single source of DDL type mapping, so a table
auto-migrate creates diffs clean against the same entity declaration.

## PostgreSQL boolean columns

`schema.Bool` is emitted as `BOOLEAN` on PostgreSQL. The auth durable
stores additionally upgrade their own legacy `INTEGER` (`0`/`1`) boolean
columns during `EnsureSchema`, including `auth_users.password_set` and
the session/2FA flags. Existing auth rows are converted with `0 = FALSE`
and non-zero = TRUE.

Auto-migrate does **not** retype arbitrary existing entity columns:
general type changes remain intentionally out of scope because they can
require data-specific conversions. If an application-managed boolean
column was created as `INTEGER` by an older deployment, apply and review
a PostgreSQL migration before booting the new code:

```sql
ALTER TABLE "items" ALTER COLUMN "enabled" DROP DEFAULT;
ALTER TABLE "items" ALTER COLUMN "enabled"
    TYPE BOOLEAN USING ("enabled" <> 0);
ALTER TABLE "items" ALTER COLUMN "enabled" SET DEFAULT FALSE;
```

Back up production data and verify any values outside `0`/`1` before
running this conversion.

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

### Groups

A migration may declare a group, scoping it to a feature or module:

```sql
-- +migrate Version 1
-- +migrate Group knowledge
-- +migrate Name create_articles
-- +migrate Up
CREATE TABLE articles (...);
-- +migrate Down
DROP TABLE articles;
```

No directive means the **default group** — exactly today's behavior.
See "Migration groups" below for semantics.

## CLI

```bash
gofastr migrate up                       # uses DATABASE_URL or .env
gofastr migrate status --db-url=file:app.db
gofastr migrate down 1
gofastr migrate generate add_email --from=gofastr.yml   # write a versioned migration file from entity changes
gofastr migrate force 7                  # mark version 7 cleanly applied
gofastr migrate force 7 --not-applied    # treat version 7 as pending again
```

| Subcommand   | Effect                                                        |
|--------------|---------------------------------------------------------------|
| `up`         | Apply all pending migrations in version order.                |
| `status`     | Print applied count, pending versions, and any dirty version. |
| `down N`     | Roll back the most recent `N` applied migrations in reverse.  |
| `generate N` | Write a versioned, reversible migration file from entity changes. |
| `force V`    | Reconcile the tracking table for version `V` (recover/baseline).|

Flags & inputs:

- `--db-url=<dsn>` — required unless `DATABASE_URL` env var is set or
  a `.env` file in the working directory contains `DATABASE_URL=...`.
- `--driver=<name>` — defaults to `sqlite3`. Postgres or MySQL require
  building a `gofastr` binary that blank-imports the matching driver.
- `--not-applied` (`force` only) — remove the version from the tracking
  table (treat as pending) instead of marking it applied.
- `--group=<name>` — scope `up`/`down`/`status` to one or more groups
  (repeat the flag); a single `--group` on `force` targets that group's
  version. `--group=default` addresses the default (ungrouped) set.
  `generate --group=<name>` stamps the `Group` directive into the
  generated file. No flag = all groups (today's behavior).

`migrate force <V>` is the recovery path out of a dirty state and the
way to **baseline** an existing database: it marks a version applied
*without running its Up SQL*, recording the migration's checksum so
later drift checks line up. Use `--not-applied` to drop a version's row
so it re-runs.

The migrations directory is hardcoded to `./migrations` relative to
the working directory. The tracking table name is `_migrations`. Both
are configurable via the programmatic API below if you embed the
runner in your own command.

## Migration groups

An app composed of optional features can scope migrations to the
feature that owns them: a `knowledge` module's tables apply only when
that module is enabled, and a battery can own its schema in its own
stream instead of injecting into the app's flat list.

- **Versions are unique per group.** Two groups may both have a
  version 1; `Register` rejects a duplicate `(group, version)` pair.
- **Selection.** `m.Up(ctx)` applies every registered group;
  `m.Up(ctx, "knowledge")` applies only that group's pending
  migrations. `Down`, `Status`, and the CLI's repeatable `--group`
  flag scope the same way. Enabling a feature later just runs its
  pending group — under the same advisory lock as everything else, so
  concurrent boots stay safe.
- **Ordering.** Within a group, strictly by version. When one run
  applies several groups, migrations interleave in `(version, group)`
  order — a deterministic tiebreak, **not** a dependency mechanism.
  Keep groups self-contained: a group must never depend on another
  group's schema, because the other group may not be enabled at all.
- **Compatibility.** Apps that never declare a group are untouched:
  the runner emits the exact pre-group SQL and never alters the
  tracking table. The first time a non-default group is in play, the
  tracking table gains a `group_name` column and its primary key is
  upgraded in place to `(group_name, version)` — atomic on Postgres,
  a transactional table rebuild on SQLite; existing rows all belong
  to the default group, so the upgrade cannot conflict.
- **Integrity.** Checksums, drift detection, and `force` all key on
  `(group, version)`. A dirty migration in the default group or any
  registered group blocks all operations; the error names the group.
  A *named* group with no registered migrations at all is a disabled
  module: its applied rows are that module's property — shown by
  `status`, but never compared, blocked on, rolled back, or dropped.
  (`force --group=<name>` remains the reconciliation escape hatch for
  a disabled module's rows.) The default group is never treated as a
  module — an applied default-group row with no matching registration
  is drift and errors, exactly as before groups existed.
- **Addressing the default group.** In selections — `--group` on the
  CLI, group args in Go — the name `default` addresses the default
  group (`m.Up(ctx, "default")`). It is reserved: `Register` rejects
  a group literally named "default".

## Generating migrations from entity changes (declarative workflow)

Boot auto-migrate is fine for development, but production change
management wants **reviewable, version-controlled, reversible**
increments — not implicit applies. `migrate generate` produces them
*offline* (no database needed):

```bash
gofastr migrate generate add_published --from=gofastr.yml --driver=postgres
```

It diffs the **entity declarations** in the `gofastr.yml` blueprint against a
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

> **Scope.** The standalone `gofastr migrate generate` CLI reads schema
> **only from the `--from=<blueprint.yml>` blueprint's entities** (see
> [blueprints](blueprints.md)) — the pre-graduation bootstrap path. It does
> **not** see anything registered in Go — neither `app.Entity(...)` entities
> nor `App.View` / `App.Routine` / `App.Table`. For Go-defined schema, use
> auto-migrate (`App.Start` applies it on boot); to emit a versioned migration
> that includes Go-registered entities/views/routines/tables, run migration
> generation from your own app's binary (it has the entities compiled in and
> diffs them against the snapshot), or call the programmatic
> `migrate.GeneratePlan(plan, snapshot, dialect)` from your own code (it returns
> the Up/Down SQL and next snapshot; write them with
> `migrate.RenderMigrationFile` / `SaveSnapshot`).

It then updates the snapshot. The typical loop:

1. Edit an entity in the blueprint (add a field, add an entity, …).
2. `gofastr migrate generate <name> --from=gofastr.yml` → review the generated `.sql`.
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
every existing row gets the default. Type changes are out of scope; express
those as a hand-written migration. The snapshot is offline state — pick
`--driver` to match your production engine so the emitted types are
right.

Flags: `--from=<blueprint.yml>` (required), `--migrations=<dir>`
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

Routines are migration objects, same as tables and views. A `migrate.Routine` runs on every
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

### Authoring routines as embedded SQL files

Storing routine bodies as Go string literals gets unwieldy past a few lines.
The primary authoring path is a directory of `.sql` files embedded with
`//go:embed` and loaded via `App.RoutinesFS`:

```go
import "embed"

//go:embed db/routines
var routinesFS embed.FS

app.RoutinesFS(routinesFS, "db/routines")
```

The filename grammar:

| File                         | Means                                                       |
|------------------------------|-------------------------------------------------------------|
| `<name>.sql`                 | Up body; runs on every dialect (the default).              |
| `<name>.down.sql`            | Down body for `<name>`.                                     |
| `<name>.pg.sql`              | Up body, Postgres-only (`Dialect: DialectPostgres`).        |
| `<name>.sqlite.sql`          | Up body, SQLite-only (`Dialect: DialectSQLite`).            |
| `<name>.pg.down.sql`         | Down body for the Postgres-only routine.                    |
| `<name>.sqlite.down.sql`     | Down body for the SQLite-only routine.                      |

Routine Name = file base name. A name must not have both a plain Up and a
dialect-suffixed Up, and must not have two dialect suffixes — both are
ambiguous authoring errors that fail loudly at registration. Empty
files, missing directories, and Down-without-Up files are likewise errors
(the framework screams rather than silently no-op'ing, the same posture as a
misconfigured entity). Dotfiles, sub-directories, and non-`.sql` files are
ignored, so a `README.md` next to the routines is fine.

### Dialect scoping

`Routine.Dialect` scopes a routine to a single SQL dialect. The zero value
(the empty string) means "runs on every detected dialect" — today's behavior.
Set `DialectPostgres` for a Postgres stored proc that has no SQLite equivalent
(typical when developing against SQLite locally but deploying to Postgres);
`DialectSQLite` for a SQLite-only trigger. During auto-migrate, routines whose
`Dialect` does not match `migrate.DetectDialect(db)` are skipped and listed in
a single boot-time log line:

```
INFO migrate: skipping routines declared for a different dialect declared=postgres running=sqlite3 routines=compute_totals,refresh_mv count=2
```

Skipped routines are NOT removed from the plan — a future dialect switch (or
running the same binary against Postgres) will pick them up. `View` does not
take a `Dialect` tag: its `render()` already emits the right DDL per engine
(`CREATE OR REPLACE VIEW` on Postgres, `DROP IF EXISTS` + `CREATE` on SQLite,
`MATERIALIZED` via the `Materialized` bool), so a redundant tag would be
ambiguous.

### The applied-routine ledger

Every boot, auto-migrate creates (if missing) a `gofastr_routines` table and
writes one row per applied routine inside the SAME transaction as the apply:

| Column       | Purpose                                                          |
|--------------|------------------------------------------------------------------|
| `name`       | Routine Name (primary key).                                      |
| `checksum`   | sha256 of the routine's Up body (`migrate.RoutineChecksum`).     |
| `applied_at` | Timestamp of the last boot that applied this routine.            |

The ledger is **reporting, not gating**: every matching routine's `Up` STILL
runs every boot (idempotent, self-healing against DB-side drift). The
checksum is how the `app_routines` MCP tool (see the Agent-ready doc) tells
you "the registered body drifted since the last boot that recorded this row"
— it does not skip application.

When a routine is removed from code, its ledger row is NOT dropped (additive-
only — boot never auto-drops). Instead, you see a loud `WARN` naming the
orphaned routine and pointing at the cure:

```
WARN migrate: previously applied routine is no longer registered; not dropped (additive-only) routine=compute_totals hint="drop via Routine.Down in a versioned migration, or remove the ledger row manually"
```

Drop the DB object explicitly via `Routine.Down` in a versioned migration, or
remove the ledger row manually if the object is already gone.

At the end of every boot, a one-line summary captures the apply outcome:

```
INFO migrate: routine apply summary applied=7 changed=2 first_time=1 skipped=1 orphaned=0
```

- `applied` — routines whose `Up` ran this boot (every matching routine).
- `changed` — applied routines whose checksum differs from the previous boot.
- `first_time` — applied routines seeing their first-ever ledger row.
- `skipped` — routines skipped for dialect mismatch this boot.
- `orphaned` — ledger rows whose name is no longer registered (the WARNed set).

### Generating versioned migrations

`App.Start` runs every routine's `Up` on boot (after tables) via the plan it
builds from `App.Table` / `App.Routine` / `App.View` / `App.RoutinesFS`. To
capture routine/view changes as **versioned** migrations instead, use the
programmatic generator — the file-based `migrate generate` CLI does not see
Go-registered routines/views (see the Scope note above):

```go
plan := migrate.Plan{Registry: reg, Routines: routines, Views: views}
up, down, next, _ := migrate.GeneratePlan(plan, prevSnapshot, migrate.DialectPostgres)
// then migrate.RenderMigrationFile(version, name, up, down) + migrate.SaveSnapshot(...)
```

`GeneratePlan` emits each new/changed routine's body forward and restores the
previous body on rollback; a removed routine is dropped (and recreated on
`Down`). Tables, then views, then routines generate into one migration with
correct rollback ordering.

### Worked Postgres example

Drop these three files in `db/routines/` and call
`app.RoutinesFS(routinesFS, "db/routines")`:

```sql
-- db/routines/compute_totals.pg.sql
CREATE OR REPLACE FUNCTION compute_totals(account_id text)
RETURNS TABLE (debits int, credits int)
LANGUAGE plpgsql AS $$
BEGIN
  SELECT COALESCE(SUM(amount), 0) INTO debits
    FROM ledger WHERE account_id = compute_totals.account_id AND amount < 0;
  SELECT COALESCE(SUM(amount), 0) INTO credits
    FROM ledger WHERE account_id = compute_totals.account_id AND amount > 0;
  RETURN NEXT;
END;
$$;
```

```sql
-- db/routines/stamp_audit.pg.sql      (function half of a trigger pair)
CREATE OR REPLACE FUNCTION stamp_audit() RETURNS trigger AS $$
BEGIN
  NEW.updated_at := NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

```sql
-- db/routines/stamp_audit_trg.pg.sql (trigger half — depends on stamp_audit())
CREATE OR REPLACE TRIGGER stamp_audit_trg
  BEFORE UPDATE ON ledger
  FOR EACH ROW EXECUTE FUNCTION stamp_audit();
```

Files are applied in name-sorted order; name them so a function precedes any
trigger or caller that depends on it (`stamp_audit` before
`stamp_audit_trg`).

A stored procedure is the same shape — call it from hooks, repos, or handlers
via `app.DB`:

```sql
-- db/routines/recompute_balance.pg.sql
CREATE OR REPLACE PROCEDURE recompute_balance(account_id text)
LANGUAGE plpgsql AS $$
BEGIN
  UPDATE accounts SET balance = (
    SELECT COALESCE(SUM(amount), 0) FROM ledger WHERE ledger.account_id = recompute_balance.account_id
  ) WHERE accounts.id = recompute_balance.account_id;
END;
$$;
```

```go
// In a hook or repository method:
if _, err := app.DB.ExecContext(ctx, "CALL recompute_balance($1)", acctID); err != nil {
    return fmt.Errorf("recompute balance: %w", err)
}

// A function call returns a row, so it goes through QueryRow:
var debits, credits int
if err := app.DB.QueryRowContext(ctx, "SELECT debits, credits FROM compute_totals($1)", acctID).
    Scan(&debits, &credits); err != nil {
    return err
}
```

In tests, point a fresh Postgres DB at the same `db/routines` directory via
`App.RoutinesFS` — the routines land in one transaction with the schema, and
the `app_routines` tool will report each one as `ledger_state=present`
once boot completes.

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
    Group:   "",  // optional; "" = default group
    Up:      "CREATE TABLE posts (...)",
    Down:    "DROP TABLE posts",
})
if err := m.Up(ctx); err != nil { … }          // all groups
if err := m.Up(ctx, "knowledge"); err != nil { … } // one group

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
older GoFastr, so upgrading needs no manual fix. When migration groups are in
use, the table additionally gains `group_name TEXT NOT NULL DEFAULT ''`
and the primary key becomes `(group_name, version)` — upgraded in
place the first time a non-default group is applied. Never edit the
table by hand — use `migrate force` to reconcile state instead.

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

It creates tables, indexes, and foreign keys, and **adds missing
columns to existing tables** (`ALTER TABLE ADD COLUMN`, built by the
same schema-diff path as `migrate generate`, so boot and a versioned
migration can never disagree) — all to make the database match the
registered entities,
**inside one transaction and under an advisory lock** (see
[Production safety](#production-safety)). A new **required** field
with **no default** is added *nullable* (a `NOT NULL ADD COLUMN`
fails on a populated table); backfill the rows, then tighten the
constraint in a versioned migration. Column adds happen before index
DDL, so a new field and its index land in one boot. It will **not**:

- Drop columns or tables.
- Rename columns (it sees a rename as add+drop).
- Change a column type when data would be lost.

Framework-managed columns are created for you: `created_at` /
`updated_at` (when `Timestamps` is on), `deleted_at` (when `SoftDelete`
is on), and `tenant_id` (when `MultiTenant` is on). You do not declare
these as fields — the framework injects them and auto-migrate creates
them, so a multi-tenant entity's table always has the `tenant_id`
column its writes scope by.

For destructive changes (drops, renames, type changes), use `gofastr
migrate generate <name>` to emit a reviewable versioned migration — a
removed field generates a reversible `DROP COLUMN` — then `gofastr migrate
up`, or write a numbered SQL file by hand and stop using auto-migrate for
that table.

`AutoMigrateContext(ctx, db, registry)` is the context-aware variant —
boot uses it so a shutdown signal cancels a migration that's waiting on
the advisory lock instead of hanging.

### Running both paths (and turning boot-time DDL off)

A generated app runs both paths on boot, and that layering is
intentional, not an accident: auto-migrate converges the schema with the
entity declarations (additive DDL only), while the versioned runner
applies everything the declarations can't express — backfills,
constraint tightening, destructive changes. They can't disagree on DDL
because both derive column types from the same entity schema.

For deployments whose policy forbids **any** unattended schema change on
boot, make migrations the single, explicit path:

```go
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithoutAutoMigrate(), // Start performs no DDL
)
```

then fold entity drift into the versioned files as part of your release
step — `gofastr migrate generate <name>` emits the drift as a reviewable
numbered migration, `gofastr migrate up` applies it, `gofastr migrate
status` shows what's pending. Entity seeds still run at Start (idempotent data, not
schema); a seeded entity whose table is missing fails Start fast instead
of the app serving against an unmigrated database.

`WithoutAutoMigrate` suppresses **entity** DDL. A few framework-owned
bookkeeping tables are still created on demand regardless — the seed
ledger (`seed_ledger`), and, when you enable `WithOutbox`, the outbox
table (`event_outbox`), which is ensured when the outbox is constructed
at `NewApp` time. These aren't entity schema and aren't emitted by
`migrate generate`; if your policy needs every table to originate from a
reviewed migration, add these to your migration set by hand. For the
outbox specifically you can opt out of the boot-time create with
`framework.WithOutbox(outbox.WithoutEnsureTable())` and manage
`event_outbox` yourself (its schema is in the [events](events.md) doc);
otherwise the framework issues its `CREATE TABLE IF NOT EXISTS` at boot.
A DB role with no DDL rights makes `NewApp` fail closed rather than
silently skip them.

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

The migration system is meant for production use, not just local
development convenience. What it guarantees:

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
  unless the caller opts in (`ApplySchemaDiffWithOptions`). The default
  never drops data; `migrate generate` instead emits the drop as a
  reviewable, reversible versioned migration you apply deliberately.
- **Baseline / recovery.** `migrate force <V>` (programmatically
  `Migrator.Force(ctx, version, applied)`) marks a version applied
  without running it (adopt an existing database) or removes it (treat
  as pending) — and clears any dirty flag either way.

This is the same feature set as other established migrators
(golang-migrate, goose, Atlas): versioned up/down, advisory locking,
transactional runs with a no-transaction escape hatch, dirty state,
checksums/drift detection, baseline, and a destructive-change safety
gate.

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
