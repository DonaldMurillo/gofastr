# The in-house SQLite engine (`sqlite/`)

There are two SQLite implementations in this repository. If you are building
an application on GoFastr, you want the first one and can stop reading after
the next paragraph.

| | Package | Driver name | Who uses it |
|---|---|---|---|
| **What apps ship** | `modernc.org/sqlite`, registered by `gofastr/sqlite/stdlib` | `sqlite3` | every generated app, `gofastr` CLI, the whole framework at runtime |
| **A test cross-check** | `gofastr/sqlite` | `gofastr-sqlite` | ~20 test files in this repo. Nothing else. |

**Applications never touch `gofastr/sqlite`.** `gofastr init` wires
`_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`, which registers
modernc.org/sqlite — a real SQLite, transpiled to Go — under the conventional
`sqlite3` name. That is what runs in production, and it is the only thing
`gofastr docs deploy` and the README ever mean by "pure-Go SQLite".

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
