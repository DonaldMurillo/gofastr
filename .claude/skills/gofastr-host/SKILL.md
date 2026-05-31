---
name: gofastr-host
description: Auto-loads when working on a *host application* that imports the GoFastr framework (not the framework itself). Encodes the "don't reinvent — reach for the battery first" rule and the import paths an agent needs. Triggers on edits to Go files in repos that import `github.com/DonaldMurillo/gofastr/...`, on `main.go` files calling `framework.NewApp`, and on phrases like "login", "signup", "session", "user table", "log out", "magic link", "forgot password", "reset password", "add admin page", "back office", "audit log", "audit trail", "compliance log", "send email", "transactional email", "welcome email", "send a notification", "notify the user", "background job", "async task", "schedule", "cron", "run every hour", "retry on failure", "upload", "store images", "store files", "S3", "MinIO", "attachments", "avatar", "full-text search", "find records containing", "outbound webhook", "signed callback", "POST to a customer URL", "cache", "memoize", "remember for N seconds", "CSRF", "RBAC", "require admin", "roles", "rate limit", "throttle", "request log", "access log", "panic recovery", "structured logging", "live debug", "per-test isolated DB", "test fixture".
---

# GoFastr host-app — load this before writing app code

You're in a Go app that uses the GoFastr framework. The framework already
ships ~70% of the surface a real app needs. Before writing anything new,
**read this skill, the project's `AGENTS.md`, and the matching detail
file under `agents/`** — they exist to keep you from reinventing what's
already there.

`AGENTS.md` is a thin TOC of every framework primitive with its
trigger phrases. When your task matches a row, open the linked file
under `agents/` (e.g. `agents/battery-admin.md`) for the full
shape / import / don't-reinvent breakdown.

If the project has no `AGENTS.md`, run `gofastr agents init` once to
generate both the TOC and the `agents/` detail files. Refresh with
`gofastr agents sync` after a framework upgrade.

## Don't reinvent — reach for these first

| Task phrasing | Use this |
|---|---|
| audit trail, who-did-what, compliance log | `framework.WithAuditLog(cfg)` |
| admin pages, ops dashboard, queue browser | `battery/admin` |
| login, signup, session, CSRF, bcrypt, "require admin" | `battery/auth` |
| send notification, password reset link, fan-out across channels | `battery/notify` |
| background job, scheduled task, retry-on-failure | `battery/queue` (use `DBQueue` for real workloads) |
| send email, SMTP, transactional mail | `battery/email` |
| file upload, S3, MinIO, attachments | `battery/storage` + `framework.WithFileStorage` |
| full-text search | `battery/search` |
| outbound signed webhooks with retry | `battery/webhook` |
| cache key/value, memoize, "remember for N seconds" | `battery/cache` |
| structured request log, panic recovery, log MCP debug tools | `battery/log` |
| browser refresh on `gofastr dev` rebuild | auto-wired if your `main.go` uses `framework.NewApp` + `uihost.New` — no host code; for custom bootstraps call `dev.RegisterLiveReload(router)` manually |
| load `.env` files | auto-wired by `framework.NewApp` — do nothing |
| per-test isolated Postgres DB | `framework/testkit.NewIsolatedDB(t, dsn)` |

## Hard rules

1. **Every `<form method="POST">` includes `auth.CSRFInputFromCtx(ctx)`.**
   The CSRF middleware rejects unsigned mutations. No exceptions.
2. **`_, _ = db.Exec(...)` is almost always wrong.** The two acceptable
   cases (best-effort telemetry, search-history logging) require a
   comment on the call line explaining why failure is tolerable.
3. **Never hand-roll bcrypt / session cookies / the users-table schema.**
   `auth.HashPassword`, `auth.CheckPassword`, `auth.SessionMiddleware`,
   `auth.UserEntityFields()` are the contract.
4. **Audit log writes are not best-effort.** Failing audit fails the
   user write — that's the point.
5. **Strict CSP, no inline scripts/styles.** Add CSS rules in your
   theme module + classes on tags. The framework's runtime is the only
   script tag; the rest is `WithExtraScripts(externalURL)`.
6. **`gofastr dev` sets `GOFASTR_DEV=1`** on the child process — the
   framework auto-wires browser reload via SSE. Don't write your own
   livereload. Opt out with `GOFASTR_DEV_LIVERELOAD=0`.

## Project shape

- `main.go` builds the App: `framework.NewApp(WithDB, WithConfig, WithAuditLog, …)`.
- `entities/` declares `EntityConfig`s — use `Seed` + `SeedFS` instead
  of hand-rolled `seedIfEmpty` helpers.
- `screens/` is one screen per page; in-page state changes are island
  RPCs, not new routes.
- Migrations: prefer `auto-migrate` for greenfield schemas; otherwise
  versioned migrations under `migrations/`.
- Tests against Postgres: `framework/testkit.NewIsolatedDB(t, dsn)`
  carves a fresh DB per test. **Don't `t.Skip` when the DB is missing —
  hard-fail.**

## When in doubt

1. Search the project's `AGENTS.md` for the keyword first.
2. Search the framework with `grep -rn <symbol> $(go env GOMODCACHE)/github.com/DonaldMurillo/gofastr@*`.
3. Run `gofastr docs --grep <term>` to search the embedded docs, or `gofastr docs <topic>` to read one.
4. Use the live `/mcp` introspection tools (`framework_docs_search`,
   `app_routes`, `app_batteries`, etc.) if the app is running locally
   with `framework.WithMCPIntrospection()`.
