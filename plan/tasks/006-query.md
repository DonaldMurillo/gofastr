# 006 — Query Builder

**Phase:** 1 (Core Primitives) | **Tier:** 1 | **Depends on:** nothing

## Goal
SQL query builder for SELECT/INSERT/UPDATE/DELETE. Fully parameterized (no string interpolation). Composable via chaining (Where, Order, Limit, Offset, Cursor). JOIN support. Type-safe query structs. PostgreSQL first. Not an ORM — just produces SQL + args.

## Deliverables
- [ ] `Select(columns...)` builder — generates `SELECT ... FROM ...` with parameterized args
- [ ] `Insert(table)` builder — generates `INSERT INTO ... VALUES ...` with parameterized args
- [ ] `Update(table)` builder — generates `UPDATE ... SET ...` with parameterized args
- [ ] `Delete(table)` builder — generates `DELETE FROM ...` with parameterized args
- [ ] Composable clauses:
  - `Where(condition, args...)` — appends `WHERE` (and-chains multiple)
  - `Order(columns...)` — `ORDER BY`
  - `Limit(n)` — `LIMIT`
  - `Offset(n)` — `OFFSET`
  - `Cursor(field, value)` — keyset/cursor-based pagination (WHERE field > value ORDER BY field LIMIT n)
- [ ] JOIN support: `InnerJoin(table, on)`, `LeftJoin(table, on)`
- [ ] `Build()` method returns `(sql string, args []any)` — the final parameterized query
- [ ] Type-safe query structs for scan targets (no reflection required, but optional scanner helper)
- [ ] `query` package at `core/query/`
- [ ] Unit tests: verify generated SQL and arg ordering for all clause combinations
- [ ] Integration tests using test database (or mocked `pgx` connection)

## Acceptance Criteria
- Zero string interpolation in generated SQL — all values passed as `$1`, `$2`, … parameters
- Chained queries compose correctly: `Select("*").From("users").Where("age > $1", 21).Order("name").Limit(10).Build()` produces valid PostgreSQL
- JOIN queries reference correct tables and bind params in order
- Cursor pagination generates `WHERE field > $1 ORDER BY field LIMIT $2`
- No external ORM dependency — package only produces `(string, []any)`
- All tests pass with `go test ./core/query/...`
