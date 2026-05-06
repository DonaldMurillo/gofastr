# Task 040: CLI Migrate Command

**Phase:** 4 — CLI & DX  
**Depends on:** 035 (CLI Framework), 008 (Migrate)  
**Status:** not started

---

## Goal

Implement the `gofastr migrate` command group — the CLI interface for database migration management. Supports creating, running, rolling back, and inspecting migrations. Includes an auto-diff feature for development that compares entity declarations to the current database schema.

---

## Context

From the proposal:

> **Migrations** ✅ Auto-diff in dev, explicit versioned for prod

From the draft:

> ```
> gofastr migrate create "add age to users" → explicit migration for prod
> gofastr migrate                           → run pending migrations
> ```

The core `Migrate` primitive (task 008) provides the migration engine (version tracking, up/down execution, transaction support). This task builds the CLI interface on top of it.

---

## Requirements

### 1. Command Structure

`gofastr migrate` is a command group with sub-commands:

```
gofastr migrate               → alias for "gofastr migrate up"
gofastr migrate up            → run all pending migrations
gofastr migrate down [N]      → rollback last N migrations (default: 1)
gofastr migrate create <desc> → generate a blank migration file
gofastr migrate status        → show applied vs pending migrations
gofastr migrate diff          → auto-diff entity schema vs DB (dev mode)
```

### 2. Global Flags (inherited from root + migrate-specific)

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `--db-url` | string | from config | Database connection URL. Overrides config. |
| `--db-driver` | string | from config | Database driver. Overrides config. |
| `--dir` | string | `"migrations/"` | Migration files directory. |
| `--json` | bool | false | Machine-readable output (inherited from root). |

### 3. `gofastr migrate up`

Run all pending migrations in order.

#### Pipeline

```
1. Load config → get database connection
2. Connect to database
3. Ensure migration tracking table exists (create if not)
4. Scan migrations/ directory for migration files
5. Compare applied versions against available versions
6. Run pending migrations in ascending order
7. For each migration:
   a. Start transaction
   b. Execute UP SQL
   c. Record version in tracking table
   d. Commit transaction
   e. Print progress
8. Report results
```

#### Human output

```
Running migrations...

  ✓ 001_create_users.sql        (0.02s)
  ✓ 002_create_posts.sql        (0.01s)
  ✓ 003_add_slug_to_posts.sql   (0.03s)

3 migrations applied. Database is up to date.
```

If no pending migrations:

```
No pending migrations. Database is up to date.
```

#### JSON output

```json
{
  "status": "success",
  "data": {
    "applied": [
      {"version": 1, "name": "create_users", "file": "001_create_users.sql", "duration_ms": 20},
      {"version": 2, "name": "create_posts", "file": "002_create_posts.sql", "duration_ms": 10},
      {"version": 3, "name": "add_slug_to_posts", "file": "003_add_slug_to_posts.sql", "duration_ms": 30}
    ],
    "total_applied": 3,
    "total_pending": 0
  }
}
```

### 4. `gofastr migrate down [N]`

Rollback the last N migrations. Default N = 1.

#### Pipeline

```
1. Load config → connect to database
2. Get list of applied migrations in descending order
3. Roll back the last N migrations
4. For each migration:
   a. Start transaction
   b. Execute DOWN SQL
   c. Remove version from tracking table
   d. Commit transaction
   e. Print progress
5. Require confirmation in interactive mode
```

#### Safety confirmations

- Interactive mode: prompt "Roll back 3 migrations? This will run DOWN for: create_users, create_posts, add_slug_to_posts. [y/N]"
- With `--json`: skip confirmation (assume CI/automation context).
- With `--force`: skip confirmation.

#### Human output

```
Rolling back 1 migration...

  ✓ 003_add_slug_to_posts.sql (down)  (0.01s)

1 migration rolled back.
```

#### JSON output

```json
{
  "status": "success",
  "data": {
    "rolled_back": [
      {"version": 3, "name": "add_slug_to_posts", "file": "003_add_slug_to_posts.sql", "duration_ms": 10}
    ],
    "total_rolled_back": 1
  }
}
```

### 5. `gofastr migrate create <description>`

Generate a new blank migration file.

#### File naming

```
migrations/
├── 001_create_users.up.sql
├── 001_create_users.down.sql
├── 002_add_email_to_users.up.sql
├── 002_add_email_to_users.down.sql
└── ...
```

Format: `<NNN>_<description_snake_case>.up.sql` and `<NNN>_<description_snake_case>.down.sql`

- NNN is auto-incremented based on the highest existing version number.
- Description is converted to snake_case.

#### Generated files

`004_add_age_to_users.up.sql`:
```sql
-- Migration: add_age_to_users
-- Created: 2025-01-15T10:30:00Z
-- Write your UP migration here

ALTER TABLE users ADD COLUMN age INTEGER DEFAULT 0;
```

`004_add_age_to_users.down.sql`:
```sql
-- Migration: add_age_to_users (down)
-- Created: 2025-01-15T10:30:00Z
-- Write your DOWN migration here

ALTER TABLE users DROP COLUMN age;
```

#### Human output

```
✓ Created migration files:
    migrations/004_add_age_to_users.up.sql
    migrations/004_add_age_to_users.down.sql

  Edit the SQL files, then run: gofastr migrate up
```

#### JSON output

```json
{
  "status": "success",
  "data": {
    "version": 4,
    "name": "add_age_to_users",
    "files": [
      "migrations/004_add_age_to_users.up.sql",
      "migrations/004_add_age_to_users.down.sql"
    ]
  }
}
```

### 6. `gofastr migrate status`

Show migration status.

#### Human output

```
Migration Status:

  Applied:
    ✓ 001_create_users           (applied 2025-01-10T08:00:00Z)
    ✓ 002_create_posts           (applied 2025-01-10T08:00:01Z)
    ✓ 003_add_slug_to_posts      (applied 2025-01-12T14:30:00Z)

  Pending:
    → 004_add_age_to_users       (not applied)

  3 applied, 1 pending
```

#### JSON output

```json
{
  "status": "success",
  "data": {
    "applied": [
      {"version": 1, "name": "create_users", "applied_at": "2025-01-10T08:00:00Z"},
      {"version": 2, "name": "create_posts", "applied_at": "2025-01-10T08:00:01Z"},
      {"version": 3, "name": "add_slug_to_posts", "applied_at": "2025-01-12T14:30:00Z"}
    ],
    "pending": [
      {"version": 4, "name": "add_age_to_users", "file": "004_add_age_to_users.up.sql"}
    ],
    "total_applied": 3,
    "total_pending": 1
  }
}
```

### 7. `gofastr migrate diff`

Auto-diff entity declarations against current database schema. **Development only.**

#### Pipeline

```
1. Load config → connect to database
2. Load all entity declarations
3. Compute expected schema from entities:
   - Tables, columns, types, constraints, indexes
   - Foreign keys from relations
   - Soft delete columns (deleted_at)
   - Multi-tenant columns (tenant_id)
4. Read current database schema (information_schema or equivalent)
5. Diff expected vs current
6. Generate migration SQL to reconcile
7. Output the diff and proposed SQL
```

#### Human output

```
Schema Diff:

  Table: posts
    + column: age INTEGER DEFAULT 0        (missing in DB)
    ~ column: title VARCHAR(200) → TEXT    (type changed)
    - column: legacy_field TEXT            (in DB, not in entity)

  Table: comments (new)
    + table with columns: id, body, post_id, created_at

  Proposed migration:
    ALTER TABLE posts ADD COLUMN age INTEGER DEFAULT 0;
    ALTER TABLE posts ALTER COLUMN title TYPE TEXT;
    ALTER TABLE posts DROP COLUMN legacy_field;
    CREATE TABLE comments (...);

  Review the changes above.
  Run 'gofastr migrate create "auto_diff"' and apply manually, or
  run 'gofastr migrate diff --apply' to apply directly (dev only!).
```

#### `--apply` flag (dangerous, dev only)

When `--apply` is passed with `diff`, automatically create and run the migration.

Safety checks:
- Requires `--force` or interactive confirmation.
- Warns if not in development mode (check config or environment).
- Creates a migration file first (so it's tracked), then runs it.

#### JSON output

```json
{
  "status": "success",
  "data": {
    "changes": [
      {"table": "posts", "type": "add_column", "column": "age", "sql": "ALTER TABLE posts ADD COLUMN age INTEGER DEFAULT 0"},
      {"table": "posts", "type": "alter_column", "column": "title", "old_type": "VARCHAR(200)", "new_type": "TEXT", "sql": "ALTER TABLE posts ALTER COLUMN title TYPE TEXT"},
      {"table": "posts", "type": "drop_column", "column": "legacy_field", "sql": "ALTER TABLE posts DROP COLUMN legacy_field"},
      {"table": "comments", "type": "create_table", "sql": "CREATE TABLE comments (...)"}
    ],
    "proposed_sql": [
      "ALTER TABLE posts ADD COLUMN age INTEGER DEFAULT 0;",
      "ALTER TABLE posts ALTER COLUMN title TYPE TEXT;",
      "ALTER TABLE posts DROP COLUMN legacy_field;",
      "CREATE TABLE comments (...);"
    ]
  }
}
```

### 8. Migration Tracking Table

The migration engine (task 008) should use a tracking table:

```sql
CREATE TABLE IF NOT EXISTS _gofastr_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
```

### 9. Database Connection

Connection details come from (in priority order):
1. `--db-url` flag
2. `--db-driver` flag + config `db.url`
3. Config file `db.driver` + `db.url`
4. Default: `sqlite://file:gofastr.db`

The CLI must handle:
- Password masking in error messages (never print `postgres://user:password@...`)
- Connection timeout (5s default)
- Retry logic for transient connection errors (3 retries, 1s backoff)

### 10. Migration File Format

Support both:
- **SQL files** (primary): `NNN_name.up.sql` + `NNN_name.down.sql`
- **Go files** (advanced): `NNN_name.go` with `func Up(tx *sql.Tx) error` and `func Down(tx *sql.Tx) error`

Go migrations allow complex data migrations that SQL alone can't handle.

---

## Error Handling

| Error | Message | Suggestion |
|-------|---------|------------|
| No database connection | `Cannot connect to database: <driver> at <url (masked)>` | `Check your config file or use --db-url to specify a connection string.` |
| No migrations directory | `Directory 'migrations/' not found.` | `Run 'gofastr migrate create "description"' to create your first migration, or use --dir to specify a path.` |
| Migration failed (mid-way) | `Migration 003_add_slug failed: <SQL error>` | `The database is at version 2. Fix the migration SQL in 003_add_slug.up.sql and run 'gofastr migrate up' again.` |
| Dirty migration state | `Migration 003 was partially applied (dirty state).` | `Manually inspect the database and fix the dirty state. Then update _gofastr_migrations to mark 003 as applied or remove it.` |
| No migration files | `No migration files found in migrations/.` | `Run 'gofastr migrate create "description"' to create a migration, or run 'gofastr migrate diff' to auto-generate from entities.` |
| diff with no database | `Cannot run diff: no database connection.` | `Run 'gofastr migrate up' first to create the initial schema, then use diff for subsequent changes.` |

---

## Acceptance Criteria

- [ ] `gofastr migrate up` runs all pending migrations in order
- [ ] `gofastr migrate down` rolls back the last migration
- [ ] `gofastr migrate down 3` rolls back the last 3 migrations
- [ ] `gofastr migrate create "add age to users"` generates paired up/down SQL files
- [ ] `gofastr migrate status` lists applied and pending migrations
- [ ] `gofastr migrate diff` compares entity schema to database and shows differences
- [ ] `gofastr migrate diff --apply` creates and runs auto-generated migration (with confirmation)
- [ ] All commands support `--json` for machine-readable output
- [ ] Database connection is loaded from config, overridable by flags
- [ ] Passwords are masked in error messages
- [ ] Migration execution is wrapped in transactions
- [ ] Failed migrations leave the database in the previous consistent state
- [ ] Dirty state is detected and reported with recovery instructions
- [ ] `--db-url` flag overrides config for CI/CD pipelines
- [ ] Confirmation prompt on `down` in interactive mode (skipped with `--json` or `--force`)
- [ ] Go migration files are supported alongside SQL files
- [ ] All tests pass: `go test ./...`

---

## Implementation Notes

- Use the core `Migrate` primitive (task 008) as the engine. The CLI is a thin wrapper.
- For schema diffing, use database-specific queries against `information_schema` (PostgreSQL) or `sqlite_master` (SQLite).
- The diff feature should be cautious: prefer false negatives (miss a change) over false positives (drop a column that shouldn't be dropped). Always show proposed SQL before applying.
- Migration ordering must handle gaps (001, 003, 005 — missing 002 and 004) gracefully.
- Consider supporting `.sql` files with `-- postgresql` or `-- sqlite` comments for driver-specific SQL.
- The tracking table name `_gofastr_migrations` uses underscore prefix to sort first in table listings and indicate it's internal.
