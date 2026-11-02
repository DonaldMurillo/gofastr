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

Export streams. Each page is written to its `.ndjson` file, and hashed on
the way, before the next page is read, so `WithExportPageSize` (1000 rows
by default) bounds how many rows an export holds at once. A table with no
key is read in one statement and still reaches the file row by row.
Import reads each file whole before it writes, because it validates the
entire archive first.

Timestamps keep the spelling a filter or cursor matches. SQLite compares a
time column as text, so the export reads a column declared `DATE`,
`DATETIME` or `TIMESTAMP` as the row stores it, and a battery's
`2024-01-03 10:00:00+00:00` restores byte for byte. Postgres hands the
export an instant, which leaves in the layout crud writes a `Timestamp`
in (`2024-01-03T10:00:05.120000Z`), or `2024-01-03` for a `DATE`. On
import, an RFC 3339 value in a column an entity declares `Timestamp` is
rewritten in that layout, and a midnight-UTC one in a `Date` column as
`YYYY-MM-DD`. Archives written before v0.96.0 hold trimmed spellings
(`...05.12Z`, `...T00:00:00Z` for a date), which a SQLite filter bound in
the canonical layout missed. A battery table's values, and a value of
any other shape, restore as the archive holds them.

## Battery tables (outside the registry)

Batteries own physical tables the entity registry doesn't know about
(`auth_sessions`, `queue_jobs`, …). A registry walk alone misses them, so a
battery registers its tables into the `datexport` registry from `init()`:

<!-- gofastr:compile
-->
```go
package mybattery

import "github.com/DonaldMurillo/gofastr/core/datexport"

func init() {
    datexport.Register(datexport.DataExporter{
        Name:       "my_things",        // unique archive key + ndjson stem
        Source:     "mybattery",        // manifest provenance
        Table:      "my_things",        // physical table
        PrimaryKey: "id",               // keyset-paging column; "" = no key, "a,b" = composite
        Columns:    []string{"id", "kind", "payload"},
    })
}
```

An empty `PrimaryKey` means the table has no key and is read in one pass; a
comma-joined value (`"provider,provider_id"`) is a composite key.
`battery/auth` (users, sessions, 2FA enrolments, OAuth links, passkeys) and
`battery/queue` (queue_jobs) register themselves this way; importing the battery == including its tables.
A store whose table name is configurable registers from its constructor
instead, under the name it was given, so a renamed table is the one
exported and erased: the auth API-token and OAuth-token stores, admin saved
views, the A2A task and push-config stores, feature flags, the SQL
idempotency store, the event outbox, the webhook delivery and inbound
stores, the entity admin's bulk jobs, and the RBAC `access.GrantStore`
(its grants and its revocation tombstones, registered by `EnsureSchema`;
restart after an import so `LoadInto` reads them). Registration replaces an entry
with the same `Name`, so these stores name each entry with
`datexport.ScopedName(base, table)`: the default table keeps the bare name
(`feature_flags.ndjson`), and any other table is appended after a `-`
(`feature_flags-rollouts.ndjson`), so two stores on two tables both stay
registered. The table's ASCII letters are lowercased first, the way
AutoMigrate folds them: `Rollouts` and `rollouts` are one table and get one
name. A name longer than 200 bytes is cut to that length and ends in
16 hex digits of the table name's SHA-256, so the file name stays inside
one 255-byte path component. A store of your own that a host can build
twice does the same.
`gofastr verify` reports a store
table that holds a user column and is registered for neither plane
(GOFASTR1415).
**Unregistered raw tables are silently excluded**: a battery or app with
custom tables registers an exporter to be included. A registered table that is
absent from the live DB is skipped with a note (e.g. the auth battery is
imported but the host didn't create that table). On Postgres the presence
check reads the current schema only (`current_schema()`, the first schema on
the `search_path`), so a same-named table in another schema does not count;
a schema-qualified name (`app.widgets`) is looked up in its own schema.
Entities and exporters whose tables differ only in case are one source,
named after the entity that sorts first.

A user table that is ALSO a registry entity is exported once, as the entity.
An exporter's columns that the entity does not declare join that source
(compared case-insensitively), so auth's `name` column on an entity user
table is exported and restored with the rest of the row.

Tables compare by what the database resolves each spelling to, so an
exporter configured as `public.users` (or `main.users` on SQLite) is the
`users` entity's table and joins its source. Two entities that spell one
table differently are refused with an error naming both. On SQLite a
schema-qualified table reads the `sqlite_master` of its own schema; a
schema the connection has not attached holds no table, and its exporter is
skipped as absent.

## Archive layout

```
<dir>/
├── manifest.json          # format version, created_at, per-source metadata, schema fingerprint
├── posts.ndjson           # one JSON row object per line
├── users.ndjson
├── auth_sessions.ndjson
└── queue_jobs.ndjson
```

The export directory is created `0700` and every file in it is written
`0600`: an archive is a raw dump of every physical column — including
`password_hash`, token hashes, and sessions on a real app — so it gets
owner-only file permissions like every other secret-bearing artifact the
framework writes.
Existing export directories are tightened before writing. On Windows, the
directory and files receive protected owner-only ACLs instead of Unix modes.

### Manifest

```json
{
  "format": "gofastr-data-v2",
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

A binary column value (a SQLite BLOB, a Postgres `bytea`) is written to
the NDJSON as `{"$bytes":"<base64>"}` and restored as bytes. Import
refuses any other JSON object or array in a column value. A
`gofastr-data-v1` archive, which has no binary tag, still imports; an
older binary refuses a `v2` archive. Restore advances the Postgres
sequence of every SERIAL column it wrote, a registered battery table's
included.

`primary_key` is the keyset column, or for a composite key the key columns
joined by commas (`"student_id,course_id"`); the export pages those by
row value. A raw `migrate.Table` with no primary key has `""` here: there is
nothing to page by, so the export reads the whole table in one statement.

`source` is `"entity"` for an entity table, `"pivot"` for a many_to_many pivot
table that AutoMigrate creates itself (named by its table, with its two link
columns as both its columns and its key), or the battery name for a registered
exporter. Sources are listed parents first: a table comes after every table
its `BelongsTo` foreign keys reference, and a pivot after both its ends.
Tables with no dependency between them stay in table-name order, and battery
exporters come last.

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
- a source whose table is not in the live database;
- an archive table name that doesn't match the live table;
- an archive column absent from the live schema (incompatible column set);
- a source whose archive columns leave out a primary-key column (a keyless
  raw table has none to leave out);
- a per-file `sha256` that doesn't match the `.ndjson` bytes (corrupt/tampered).

A row whose primary-key value is null or missing is refused too, in the
write phase, and the transaction rolls back. Without these two checks a key
column was written NULL, which SQLite accepts in a non-integer key (the
restored rows can never be read by key), or Postgres minted new ids that no
reference pointed at.

Restore into an empty (or freshly migrated) database. Import uses plain
`INSERT` and will fail loud on a primary-key conflict if a row already exists,
rolling the whole thing back.

The write order comes from the live registry, not the archive listing, so an
archive written before this order existed restores too:

- **Parents first.** Each table is written after the tables its foreign keys
  reference, and a pivot table after both its ends, so no foreign key
  constraint refuses a row whose parent has not landed yet.
- **Self-references.** In a table that references itself (a comment's
  `parent_id`), rows are written parent row first, whatever their key order.
  Rows that reference each other in a cycle (two comments naming each
  other) have no such order: the row that closes the cycle is written with
  that reference empty, and an `UPDATE` sets it once the table's rows are
  all in. The column must accept NULL for that moment.
- **Cycles of tables.** Two tables whose foreign keys name each other
  (`a.b_id` → `b`, `b.a_id` → `a`) have no parents-first order either. The
  table written first goes in with its reference to the later one empty,
  and an `UPDATE` sets it once every source is written. Again the column
  must accept NULL.
- **Exact numbers.** Numbers are read as written: an integer that fits in
  `int64` is bound as an integer, so `9007199254740993` restores as
  `9007199254740993` and not as the nearest `float64`. A larger integer is
  bound as its decimal text, and anything else as a `float64`.
- **Postgres sequences.** After writing a table with an
  `AutoGenerate: schema.AutoIncrement` column, import moves that column's
  sequence to the largest restored value (`setval(pg_get_serial_sequence(…),
  MAX(col))`), so the next generated id follows the restored ones instead of
  colliding with them. On an empty table the sequence restarts at its
  minimum (1 by default), and so it does when every restored id sits below
  that minimum (an id of 0, or negative ids): `setval` refuses a value under
  the minimum, which is already above every restored id. SQLite needs no
  step: its rowid already follows explicit ids.

## SQL safety

Table and column names must be interpolated into SQL (identifiers can't be
`$1` placeholders), so every one is **whitelisted**: names are derived from the
registry schema (`entity.GetTable` / `entity.GetFields`) or a registered
`DataExporter`, and each passes through `core/query.SafeIdent` before it is
quoted. A table is quoted as the catalog spells it, each dot-separated part on
its own. AutoMigrate's DDL leaves names unquoted and Postgres folds them, so an
entity declared as `Widgets` with a `UserName` field lives in `widgets.username`,
and the export, import and erasure name it that way. A battery may quote its
table and keep the case, so an exporter's or eraser's table is matched by its
own spelling first and its folded one second, never another case variant. Archive table/column names
are **never** trusted into SQL. They are checked against the live known set first and unknown ones are
rejected. All row values are `$n` bound arguments. This is why a malicious or
corrupt archive cannot inject SQL: a smuggled identifier is rejected at the
membership check before any query is built.

## Registering a renamed or custom table

A battery's table name is sometimes host-configured (e.g. the auth user table
is commonly `users` or `auth_users`). The auth battery re-registers its
exporters against the tables its configured stores use when it starts, so
`NewEntityUserStore(db, "users")` exports `users`. For any other battery or
app table you renamed, register the actual name from your
app's `main.go` (or a `main`-owned `init`) so it is included:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/datexport"
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
- **Exporting a live app**: on Postgres the export reads every table in one
  read-only `REPEATABLE READ` snapshot, so the archive is the database at a
  single moment, however busy the app is, and writers are never blocked. On
  SQLite each table is read on its own: stop writes during the export, or a
  child row written after its parent's table was read can land in the archive
  without its parent.
- **Cross-dialect restore**: an archive is portable across SQLite and Postgres
  at the row level, but Postgres enforces column types strictly: bind a string
  into a `TIMESTAMPTZ` column and it parses; bind a JSON number into a typed
  column and it must fit. Validate a cross-engine restore against the target
  dialect before relying on it.
- **Not a substitute for DB-native backup**. This is application-level,
  declaration-aware portability (and anti-lock-in). For point-in-time disaster
  recovery, use your database's own backup tooling. See
  [Backups and restore](backups.md).

## What this does not do

Two jobs look close to export and import and are deliberately left to
the app:

- **Copying rows in from another database.** `ImportData` reads only
  archives in the GoFastr archive format (the layout `ExportData`
  writes), and `core/migrate` never connects to a second database: its
  SQL files can backfill rows inside the app's own database, not copy
  them in from another one. Moving rows from a legacy database is a one-time job
  whose column mapping only the app knows. Write it as a small program:
  open both databases with `database/sql`, read from the old one, and
  insert into the new one inside one transaction on the target
  (`db.BeginTx`), so a failed run leaves nothing half-copied. Run it
  once, after `AutoMigrate` or `migrate up` has created the tables.
  Writing through the entity routes instead would run hooks, validation
  and audit for every row; copy raw unless you want that.
- **Reading spreadsheets.** GoFastr ships no XLSX (Excel) reader and
  will not: it is a large file format to parse safely, and maintained Go
  libraries already do it. Use one such as
  [excelize](https://github.com/xuri/excelize), turn each row into your
  entity's fields, and write them as above. Cap the upload size
  (`http.MaxBytesReader`) before you open the file: an XLSX is a zip, and
  a small upload can expand to a large one.

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
   undo. Rows that reference an erased row through a foreign key go with it,
   in any table and whoever owns them; see [Cascade](#cascade) below.
2. **Battery plane.** Every registered `datexport.DataEraser` runs. A battery
   declares each table it owns and how to erase it: hard-delete, or anonymize
   (overwrite named columns with a tombstone and keep the row). `battery/auth`
   registers `auth_sessions` (delete by `user_id`), `auth_users` (delete by
   `id`), `auth_twofa`, `users_oauth_links` and `auth_passkeys` (each delete
   by `user_id`), and `magic_link_tokens` (every magic-link, reset,
   verification and passkey-registration token minted for the user; the
   magic-link one matches by email via the `IdentityEmail` resolver, see
   [Identity-keyed tables](#identity-keyed-tables-non-user-id-match) below).
   `battery/queue` registers `queue_jobs` (delete by `user_id`), which reaches
   a job only when the producer set `Job.UserID` — see
   [the queue reference](queue.md). A job whose payload is personal data but
   which names no user is not erasable, and its payload column is exported
   verbatim. The other stores, each under its configured table name:

   | Store | Erased by | Mode |
   |---|---|---|
   | `auth_api_tokens` | `owner_id` where `owner_kind = 'user'` | delete |
   | `oauth_tokens` | `user_id` | delete |
   | the per-account login limiter's `SQLRateLimitStore` (both tables) | `rl_key`, the limiter scope plus `account:` plus the email (`IdentityEmail`) | delete |
   | `gofastr_embed_nonces` (`embed.SQLBurnStore`) | `subject` (the grant's subject) | anonymize: `grant_token` overwritten, the row kept as the nonce's tombstone |
   | `admin_saved_views` | `owner` | delete |
   | `a2a_tasks`, `a2a_push_configs` | `owner` | delete |
   | `feature_flags` | the user's id in the `users` allow list | remove member |
   | `idempotency_keys` | `principal` (the cached response's caller) | delete |
   | `event_outbox` | `owner` (the record's owner) | anonymize: `payload` set to JSON `null` |
   | `webhook_deliveries` | `owner` (`Manager.PublishFor`) | delete |
   | `webhook_inbound` | `owner` (`IngestConfig.Owner`) | delete |
   | the durable scheduler's schedules (`<queue table>_scheduler_schedules`) | `user_id` (`DurableScheduleBuilder.UserID`) | delete |
   | entity admin bulk jobs | `creator` | anonymize |
   | search indexes, pgvector chunks | `owner` (`Document.Owner`) | delete |

   Search and semantic indexes register an eraser and no exporter: they are
   rebuilt from the records that are exported, not restored. A row a store
   wrote with an empty owner names nobody and stays. Stores outside the
   database (a semantic index's files or memory, a Redis or in-memory
   queue, an in-memory scheduler) are erased after the transaction
   commits; see
   [Stores outside the database](#stores-outside-the-database).
3. **Audit plane (built-in).** The audit table is retained, since it is the
   compliance record of who did what, but the user's `actor_id` is
   anonymized (every row where `actor_id = userID` is set to `[erased]`) and
   the `diff` that copies the user's data is replaced. See
   [Audit retention](#audit-retention) below.

### Cascade

Erasure follows foreign keys the way `ON DELETE CASCADE` would. A row whose
`BelongsTo` key points at a row being erased is erased too, whether or not its
table is owner-scoped and whoever owns it: the items of a user's list (an
entity with no `OwnerField`), another user's comment on the user's post, the
replies under that comment, and the pivot rows of a many_to_many. The walk is
recursive and runs in the same transaction, deleting children before parents,
so the constraints AutoMigrate generates never refuse the delete.

The walk starts from two kinds of row: the rows of owner-scoped tables, and
the rows a delete eraser (`datexport.EraseDelete`) matches on a table the
registry declares, such as a host entity mapped onto the user table that
`battery/auth` deletes from. A profile that belongs to that user row is
staged with it, so its stored files are deleted and its audit diffs scrubbed
the same way. The database's own `ON DELETE CASCADE` no longer removes it
unseen. The eraser's `RowsAffected` counts the rows it matched; the profile
counts in `Cascaded`. A matched row with a NULL in its key is left to the
eraser's own delete, since no foreign key can name it. The eraser's table
matches the entity's by the table the catalog resolves each name to, so an
eraser on `public.users` (`main.users` on SQLite) seeds the walk from an
entity whose table is `users`. The entity's name resolves the way
AutoMigrate's unquoted DDL folds it and the eraser's name resolves as its
own table is spelled, and on Postgres the two compare with their case kept:
an eraser on a quoted `"Users"` a battery created never seeds deletes from
the entity's `users`.

- **Composite keys** are followed column for column: an `order_lines` row is
  reached through `(store, order_no)`, never through one of the two alone.
  A foreign key whose column count does not match the referenced table's key
  is refused with an error before anything is deleted.
- **`OnDelete: entity.OnDeleteSetNull`** relations are not followed: the
  database detaches that child and it survives with a NULL key.
- **`entity.OnDeleteRestrict` and `entity.OnDeleteNoAction`** relations are
  not followed either. Declaring one says a child row blocks its parent's
  delete, so erasure does not delete that row to get around it. If a row the
  erasure does not remove references a row it would, `EraseUserData` returns
  an error naming the relation (`<entity>.<relation>`) and the child table,
  and erases nothing. A dry run refuses the same way. The user's own child
  rows (an owner-scoped child table) do not block: they are deleted before
  the parents they reference. Remove or reassign the blocking rows, or change
  the relation to CASCADE or SET NULL, then erase again. An unset `OnDelete`
  and `entity.OnDeleteCascade` are followed.
- **Unmanaged entities** (`Unmanaged: true`: views, host-owned tables) are not
  reached. A cascade table absent from the live database is skipped.
- **A raw `migrate.Table` with no primary key** is reached through its
  foreign keys like any table. Nothing can reference it, so its rows are
  matched by the foreign key that names an erased row rather than staged by
  key, and they are counted and deleted the same way. Such tables are
  deleted before any other, so a SET NULL relation from one of them cannot
  empty the column the match reads before its rows go.
- **Cycles between erased rows.** When two tables name each other
  (`a.b_id` → `b`, `b.a_id` → `a`), or a table names itself through an
  `entity.OnDeleteRestrict` relation, no delete order satisfies every
  constraint. Erasure empties those references on the rows it erases, then
  deletes them. A reference column declared NOT NULL cannot be emptied, and
  the erasure fails naming the relation.
- **A NULL in a primary key.** SQLite lets a primary key that is not an
  `INTEGER` hold NULL. Such a row cannot be told apart by its key, so an
  erasure that would remove one is refused with an error naming the table,
  and erases nothing. Give the row a key, then erase again.
- **Stored files** named by a cascaded row are deleted with it, under the same
  rules as the user's own rows (see [Stored files](#stored-files)).

The rows the user owns are reported in `Entities`. The rows deleted because
they referenced an erased row are reported in `Cascaded`, one entry per table,
with `Source: "cascade"` and `Mode: "delete"`. A row the user owns that the
cascade also reaches counts once, in `Entities`. Dry-run runs the same
cascade and rolls it back.

There is no per-user export, only the full `ExportData` dump, so nothing on the
export side needs to mirror the cascade.

### Stored files

When the app has `WithFileStorage`, the entity plane also deletes the stored
objects the erased rows name: every `schema.Image` / `schema.File` value and
every `storage_ref` in a `<field>_variants` column. The keys are read before
the rows go and deleted after the transaction commits.

Erasure deletes only objects it can attribute to the erased user:

- An absolute `http(s)` URL is an external link, not a storage key, and is
  never deleted.
- A key that a surviving row still names, in any entity's file or variants
  column (another user's row, or a row of an entity without an
  `OwnerField`), is kept. The CRUD write path refuses a key the caller did
  not upload (see [uploads](uploads.md#storage-keys-on-json-and-in-process-writes)),
  so this only matters for rows written before that check or by host code
  under `WithServerWrites`.

If the check for other references fails, the rows stay erased, no object is
deleted, and `EraseUserData` returns an error.

### Stores outside the database

A `DataEraser` names a SQL table, so it cannot reach data a battery keeps
elsewhere. Such a store registers a `datexport.StoreEraser` instead: a name,
a source, and an `Erase(ctx, userID, dryRun)` func that removes everything
in the store naming the user and returns the count. `EraseUserData` runs
every registered store eraser after the transaction commits and after the
stored files are deleted. Each result is listed in `report.Stores` with
`Mode: "delete"`.

| Store | Registered by (name) | What is erased |
|---|---|---|
| a semantic index opened with `Options.Path` | `semantic.Open` (`semantic:<path>`) | the chunks whose `Owner` is the user; the snapshot is rewritten and the WAL emptied, so no copy of the text stays on disk |
| a semantic index with no `Path` | `semantic.Open` (`semantic:memory`) | the chunks whose `Owner` is the user, from memory |
| `queue.RedisQueue` | `queue.NewRedisQueue` (`queue_redis:<queue>`) | every job whose `UserID` is the user, from the main, delayed and dead lists and the pop and processing hashes |
| `queue.MemoryQueue` | `queue.NewMemoryQueue` (`queue_memory`) | every job whose `UserID` is the user: pending, delayed, handed out by `Dequeue`, and dead; the unique keys they held are freed |
| `queue.Scheduler` (in-memory) | `queue.NewScheduler` (`queue_scheduler_memory`) | every schedule whose `UserID` is the user, so it is neither kept nor fired again |

**A store eraser stays registered until the store closes**: `Index.Close`,
`RedisQueue.Close`, `MemoryQueue.Close` or `Scheduler.Close` removes it.
Until then the registry holds the store, so it is never garbage-collected,
and every erasure reaches it. Close each one you are done with, including
one built for a single request or a test.

Two stores of one name both stay registered and are both erased; the later
one is listed under the first free `#n` suffix (`queue_redis:jobs#2` for a
second `RedisQueue` named `jobs` on another Redis server). An index opened
`ReadOnly` registers nothing, since the process writing the index erases
it. A semantic index over a store shared across processes
(`PgVectorStore`) registers nothing either, since its table has a SQL
eraser, and neither does one over a custom `Store` that cannot list its
chunks (`AllChunks`): register a store eraser for it yourself.

A job a worker is running when the sweep passes is not stopped. If it fails
and is retried after the sweep, it is back in the queue until the next
erasure. The Redis erasure is one Lua script, which the server runs with
no other command in between, so no worker step lands between its reads and
its deletes. A worker that read a job just before the script ran (a job
`Dequeue` popped but has not claimed, a `Nack`, a `Replay`) does not write
it back: the queue's moves between keys are scripts too, and each finds
its source empty and drops the job. It needs an adapter that implements
[`queue.ScriptRunner`](queue.md#optional-scriptrunner); with one that does
not, the store fails and nothing is removed from it. While it runs the
server serves no other client, for a time that grows with the queue's
length.

**Failure.** The SQL erasure has committed by the time a store eraser runs,
so a failure cannot roll it back. Every store eraser runs even when another
fails, and `EraseUserData` returns an error naming each store that failed:
`rows deleted but N store(s) failed and may still hold the user's data`. The
report lists the rows deleted and the stores that succeeded. Erase again
once the store is reachable: the SQL planes match nothing, and each store
removes what is left. A panicking eraser counts as a failed one.

A dry run rolls the transaction back first, then asks each store for its
count with `dryRun` set, so nothing in the store changes.

A custom store registers the same way:

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/core/datexport"
func eraseFromCache(ctx context.Context, userID string, dryRun bool) (int, error) { return 0, nil }
-->
```go
name, unregister := datexport.RegisterStoreEraser(datexport.StoreEraser{
    Name:   "profile_cache",
    Source: "app",
    Erase:  eraseFromCache, // func(ctx, userID, dryRun) (int, error)
})
defer unregister() // when the store closes
_ = name           // "profile_cache", or "profile_cache#2" beside a live one
```

Store erasers run in name order, except that a store which puts items into
other stores sets `Producer: true` and runs first. The in-memory `Scheduler`
is one: erased after the queue it feeds, a tick between the two put the
user's job back into a queue already erased. A producer's `Erase` returns
only after any dispatch it had already started has finished; the
`Scheduler` holds the lock its tick enqueues under.

### Audit retention

Audit rows are **not deleted**. This is deliberate and is the industry-standard
posture for a compliance trail. Instead the personal link is cut:

- `actor_id` (the *who*) is overwritten with `[erased]` for every row the
  erased user acted from.
- `record_id` (the *what*) is left intact. It is heterogeneous, a resource id
  for CRUD events, sometimes a user id for auth events, and records which
  object was acted on, which is legitimate audit content. Blanket-anonymizing
  it would also destroy the resource ids that make the trail useful.
- `diff` (the before and after copy of the row) is replaced with
  `{"erased":true}` on every row the erased user acted from, and on every row,
  whoever acted, that records an erased row (its `entity` and `record_id`
  match a row the entity plane or the cascade deleted, by raw id or by the
  `sha256:` digest a sensitive key is stored as, each with control bytes
  stripped the way the audit log stores them). An admin's edit of the
  user's post keeps its actor and record id but loses the post's text. The
  marker is JSON, so a reader decoding `{"old":…,"new":…}` sees an empty
  change. A NULL diff stays NULL.

`report.Audit.RowsAffected` counts the rows whose actor was anonymized plus the
other rows whose diff was replaced, each row once.

The audit table needs an `actor_id` column, or erasure fails loud. A custom
table with no `diff` column has no diff to scrub, and one with no `entity` or
`record_id` column cannot name the rows that record an erased row, so those
scrubs are skipped there. Two cases are not scrubbed: a row whose `record_id`
a host `Redact` callback rewrote to something other than the raw id or its
digest, and an `auth` event about the user that another actor wrote (its
`record_id` is the user id, not an erased row).

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
    Entities  []EraseTableResult // owner-scoped entity tables: the rows the user owns (always delete)
    Cascaded  []EraseTableResult // rows deleted because a foreign key named an erased row
    Batteries []EraseTableResult // registered erasers (delete or anonymize)
    Audit     *EraseTableResult  // built-in audit plane; nil if the audit table is absent
    Stores    []EraseTableResult // store erasers that ran after the commit (semantic files, Redis)
    Skipped   []string           // erasers skipped because their identity could not be resolved (idempotent re-run)
}
```

Each `EraseTableResult` carries the table name, its source (`entity`,
`cascade`, the battery name, or `audit`), the mode (`delete` or `anonymize`),
and the row count. A `Stores` entry has the store's name and no table.
`report.TotalErased()` sums every plane, the stores included.
`Skipped` names identity-resolved erasers that were skipped because the
resolved identity row was already gone, the idempotent-re-run case (see
"Identity-keyed tables" above): skipping is not an error, and the erasure
reports zero for those tables rather than failing.

### Dry-run

`WithEraseDryRun()` switches the call to count-only: the report carries the
rows that *would* be affected, but nothing is deleted or scrubbed. Use it for a
compliance review before committing to an erasure.

The dry run performs the erasure inside a transaction and rolls it back, and
deletes no stored file. Its counts are the real run's: a row two registered
erasers both reach counts once, as the real run deletes it once. A
constraint that would refuse the real erasure refuses the dry run too. While
it runs it holds the same row locks the real erasure would. The rollback
undoes rows, not what the database does outside a transaction: a trigger
that draws a Postgres sequence value (a `SERIAL` audit row) leaves the
sequence advanced.

```go
dry, _ := app.EraseUserData(ctx, uid, framework.WithEraseDryRun())
// dry.TotalErased() == the count a real call would erase; no row changed
```

### Idempotency

`EraseUserData` is idempotent: a second call for the same user matches zero
rows and returns a zero report without error. The delete plane is naturally
idempotent (the rows are gone). The anonymize plane is idempotent by count
because it scrubs its match column too: once `user_id` (or `actor_id`) holds
the tombstone, a re-run's `WHERE` matches nothing.

### Registering an eraser

A battery or app registers a table for erasure the same way it registers one
for export. Three modes:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/datexport"
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

// Keep the row, take the user out of a JSON array of ids it shares.
datexport.RegisterEraser(datexport.DataEraser{
    Name: "beta_lists", Source: "rollout", Table: "beta_lists",
    Column: "members", Mode: datexport.EraseRemoveMember,
})

// Match only the rows that also meet every Where condition.
datexport.RegisterEraser(datexport.DataEraser{
    Name: "api_tokens", Source: "keys", Table: "api_tokens",
    Column: "owner_id", Mode: datexport.EraseDelete,
    Where: []datexport.Equals{{Column: "owner_kind", Value: "user"}},
})
```

`EraseRemoveMember` reads `Column` as a TEXT column holding a JSON array of
strings and rewrites only the rows that list the user, without the user;
the row stays. `Where` narrows any mode: each column is `SafeIdent`-checked
and must exist on the table, and each value is bound, so a service account
that shares a user's id survives that user's erasure.

`Column` is the user-id column matched against the erased user's id. For
`EraseAnonymize`, both `ScrubColumns` and `Column` are overwritten with
`Tombstone`, and an anonymizer with no `ScrubColumns` is an error. Every
eraser's rows are read before anything is written, deletes run before
anonymizers, and the anonymizers on one table run as a single UPDATE, so
two erasers matching on the same column both apply. Each eraser's
`RowsAffected` is the number of rows it matched. Rows an eraser deletes
or anonymizes in an entity table get the same audit scrubbing and stored
file deletion as the entity plane's own rows. As with export: a registered table that is absent from the live
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
import "github.com/DonaldMurillo/gofastr/core/datexport"
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
    ValuePrefix: "magiclink:",         // rows store "magiclink:<email>"
})
```

`ValuePrefix` is prepended to the bound value, on either identity. The auth
token table is shared by magic-link login, password reset and email
verification and stores each payload tagged with its flow
(`magiclink:<email>`, `pwreset:<user id>`, `verify:<user id>`), so
`battery/auth` registers one eraser per tag; the reset and verification
erasers match by user id with their tag as the prefix. Matching the bare
email, as the first registration did, reached no row the battery writes.

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
before it is quoted as the catalog spells it; the user id and the tombstone
are `$n` bound
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
