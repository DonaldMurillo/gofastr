# Changelog

All notable changes to GoFastr. Follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) with semver-ish
calendar versions (`YYYY-MM-DD` per substantive release until the API
stabilises). Breaking changes are clearly marked with **BREAKING**.

## [Unreleased]

### Security

- **BREAKING — admin battery is default-deny for non-admins.** With no custom
  `Config.Authorize`, the admin now requires an authenticated user holding the
  admin role (`Config.AdminRole`, default `"admin"`) — detected via the
  structural `GetRoles() []string` interface (`battery/auth.User` satisfies it).
  Previously any authenticated, non-nil user reached full admin CRUD over every
  exposed entity, so a freshly-registered reader was effectively an admin.
  Authenticated-but-unauthorized now returns `403` (vs `401` for anonymous).
  Docs: `framework/docs/content/admin.md`.
- **Per-operation RBAC on auto-CRUD — `EntityConfig.Access`.** Declare the
  permission required for each operation (`Read` covers List+Get, plus
  `Create`/`Update`/`Delete`) and auto-CRUD refuses requests lacking it with
  `403` — across List/Get/Create/Update/Delete and the batch/stream variants.
  Previously auto-CRUD had **no permission check at all**: exposing an entity
  granted every authenticated user full CRUD unless the host hand-composed
  route-group middleware. New seams: package-level **`access.Can(ctx, perm)`**
  and **`access.Middleware(policy, roles)`** (re-exported as `framework.Can` /
  `framework.AccessMiddleware`) to install policy+roles in one line. **BREAKING:**
  `access.Policy.Can` / `RolePolicy.Can` drop the unused `resource any`
  parameter. Docs: `framework/docs/content/access-control.md`.
- **BREAKING — multi-tenant CRUD is now fail-CLOSED over HTTP.** A
  `MultiTenant` entity served with no tenant id in the request context is
  refused with `401` on every operation (list/get/create/update/delete, batch,
  stream, SSE), matching the in-process CRUD API which already failed closed.
  Previously the HTTP path failed *open* — an empty tenant id disabled filtering
  and returned/mutated every tenant's rows, a silent cross-tenant data leak.
  Deliberate cross-tenant access (admin tooling) must now opt in explicitly and
  server-side via the new **`tenant.AllowCrossTenant(ctx)`** marker (never from
  a client header). New seam: **`CrudHandler.RequireTenant(w, r)`**, the HTTP
  mirror of `RequireOwner`, run alongside the owner gate through a single
  `requireScope` chokepoint. Docs: `framework/docs/content/multi-tenant.md`.

### Fixed

- **Scaffolded apps accept a bare `$PORT`.** `isolation.Runtime.Addr` now
  normalizes a bare numeric port (e.g. `PORT=8088`, as Heroku/Render/Railway/
  Cloud Run inject) to `":8088"`. Previously the generated `main.go` printed
  `http://8088` and then died with `missing port in address` on every such PaaS.
- **`examples/blog` runs again.** It loaded entities from a nonexistent
  `entities/` directory (`go run ./examples/blog` failed immediately, despite
  being the README's first step). Entities are now declared in Go (self-
  contained, runs from any cwd; `gofastr.yml` still mirrors them for the
  codegen path), and seeding runs after AutoMigrate so the demo data actually
  lands. Added a boot+HTTP-200 test (`examples/blog`) — the missing test layer
  the assessment flagged.

- **kiln: free-order authoring no longer bricks the rebuild.** Adding an entity
  with a `BelongsTo` to a not-yet-created entity (e.g. `posts`→`users` before
  `users` exists) failed the live auto-migrate and left the session unable to
  rebuild. The live migrator now defers a dangling `BelongsTo` and re-derives it
  once the target is added; the durable world and `kiln freeze` keep the full
  relation. Fixes the deterministically-red `TestFreezeRoundTripWithRichWorld`.
- **kiln: poison journal entries can no longer persist.** `live.Apply` now
  validates an entry with a trial rebuild **before** the durable journal append,
  so an entry that fails to rebuild is rejected and never written (previously it
  was fsynced first, then re-failed on every restart). On any failure the
  in-memory session is restored by replaying the journal.

### Added

- **`framework.WithPublicOpenAPI()` / `AppConfig.PublicOpenAPI`.** Serves
  `/openapi.json` without the auth gate. The spec is auth-gated by default (it
  enumerates every route), so a minimal app returned `401` there — surprising
  anyone following the quickstart `curl`. Swagger UI at `/api/docs/` is
  unaffected. README quickstart updated to call this out.
- **LICENSE — GoFastr is now MIT licensed.** A top-level `LICENSE` file (MIT)
  replaces the previous "all rights reserved / no license chosen" note. The code
  is now free to use, modify, and redistribute (including commercially) with the
  copyright notice preserved. This unblocks adoption, vendoring, and deployment.
- **Framework DX round-4.** Closes a focused batch from the V4 host-app feedback:
  - **`render.If(cond, html) HTML` / `render.When(cond, fn) HTML`** — inline conditional fragments. `When` is the lazy form for expensive truthy branches.
  - **`render.Classes(parts ...string) string`** — joins non-empty class strings with spaces. Pair with **`render.ClassIf(cond, name) string`** for sparse conditionals: `render.Classes("base", render.ClassIf(isActive, "active"))`. Coexists with `html.Classes(map[string]bool)` for predicate-dense cases.
  - **`html.InputConfig.Value` / `.Placeholder`** and **`html.TextAreaConfig.Content` / `.Placeholder` / `.Rows` / `.Cols`** — typed fields for the common attributes; killed the V4 papercut of falling back to `render.Tag("textarea", attrs, render.Text(content))` for prefilled edit sheets. `Attrs` remains as the escape hatch.
  - **`EntityConfig.Seed func(ctx, *sql.DB) error`** — runs once per entity after `AutoMigrate`. Completion is recorded in a new `_gofastr_seeded` ledger table; subsequent restarts skip seeded entities. Errors abort `App.Start`.
  - **`EntityConfig.SeedFS fs.FS` + `EntityConfig.SeedPath string`** — bind embedded seed data to an entity; reachable inside `Seed` via **`entity.SeedDataFromContext(ctx) ([]byte, error)`**. Removes loose JSON files from tarball-style single-binary deploys.
  - **`App.RegisterEntities(map[string]entity.EntityConfig) *App`** — sugar over multiple `Entity(...)` calls. Iterates the map in alphabetical-by-name order so route registration, OpenAPI tag emission, and MCP tool list order are deterministic across restarts. FK ordering stays correct because AutoMigrate also topologically sorts.
  - **`style.Contribute(func(*StyleSheet)) / style.Apply(*StyleSheet)`** — co-located scoped styles. Declare CSS next to the Go render code via `var _ = style.Contribute(...)` at package scope; the host calls `style.Apply(ss)` inside `createStyleSheet`. Final CSS is identical between dev and prod — no nonces, no inline `<style>`, no CSP relaxation. Distinct from `registry.RegisterStyle` (named, lazy-loaded per-component sheet); `Contribute` adds fragments to the host's global theme stylesheet. Kills the 3-file (screen + theme + reload) iteration cycle.
  - `App.Router()` doc comment now points application-level code at `App.Use` / `App.Group` and documents `Router()` as the plugin/internal surface.
  - **`App.Entity` panics at registration** when `SeedFS` is set but `SeedPath` is empty — a misconfiguration that would otherwise silently mark the entity as seeded with empty data on first run.
  - **`App.Start` failure paths drain via `Shutdown`** — AutoMigrate / RunSeeds / InitPlugins / runStartHooks errors no longer leak goroutines past Start returning. The app lifecycle context is created before AutoMigrate so RunSeeds and individual Seed functions can observe cancellation.
  - **`migrate.RunSeeds` reads the ledger in one round-trip** (was N+1 per entity) and emits per-seed lifecycle slog events (`seed start`, `seed done`, `seed skip`, `seed ledger read`) when a logger is attached via `migrate.WithSeedLogger(ctx, l)`.
  - **`webhook.VerifyTimestamped` rejects non-positive tolerance** (was: silently skipped the replay check) and out-of-range timestamps. Added **`webhook.DefaultTimestampTolerance = 5 * time.Minute`** as the suggested default.
  - **`entity.Registry.AllSorted() []*Entity`** — returns entities in alphabetical-by-name order so order-sensitive consumers (`OpenAPI` tag emission, `crud.RegistryLLMMD`) produce byte-stable output across restarts. Existing `All()` keeps the map shape but its godoc now spells out that map iteration is randomised. Fixes a pre-existing non-determinism that broke ETag caching of `/openapi.json` and `/api/llm.md`.
  - **`gofastr audit deps`** CLI command — scans the project for packages whose `init()` mutates framework-wide state (`style.Contribute`, `registry.RegisterStyle`, `render.RegisterComponent` / `RegisterLayout` / `RegisterFunc`). Output is grouped by Go import path; pairs with the documented supply-chain trust model on `style.Contribute`. Docs: `framework/docs/content/audit-deps.md`.
- **`core/dotenv` package + auto-load in `framework.NewApp()`.** Probes `.env.local`, `.env.<APP_ENV>` (when `APP_ENV` set), and `.env` from CWD before option processing. Existing `os.Environ` always wins. Parser handles double/single-quoted values, escapes, optional `export` prefix, comments; rejects malformed input loudly. Bracket-form `${VAR}` expansion with cycle detection, depth cap, undefined-as-empty, and `\${literal}` escape. Disable via `GOFASTR_DOTENV=off` in the process env. `cmd/gofastr migrate` now routes through this instead of its ad-hoc 1-key scanner. Docs: `framework/docs/content/dotenv.md`.
- **SSR auth policies.** `core-ui/app` exposes a `Policy { Decide(ctx) Decision }` machinery with four decision kinds (Allow / Redirect / RenderAlt / Block). Attach via `Screen.WithPolicy(p)` or `NewScreenGroup(prefix, layout, policies...)`. Construct decisions through the new `core-ui/app/decide` subpackage so call sites don't shadow common variable names: `decide.Allow()`, `decide.Redirect(url)`, `decide.RenderAlt(factory)`, `decide.Block(status, msg)`.
- **`battery/auth.SessionPolicy(opts...)` and `RolePolicy(roles, opts...)`** are the SSR counterparts to the existing `RequireSession` / `RequireRole` middleware. Options: `WithRedirect(url, ...RedirectOpt)`, `WithRenderAlt(factory)`, `WithBlock(status, msg)`. `RedirectOpt`: `NoNext()` to suppress the auto-appended `?next=<request-path>`.
- **`auth.SessionFrom(ctx) (User, bool)`** — cheap in-component getter for ctx-aware chrome (sibling nav, conditional CTAs). Pair with `RenderCtx` for in-page gating without policy machinery.
- **`auth.Roles(roles ...string) []string`** — ergonomic literal-list helper so `auth.RolePolicy(auth.Roles("admin", "owner"), ...)` reads cleanly. Documents the asymmetry with the variadic `auth.RequireRole`.
- **`component.ContextComponent { RenderCtx(ctx) HTML }`** — the optional ctx-aware render interface. Does NOT embed `Component` (so a type can satisfy it via just one method). Embed `component.ContextOnly{}` to also satisfy `Component` with a stub `Render` that the framework never calls.
- **`framework.entity.EntityDeclaration.OwnerField` JSON key (`owner_field`).** Mirrors `EntityConfig.OwnerField` so per-user CRUD scoping works for entities declared in JSON, not just Go.
- **DevMode auto-mints a random JWT secret** when `AuthConfig.JWTSecret == ""`. 32 cryptographically-random bytes, base64-encoded, logged as WARN. Sessions invalidate on restart — set `JWTSecret` for stable dev tokens.
- **`X-Gofastr-Location` partial-redirect signal.** Policy-Redirect outcomes on a partial fetch return 200 + that header + empty body (NOT 303 — the runtime fetcher uses `redirect:'follow'` and would auto-chase a 303, losing the header). The runtime's `loadPage` calls itself with the redirected URL and updates `pushState`.

### Removed (greenfield cleanup)

- **BREAKING — escape-hatch field `Attrs` renamed to `ExtraAttrs`** across `core-ui/html/*.Config`, `core-ui/patterns/{disclosure,sortablelist,multiselect}.Config`, and every `framework/ui/*.Config` that exposes a passthrough HTML attribute bag. The new name signals "extra attributes beyond the typed surface" so callers reach for typed fields first. `core/featureflag.Flag.Attrs` stays — it's primary data, not an escape hatch. `html.Attrs` *type* alias is unchanged.
- **BREAKING — 410 GONE compat endpoints removed**. `/__gofastr/theme.css`, `/__gofastr/styles.css`, `/__gofastr/routes.js`, `/__gofastr/catalog.js`, `/__gofastr/css/<path>` now 404 instead of serving a 410 with a migration hint. Use `/__gofastr/app.css` for CSS; routes + catalog ship inline as `<script type="application/json">` in the SSR'd page; per-component CSS comes from `/__gofastr/comp/<name>.css` via `registry.RegisterStyle`.
- **Dead code removed**: `migrate.alreadySeeded` (replaced by batch `readSeededSet`), `i18nui.replaceAll` (inlined to `strings.ReplaceAll`).
- **Doc framing cleanup**: removed "legacy", "back-compat", "kept for", "transitionally" language from comments that describe current first-class APIs (cursor pagination, runtime.js, framework facade, decodeCursorAny, App.Shutdown).

### Changed

- **BREAKING — form intercept is opt-in.** `<form>` elements with the default `application/x-www-form-urlencoded` or `multipart/form-data` enctype are NOT intercepted by `runtime.js`. The browser submits them natively (cookies set, `Location:` followed, file uploads, password-manager UX all work without any framework involvement). Forms posting to a JSON endpoint must opt INTO interception with `enctype="application/json"` OR `data-fui-spa`. `data-fui-rpc` still triggers RPC dispatch as before. **Migration:** `grep -rn '<form' .` — forms that POST to a JSON CRUD/island handler need `enctype="application/json"` added; forms that POST to a redirect-returning handler (auth, settings) need no change.
- **BREAKING — `core-ui/app.App.RenderPage` / `RenderPartial` now wrap richer `*Result` variants.** Returns an error for `Redirect` and `Block` decisions (the legacy shape can't express them). Use `App.RenderPageResult` / `RenderPartialResult` for the policy-aware shape.
- **BREAKING — `core-ui/app.Router.Render` → `Router.RenderRaw`** and **`App.RenderScreen` → `App.RenderScreenRaw`**. Renamed to call out that they bypass the Policy chain. HTTP-serving code must use `App.RenderPageResult`; `RenderRaw` is for SSG/internal callers.
- **BREAKING (effectively no-op) — `core/router.Middleware` is now a type ALIAS for `core/middleware.Middleware`.** Anonymous-func cast no longer needed when feeding `battery/auth.SessionMiddleware(mgr)` (or any battery middleware) into `Router.Use(...)`. Existing `router.Middleware(x)` conversions still compile. NOTE: `core/middleware/tracing_test.go` moved to `package middleware_test` because the alias introduces a test-only cycle.
- **BREAKING — `Screen.Policies` field unexported.** Use `Screen.WithPolicy(p)` to add, `Screen.PolicyChain()` to read a copy. Matches `ScreenGroup.policies` (already unexported).
- **Kiln-rendered `form` nodes default `enctype="application/json"`** because they target CRUD endpoints. The world API accepts an explicit `enctype` prop to override.

### Fixed

- **SECURITY (P0) — `/auth/register` no longer honors client-supplied `roles`.** Was an anonymous privilege escalation: any visitor POSTing `roles=admin` (form or JSON) was created with admin role. Form-encoded requests were CSRF-reachable from any origin. Now roles are server-assigned to `["user"]` by default; role elevation must happen via a separate admin-gated flow. Regression tests in `battery/auth/register_roles_security_test.go`.
- **SECURITY (P0) — `X-Gofastr-Location` open-redirect sealed.** A policy returning `decide.Redirect("//evil.com")` (or any non-relative URL) was emitted into the header raw, which the runtime feeds to `loadPage()` — a cross-origin fetch with credentials. Sealed via `isSafePartialRedirect` in uihost: only same-origin relative paths flow through the header path; absolute / protocol-relative / scheme-bearing / backslash-bypass URLs fall through to a hard 303 (which the browser handles safely). 8-case regression table in `framework/uihost/partial_redirect_test.go`.
- **(P0) Mutex copy in `renderComponentInScreen`.** The previous `tmp := *screen` copied a `sync.Mutex` while the caller held the lock; `go vet` flags it as a contract violation and it was a real concurrent-render corruption risk. Replaced with a free `wrapByScreenType(t, title, content)` helper reused from `Screen.RenderCtx`.
- **(P0) `RenderAlt` cross-user data leak via shared instance.** `WithRenderAlt(alt component.Component)` captured `alt` by pointer; concurrent anonymous requests racing through different screens with the same `landing` instance would clobber its `SetParams`/`Inject`/`Load` mutations across users. Changed to `WithRenderAlt(factory func() component.Component)` — framework calls the factory once per request. Race-tested under `-race` with 32 parallel requests across 8 distinct gated screens.
- **(P0) Partial-redirect `X-Gofastr-Location` was dead-lettered.** `handlePartialPage` previously set the header AND `http.Redirect(303)`. The runtime fetch silently chased the 303 server-side and the header never reached client JS. Now: 200 + header + empty body; runtime detects, replaces `pushState`, loads the redirect target. Chromedp e2e in `framework/uihost/partial_redirect_e2e_test.go`.
- **(P0) TagInput Enter swallow ate legitimate submits.** Chromium dispatches the implicit form submit despite a bubble-phase `preventDefault` on single-input forms. The prior defensive one-shot listener on the form ate the NEXT submit (the user's actual Save click). Replaced with a same-tick timestamp guard: a document-level capture-phase submit listener swallows submits within 50ms of the last tag-input Enter; legitimate submits a few hundred ms later proceed.

### Tests

New coverage added during the adversarial review + tightening pass:

- `framework/uihost/partial_redirect_e2e_test.go` — full chromedp chain for SPA-nav into a Redirect-policy screen.
- `framework/uihost/partial_redirect_test.go` — httptest for the 200+header contract, full-page 303 non-regression, `X-Gofastr-Location` open-redirect rejection (8-case table), ContextOnly screens through full uihost dispatch.
- `framework/uihost/native_form_e2e_test.go` — chromedp confirming an unadorned `<form action="/x" method="POST">` (no enctype, no opts) submits browser-native, Set-Cookie sticks, 303 followed.
- `framework/uihost/render_alt_visual_test.go` — RenderAlt anon→landing screenshot.
- `framework/uihost/safe_path.go` — `isSafePartialRedirect` helper.
- `core-ui/app/policy_test.go` — RenderAlt factory-per-request (concurrent across 8 screens), policy resolver edge cases.
- `battery/auth/policy_test.go` — `SessionPolicy` / `RolePolicy` matrix incl. `?next=` table (6 cases), `WithRenderAlt`, anon→403 default, anon→redirect override, `NoNext()`.
- `battery/auth/register_roles_security_test.go` — privilege-escalation regression (JSON + form).
- `battery/auth/manager_dev_secret_test.go` — random JWT secret minting / explicit-secret preservation / prod-mode opt-out.
- `core/router/middleware_alias_test.go` — alias compile-time + Router.Use acceptance.
- `core-ui/component/context_component_test.go` — ContextOnly satisfies Component, ContextComponent preferred over Render.
- `framework/entity/declaration_owner_field_test.go` — JSON round-trip + omitempty.

## 2026-05-23 — round-1 DX feedback + 6 rounds of adversarial review

Commit `2044154`. Addressed FRAMEWORK-FEEDBACK.md from a third-party
app (`wtf-do-i-eat`). Highlights:

### Added

- **`EntityConfig.OwnerField`** — declarative per-user CRUD scoping. Auto-CRUD now injects `WHERE owner_field = <ctx user>` for List/Get/Update/Delete and auto-stamps Create.
- **`battery/auth.SessionMiddleware(mgr)`** — cookie → ctx user loader (the missing counterpart to JWT-only `RequireAuth`).
- **`battery/auth.RequireSession(opts...)` + `WithRedirectOnFail(path)`** — HTTP middleware to gate JSON/API routes (or, with redirect option, browser flows).
- **`battery/auth.VerifyAuthEntitiesPrivate()`** — startup audit that fails fast if `users`/`sessions` entities are exposed via REST or MCP.
- **CSRF helpers + form-encoded auth endpoint negotiation.**

### Fixed (security)

- Open-redirect via `next=/\evil.example` and percent-encoded backslash variants in `successRedirect`.
- Anonymous SSE event leak.
- Anonymous batch endpoints mutating others' rows.
- Hook OR-clause precedence bypass.

## 2026-05-22 — worktree isolation mode

Commit `118605c`. First-class runtime resolver for git-worktree
collisions on `PORT`, SQLite files, Postgres database names, and
service env values. See `framework/docs/content/isolation.md`.
