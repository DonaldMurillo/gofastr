# The SQLite driver GoFastr ships

GoFastr apps talk to SQLite through
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), which is the
SQLite C source translated to Go. It is real SQLite with no cgo, so
`CGO_ENABLED=0 go build` works and the binary has no C toolchain
dependency.

You get it by importing `gofastr/sqlite/stdlib` for its side effect. That
package registers modernc under the conventional `sqlite3` name and
rewrites every DSN opened through it.

```go
import (
    "database/sql"

    _ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

db, err := sql.Open("sqlite3", "app.db")
```

`gofastr init` writes that import into the app it scaffolds, so a
generated app already has it.

## What the DSN rewrite adds

Three parameters go onto every DSN. Set any of them yourself and your
value wins.

### Foreign keys are on

`_pragma=foreign_keys(1)`.

SQLite ignores `FOREIGN KEY` clauses unless a connection turns them on,
and every SQLite driver leaves them off. `AutoMigrate` writes a
`FOREIGN KEY` clause for each `belongs_to` relation, except where the
relation's column is the entity's `Scope.OwnerField`: that column holds
the session identity, which lives in the auth battery's table rather than
in the related entity, so a key on it is one the framework would violate
on every create. Without this parameter an app read as though the
remaining relations were enforced while nothing enforced them.
The same entity declarations on Postgres did enforce them, which meant
one schema had two different guarantees depending on the database.

Turn it off with `_pragma=foreign_keys(0)` if an existing database
accumulated rows that point at nothing. Those rows surface as errors on
the next write that touches them.

### Timestamps use SQLite's own text layout

`_time_format=sqlite`.

modernc's default for `time.Time` binds is Go's `String()` output, which
looks like `2026-07-20 23:59:59.123456789 +0000 UTC`. Nothing in GoFastr
parses that. `battery/auth`, `framework/outbox` and the hand written
timestamp scans all read either RFC3339 or the layout SQLite writes, so
a timestamp stored in the default format read back as the zero time and
every session looked expired.

This setting also matches what mattn/go-sqlite3 wrote, so databases
created before the driver change stay readable with no migration.

### Writers wait instead of failing

`_pragma=busy_timeout(5000)`.

SQLite allows one writer at a time. modernc defaults the timeout to 0,
so a second writer gets `SQLITE_BUSY` immediately rather than waiting.
Any app serving concurrent requests depends on the wait. mattn defaulted
to 5000ms and this restores it.

## In-memory databases need one connection

`sql.Open("sqlite3", ":memory:")` gives each pooled connection its own
private database. A test that writes on one connection and reads on
another finds an empty schema, and the failure looks like a missing
table rather than a pooling problem.

```go
db, _ := sql.Open("sqlite3", ":memory:")
db.SetMaxOpenConns(1)
```

Use a file under `t.TempDir()` when a test needs more than one
connection.

## There is one SQLite here

This repository used to carry a second, from scratch SQLite engine that
some test suites ran against as a cross check. It was removed. modernc
is real SQLite rather than a second reading of the SQL spec, so whenever
the two disagreed the answer was always that the local engine was wrong,
and its own bugs cost more to find and fix than the cross check ever
returned. The suites that used it now run against the driver
applications ship.

## Common mistakes

**Opening `:memory:` without capping the pool.** Each connection gets its
own database, so the writer and the reader see different schemas. The
error says the table does not exist, which sends you looking at your
migration instead of your pool. Call `db.SetMaxOpenConns(1)`, or use a
file under `t.TempDir()`.

**Importing the package for its name instead of its side effect.** The
import is blank on purpose. `import "github.com/DonaldMurillo/gofastr/sqlite/stdlib"`
without the `_` fails to compile because nothing references the package,
and dropping the import entirely gives you `sql: unknown driver "sqlite3"`
at `sql.Open`.

**Assuming a dangling foreign key still inserts.** It did before this
release and it does not now. An app upgraded onto the new default sees
errors on writes touching rows that already point at nothing, which is
existing corruption becoming visible rather than a new failure. Read the
rows, fix or delete them, then remove any `_pragma=foreign_keys(0)` you
added while cleaning up.

**Deleting a parent before its children.** `AutoMigrate` declares no
`ON DELETE` action and `entity.Relation` cannot express one, so nothing
cascades. Delete the children first.

**Removing `_time_format=sqlite` from a DSN you build yourself.** The
readers in `battery/auth` and `framework/outbox` accept either RFC3339 or
SQLite's space-separated layout, so a hand-written timestamp in either
form reads back fine. What they cannot read is modernc's own default,
Go's `time.Time` `String()` output (`2026-07-20 23:59:59.123456789 +0000
UTC`); a value stored that way parses as the zero time, so a session
looks expired and a scheduled job looks overdue. That is the whole reason
the parameter is added. If you assemble a DSN by hand rather than letting
`sqlite/stdlib` rewrite it, carry the parameter across.
