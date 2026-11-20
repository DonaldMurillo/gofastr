# Data export, import, and erasure

GoFastr gives you schema-aware data portability and GDPR/CCPA user erasure straight from your entity declarations: `App.ExportData`, `App.ImportData`, and `App.EraseUserData`.

These tools complement — and do **not** replace — native database backups (`pg_dump`, SQLite `.backup`). Use database backups for disaster recovery (see [Backups](backups.md)), and use `ExportData` / `EraseUserData` for user portability, migrations between SQLite and Postgres, and account deletion.

## Exporting data

`App.ExportData` streams a structured archive of registered entities, honoring tenant or owner scopes when requested:

- **Full or scoped:** export the entire database, a single tenant, or a single user's owned records (every entity with `Scope.OwnerField` plus registered custom exporters).
- **Redaction:** `Hidden` fields (such as password hashes or secrets) are excluded by default so user exports never leak internal credentials.
- **Streaming:** rows stream sequentially rather than loading entire tables into RAM.

## Importing data

`App.ImportData` reads an export archive back into an app's database:
- Inserts entities in dependency order (respecting foreign-key relations) inside transactions.
- Validates columns against the target entity declarations and supports dry-run verification before committing.
- Works across database drivers (for example exporting from SQLite and importing into PostgreSQL).

## User erasure (`App.EraseUserData`)

`App.EraseUserData(ctx, userID, opts...)` deletes or scrubs a user's personal data across the framework in one call:
- Deletes rows owned by `userID` on every entity declaring `Scope.OwnerField` (including hard-purging soft-deleted rows so erased data does not linger in trash).
- Invokes every registered battery and store eraser: auth sessions/tokens/2FA/passkeys, audit log actor references, `featureflag` SQL allowlists, `idempotency` cached responses, `search` documents tagged with `Owner`, and uploaded files.
- Scopes by tenant when `WithEraseTenant` (or a request tenant) is present so erasing user `42` in tenant `acme` never touches another tenant's data.

## Stores outside the database

Not every piece of user data lives in a SQL entity table — search indexes (`battery/search`), uploaded blobs (`battery/storage`), Redis caches, or third-party SaaS tools can hold copies. Built-in batteries register their own store erasers automatically (`search.Memory`, SQL search backends, `featureflag`, `idempotency`), and you should register erasers for any custom stores your app introduces.

## Registering an eraser

Register custom cleanup logic for non-entity tables or external systems so `App.EraseUserData` stays your single source of truth for deletion:

```go
// Register an eraser on your app for any custom table, file store, or
// third-party API (Stripe, CRM, analytics) that holds per-user data.
```

Each registered eraser receives `ctx` and the erased `userID` (plus tenant context if scoped). Make erasers idempotent so retrying a partially failed erasure completes cleanly.

## What this does not do

- **Not a point-in-time backup:** `ExportData` reads tables sequentially without a cluster-wide MVCC snapshot across external blob stores. Use `pg_dump` or SQLite `.backup` for disaster recovery.
- **Does not erase third-party systems automatically:** email relays, log archives, and external webhooks only get erased if you register an eraser for them.
- **Does not rewrite immutable DB backups:** follow your backup retention window policy for historical snapshots.

## Common mistakes

- **Using `ExportData` instead of `pg_dump` / SQLite `.backup` for disaster recovery:** native DB tooling is faster, captures unmodeled tables, and gives point-in-time consistency.
- **Forgetting to tag `search.Document.Owner` when indexing user data:** `App.EraseUserData` can only delete search documents whose `Owner` field was populated at index time.
- **Leaving custom non-entity tables unregistered:** any raw SQL table holding PII needs a registered eraser or user erasure will leave orphaned personal data behind.
- **Soft-deleting instead of purging during GDPR erasure:** `EraseUserData` hard-deletes owned records; do not intercept it to convert erasure back into a soft delete.
