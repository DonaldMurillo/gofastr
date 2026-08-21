# Backups and restore

GoFastr does not back up your database. This page is the operator's guide to
doing it yourself: what to back up, the commands for SQLite and Postgres, and
how to prove a restore works before you need it. For application-level,
schema-aware export (portability, GDPR, anti-lock-in) see
[Data export & import](data-export.md). It complements a database backup and
does not replace one.

## What to back up

- **The database.** Everything the framework persists lives here: entity rows,
  auth sessions and tokens, queue jobs, audit log, migration state.
- **Uploaded files**, if you use `battery/storage` with the local backend.
  The directory you configured is not inside the database. S3-backed storage
  is covered by your bucket's own versioning/replication.
- **Secrets are not backups.** `GOFASTR_SECRET`, auth secrets, and OAuth
  credentials belong in your platform's secret store. A restored database is
  useless without the same secrets, so record where they live, but keep them
  out of the backup archive itself.

## SQLite

Copying the `.db` file while the app is running can capture a torn state, and
in WAL mode it misses writes still sitting in the `-wal` file. Use SQLite's
own online-backup paths. Both are safe while the app is up:

```sh
# Option 1: the backup API via the CLI
sqlite3 /srv/myapp/app.db ".backup '/backups/app-2026-08-16.db'"

# Option 2: VACUUM INTO, which also compacts the copy
sqlite3 /srv/myapp/app.db "VACUUM INTO '/backups/app-2026-08-16.db'"
```

A plain file copy is fine only when the app is stopped. Either way, copy the
result off the host; a backup on the same disk as the database shares its
failure.

## Postgres

`pg_dump` in custom format is the baseline. It gives a consistent snapshot
with no downtime and supports selective restore:

```sh
pg_dump --format=custom --file=/backups/app-2026-08-16.dump "$DATABASE_URL"

# restore into an empty database
pg_restore --dbname="$RESTORE_URL" --no-owner /backups/app-2026-08-16.dump
```

A nightly dump bounds your data loss at up to a day. If that is too much, add
point-in-time recovery: WAL archiving (`archive_command`, or tooling like
pgBackRest/WAL-G), or your managed provider's PITR switch. Most managed
Postgres offerings make this a checkbox and it is the single cheapest
durability upgrade available.

## Prove the restore

An untested backup is a hope, not a plan. On a schedule (monthly is a
reasonable floor):

1. Restore the latest backup into a scratch database or file.
2. Run the same migration step your deploy runs, so the drill exercises
   the real upgrade path: `gofastr migrate up --db-url=<scratch-dsn>`,
   passing the scratch URL explicitly (a bare `--db-url` falls back to
   `DATABASE_URL` and migrates the wrong database), or nothing, if you
   accept on-boot auto-migrate.
3. Point a build of your app at it (`DATABASE_URL` set to the scratch copy)
   and boot it. Boot surfaces startup and schema failures; it does not
   prove the restored rows and uploads are complete. That is what the
   next step checks.
4. Spot-check what matters: row counts on your core entities, one login, one
   file download if you back up uploads.

Write down the restore time. That number is your real recovery window, and it
only ever grows with the data.

## Common mistakes

- **Treating `ExportData` as the backup.** The export is application-level
  and declaration-aware, good for moving between databases or handing users
  their data, but it is not point-in-time, does not capture tables the
  framework does not know about, and both export and re-import take longer
  than a native restore. Use the database's own tooling for disaster recovery
  and [Data export & import](data-export.md) for portability.
- **Copying a live SQLite file with `cp`.** It can capture a torn state and
  silently drops writes still in the `-wal` file. Use `.backup` or
  `VACUUM INTO` (above), which are safe while the app runs.
