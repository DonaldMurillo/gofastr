# Testing toolkit (`framework/testkit`)

GoFastr ships a suite of testing helpers covering in-memory HTTP dispatch, render-panic detection, retired-markup detection, no-JS verification, CSS weight budgets, isolated Postgres databases, and isolated style registries.

## In-memory request harness (`framework.TestHarness`)

Drive your app's full router, middleware, and hooks with zero open sockets, chaining `.AsUser(...)` and `.AsTenant(...)`:

```go
ta := framework.TestHarness(t, app).
	AsUser(struct{ ID string }{ID: "u1"}).
	AsTenant("acme")
ta.Post("/invoices", body).AssertStatus(t, http.StatusCreated)
```

Built-in test guards active automatically in Go test binaries:
- **Render panics fail tests loudly:** any component or layout panic fails `t` (and plain UI host requests return `500` in test binaries) even if a production error boundary would fall back gracefully. For tests deliberately asserting production recovery, wrap the handler with `testkit.AllowRenderPanics(t, handler)`.
- **Retired markup scanner:** flags any rendered HTML/JSON response containing deprecated classes or `data-cui-*` attributes from the upgrade registry (never runs in production).

## Browser-free and headless-Chrome quality gates

- **Accessibility (`framework/testkit/axetest`):** headless Chromium + vendored axe-core across light and dark schemes (`axetest.ScanSchemes`); see [Accessibility](accessibility.md).
- **Works without JavaScript (`framework/testkit/nojstest`):** loads pages in Chrome with script disabled to catch empty `<main>`, dead `#`/`javascript:` links, forms with no enabled submit button, and 4xx/5xx responses.
- **CSS weight budget (`framework/testkit/cssweight`):** measures total and gzipped `<style>` + linked stylesheet bytes per page over plain HTTP (no browser required) so CSS bloat fails CI.

## Isolated Postgres databases (`testkit.NewIsolatedDB`)

Carve a fresh per-test Postgres database from an admin DSN (`postgres://...`), run your migration callback, and drop the database automatically on `t.Cleanup`:

```go
import (
	"testing"
	_ "github.com/lib/pq"
	"github.com/DonaldMurillo/gofastr/framework/testkit"
)

func TestMyFeature(t *testing.T) {
	adminDSN := os.Getenv("GOFASTR_TEST_POSTGRES_DSN")
	if adminDSN == "" {
		t.Skip("GOFASTR_TEST_POSTGRES_DSN unset")
	}
	db := testkit.NewIsolatedDB(t, adminDSN, migrateFn)
	// Use db with framework.NewApp(framework.WithDB(db)) + framework/factory
}
```

Helpers: `NewIsolatedDB`, `NewIsolatedDBWithName`, `ValidateAdminDSN`, and white-box `RewriteDBNameForTest`:

<!-- gofastr:compile
stmt: _ = out
stmt: _ = err
import "github.com/DonaldMurillo/gofastr/framework/testkit"
-->
```go
out, err := testkit.RewriteDBNameForTest("postgres://u:p@host/db", "new_db")
```

## Isolating the global style registry (`registry.IsolateForTest`)

Because `core-ui/registry` is process-global, call `registry.IsolateForTest(t)` in sequential (non-`t.Parallel()`) unit tests that assert exact registered stylesheets so imports like `framework/ui` don't pollute assertions.

## Common mistakes

- **Passing a libpq key-value string (`host=localhost ...`) to `NewIsolatedDB`:** must be a `postgres://` or `postgresql://` URL.
- **Forgetting to blank-import a Postgres driver (`_ "github.com/lib/pq"` or `_ "github.com/jackc/pgx/v5/stdlib"`):** `sql.Open("postgres", ...)` fails immediately without one.
- **Calling `t.Parallel()` alongside `registry.IsolateForTest(t)`:** silently swaps the global registry out from under sibling parallel tests.
- **Opening extra `*sql.DB` pools to the carved DB without closing them before cleanup:** races with `DROP DATABASE`.
