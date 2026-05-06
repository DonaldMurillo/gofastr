# 040 — CLI: `gofastr migrate`

**Phase:** 4 (CLI & DX) | **Depends on:** 035, 008

## Goal
CLI commands for database migration management.

## Deliverables
- [ ] `gofastr migrate` — run all pending migrations
- [ ] `gofastr migrate create "description"` — generate blank migration file
- [ ] `gofastr migrate up` — run pending
- [ ] `gofastr migrate down [N]` — rollback last N (default 1)
- [ ] `gofastr migrate status` — show applied vs pending
- [ ] `gofastr migrate diff` — auto-diff entity schema vs DB (dev mode)
- [ ] `--json` output for all commands
- [ ] DB connection from config file

## Acceptance Criteria
- `migrate create` generates timestamped migration file
- `migrate up` applies pending migrations in order
- `migrate down` rolls back specified number
- `migrate status` shows correct state
- `migrate diff` generates ALTER statements from schema changes
