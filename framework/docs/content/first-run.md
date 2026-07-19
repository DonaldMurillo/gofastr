# First-run setup

An operator deploying your binary against an empty database gets no
admin account, no verified adapters, and no way in. The setup battery
(`battery/setup`) gives every self-hosted GoFastr app the same minute
one: an interactive setup wizard on first boot, and a fully scriptable
headless path for IaC installs — two skins over one bootstrap API.

## Wiring

```go
import (
    "github.com/DonaldMurillo/gofastr/battery/auth"
    "github.com/DonaldMurillo/gofastr/battery/setup"
)

adminStep, complete := setup.AdminStep(authManager, db, "auth_users")
runner := setup.New(setup.Config{
    Steps:    []setup.Step{adminStep, setup.HealthStep(app)},
    Complete: complete,
})
app := framework.NewApp(
    framework.WithDB(db),
    framework.WithSetup(runner),
)
```

`Complete` decides everything: while it reports false, the app is in
setup; the moment it reports true, setup is over. It is **derived
state** — "at least one user exists" for `AdminStep` — never a marker
file, so a crash mid-setup simply re-enters setup on the next boot.

## The interactive skin

When setup is incomplete and required values are missing from the
environment, `Start` binds the port but serves setup pages instead
of the app router: the SSR wizard (composed from the design
system — `AuthCard`, `ProgressSteps`, `FormField`), plus `/healthz`
and `/readyz`. Every other path answers 503 "setup required" — no
entity CRUD, no OpenAPI, no admin, nothing reachable before bootstrap.

The banner prints a one-time setup URL:

```
Setup: http://192.168.1.20:8080/setup?token=9f2c…
```

The token is required (a fresh instance's setup page is an
instance-takeover window — whoever reaches it first owns the app).
It is **single-use**: the first successful visit exchanges it for an
HttpOnly cookie and invalidates the URL form, so a token that leaked
into an access log cannot be replayed. Lost the session? Restart the
app — setup re-enters and a fresh token is printed.
`Config.DisableToken` opts out for trusted networks; wizard POSTs are
still origin-guarded either way.

When the final step succeeds, the serving handler swaps atomically to
the real router — no restart. Background consumers (cron, queues, the
outbox relay) do not start until that swap: setup owns the schema's
initial state.

## The headless skin (IaC)

Every wizard field maps to an environment variable. When setup is
incomplete and **all** required values resolve from the environment,
the steps run synchronously during `Start`, before the port binds —
no wizard is ever exposed:

```sh
GOFASTR_ADMIN_EMAIL=ops@example.com \
GOFASTR_ADMIN_PASSWORD=$(cat /run/secrets/admin_pw) \
./myapp
```

A failing step aborts `Start` with an error naming the step — a bad
password (the auth battery's policy applies) fails the boot, loudly.
Partially-set env is not headless: the wizard appears with the known
values prefilled (secrets excepted — never echoed into HTML).

## Overrides

- `GOFASTR_SETUP=off` — never enter setup mode; `Start` proceeds even
  with `Complete` false. Sharp edge, deliberate: you are declaring
  bootstrap is handled elsewhere.
- `GOFASTR_SETUP=force` — enter setup even when complete (rescue). The
  wizard warns that setup already completed.
- An invalid value fails `Start`, mirroring `GOFASTR_ROLE`.

Worker-role processes (`GOFASTR_ROLE=worker`) refuse to start while
setup is incomplete — run a serve/all process to complete setup first.

## Custom steps

A step is a name, its form fields, and a `Run`:

```go
setup.Step{
    Name: "instance",
    Fields: []setup.Field{{
        Name: "site_url", Label: "Public URL",
        EnvVar: "MYAPP_SITE_URL",
        Validate: func(v string) error { … },
    }},
    Run: func(ctx context.Context, values map[string]string) error {
        return saveInstanceConfig(ctx, values["site_url"])
    },
}
```

Fields with `Secret: true` render as password inputs and are never
prefilled or logged. `setup.HealthStep(app)` runs the app's registered
readiness checks (the same ones `/readyz` serves) and fails with a
per-adapter, actionable error list — DB, storage, and email
verification come free if those batteries registered checks.

## Ordering with proxies and TLS

The setup cookie is marked `Secure` when the request arrived over TLS
or with `X-Forwarded-Proto: https`; plain-http LAN installs work
without ceremony. If a TLS-terminating proxy fronts the app, forward
that header (every mainstream proxy does by default).

## Common mistakes

- **A `Complete` predicate that doesn't observe the steps' writes.**
  E.g. `AdminStep(mgr, db, "users")` while the auth battery writes to
  `auth_users` — every step succeeds but setup stays incomplete, and
  the wizard tells you so instead of showing the completion page. Pass
  the table your auth store actually writes to.
- **Writing a marker file to record completion.** Completion is
  derived state on purpose: a marker file survives a crash mid-setup
  and would boot a half-bootstrapped app straight into serving. Derive
  it from the data the steps create.
- **Sharing the setup URL with the token in it.** The token is
  single-use — the first visit exchanges it for a cookie and
  invalidates the URL form. A second person following the same link
  gets a 403; restart the app to mint a fresh token.
- **Expecting cron/queue/outbox work during setup.** Background
  consumers deliberately wait for the handler swap. If a step needs a
  side effect, do it in the step's `Run`.
- **`GOFASTR_SETUP=off` as a "fix" for a stuck wizard.** It declares
  bootstrap is handled elsewhere and boots the app with `Complete`
  still false. Fix the predicate or the step instead; use `force` to
  re-enter the wizard on a completed install.
