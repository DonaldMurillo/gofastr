# Data export & import

`App.ExportData` / `App.ImportData` dump every entity's rows (plus every
registered battery table) to a portable archive and restore it with
validation. This is a **data** export — anti-lock-in for the rows you own —
and is distinct from `ExportStatic`, which renders the site to static HTML.

```go
app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", framework.EntityConfig{ … })
// …register entities, batteries, migrate…

// Dump everything to a directory:
if err := app.ExportData(context.Background(), "/var/backups/app-2026-07-12"); err != nil {
    log.Fatal(err)
}

// Restore into a fresh database:
if err := app.ImportData(context.Background(), "/var/backups/app-2026-07-12"); err != nil {
    log.Fatal(err)
}
```

## Why raw, not the CRUD pipeline

Export/import is an **operator/admin** operation. It round-trips data
**faithfully** — original primary keys, `created_at`/`updated_at`, `owner_id`,
`tenant_id`, hidden columns, and soft-deleted rows included. The CRUD
pipeline can't do this: `ListAll` is owner/tenant/soft-delete scoped and drops
hidden columns, and `BatchCreateMany` regenerates ids, stamps tenant/owner, and
re-validates. Regenerating ids on import would break every cross-entity
foreign key.

So export reads raw (`SELECT <all physical columns> FROM <table>`, all rows,
paged by primary-key keyset) and import writes raw (parameterized `INSERT`
preserving every column value verbatim). No hooks, no validation, no
auto-generation — the data was already valid when it was written.

## Battery tables (outside the registry)

Batteries own physical tables the entity registry doesn't know about
(`auth_sessions`, `queue_jobs`, …). A registry walk alone misses them, so a
battery registers its tables into the `datexport` registry from `init()`:

```go
package mybattery

import "github.com/DonaldMurillo/gofastr/framework/datexport"

func init() {
    datexport.Register(datexport.DataExporter{
        Name:       "my_things",        // unique archive key + ndjson stem
        Source:     "mybattery",        // manifest provenance
        Table:      "my_things",        // physical table
        PrimaryKey: "id",               // keyset-paging column
        Columns:    []string{"id", "kind", "payload"},
    })
}
```

`battery/auth` (auth_users, auth_sessions) and `battery/queue` (queue_jobs)
register themselves this way; importing the battery == including its tables.
**Unregistered raw tables are silently excluded** — a battery or app with
custom tables registers an exporter to be included. A registered table that is
absent from the live DB is skipped with a note (e.g. the auth battery is
imported but the host didn't create that table).

A user table that is ALSO a registry entity is already covered by the registry
walk; the framework dedups by table name, so it is never exported twice.

## Archive layout

```
<dir>/
├── manifest.json          # format version, created_at, per-source metadata, schema fingerprint
├── posts.ndjson           # one JSON row object per line
├── users.ndjson
├── auth_sessions.ndjson
└── queue_jobs.ndjson
```

### Manifest

```json
{
  "format": "gofastr-data-v1",
  "created_at": "2026-07-12T00:00:00Z",
  "entities": [
    {
      "name": "posts",
      "source": "entity",
      "table": "posts",
      "primary_key": "id",
      "row_count": 1283,
      "sha256": "ab12…",
      "columns": ["id", "title", "body", "created_at", "updated_at"]
    }
  ],
  "schema": { "tables": { "posts": { "id": "TEXT", "title": "TEXT", … } } }
}
```

`created_at` is **caller-supplied**, not `time.Now`, so an archive is
reproducible:

```go
app.ExportData(ctx, dir, framework.WithExportTime(someFixedTime))
```

`schema` is the `migrate.SchemaSnapshot` of the entity registry at export time
— a column→type fingerprint for compatibility inspection. (Import recomputes
the live column set rather than trusting the manifest, so the fingerprint is
provenance, not an authority.)

## Staged import (validate before write)

Import validates the **whole** archive before writing a single row, then
writes every source inside one transaction (rollback on any error). Each of
these is rejected up front, leaving the database untouched:

- missing or unparseable `manifest.json`;
- unsupported `format` version;
- a source that isn't a live entity or registered exporter;
- an archive table name that doesn't match the live table;
- an archive column absent from the live schema (incompatible column set);
- a per-file `sha256` that doesn't match the `.ndjson` bytes (corrupt/tampered).

Restore into an empty (or freshly migrated) database — import uses plain
`INSERT` and will fail loud on a primary-key conflict if a row already exists,
rolling the whole thing back.

## SQL safety

Table and column names must be interpolated into SQL (identifiers can't be
`$1` placeholders), so every one is **whitelisted**: names are derived from the
registry schema (`entity.GetTable` / `entity.GetFields`) or a registered
`DataExporter`, and each passes through `core/query.SafeIdent` before
`core/query.QuoteIdent`. Archive table/column names are **never** trusted into
SQL — they are checked against the live known set first and unknown ones are
rejected. All row values are `$n` bound arguments. This is why a malicious or
corrupt archive cannot inject SQL: a smuggled identifier is rejected at the
membership check before any query is built.

## Registering a renamed or custom table

A battery's table name is sometimes host-configured (e.g. the auth user table
is commonly `users` or `auth_users`). The registered entries cover the
canonical names. If you renamed a table, register the actual name from your
app's `main.go` (or a `main`-owned `init`) so it is included:

```go
datexport.Register(datexport.DataExporter{
    Name: "my_users", Source: "app", Table: "users",
    PrimaryKey: "id", Columns: []string{"id", "email", "roles"},
})
```

## Operational notes

- **SQLite in-memory**: a `:memory:` DB needs `db.SetMaxOpenConns(1)` so export
  and import share the same database (each pool connection otherwise gets its
  own private `:memory:`). A file-backed SQLite or Postgres DB has no such
  constraint.
- **Cross-dialect restore**: an archive is portable across SQLite and Postgres
  at the row level, but Postgres enforces column types strictly — bind a string
  into a `TIMESTAMPTZ` column and it parses; bind a JSON number into a typed
  column and it must fit. Validate a cross-engine restore against the target
  dialect before relying on it.
- **Not a substitute for DB-native backup**. This is application-level,
  declaration-aware portability (and anti-lock-in). For point-in-time disaster
  recovery, use your database's own backup tooling.

## Common mistakes

- **Importing into a non-empty database.** Import preserves original
  primary keys, so restoring on top of existing rows conflicts. Import into
  a fresh/empty schema (the transaction rolls back cleanly on conflict).
- **Expecting the CRUD pipeline to run.** Import writes raw to preserve
  ids/timestamps/owner/tenant faithfully — validators, hooks, and
  auto-generated fields do NOT fire. It restores already-valid data; it is
  not an ingestion endpoint for untrusted input.
- **Forgetting battery tables.** A registry walk only sees declared
  entities. Battery-owned tables (auth, queue) are included because those
  batteries register exporters; a custom raw table needs its own
  `datexport.Register` to be in the archive.
- **Editing an archive by hand.** The manifest carries a SHA-256 per file;
  a hand-edited NDJSON fails the checksum and the whole import is rejected
  before any write.
