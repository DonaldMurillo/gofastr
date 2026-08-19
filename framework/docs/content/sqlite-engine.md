# The in-house SQLite engine (`sqlite/`)

There are two SQLite implementations in this repository. If you are building
an application on GoFastr, you want the first one and can stop reading after
the next paragraph.

| Which engine | Package | Driver name | Who uses it |
|---|---|---|---|
| **What apps ship** | `modernc.org/sqlite`, registered by `gofastr/sqlite/stdlib` | `sqlite3` | every generated app, `gofastr` CLI, the whole framework at runtime |
| **A test cross-check** | `gofastr/sqlite` | `gofastr-sqlite` | ~20 test files in this repo. Nothing else. |

**Applications never touch `gofastr/sqlite`.** `gofastr init` wires
`_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`, which registers
modernc.org/sqlite — a real SQLite, transpiled to Go — under the conventional
`sqlite3` name. That is what runs in production, and it is the only thing
`gofastr docs deploy` and the README ever mean by "pure-Go SQLite".

## Where the two engines deliberately differ

The cross-check is close but not identical, and a handful of differences are
chosen rather than accidental. All of them are in `gofastr/sqlite` only;
nothing here describes what applications run, and in every case the in-house
engine is the STRICTER of the two.

Each one is pinned by a scenario in `sqlite/differential_scenarios_test.go`
marked `wantDiff`, and those scenarios fail if the engines ever AGREE — so a
divergence that gets fixed cannot quietly stay on this list.

**Foreign keys are enforced by default.** Real SQLite defaults `foreign_keys`
off and the driver apps ship turns it on for every DSN, so an engine defaulting
off would let the cross-check suites validate dangling writes that production
refuses — a false green. `PRAGMA foreign_keys` reports the real state and its
setter works, with SQLite's own rule that the setter is a no-op inside a
transaction.

**Through a pool, enforcement can be turned on but not off.** The in-house
driver builds one engine per `sql.Open` and shares it across every connection,
so a `PRAGMA foreign_keys` setting is database-wide rather than
per-connection as SQLite scopes it. Turning it ON that way is harmless.
Turning it OFF would silently stop checking foreign keys for every other
connection, so `PRAGMA foreign_keys = OFF` is **refused** on a pooled handle
rather than honoured with a scope the driver cannot provide. Code that
genuinely needs enforcement off drives the `Engine` directly, where there is
exactly one owner.

**Referential actions are parsed and ignored.** `ON DELETE CASCADE`, `MATCH`,
and `DEFERRABLE` parse — refusing them would make an ORM-generated or
`sqlite3 .dump` schema unopenable — but no cascade or deferral happens. The
engine refuses a delete real SQLite would cascade, which is stricter, never
looser.

**A composite `FOREIGN KEY (a, b)` is refused at declaration**, since the
engine's foreign-key metadata holds a single column and storing it partially
would enforce half a constraint.

**Three query-semantics differences, all on the strict side.** A double-quoted
name that matches no column is an error here; SQLite reads it as a string
literal, so a typo'd column name silently becomes a constant and the filter
stops filtering. Comparison affinity is not applied to a literal, so
`WHERE int_col = '7'` matches nothing here and matches a stored `7` in SQLite
— fixing that correctly needs the declared affinity to reach the expression
evaluator, and the value-shaped approximation gets the reverse case wrong in
the direction that ADDS rows. Rowids are never reused; SQLite hands out the
largest rowid plus one, so a deleted row's id can come back and a stale
reference can resolve to a different row.

## How the two are kept honest

`sqlite/differential_test.go` runs the same SQL script through both engines
statement for statement and compares every accept/refuse outcome and every
probe's rows. A scenario must agree unless it carries a `wantDiff` reason, and
a `wantDiff` scenario must disagree.

It exists because inspection did not work. Four consecutive hand-review passes
over the foreign-key implementation each declared it complete; running both
engines side by side then found enforcement paths that had never been wired at
all — `DROP TABLE` among them, which silently orphaned every child row. Nobody
had to think of `DROP TABLE`. Adding a scenario costs one table entry, which is
the point: the failures this catches are the ones nobody thought to look for.

## Why a second engine exists

`gofastr/sqlite` is a from-scratch SQLite: pager, B-tree, lexer, parser, file
format, varints, indexes, foreign keys, views, joins, subqueries, compound
selects. Roughly 7,000 lines, with 7,000 more of its own tests.

It earns its place as an **independent implementation to test the framework's
SQL against.** GoFastr generates SQL from entity declarations. Validating that
SQL only against the engine it was developed on is a weak test: the two drift
together, and an assumption baked into both looks like correctness. Running
the same queries through a second engine that shares no code catches the
class of bug where GoFastr leans on a behaviour real SQLite happens to allow.

The suites that use it say so in their names — `*_pure_sqlite_test.go` across
`framework/{access,outbox,crud}` and `battery/{auth,queue}`.

## What it is not

- **Not the default.** Registered as `gofastr-sqlite`, never `sqlite3`.
  `sqlite/driver_name_test.go` fails if that ever changes, because the day it
  does the framework's suite silently starts validating against the wrong
  engine while every application still ships modernc.
- **Not a supported public API.** It is in the module's root for historical
  reasons, not as an offer. Nothing outside this repo should import it, and it
  carries no compatibility promise under
  [stability](stability.md).
- **Not competitive on writes.** Its own `sqlite/BENCHMARKS.md` measured
  inserts 97–148× slower than CGO SQLite. Reads, table creation, deletes, and
  transactions were comparable or faster. That is fine for a correctness
  cross-check and disqualifying for anything else. **That file is a snapshot
  from 2025-05-15 and has not been re-measured since; treat its numbers as
  historical.**

## Common mistakes

- **Reaching for `gofastr/sqlite` in an application.** You want
  `sqlite/stdlib`, which `gofastr init` already imports for you. Opening
  `gofastr-sqlite` in an app means running your production data through an
  engine with a fraction of real SQLite's testing.
- **Assuming a `*_pure_sqlite_test.go` failure means your code is broken.**
  It can equally mean the in-house engine lacks a SQL feature real SQLite has.
  Check the same query against `sqlite3` before changing framework code.
- **Quoting `BENCHMARKS.md` as current.** It predates more than a year of
  changes to both engines.
