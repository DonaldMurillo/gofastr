# 008 — Database Migrations

**Phase:** 1 (Core Primitives) | **Tier:** 2 | **Depends on:** 002 (Schema), 006 (Query Builder)

## Goal
Versioned database migrations. Migration struct with version, up SQL, and down SQL. Runner tracks applied migrations in `_migrations` table. Up: run all pending. Down: rollback N. Auto-diff mode compares schema definitions vs live DB for dev workflow. Explicit mode generates blank migration files. PostgreSQL.

## Deliverables
- [ ] `Migration` struct: `Version (int64)`, `Name (string)`, `Up (string)`, `Down (string)`
- [ ] `_migrations` table auto-created on first run (version, name, applied_at)
- [ ] `Runner` struct accepting `*pgx.Conn` (or generic `db` interface)
- [ ] `Runner.Up(ctx)` — apply all pending migrations in version order
- [ ] `Runner.Down(ctx, n)` — rollback the last N applied migrations
- [ ] `Runner.Status(ctx)` — list applied and pending migrations
- [ ] Explicit mode: `Generate(name)` creates a blank migration file with timestamp version
- [ ] Auto-diff mode: compare registered schema structs (from 002-schema) against live DB, generate ALTER/CREATE statements
- [ ] Transactional migrations — each migration runs in a transaction where possible
- [ ] Migration file format: `NNNN_description.up.sql` / `NNNN_description.down.sql`
- [ ] `migrate` package at `core/migrate/`
- [ ] Tests using test database or transactional rollback strategy

## Acceptance Criteria
- `_migrations` table created automatically on first run
- `Up` applies pending migrations in order and records each in `_migrations`
- `Down` rolls back N migrations in reverse order and removes records
- Re-running `Up` with no pending migrations is a no-op (idempotent)
- Auto-diff detects missing tables/columns and generates correct ALTER statements
- Explicit mode produces valid migration file pair (up + down)
- Failing migration rolls back its transaction and stops the migration chain
- All tests pass with `go test ./core/migrate/...`
