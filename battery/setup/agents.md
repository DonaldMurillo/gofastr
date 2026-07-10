# battery/setup

First-run setup for self-hosted apps. While the app's `Complete`
predicate reports false, `Start` serves an SSR setup wizard instead of
the app router: every other path 503s, `/healthz` + `/readyz` stay up,
and background consumers (cron, queue, outbox relay) wait. When the
final step completes, the handler swaps to the real app atomically —
no restart. The same steps also run **headless** when their env vars
fully resolve, for IaC installs.

**Use this when** the prompt mentions: first run, first boot, install
wizard, initial admin, bootstrap an empty database, "create the first
user", onboarding a fresh deploy, setup token.

**Import:** `github.com/DonaldMurillo/gofastr/battery/setup`

**Shape:**
```go
runner := setup.New(setup.Config{
    Title: "My App Setup",
    Steps: []setup.Step{
        setup.AdminStep(authManager, db, "auth_users"), // initial admin
        setup.HealthStep(app),                          // readiness checks
    },
    Complete: func(ctx context.Context) (bool, error) {
        // Derived state — e.g. "does an admin user exist?".
        var n int
        err := db.QueryRowContext(ctx,
            `SELECT COUNT(*) FROM auth_users`).Scan(&n)
        return n > 0, err
    },
})
app := framework.NewApp(framework.WithSetup(runner), …)
```

**Behavior:**
- Interactive wizard at `/setup`, gated by a **single-use token**
  printed in the boot banner (exchanged for an HttpOnly cookie on
  first visit; restart mints a fresh one). `DisableToken: true` opts
  out. POSTs are origin-guarded (Sec-Fetch-Site) regardless.
- Headless: if every field's `EnvVar` resolves
  (`GOFASTR_ADMIN_EMAIL`/`GOFASTR_ADMIN_PASSWORD` for `AdminStep`),
  steps run before the port binds and failures abort `Start` loudly.
- `GOFASTR_SETUP=off` skips setup mode entirely; `force` re-enters it
  as a rescue mode; invalid values fail `Start`.
- Completion is **derived**, never a marker file — a crash mid-setup
  re-enters setup on next boot. Worker-role processes refuse to start
  while setup is incomplete.

**Rules for agents:**
- Never render the completion page yourself or claim setup is done —
  the battery only shows "Setup Complete" after `Complete` confirms it
  and the handler swap fired.
- The wizard UI is composed exclusively from `framework/ui`
  (AuthCard, ProgressSteps, Form, FormField, Notification, Stack) —
  zero bespoke CSS, zero hand-rolled markup. Keep it that way.
- `AdminStep` takes the users table name explicitly — pass the same
  table the auth battery writes to (canonically `auth_users`).

Full doc: `gofastr docs first-run` / `framework/docs/content/first-run.md`.
