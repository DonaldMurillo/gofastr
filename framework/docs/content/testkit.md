# Testkit: isolated Postgres helpers for integration tests

`framework/testkit` provides public test helpers for host apps that need
real Postgres isolation in integration tests. The package carves a fresh
database per test, optionally runs your migration callback, and drops the
database on `t.Cleanup`.

> **Framework-internal vs public.** The internal helpers in
> `framework/internal/testdb` follow a schema-based isolation strategy
> and are not exported. `testkit` is the stable public API for
> host-app test code.

## Harness identity: AsUser and AsTenant

`framework.TestHarness(t, app)` sends requests straight through the router
with no session. Two methods return a copy whose requests carry an identity
in their context; each keeps whatever the other set, so they chain:

```go
ta := framework.TestHarness(t, app).
	AsUser(struct{ ID string }{ID: "u1"}).
	AsTenant("acme")
ta.Post("/invoices", body).AssertStatus(t, http.StatusCreated)
```

- `AsUser(user any)` sets the request user, which passes the default
  session gate. Use a real user type when the test also needs an owner
  extractor or an `access.Policy` to resolve roles from it.
- `AsTenant(t any)` sets the request tenant. A string value is also
  stored as the tenant id, which is what `MultiTenant` entities scope by
  and stamp on insert. A non-string value only reaches code that reads the
  tenant object itself.

## Render failures

`framework.TestHarness(t, app)` fails `t` when a component or layout
panics while rendering a harness request, even if an error boundary
returns normal-looking HTML and the response is 200. The failure names
the component type and panic message. `AsUser` and the request builder
keep this check.

Plain UI host requests in Go test binaries also return HTTP 500 after a
recovered render panic, preserving the fallback body. This covers full
pages, navigation partials, intercepted screens, fills envelopes, and
deferred parts. Production binaries keep the screen's recovery status.

For a test that panics on purpose to check production recovery, wrap its
handler with `testkit.AllowRenderPanics(t, handler)`:

```go
srv := httptest.NewServer(testkit.AllowRenderPanics(t, app.Router()))
t.Cleanup(srv.Close)
```

`AllowRenderPanics(t testing.TB, next http.Handler) http.Handler` changes
only requests through the returned handler. `t.Cleanup` revokes the
exemption: later requests again get the test-only 500. Requests already
admitted may finish with production status. Register server cleanup
after creating the wrapper so the server closes before revocation.
Parallel tests are safe when each owns its wrapper and server; do not
share them with tests that did not opt out. There is no process-wide
switch, header, or query parameter. Calling the helper outside a Go test
binary panics. Error logs and TestHarness failure reporting remain active;
use plain HTTP requests to assert intentional recovery.

Generated app end-to-end tests capture the child server's logs and fail
on recovered render panics, including those reached by browser requests.
Recovered render panics log at error level with the component type,
scrubbed panic message, and stack. Logging also runs in production;
the existing fallback HTML and HTTP status behavior do not change.

## Retired markup

`TestHarness` also fails `t` when a rendered response carries markup
the upgrade registry has retired: a class name or `data-cui-*`
attribute no longer emitted by the kit. This is what catches the names
a source scan cannot see — a class built at run time
(`fmt.Sprintf("ui-%s", kind)`), one read from the database, a template
value. The failure reads:

```text
GET /orders: retired markup: class "ui-button" (v0.86.0: the button classes
are fui-button*; the ui-button class no longer exists in any emitted markup
or stylesheet); run gofastr upgrade
```

Attribute values never match — `data-cui-comp="ui-sidebar"` is a kept
component marker, not the retired `ui-sidebar` class — and a migrated
spelling reports nothing.

Every response on the app router is read by its `Content-Type`: full
pages, navigation partials, deferred parts, 404/405/error documents,
island RPC answers, and widget chrome are scanned as markup (`text/html`
and `text/plain`, which the runtime applies to an html-mode signal as
markup). JSON answers and widget `/state` snapshots are walked string
by string, and each string holding a tag is scanned, so an island that
renders a retired class only after a click still fails the test.
Stylesheets, scripts, event streams, and downloads are not read, and a
hijacked connection is dropped from the scan.

The scan runs only in test binaries and under `gofastr dev` (which
warns once per path and name, so a livereload loop cannot flood the
console). Production never scans and never loads the registry. Apps
that build their own `httptest.Server` instead of the harness get the
same check: findings log at warn level in the test binary.

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

1. Validates `adminDSN`: hard-fails (`t.Fatalf`) if empty or wrong scheme.
   Tests that skip on a missing DB prove nothing; this helper refuses to skip.
2. Opens a connection to `adminDSN` and pings it (retries for up to 3s).
3. Creates a uniquely-named database: `ftest_<sanitised-test-name>_<random>`.
4. Calls `migrate(carved)` if non-nil: run your schema DDL here.
5. Registers `t.Cleanup` to terminate lingering connections and `DROP DATABASE`.

Returns the `*sql.DB` for the carved database.

### `NewIsolatedDBWithName`

Same as `NewIsolatedDB` but also returns the database name as a string,
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
key-value form (`host=… dbname=…`) is **not** supported: the helper
rewrites the path component of the URL to carve the new database name, which
requires a URL-parseable DSN.

A common pattern is to read the DSN from an environment variable:

```go
adminDSN := os.Getenv("GOFASTR_TEST_POSTGRES_DSN")
if adminDSN == "" {
    t.Skip("GOFASTR_TEST_POSTGRES_DSN unset; skipping live-PG test")
}
```

> The helper itself does **not** skip on a missing DSN: it hard-fails.
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

<!-- gofastr:compile
stmt: _ = out
stmt: _ = err
import "github.com/DonaldMurillo/gofastr/framework/testkit"
-->
```go
out, err := testkit.RewriteDBNameForTest("postgres://u:p@host/db", "new_db")
```

Returns an error for libpq key-value DSNs, non-postgres schemes, or
unparseable inputs: by design, to prevent the carved connection from
accidentally pointing at the admin database on parse failure.

## `registry.IsolateForTest`

The component style registry (`core-ui/registry`) is **process-global**. A test
that asserts on what it contains — which stylesheets a page links, the exact
names in a bundle — is asserting about every package linked into that test
binary, not just the component under test.

That bites in a specific way: `framework/ui`'s package `init` registers
`ui-button`, `ui-page-header`, and `ui-sidebar` as `LoadAlways`. So merely
importing `framework/ui` anywhere in a package changes the eager set every test
in it sees, and a registry assertion two files away turns red with nothing in
its own file changed.

`IsolateForTest` swaps the registry for a fresh one and restores the previous
contents when the test finishes:

```go
func TestComponentEmitsItsOwnLink(t *testing.T) {
    registry.IsolateForTest(t)

    st := registry.RegisterStyle("my-component", myCSS)
    // ... assert on what the host emits for st, with nothing else registered
}
```

Two properties worth knowing:

- **Registrations made during isolation are dropped on restore.** They cannot
  leak into a later test. Without isolation they accumulate for the whole
  package run.
- **Restore runs through `t.Cleanup`**, so it also runs on `t.Fatal` and on a
  panic.

**Sequential tests only, and misuse is silent.** With `t.Parallel()`, a sibling
test's registrations land in the isolated test's throwaway map, and the restore
destroys them — neither test fails, the sibling just quietly loses its styles.
Nothing structural can prevent that without `core-ui/registry` importing
`testing`, so this is a constraint you have to keep, not one the compiler keeps
for you.

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
