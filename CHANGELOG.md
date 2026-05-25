# Changelog

All notable changes to GoFastr. Follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) with semver-ish
calendar versions (`YYYY-MM-DD` per substantive release until the API
stabilises). Breaking changes are clearly marked with **BREAKING**.

## [Unreleased]

### Added

- **`core/dotenv` package + auto-load in `framework.NewApp()`.** Probes `.env.local`, `.env.<APP_ENV>` (when `APP_ENV` set), and `.env` from CWD before option processing. Existing `os.Environ` always wins. Parser handles double/single-quoted values, escapes, optional `export` prefix, comments; rejects malformed input loudly. Bracket-form `${VAR}` expansion with cycle detection, depth cap, undefined-as-empty, and `\${literal}` escape. Disable via `GOFASTR_DOTENV=off` in the process env. `cmd/gofastr migrate` now routes through this instead of its ad-hoc 1-key scanner. Docs: `framework/docs/content/dotenv.md`.
- **SSR auth policies.** `core-ui/app` exposes a `Policy { Decide(ctx) Decision }` machinery with four decision kinds (Allow / Redirect / RenderAlt / Block). Attach via `Screen.WithPolicy(p)` or `NewScreenGroup(prefix, layout, policies...)`. Construct decisions through the new `core-ui/app/decide` subpackage so call sites don't shadow common variable names: `decide.Allow()`, `decide.Redirect(url)`, `decide.RenderAlt(factory)`, `decide.Block(status, msg)`.
- **`battery/auth.SessionPolicy(opts...)` and `RolePolicy(roles, opts...)`** are the SSR counterparts to the existing `RequireSession` / `RequireRole` middleware. Options: `WithRedirect(url, ...RedirectOpt)`, `WithRenderAlt(factory)`, `WithBlock(status, msg)`. `RedirectOpt`: `NoNext()` to suppress the auto-appended `?next=<request-path>`.
- **`auth.SessionFrom(ctx) (User, bool)`** — cheap in-component getter for ctx-aware chrome (sibling nav, conditional CTAs). Pair with `RenderCtx` for in-page gating without policy machinery.
- **`auth.Roles(roles ...string) []string`** — ergonomic literal-list helper so `auth.RolePolicy(auth.Roles("admin", "owner"), ...)` reads cleanly. Documents the asymmetry with the variadic `auth.RequireRole`.
- **`component.ContextComponent { RenderCtx(ctx) HTML }`** — the optional ctx-aware render interface. Does NOT embed `Component` (so a type can satisfy it via just one method). Embed `component.ContextOnly{}` to also satisfy `Component` with a stub `Render` that the framework never calls.
- **`framework.entity.EntityDeclaration.OwnerField` JSON key (`owner_field`).** Mirrors `EntityConfig.OwnerField` so per-user CRUD scoping works for entities declared in JSON, not just Go.
- **DevMode auto-mints a random JWT secret** when `AuthConfig.JWTSecret == ""`. 32 cryptographically-random bytes, base64-encoded, logged as WARN. Sessions invalidate on restart — set `JWTSecret` for stable dev tokens.
- **`X-Gofastr-Location` partial-redirect signal.** Policy-Redirect outcomes on a partial fetch return 200 + that header + empty body (NOT 303 — the runtime fetcher uses `redirect:'follow'` and would auto-chase a 303, losing the header). The runtime's `loadPage` calls itself with the redirected URL and updates `pushState`.

### Changed

- **BREAKING — runtime form intercept default INVERTED.** `<form>` elements with the default `application/x-www-form-urlencoded` or `multipart/form-data` enctype are NO LONGER intercepted by `runtime.js`. The browser submits them natively (cookies set, `Location:` followed, file uploads, password-manager UX all work without any framework involvement). Forms posting to a JSON endpoint must now opt INTO interception with `enctype="application/json"` OR `data-fui-spa`. `data-fui-rpc` still triggers RPC dispatch as before. The legacy `data-fui-native` opt-out attribute still works (now a no-op for the default-enctype case it used to be needed for).
  - **Migration audit:** `grep -rn '<form' .` — for each form, decide:
    - Posts to JSON-shaped CRUD/island handler? → add `enctype="application/json"`.
    - Posts to a redirect-returning handler (auth, settings)? → no change needed; browser handles it natively now (and that's almost certainly what you wanted).
    - `grep -rn 'data-fui-native' .` — these are now no-ops; safe to delete.
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
