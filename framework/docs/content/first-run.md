# First-run setup (`battery/setup`)

`battery/setup` gives self-hosted GoFastr apps a clean minute-one bootstrap: an interactive SSR setup wizard when starting against an empty database, plus a headless environment-variable path for scripted/IaC deploys.

## Quickstart

<!-- gofastr:compile
import "database/sql"
var db *sql.DB
var authManager *auth.AuthManager
stmt: _ = runner
-->
```go
import (
	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/battery/setup"
	"github.com/DonaldMurillo/gofastr/framework"
)

app := framework.NewApp(framework.WithDB(db))

adminStep, complete := setup.AdminStep(authManager, db, "auth_users")
runner := setup.New(setup.Config{
	// Run HealthStep before AdminStep so a failed readiness check aborts
	// before creating the admin user (otherwise `complete` would already be
	// true on the next restart and skip setup).
	Steps:    []setup.Step{setup.HealthStep(app), adminStep},
	Complete: complete,
})
app.SetSetup(runner)
```

`Complete` is **derived state** (for `AdminStep`, "at least one user exists in `auth_users`"), never a marker file. Make sure `Complete` reflects every required step's persisted state.

## Interactive wizard vs. headless IaC

- **Interactive mode:** when setup is incomplete and required env vars are missing, `App.Start` binds the listener, serves the setup wizard plus `/healthz`/`/readyz`, answers `503` on all other routes, and prints a **single-use** tokenized URL (`http://.../setup?token=...`). The first visit exchanges the URL token for an `HttpOnly`, `SameSite=Strict` cookie and invalidates the URL token (cross-site clicks from chat/log viewers perform a safe hop before exchanging). Once the final step succeeds, the server atomically swaps to your real router and starts background consumers (cron, queues, outbox) with no restart. On multi-replica deploys, peer replicas detect completion on their next request and swap too.
- **Headless mode (IaC):** when every required field's `EnvVar` is present (for example `GOFASTR_ADMIN_EMAIL` and `GOFASTR_ADMIN_PASSWORD`), steps run synchronously during `Start` before the port binds — the wizard is never exposed. Failing validation aborts boot loudly.

### Overrides and roles

- `GOFASTR_SETUP=off` — skip setup mode (declare bootstrap is handled externally).
- `GOFASTR_SETUP=force` — re-enter the wizard even when already complete (rescue mode).
- `GOFASTR_ROLE=worker` refuses to start while setup is incomplete; finish setup on a `serve`/`all` process first.

## Custom steps

Declare steps with `setup.Step{Name, Title, Fields, Run}` using `i18n.Define` phrases so the wizard localizes automatically. Mark sensitive fields `Secret: true` (rendered as password inputs, never prefilled, logged, or space-trimmed). Include `setup.HealthStep(app)` to verify the app's registered readiness probes (`App.RegisterReadiness` — DB is registered automatically; register readiness probes for storage or email if you want `HealthStep` to verify them too).

## Common mistakes

- **Passing the wrong table name to `AdminStep`:** if `Complete` checks `"users"` while auth writes `"auth_users"`, setup stays incomplete forever.
- **Sharing a setup link with the single-use token:** the first click spends it; restart the app if you lose the setup cookie.
- **Expecting cron or queues to run during setup:** background workers wait until the wizard completes and swaps to the app router.
- **Using `GOFASTR_SETUP=off` to bypass a broken step:** fix the step or `Complete` predicate instead.
