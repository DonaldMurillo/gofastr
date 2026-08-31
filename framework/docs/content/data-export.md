# Data export & import

`App.ExportData` / `App.ImportData` dump every entity's rows (plus every
registered battery table) to a portable archive and restore it with
validation. This is a **data** export, anti-lock-in for the rows you own,
and is distinct from `ExportStatic`, which renders the site to static HTML.

`App.EraseUserData` is the matching **erasure** primitive (GDPR
right-to-be-forgotten): it expunges a user's rows across the same surfaces and
anonymizes their actor reference in the audit trail. It is documented in the
[Data erasure](#data-erasure) section below.

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
**faithfully**: original primary keys, `created_at`/`updated_at`, `owner_id`,
`tenant_id`, hidden columns, and soft-deleted rows included. The CRUD
pipeline can't do this: `ListAll` is owner/tenant/soft-delete scoped and drops
hidden columns, and `BatchCreateMany` regenerates ids, stamps tenant/owner, and
re-validates. Regenerating ids on import would break every cross-entity
foreign key.

So export reads raw (`SELECT <all physical columns> FROM <table>`, all rows,
paged by primary-key keyset) and import writes raw (parameterized `INSERT`
preserving every column value verbatim). No hooks, no validation, no
auto-generation. The data was already valid when it was written.

## Battery tables (outside the registry)

Batteries own physical tables the entity registry doesn't know about
(`auth_sessions`, `queue_jobs`, …). A registry walk alone misses them, so a
battery registers its tables into the `datexport` registry from `init()`:

<!-- gofastr:compile
-->
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
**Unregistered raw tables are silently excluded**: a battery or app with
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

`schema` is the `migrate.SchemaSnapshot` of the entity registry at export
time, a column→type fingerprint for compatibility inspection. (Import recomputes
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

Restore into an empty (or freshly migrated) database. Import uses plain
`INSERT` and will fail loud on a primary-key conflict if a row already exists,
rolling the whole thing back.

## SQL safety

Table and column names must be interpolated into SQL (identifiers can't be
`$1` placeholders), so every one is **whitelisted**: names are derived from the
registry schema (`entity.GetTable` / `entity.GetFields`) or a registered
`DataExporter`, and each passes through `core/query.SafeIdent` before
`core/query.QuoteIdent`. Archive table/column names are **never** trusted into
SQL. They are checked against the live known set first and unknown ones are
rejected. All row values are `$n` bound arguments. This is why a malicious or
corrupt archive cannot inject SQL: a smuggled identifier is rejected at the
membership check before any query is built.

## Registering a renamed or custom table

A battery's table name is sometimes host-configured (e.g. the auth user table
is commonly `users` or `auth_users`). The registered entries cover the
canonical names. If you renamed a table, register the actual name from your
app's `main.go` (or a `main`-owned `init`) so it is included:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/datexport"
-->
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
  at the row level, but Postgres enforces column types strictly: bind a string
  into a `TIMESTAMPTZ` column and it parses; bind a JSON number into a typed
  column and it must fit. Validate a cross-engine restore against the target
  dialect before relying on it.
- **Not a substitute for DB-native backup**. This is application-level,
  declaration-aware portability (and anti-lock-in). For point-in-time disaster
  recovery, use your database's own backup tooling. See
  [Backups and restore](backups.md).

## Common mistakes

- **Importing into a non-empty database.** Import preserves original
  primary keys, so restoring on top of existing rows conflicts. Import into
  a fresh/empty schema (the transaction rolls back cleanly on conflict).
- **Expecting the CRUD pipeline to run.** Import writes raw to preserve
  ids/timestamps/owner/tenant faithfully: validators, hooks, and
  auto-generated fields do NOT fire. It restores already-valid data; it is
  not an ingestion endpoint for untrusted input.
- **Forgetting battery tables.** A registry walk only sees declared
  entities. Battery-owned tables (auth, queue) are included because those
  batteries register exporters; a custom raw table needs its own
  `datexport.Register` to be in the archive.
- **Editing an archive by hand.** The manifest carries a SHA-256 per file;
  a hand-edited NDJSON fails the checksum and the whole import is rejected
  before any write.

## Data erasure

`App.EraseUserData(ctx, userID, opts...)` is the right-to-be-forgotten
primitive. It mirrors `ExportData`'s two-plane design, the entity registry
plus the `datexport` registry, so an erasure reaches exactly the tables an
export does, and adds a third, built-in plane for the audit trail.

```go
report, err := app.EraseUserData(ctx, "user_42")
if err != nil { return err }
log.Printf("erased %d rows for user_42", report.TotalErased())
```

### The three planes

1. **Entity plane.** Every owner-scoped entity (`Scope.OwnerField` set) is
   hard-deleted: `DELETE FROM <table> WHERE <owner_field> = $1`. The delete is
   raw, so it removes soft-deleted rows too: a row the user previously
   "deleted" is now actually expunged. Erasure means erasure: no tombstone, no
   undo. Entities without an `OwnerField` hold no per-user data and are
   skipped.
2. **Battery plane.** Every registered `datexport.DataEraser` runs. A battery
   declares each table it owns and how to erase it: hard-delete, or anonymize
   (overwrite named columns with a tombstone and keep the row). `battery/auth`
   registers `auth_sessions` (delete by `user_id`), `auth_users` (delete by
   `id`), and `magic_link_tokens` (delete by `email` via the `IdentityEmail`
   resolver, see [Identity-keyed tables](#identity-keyed-tables-non-user-id-match) below).
   `battery/queue` registers `queue_jobs` (delete by `user_id`), which reaches
   a job only when the producer set `Job.UserID` — see
   [the queue reference](queue.md). A job whose payload is personal data but
   which names no user is not erasable, and its payload column is exported
   verbatim.
3. **Audit plane (built-in).** The audit table is retained, since it is the
   compliance record of who did what, but the user's `actor_id` is
   anonymized: every row where `actor_id = userID` is set to `[erased]`. See
   [Audit retention](#audit-retention) below.

### Audit retention

Audit rows are **not deleted**. This is deliberate and is the industry-standard
posture for a compliance trail. Instead the personal link is cut:

- `actor_id` (the *who*) is overwritten with `[erased]` for every row the
  erased user acted from.
- `record_id` (the *what*) is left intact. It is heterogeneous, a resource id
  for CRUD events, sometimes a user id for auth events, and records which
  object was acted on, which is legitimate audit content. Blanket-anonymizing
  it would also destroy the resource ids that make the trail useful.

The default audit table is `audit_log` (matching `EnsureAuditTable`). If your
app renamed it via `AuditConfig.Table`, pass the name to `EraseUserData`:

```go
app.EraseUserData(ctx, uid, framework.WithEraseAuditTable("my_audit"))
```

### EraseReport

`EraseUserData` returns a structured summary so a host can log or persist what
was erased:

```go
type EraseReport struct {
    DryRun    bool
    Entities  []EraseTableResult // owner-scoped entity tables (always delete)
    Batteries []EraseTableResult // registered erasers (delete or anonymize)
    Audit     *EraseTableResult  // built-in audit anonymization; nil if the audit table is absent
    Skipped   []string           // erasers skipped because their identity could not be resolved (idempotent re-run)
```

Each `EraseTableResult` carries the table name, the mode (`delete` or
`anonymize`), and the row count. `report.TotalErased()` sums every plane.
`Skipped` names identity-resolved erasers that were skipped because the
resolved identity row was already gone, the idempotent-re-run case (see
"Identity-keyed tables" above): skipping is not an error, and the erasure
reports zero for those tables rather than failing.

### Dry-run

`WithEraseDryRun()` switches the call to count-only: the report carries the
rows that *would* be affected, but nothing is deleted or scrubbed. Use it for a
compliance review before committing to an erasure.

```go
dry, _ := app.EraseUserData(ctx, uid, framework.WithEraseDryRun())
// dry.TotalErased() == the count a real call would erase; DB unchanged
```

### Idempotency

`EraseUserData` is idempotent: a second call for the same user matches zero
rows and returns a zero report without error. The delete plane is naturally
idempotent (the rows are gone). The anonymize plane is idempotent by count
because it scrubs its match column too: once `user_id` (or `actor_id`) holds
the tombstone, a re-run's `WHERE` matches nothing.

### Registering an eraser

A battery or app registers a table for erasure the same way it registers one
for export. Two modes:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/datexport"
-->
```go
// Hard-delete every row owned by the user.
datexport.RegisterEraser(datexport.DataEraser{
    Name: "orders", Source: "billing", Table: "orders",
    Column: "customer_id", Mode: datexport.EraseDelete,
})

// Keep the row, scrub the personal columns.
datexport.RegisterEraser(datexport.DataEraser{
    Name: "support_tickets", Source: "helpdesk", Table: "support_tickets",
    Column: "reporter_id", Mode: datexport.EraseAnonymize,
    ScrubColumns: []string{"email", "phone"},
    Tombstone:    "[redacted]",   // empty defaults to "[erased]"
})
```

`Column` is the user-id column matched against the erased user's id. For
`EraseAnonymize`, both `ScrubColumns` and `Column` are overwritten with
`Tombstone`. As with export: a registered table that is absent from the live
DB is skipped with a note; an unregistered raw table is silently excluded.

### Identity-keyed tables (non-user-id match)

Most tables are reached by the user id: `Column` holds the user id directly.
Some tables are keyed by a *different* identity: `battery/auth`'s
`magic_link_tokens` is keyed by **email**, not user id, so a plain user-id match
cannot reach it. Before this seam, a magic link minted before an erasure and
redeemed after it found no user and *created a new account* for the erased
address, an account-restoration path straight through a completed erasure.

An eraser declares the identity with `Identity`; the framework resolves it ONCE
at erase time (before the write transaction opens) through a registered
`DataIdentityResolver` and binds the resolved value for `Column` instead of the
user id:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/datexport"
-->
```go
// Resolve email from the user table: SELECT email FROM auth_users WHERE id = $1
datexport.RegisterIdentityResolver(datexport.IdentityEmail, datexport.DataIdentityResolver{
    Table: "auth_users", IDColumn: "id", ValueColumn: "email",
})

datexport.RegisterEraser(datexport.DataEraser{
    Name: "magic_link_tokens", Source: "auth", Table: "magic_link_tokens",
    Column: "email", Mode: datexport.EraseDelete,
    Identity: datexport.IdentityEmail, // default IdentityUserID matches by user id
})
```

Resolution stays **declarative**: the framework remains the single place raw
SQL is built (`SafeIdent`-guarded identifiers, `$n`-bound values); a battery
never runs arbitrary SQL. Two guarantees:

- **No resolver for a declared identity → fail loud.** An erasure that cannot
  reach a declared table is incomplete, and silently-incomplete is the failure
  mode this primitive exists to prevent.
- **Resolver finds no row → skip, don't fail.** On an idempotent re-run the
  user row is already gone, so the identity cannot be resolved. That means
  "nothing left to match": the eraser is skipped (named in `report.Skipped`),
  the rest of the erasure reports zero, and no error is returned.

`battery/auth` registers the `IdentityEmail` resolver and the `magic_link_tokens`
eraser from `init()`, so any app importing `battery/auth` closes the gap
automatically. The token table is created lazily by
`NewSQLMagicLinkTokenStore`; on the in-memory token store there is no such table
and the eraser is a no-op. A host that renames the user table must re-register
the resolver against the actual name, mirroring how a renamed eraser table is
re-registered.

### SQL safety

Erasure rides the same identifier firewall as export: every table and column
name is registry- or eraser-derived and passes through `core/query.SafeIdent`
before `core/query.QuoteIdent`; the user id and the tombstone are `$n` bound
arguments. A misconfigured eraser fails loud at `SafeIdent` rather than
interpolating an unsafe name. The whole write runs inside one transaction and
rolls back on any error.

### Common mistakes (erasure)

- **Expecting soft-delete to protect rows.** Erasure is a raw hard-delete that
  ignores `deleted_at`. If you need a recoverable "soft erasure," do it in app
  code. `EraseUserData` is for the irrevocable case.
- **Forgetting battery tables.** Just as with export, a registry walk only
  sees declared entities. `battery/auth` registers its tables; a custom raw
  table with per-user data needs its own `datexport.RegisterEraser`, or it is
  silently left in place.
- **Renaming the audit table without telling erasure.** The built-in audit
  plane defaults to `audit_log`. A renamed audit table must be passed via
  `WithEraseAuditTable`, or the user's `actor_id` is left intact.
