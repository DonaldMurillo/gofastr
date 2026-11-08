# Data fixes: C02, C12

Findings: [2026-11-07-query-correctness.md](2026-11-07-query-correctness.md)
(C02) and [2026-11-07-decimal-correctness.md](2026-11-07-decimal-correctness.md)
(C12). Base: main at 7d8cd9eea. Working-tree changes only; nothing
committed.

## Coordinator validation

The coordinator independently passed full `core/schema` and
`framework/crud` suites, then reran the new SQLite Decimal and timestamp
regressions with verbose output. The Postgres-specific regression skipped
because `TEST_POSTGRES_DSN` was unset; the worker's container run is
separate evidence, not a coordinator Postgres claim. Repository vettool
passed both packages. Fresh docs and changelog gates passed after the
coordinator documented base-CRUD-only refusal coverage and release notes.

The filter refusal is deliberately not a claim that every SQL comparison
surface now supports legacy layouts. Nested relation targets, DSL/typed
queries, read-scope binds and direct-SQL writes into managed tables still
require canonical storage. The check scans the column per request and
returns 500, not a user-input 4xx.

| ID | Status | Fix |
| --- | --- | --- |
| C02 | Fixed: cursor; refused: filters | An Unmanaged SQLite cursor binds the anchor's stored text. A timestamp filter on a column holding another layout answers 500 `timestamp_layout` |
| C12 | Fixed | `schema.SQLiteDecimalRefusal` follows SQLite's NUMERIC affinity: whole digits within int64 pass, nonzero values below 1e-307 are refused |

Both were reproduced on 7d8cd9eea before the change: the retained
`main.go` from the query review printed the skipped and repeated pages,
and a driver probe showed the decimal storage below.

## C02: timestamp text outside `schema.TimestampLayout`

SQLite compares a Timestamp column as text, and crud binds every
Timestamp value in `schema.TimestampLayout`. An Unmanaged table that
another writer filled holds other spellings, and the modernc driver
parses a `DATETIME` or `TIMESTAMP` column into a `time.Time`, so the
stored spelling never reached crud.

- **Cursor (fixed).** When the entity is `Unmanaged` and the dialect is
  not Postgres, each Timestamp cursor field is also selected as
  `"col"||''`. That expression has no declared type, so the driver
  returns the stored text. The cursor carries that text and binds it
  unchanged, so the keyset compares the same bytes `ORDER BY` sorts.
  This holds for every layout, a table that mixes layouts included. The
  key-only anchor lookup reads the same expression. A carried value must
  still parse as RFC 3339 or as one of SQLite's date-function forms
  (`storedTimestampLayouts`), or the cursor is a 400. Managed entities
  and Postgres skip all of this and bind the canonical value as before.
  The dialect probe (`SELECT version()`) runs only for an Unmanaged
  entity with a Timestamp cursor field.
- **Filters (refused).** A filter cannot know the stored layout, so on
  the same entities `refuseLegacyTimestampComparisons` checks each
  Timestamp column that `?f_op=`, `?where=`, `ListOptions.Filters` or
  `ListOptions.Where` compares (every op except `like`). The check is
  `SELECT col FROM t WHERE col||'' NOT GLOB <canonical> LIMIT 1`. If it
  finds a row, HTTP List answers 500 `timestamp_layout` naming the field
  and logs the table, and `ListAll`, the aggregates and `CountAll` return
  the error. Canonical rows filter exactly as before, and filter-value
  normalization is unchanged.

Files: `framework/crud/timestamp_layout.go` (new), `crud_cursor.go`,
`crud.go` (`whereTreeClausesQ` also returns the tree), `crud_api.go`,
`reasons.go`; docs in `cursor-pagination.md` and
`entity-declarations.md`.

Tests: `framework/crud/timestamp_layout_test.go`.

- `TestLegacyTimestampLayouts` covers canonical, space-separated,
  RFC 3339 without a fraction, the mattn `+00:00` form, and a mix of
  canonical and space-separated rows, each in both `TEXT` and `DATETIME`
  columns. Through `CrudHandler.List` it checks that the two-row cursor
  walk equals `?sort=at,id`, that a key-only cursor resumes after its
  row, that `?at_gt=` and `?where=` give m3,m4,m5 on canonical data and
  500 `timestamp_layout` otherwise, and that `ListAll` does the same.
- `TestLegacyTimestampCursorRefusesNonTimestamp` checks that a tampered
  `at="yesterday"` cursor is a 400.
- `TestLegacyTimestampPostgresUnchanged` runs an Unmanaged `TIMESTAMPTZ`
  walk and filter through the existing `pgtest` helper (it skips without
  `TEST_POSTGRES_DSN`; it passed against the local container).

Mutations that each failed a test: no stored-text keyset, no filter
refusal, the `?where=` tree not walked, the anchor lookup rebinding the
canonical value, no check on the carried value, and no Postgres gate
(Postgres then got a 400 cursor).

Limits:

- The filter check scans the column on every such request, because
  noncanonical rows can be anywhere. Views count as Unmanaged and pay
  the same scan.
- Surfaces outside these call sites still compare canonical text
  against legacy rows without the check: nested relation filters
  (`?rel.field=`) whose target is an Unmanaged table, the
  `framework/dsl` predicates, typed queries, and read-scope binds.
- A managed table that a direct SQL writer filled is outside the
  contract and is not checked.
- Values stored as numbers in a `DATETIME` column (unix seconds, Julian
  days) fail the GLOB check, so filters refuse them; a cursor carrying
  one is a 400.

## C12: SQLite decimal exactness

The modernc driver showed how a `NUMERIC` column stores each literal.
Plain digits that fit int64 are stored exactly as INTEGER
(`9223372036854775807`, `-9223372036854775808`). Anything with a point
or an exponent goes through a double, even a whole value:
`123456789012345678.0` stored 123456789012345680, a false acceptance
the old 18-digit rule let through. `1e-400` stored INTEGER 0, and
`1e-320` came back as 9.9998886718268301e-321.

- `schema.SQLiteDecimalRefusal(colType, literal) (Violation, bool)` is
  the rule. TEXT, CHAR, CLOB and BLOB affinity always pass. Literals
  that `strconv.ParseInt(…, 10, 64)` accepts pass. Zero passes. More
  than 15 significant digits gets `decimal_significant_digits`. A
  nonzero magnitude below `schema.SQLiteDecimalMinMagnitude` (1e-307,
  just above the smallest normal double) gets the new
  `decimal_underflow` ("must be 0 or at least 1e-307 in absolute
  value"). `DecimalExactOnSQLite` wraps it, and the CRUD write check
  returns the violation's own reason.
- The default `DECIMAL(19,4)` precision and scale checks still run
  first and are unchanged. So is Postgres: the check returns early
  after the dialect probe.

Files: `core/schema/decimal.go`, `violation.go`,
`framework/crud/decimal_sqlite.go`; docs in `entity-declarations.md`.

Tests:

- `core/schema/decimal_sqlite_test.go` covers the int64 bounds and one
  past each, the `.0` whole values, `1e18`, underflow, subnormal,
  `1e-307`, `0e-400` and `-0.0`. The old 19-digit `false` expectation
  is flipped. `TestSQLiteDecimalRefusalReason` checks the reasons.
- `framework/crud/decimal_sqlite_storage_test.go`
  (`TestDecimalSQLiteStorageBoundaries`) runs POST and GET through
  `RegisterCrudRoutes` on fresh SQLite and checks the stored
  `CAST(… AS TEXT)`. Accepted values read back as sent. Refused values
  get 400 with the right code and store no row. The `DECIMAL(19,4)`
  `decimal_integer_digits` control holds.

Mutations that each failed a test: no int64 branch, no underflow
branch. A third, the redundant `ContainsAny(".eE")` guard, survived
because `ParseInt` already refuses a point or exponent, so the guard was
removed.

Limit: the 1e-307 floor refuses normal doubles in
[2.2250738585072014e-308, 1e-307) that SQLite would keep. That keeps the
threshold one clean decimal.
