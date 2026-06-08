# Testkit — isolated Postgres helpers for integration tests

`framework/testkit` provides public test helpers for host apps that need
real Postgres isolation in integration tests. The package carves a fresh
database per test, optionally runs your migration callback, and drops the
database on `t.Cleanup`.

> **Framework-internal vs public.** The internal helpers in
> `framework/internal/testdb` follow a schema-based isolation strategy
> and are not exported. `testkit` is the stable public surface for
> host-app test code.

## Isolated databases

```go
import (
    "testing"
    _ "github.com/lib/pq"
    "github.com/DonaldMurillo/gofastr/framework/testkit"
)

func TestMyFeature(t *testing.T) {
    db := testkit.NewIsolatedDB(t, adminDSN, func(db *sql.DB) error {
        _, err := db.ExecContext(ctx, `CREATE TABLE posts (id TEXT PRIMARY KEY)`)
        return err
    })
    // db is a *sql.DB pointing at the fresh database.
    // The database and connection are automatically closed + dropped on t.Cleanup.
}
```

### `NewIsolatedDB`

```go
func NewIsolatedDB(t *testing.T, adminDSN string, migrate func(*sql.DB) error) *sql.DB
```

1. Validates `adminDSN` — hard-fails (`t.Fatalf`) if empty or wrong scheme.
   Tests that skip on a missing DB prove nothing; this helper refuses to skip.
2. Opens a connection to `adminDSN` and pings it (retries for up to 3s).
3. Creates a uniquely-named database: `ftest_<sanitised-test-name>_<random>`.
4. Calls `migrate(carved)` if non-nil — run your schema DDL here.
5. Registers `t.Cleanup` to terminate lingering connections and `DROP DATABASE`.

Returns the `*sql.DB` for the carved database.

### `NewIsolatedDBWithName`

Same as `NewIsolatedDB` but also returns the database name as a string —
useful when a test wants to assert the database exists (or is gone) via a
separate admin connection.

```go
db, name := testkit.NewIsolatedDBWithName(t, adminDSN, migrate)
```

### Admin DSN

Pass a Postgres DSN with permission to `CREATE DATABASE` and `DROP DATABASE`
(typically a superuser connecting to the `postgres` maintenance database):

```
postgres://postgres:secret@localhost:5432/postgres?sslmode=disable
```

Both `postgres://` and `postgresql://` schemes are accepted. The libpq
key-value form (`host=… dbname=…`) is **not** supported — the helper
rewrites the path component of the URL to carve the new database name, which
requires a URL-parseable DSN.

A common pattern is to read the DSN from an environment variable:

```go
adminDSN := os.Getenv("GOFASTR_TEST_POSTGRES_DSN")
if adminDSN == "" {
    t.Skip("GOFASTR_TEST_POSTGRES_DSN unset; skipping live-PG test")
}
```

> The helper itself does **not** skip on a missing DSN — it hard-fails.
> Skipping is the caller's responsibility. The framework's own
> self-tests accept both `GOFASTR_TEST_POSTGRES_DSN` and
> `WTF_TEST_DATABASE_URL`.

## Using testkit with factory

Combine `testkit` with `framework/factory` to create fixture rows against
the isolated database:

```go
db := testkit.NewIsolatedDB(t, adminDSN, migrate)

app := framework.NewApp(framework.WithDB(db))
app.Entity("posts", postsConfig)

postFactory, err := factory.New(app.Registry, "posts", func() map[string]any {
    return map[string]any{"title": "test post", "status": "draft"}
})
if err != nil {
    t.Fatal(err)
}

post, err := postFactory.Create(ctx)
```

Because `factory` goes through the CRUD handler's full pipeline, hooks
and validations fire as they would for real HTTP traffic.

## `ValidateAdminDSN`

Exported so tests can assert on the error wording:

```go
err := testkit.ValidateAdminDSN("")
// err.Error() contains "empty"

err = testkit.ValidateAdminDSN("mysql://...")
// err.Error() contains "postgres:// scheme"
```

## `RewriteDBNameForTest`

Exposed for white-box testing of the DSN-rewrite logic. Not for production
callers:

```go
out, err := testkit.RewriteDBNameForTest("postgres://u:p@host/db", "new_db")
```

Returns an error for libpq key-value DSNs, non-postgres schemes, or
unparseable inputs — by design, to prevent the carved connection from
accidentally pointing at the admin database on parse failure.

## Common mistakes

- **Using the libpq key-value form for `adminDSN`.**
  `host=localhost user=postgres dbname=postgres` is not URL-parseable.
  The helper rejects it with a scheme error. Use the URL form:
  `postgres://postgres@localhost/postgres`.
- **Not closing the `*sql.DB` before assertions that check the DB was
  dropped.** `t.Cleanup` closes the connection and drops the database.
  If you open an extra connection before cleanup runs, Postgres refuses the
  `DROP DATABASE` while that connection is open. The cleanup kills lingering
  backends with `pg_terminate_backend`, but a connection inside the same
  process that `t.Cleanup` hasn't had a chance to close will race.
- **Calling `NewIsolatedDB` without the `github.com/lib/pq` (or
  `pgx`) driver blank-import.** The helper uses `database/sql` with the
  `"postgres"` driver name. Import `_ "github.com/lib/pq"` or
  `_ "github.com/jackc/pgx/v5/stdlib"` to register the driver; without
  it, `sql.Open("postgres", …)` returns an error immediately.
