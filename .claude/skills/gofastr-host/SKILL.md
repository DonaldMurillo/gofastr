---
name: gofastr-host
description: Auto-loads when working on a *host application* that imports the GoFastr framework (not the framework itself). Encodes the "don't reinvent — reach for the battery first" rule and the import paths an agent needs. Triggers on edits to Go files in repos that import `github.com/DonaldMurillo/gofastr/...`, on `main.go` files calling `framework.NewApp`, and on phrases like "login", "signup", "session", "user table", "log out", "magic link", "forgot password", "reset password", "add admin page", "back office", "audit log", "audit trail", "compliance log", "send email", "transactional email", "welcome email", "send a notification", "notify the user", "background job", "async task", "schedule", "cron", "run every hour", "retry on failure", "upload", "store images", "store files", "S3", "MinIO", "attachments", "avatar", "full-text search", "find records containing", "outbound webhook", "signed callback", "POST to a customer URL", "cache", "memoize", "remember for N seconds", "CSRF", "RBAC", "require admin", "roles", "rate limit", "throttle", "request log", "access log", "panic recovery", "structured logging", "live debug", "per-test isolated DB", "test fixture", "favicon", "app icon", "SEO", "meta tags", "Open Graph", "JSON-LD", "structured data", "sitemap", "robots.txt", "accessibility", "a11y", "WCAG", "aria-label", "upgrade gofastr", "bump the framework version", "migrate to the new version".
---

# GoFastr host-app — load this before writing app code

You're in a Go app that uses the GoFastr framework. The framework already
ships ~70% of the surface a real app needs. Before writing anything new,
**read this skill, the project's `AGENTS.md`, and the matching detail
file under `agents/`** — they exist to keep you from reinventing what's
already there.

For UI work, also read and complete the project's `DESIGN.md` before selecting
components, then open `agents/ui.md` and run
`gofastr docs ui-composition-recipes`. The app owns product intent and
information hierarchy; the framework supplies the structural and visual
primitives. Render desktop/mobile in both schemes and revise the three weakest
visible decisions before calling the UI complete.

For record, incident, and operational pages, reach first for
`ui.RecordSummary` + `ui.MetricBand`: one dominant summary, one compact signal
band, and actions in the summary's natural-width `Actions` slot. Do not repeat
the same state in a Banner. Keep its description to one or two sentences and
its highlight to one decision plus one short condition. Use the compact
`Aside` for owner/presence context, use `MetricBandItem.Hint` for trends, and
move the full narrative or roster later. The component keeps `Actions` in its
lead region so the primary path stays in the first useful phone viewport. Use
`SiteHeaderConfig.MobileBrand` when the desktop identity is too long for the
phone header. On wide detail routes, pair related bounded modules such as
`DetailList`s in `ui.Grid`; do not leave a narrow stacked column beside an
accidental empty rail. Reflow the pair to one column on phones. `ui.Cluster`
wraps whole controls by default; use `ClusterConfig.NoWrap` only for compact
chrome that is guaranteed to fit.

A fresh scaffold already mounts the adaptive `framework/ui/theme.Default()`
palette, so `ui.ThemeToggle` and OS dark preference have complete light/dark
tokens. Keep that `WithTheme` call. If you replace it with an app-owned theme,
define every semantic `DarkColors` value before rendering a toggle. Use
`ui.Link` for visible text links and `ui.SiteFooter` for linked footer chrome;
do not rely on browser-default `<a>` colors. A linked `SiteHeader` Brand slot is
the exception because SiteHeader owns its appearance.

`AGENTS.md` is a thin TOC of every framework primitive with its
trigger phrases. When your task matches a row, open the linked file
under `agents/` (e.g. `agents/battery-admin.md`) for the full
shape / import / don't-reinvent breakdown.

If the project has no `AGENTS.md`, run `gofastr agents init` once to
generate both the TOC and the `agents/` detail files.

Upgrading the framework: install the target CLI first (an old binary
can't know newer migration notes), run `gofastr upgrade [--to vX.Y.Z]`
to see every migration-relevant change between your version and the
target (with file:line hits in this app), `--apply` to run
go get / tidy / build / test, then `gofastr agents sync` to refresh the
generated guidance.

## Don't reinvent — reach for these first

| Task phrasing | Use this |
|---|---|
| audit trail, who-did-what, compliance log | `app.WithAuditLog(cfg)` (chained *App method) |
| admin pages, ops dashboard, queue browser | `battery/admin` |
| login, signup, session, CSRF, bcrypt, "require admin" | `battery/auth` |
| send notification, password reset link, fan-out across channels | `battery/notify` |
| background job, scheduled task, retry-on-failure | `battery/queue` (use `DBQueue` for real workloads) |
| send email, SMTP, transactional mail | `battery/email` |
| file upload, S3, MinIO, attachments | `battery/storage` + `framework.WithFileStorage` |
| full-text search | `battery/search` |
| outbound signed webhooks with retry | `battery/webhook` |
| cache key/value, memoize, "remember for N seconds" | `battery/cache` |
| UI components, page layout, forms, tables, theming, dark mode | `framework/ui` (~100 components) — `gofastr docs ui-composition-recipes` for page grammar, `gofastr docs ui-new-components` for the catalog, live demos at `/components/<slug>` on the docs site (`examples/site`) |
| structured request log, panic recovery, log MCP debug tools | `battery/log` |
| browser refresh on `gofastr dev` rebuild | auto-wired if your `main.go` uses `framework.NewApp` + `uihost.New` — no host code; for custom bootstraps call `dev.RegisterLiveReload(router)` manually |
| agent debugging under `gofastr dev` | auto-wired by `framework.NewApp`: /mcp mount + introspection (`app_routes`, `framework_docs_search`, …) + control (`app_module_enable/disable`) + battery/log debug tools — opt out with `GOFASTR_DEV_MCP=0`; production needs explicit `WithMCP`/`WithMCPIntrospection`/`WithMCPControl` |
| load `.env` files | auto-wired by `framework.NewApp` — do nothing |
| per-test isolated Postgres DB | `framework/testkit.NewIsolatedDB(t, adminDSN, migrate)` |
| favicon, app icon, PWA icons | `uihost.WithAppIcon(pngBytes)` — one source image becomes 32/180/192/512 PNGs, `/favicon.ico`, head links, and the PWA manifest icons; generate placeholder art in code with `framework/image.NewGradient` (no committed binaries) |
| SEO: page title/description, Open Graph, Twitter cards, canonical, JSON-LD, sitemap, robots | uihost `WithDescription`/`WithOpenGraph`/`WithSitemap`/`WithRobots`/`WithRobotsMeta` + per-screen `ScreenSEO`/`ScreenSchema` interfaces — `gofastr docs seo`. Static export writes sitemap.xml/robots.txt from the same config |
| accessibility, ADA/WCAG compliance, "aria-label", a11y audit | `gofastr audit a11y` (static guided lint) and `gofastr audit a11y --url <base>` (axe-core in headless Chrome, both color schemes, pages from /sitemap.xml). Note `gofastr build` enforces the static lint — fix findings, don't reach for `--no-a11y` — `gofastr docs accessibility` |
| upgrading the framework version, migration notes between releases | install the target CLI first, then `gofastr upgrade [--to vX.Y.Z] [--apply]` — shows every migration-relevant change in range with file:line hits in your app — `gofastr docs upgrading`; finish with `gofastr agents sync` |

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
5. **Strict CSP and one styling surface.** Host apps ship zero bespoke CSS and
   zero hand-rolled structural markup. Themes mutate tokens; missing visual or
   layout treatments are framework component gaps to add upstream. The
   framework runtime is the only script tag; extra scripts use
   `WithExtraScripts(externalURL)`.
6. **`gofastr dev` sets `GOFASTR_DEV=1`** on the child process — the
   framework auto-wires browser reload via SSE. Don't write your own
   livereload. Opt out with `GOFASTR_DEV_LIVERELOAD=0`.

## Project shape

- `main.go` builds the App: `framework.NewApp(WithDB, WithConfig, …)`; chained
  methods like `app.WithAuditLog(cfg)` come after.
- `entities/` declares `EntityConfig`s — use `Seed` + `SeedFS` instead
  of hand-rolled `seedIfEmpty` helpers.
- `screens/` is one screen per page; in-page state changes are island
  RPCs, not new routes.
- Migrations: prefer `auto-migrate` for greenfield schemas; otherwise
  versioned migrations under `migrations/`.
- Tests against Postgres: `framework/testkit.NewIsolatedDB(t, adminDSN, migrate)`
  carves a fresh DB per test. **Don't `t.Skip` when the DB is missing —
  hard-fail.**

## When in doubt

1. Search the project's `AGENTS.md` for the keyword first.
2. Search the framework with `grep -rn <symbol> $(go env GOMODCACHE)/github.com/DonaldMurillo/gofastr@*`.
3. Run `gofastr docs --grep <term>` to search the embedded docs, or `gofastr docs <topic>` to read one.
4. Use the live `/mcp` introspection tools (`framework_docs_search`,
   `app_routes`, `app_batteries`, etc.) if the app is running locally
   with `framework.WithMCPIntrospection()` — blueprint-generated apps
   wire it (plus `framework.WithMCP()`) by default.
