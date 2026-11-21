# Data export, import, and erasure

GoFastr gives you schema-aware archive export/import and GDPR/CCPA user erasure straight from your entity declarations: `App.ExportData`, `App.ImportData`, and `App.EraseUserData`.

These tools complement — and do **not** replace — native database backups (`pg_dump`, SQLite `.backup`). Use database backups for disaster recovery (see [Backups](backups.md)), `ExportData` / `ImportData` for privileged full-database archives and SQLite-to-Postgres migrations, and `EraseUserData` for user account deletion.

## Exporting data (privileged archive)

`App.ExportData(ctx, dir, opts...)` writes **every row** of every registered entity, pivot, and battery table — **including soft-deleted rows and every physical column (even `Hidden` columns)** — to `dir` as owner-only `<name>.ndjson` files plus `manifest.json`:

- **Privileged operator tool, NOT a user-portability endpoint:** `ExportData` does **not** filter by tenant or user and does **not** redact `Hidden` columns (such as password hashes or secrets). Never return its output directly to an end user or tenant admin.
- **Streaming:** rows stream sequentially in pages (`WithExportPageSize`, default 1000) rather than loading entire tables into RAM.

## Importing data

`App.ImportData` reads an export archive back into an app's database:
- Inserts entities in dependency order (respecting foreign-key relations) inside transactions.
- Verifies manifest checksums and validates columns against the target entity declarations (there is no import dry-run mode — test against a scratch database first).
- Works across database drivers (for example exporting from SQLite and importing into PostgreSQL).

## User erasure (`App.EraseUserData`)

`App.EraseUserData(ctx, userID, opts...)` deletes or scrubs a user's personal data across the framework in one call (pass `framework.WithEraseDryRun()` to run in a rolled-back transaction and inspect the report first):
- Deletes rows owned by `userID` on every entity declaring `Scope.OwnerField` (including hard-purging soft-deleted rows so erased data does not linger in trash).
- Invokes every registered battery and store eraser: auth sessions/tokens/2FA/passkeys, audit log actor references, `featureflag` SQL allowlists, `idempotency` cached responses, `search` documents tagged with `Owner`, and uploaded files.
- **Tenant caveat (`WithEraseTenant`):** `WithEraseTenant(tenantID)` scopes `MemberSeparator` erasers (such as allowlists), **not** entity rows — entity rows are matched by owner ID alone. Use globally unique user IDs (such as UUIDs) across tenants; if two tenants reuse the same local owner ID, erasing that ID matches rows in both.

## Stores outside the database

Not every piece of user data lives in a SQL entity table — search indexes (`battery/search`), uploaded blobs (`battery/storage`), Redis caches, or third-party SaaS tools can hold copies. Built-in batteries register their own store erasers automatically (`search.Memory`, SQL search backends, `featureflag`, `idempotency`), and you should register erasers for any custom stores your app introduces.

## Registering an eraser

Register custom cleanup logic with `core/datexport` so `App.EraseUserData` stays your single source of truth for deletion:

```go
// SQL table eraser (deletes or scrubs columns for a table):
datexport.RegisterEraser(datexport.DataEraser{
	Name:       "custom_preferences",
	Table:      "user_preferences",
	OwnerField: "user_id",
})

// External / non-SQL store eraser (Redis, Stripe, CRM, blob store):
_, unregister := datexport.RegisterStoreEraser(datexport.StoreEraser{
	Name: "stripe_customer",
	Erase: func(ctx context.Context, userID string) (int, error) {
		return 1, deleteStripeCustomer(ctx, userID)
	},
})
defer unregister()
```

Make erasers idempotent so retrying a partially failed erasure completes cleanly.

## What this does not do

- **Not a user-facing data export:** `ExportData` includes all tenants, soft-deleted rows, and `Hidden` columns. Build a dedicated scoped query for GDPR user-portability downloads.
- **Not a point-in-time backup:** `ExportData` reads tables sequentially without a cluster-wide MVCC snapshot across external blob stores. Use `pg_dump` or SQLite `.backup` for disaster recovery.
- **Does not erase third-party systems automatically:** email relays, log archives, and external webhooks only get erased if you register an eraser for them.
- **Does not rewrite immutable DB backups:** follow your backup retention window policy for historical snapshots.

## Common mistakes

- **Exposing `App.ExportData` directly to users or tenants:** `ExportData` dumps every tenant's rows and every physical column including `Hidden` credentials. Keep it operator-only.
- **Assuming `WithEraseTenant` prevents cross-tenant entity deletion when owner IDs collide:** entity erasure filters by owner ID alone; keep user IDs globally unique across tenants.
- **Using `ExportData` instead of `pg_dump` / SQLite `.backup` for disaster recovery:** native DB tooling is faster, captures unmodeled tables, and gives point-in-time consistency.
- **Forgetting to tag `search.Document.Owner` when indexing user data:** `App.EraseUserData` can only delete search documents whose `Owner` field was populated at index time.
- **Leaving custom non-entity tables unregistered:** register them with `datexport.RegisterEraser` / `RegisterStoreEraser` or user erasure will leave orphaned personal data behind.
