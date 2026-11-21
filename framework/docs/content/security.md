# Security defaults

`core/middleware` provides the defensive HTTP stack that `framework.NewApp` wires by default. Keep the defaults and tune individual knobs rather than rebuilding the chain from scratch.

## The default stack

`framework.NewApp` installs this middleware chain on `app.Router()` unless you pass `WithoutDefaultMiddleware()`. Calling `app.Use(...)` appends to this chain—it does not replace or disable the defaults.

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/middleware"
import "time"
-->
```go
middleware.Recovery()
middleware.RequestID()
middleware.Compress()          // only when AppConfig.Compression is true
middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})
middleware.Timeout(30 * time.Second)
```

When enabled, `WithI18n` sits between `RequestID` and `SecurityHeaders`, and `WithIdempotency` mounts innermost via `router.UseInner` (after your auth and tenant middleware).

- **`Recovery`** catches panics and responds with `500` (`Cache-Control: no-store`, stripping handler representation headers like `ETag` or `Content-Type` so a panicking asset handler is never cached as the asset). If a panic occurs after headers/bytes were already sent, it logs the stack and closes the connection so error text is never appended to a `200` body.
- **`RequestID`** generates or echoes `X-Request-ID` so downstream logs share a trace ID.
- **`Timeout`** enforces a 30s request deadline (override app-wide with `AppConfig.RequestTimeout` or per route with `SetRouteTimeout`).
- **Listener-wide gates:** Router middleware only covers router-handled paths. To guard every request on the listener (including setup wizards or worker muxes), call `app.WrapServer(func(http.Handler) http.Handler)` before `Start`.
- **Access logging:** Deliberately omitted from the default chain so `battery/log` doesn't double-log; for basic request logs without `battery/log`, add `middleware.LoggingFn(app.Logger)`.

### Compression

Compression is off by default because most deployments terminate it at a CDN or reverse proxy. Enable it when the app serves clients directly:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
app := framework.NewApp(framework.WithConfig(framework.AppConfig{
    Compression: true,
}))
_ = app
```

`Compress` gzips HTML, CSS, JS, JSON, XML, SVG, and plain text responses ≥1 KB when `Accept-Encoding` allows gzip, and adds `Vary: Accept-Encoding`. It skips `HEAD`, `Range`, `text/event-stream`, pre-encoded bodies, `204`/`206`/`304`, and hijacked connections. Strong `ETag` values gain a `-gzip` suffix and still revalidate cleanly (`If-None-Match` and `If-Match`). Responses marked `immutable` (hashed static assets, `runtime.js`, `manifest.js`) are compressed at best level and cached by content hash after their second request.

**BREACH note:** Compressing a page that reflects attacker input next to a secret leaks the secret via response size. GoFastr masks CSRF tokens automatically (see [Masked tokens](#masked-tokens)); avoid rendering raw app secrets (API keys, invite tokens) beside reflected input.

## SecurityHeaders

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/middleware"
-->
```go
middleware.SecurityHeaders(middleware.SecurityHeadersConfig{
    ContentSecurityPolicy: "default-src 'self'; img-src 'self' https://cdn.example.com",
    ReferrerPolicy:        "strict-origin-when-cross-origin",
    FrameOptions:          "SAMEORIGIN",
    PermissionsPolicy:     "geolocation=(self)",
})
```

| Header | Default |
|---|---|
| `Content-Security-Policy` | `default-src 'self'; img-src 'self' data:; object-src 'none'; worker-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'` |
| `X-Content-Type-Options` | `nosniff` (always on, not configurable) |
| `Referrer-Policy` | `no-referrer` |
| `X-Frame-Options` | `DENY` |
| `Permissions-Policy` | `geolocation=(), microphone=(), camera=()` |
| `Cross-Origin-Opener-Policy` | `same-origin` (can isolate OAuth popups; override if your flow uses `window.opener`) |
| `Cross-Origin-Resource-Policy` | `same-origin` (blocks cross-origin `<img>`/asset embeds; set `cross-origin` on public asset routes if needed) |
| `Strict-Transport-Security` | `max-age=31536000` (1 year), **HTTPS responses only** |

If you supply a custom `ContentSecurityPolicy`, it replaces the default whole. Keep `object-src 'none'`, `form-action 'self'`, `worker-src 'self'`, and `base-uri 'none'`—`default-src` does not cover `form-action` or `object-src`.

**HSTS** is emitted automatically on direct TLS and proxy-terminated HTTPS (`X-Forwarded-Proto: https`), never on plain-HTTP local dev. Set `HSTSMaxAge: -1` to disable, or set `HSTSIncludeSub` / `HSTSPreload`.

**First-paint inline hashes:** To paint on the first response, `uihost` inlines small (≤16 KiB compressed) head resources (color-scheme bootstrap, matched component CSS, `app.css`) and appends their `'sha256-...'` hashes to the response CSP header. If a reverse proxy or CDN overwrites CSP outside Go, pass `uihost.WithNoInlineFirstPaint()` so all head assets stay external links. Avoid adding `'strict-dynamic'` unless you also allow third-party script hosts—`'strict-dynamic'` causes CSP3 browsers to ignore `'self'`, blocking `/__gofastr/runtime.js` unless you hash or nonce every script tag.

### Configuring the default chain's headers

Override individual headers on `NewApp` without duplicating the middleware:

<!-- gofastr:compile
stmt: _ = app
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/core/middleware"
-->
```go
app := framework.NewApp(framework.WithSecurityHeaders(middleware.SecurityHeadersConfig{
    ContentSecurityPolicy: "default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data:",
}))
```

Unset fields keep their strict defaults.

### Static exports

A static export (the app binary's `--export <dir>` flag) embeds the CSP into each page via `<meta http-equiv="Content-Security-Policy">` and writes a `_headers` file for hosts like Netlify or Cloudflare Pages.

**The `<meta>` tag cannot enforce clickjacking protection.** Per CSP Level 3, browsers ignore `frame-ancestors` inside `<meta>` policies (and static exports emit no `X-Frame-Options` header). Hosts that ignore `_headers` (S3, GitHub Pages) serve exported pages with no clickjacking guard unless you configure your CDN to send `Content-Security-Policy` (or `X-Frame-Options: DENY`) as a real HTTP response header.

## CORS

CORS is **not** in the default chain. Mount it globally (`app.Use`) or on a route group (`Group("/api", middleware.CORS(cfg))`) when browser clients call your API from another origin:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/middleware"
import "net/http"
-->
```go
middleware.CORS(middleware.CORSConfig{
    AllowedOrigins: []string{"https://app.example.com"},
    AllowedMethods: []string{http.MethodGet, http.MethodPost},
    AllowedHeaders: []string{"Authorization", "Content-Type"},
})
```

Preflights (`OPTIONS` with both `Origin` and `Access-Control-Request-Method`) return `204` for allowed origins and `403` for rejected ones; non-preflight `OPTIONS` requests reach your router. Set `AllowAnyHeader: true` or `AllowAnyMethod: true` to reflect requested header/method names (capped at 64 well-formed names, with the appropriate `Vary` header).

## CSRF

```go
middleware.CSRF(middleware.CSRFConfig{
    CookieName:   "fui_csrf",
    HeaderName:   "X-CSRF-Token",
    Skip:         middleware.SkipBearerAuth(),
    SecretKey:    loadCSRFKeyFromEnv(), // 32+ random bytes
    CookieSecure: true,                 // production HTTPS
})
```

- Sets a signed cookie on safe requests and requires a matching header (`X-CSRF-Token`) or form field on mutating requests (`POST`, `PUT`, `PATCH`, `DELETE`).
- `SkipBearerAuth()` skips CSRF for `Authorization: Bearer ...` and `X-API-Key` (never for Basic/Negotiate, which browsers auto-attach).
- **Always set `SecretKey` in production.** Rotate keys without breaking in-flight forms by listing retiring keys in `AdditionalKeys`. Safe-method requests automatically self-heal stale or invalid CSRF cookies.
- Over HTTPS, cookies get the `__Host-` prefix (blocking sibling-subdomain cookie tossing) unless `NoHostPrefix: true`.

### Masked tokens

Render `middleware.MaskCSRFToken(token)` into HTML—never the raw cookie value (`middleware.TokenFromContext` returns the raw token). Masking XORs the token with a fresh per-render pad (`m1....`) to prevent BREACH attacks; the middleware unmasks automatically on submit. `ui.Form` and `battery/auth`'s `CSRFToken` / `CSRFTokenFromCtx` already return masked tokens.

Entity MCP write tools and A2A skills re-dispatch writes through your router with the caller's cookies and forward `middleware.VerifiedCSRFSubmission(ctx)` so nothing bypasses CSRF.

## Cross-site form guard (Fetch Metadata)

CRUD and declared entity endpoints refuse forged cross-site writes automatically, but routes you mount directly with `app.Router().HandleFunc` get **no** automatic CSRF check. Wrap hand-mounted mutating routes with `handler.RefuseCrossSite` (or mount `middleware.CSRF`):

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/handler"
import "net/http"
var saveProfile http.HandlerFunc
var mux = http.NewServeMux()
-->
```go
mux.Handle("POST /profile", handler.RefuseCrossSite(saveProfile))
```

It returns `403` for non-GET/HEAD/OPTIONS requests with a form-forgeable content type (`handler.IsForgeableRequest`) coming from another site (`handler.IsCrossSiteRequest`: checks `Sec-Fetch-Site` with an `Origin`-vs-`Host` fallback, including sibling-subdomain checks). For `fetch()`-only endpoints where `Origin: null` (sandboxed iframes) should also be refused, use `handler.IsCrossSiteRequestStrict`. Leave routes that legitimately receive cross-site POSTs (such as Apple's OAuth `form_post` callback) unwrapped.

**Same-site hop pages:** Because session cookies are `SameSite=Strict`, top-level links arriving from another site (email, chat) omit the cookie. Handlers that need the cookie on arrival (OAuth callbacks, email verification, guarded pages under `auth.SessionMiddleware`) return a tiny same-origin hop page (`Refresh: 0;url=...` + Continue link) using `handler.NeedsSameSiteHop` / `handler.SameSiteHopTarget` so the browser reloads same-site with the cookie attached. See [auth.md](auth.md#a-link-from-another-site-into-a-guarded-page).

## Secret rotation

Both `GOFASTR_SECRET` (uihost session tokens) and `AuthConfig.JWTSecret` (auth JWTs) rotate without mass logouts: sign with the new key, verify against new **or** previous keys for one TTL, then remove the old key. Every secret passed to `GOFASTR_SECRET` or `WithSecretRotation` must be at least 32 characters (`WithSecretRotation` panics on shorter values).

```go
// uihost sessions (or env: GOFASTR_SECRET + GOFASTR_SECRET_PREVIOUS)
app := framework.NewApp(framework.WithSecretRotation(newSecret, oldSecret))

// battery/auth JWTs
mgr := auth.New(auth.AuthConfig{
    JWTSecret:          newSecret,
    JWTPreviousSecrets: []string{oldSecret},
})
```

Production mode still requires a non-empty current `JWTSecret`.

## Rate limiting

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/middleware"
import "time"
-->
```go
middleware.RateLimit(middleware.RateLimitConfig{
    Capacity:    100,         // peak burst
    RefillEvery: time.Minute, // +RefillBy tokens per interval
    RefillBy:    100,
    MaxKeys:     100_000,     // resident-bucket cap (default)
})
```

- Token-bucket per key, defaulting to `RemoteAddr`. `X-Forwarded-For` is ignored unless both `TrustProxyHeaders` and `TrustedProxies` are configured (parsed right-to-left to ignore client-spoofed left hops).
- Caps memory at `MaxKeys` (shedding nearly-full buckets first under IPv6 floods).
- Emits IETF `RateLimit-Limit`, `RateLimit-Remaining`, and `RateLimit-Reset` headers (disable with `OmitBudgetHeaders: true`), plus `Retry-After` on 429.
- For sliding-window lockouts (e.g., login throttling, where leaking remaining attempts helps brute-forcers) or multi-replica shared stores, use `framework/ratelimit` (see [rate-limit.md](rate-limit.md)).

## Cache policy for per-user responses

RFC 9111 restricts shared caching for `Authorization` headers, **not** cookie-authenticated responses. GoFastr therefore stamps `Cache-Control: no-store` (plus `Vary: Cookie` on pages) on authenticated CRUD responses, `uihost` pages and guarded recovery screens (`uihost.UIHost.RenderScreen`), auth listings, and A2A responses unless a handler explicitly sets its own `Cache-Control`.

For guarded recovery screens:
- Never cache per-user failure screens in shared proxies.
- Return the same status/screen for expired and missing sessions so callers can't probe session existence.
- Keep status codes truthful (`401`/`403` for auth failures, `410`/`404` for missing resources, plain `404` for unmatched paths).

## Local control planes & DNS rebinding

Loopback-bound control surfaces verify `Host` (not just `Origin`) to block DNS rebinding attacks:

- **`/mcp`:** `core/mcp.Server` checks same-origin `Origin`; use `SetAllowedHosts` or `SetRequireLoopbackHost(true)` on local transports (`gofastr dev` pins loopback automatically). Allow tunnels via `SetAllowedOrigins`.
- **`gofastr harness`**, **`kiln serve`**, and **`battery/service`**: all pin `Host` to their loopback address when bound locally. `kiln` also refuses request-supplied agent commands unless started with `--allow-custom-agent`.

Binding publicly (`--addr 0.0.0.0:...`) skips loopback pinning and logs an unauthenticated warning on startup.

## Entity, CRUD & widget security

### Default CRUD authentication

Auto-CRUD is **secure by default**: any entity declaring none of `OwnerField`, `Access`, or `Public: true` requires an authenticated session (`core/handler.GetUser`) on every REST, MCP, and `/{table}/llm.md` route.

Opt-outs in precedence order:
1. **`Scope.OwnerField`:** Enforces per-user ownership on every read and write (anonymous requests get `401`). Set `Scope.CrossOwnerRead` to an RBAC permission to widen **reads only** for staff/admin roles (fail-closed; multi-tenant boundaries still hold).
2. **`Exposure.Access`:** RBAC controls each operation (`Read`, `Create`, `Update`, `Delete`). Once any field is set, a blank `Read` is public, while a blank write (`Create`/`Update`/`Delete`) is denied to everyone—including wildcards. Use `framework.Anyone` to deliberately open a write (e.g., a public contact form).
3. **`Exposure.Public: true`:** Opens every read and write to anonymous callers (`gofastr generate` prints a warning for every public entity).

Pair with `Exposure.ReadScope` to filter *which rows* a caller sees (e.g., anonymous visitors see `status=published`, signed-in or `Unrestricted` roles see drafts). `BeforeGet` read scopes also bind `PUT`/`PATCH`/`DELETE`/transitions/`_batch` so callers get `404` on rows they cannot read.

### Masked fields (`NoQuery`) & FK scope checks

Redacting a field in `AfterGet`/`AfterList` does not stop callers from extracting the stored value character-by-character via `?field_like=...` or `?sort=field`. Mark redacted columns `NoQuery` (`no_query: true`):
- Every HTTP/wire query surface (flat filters, `?sort=`, `?where=`, `?rel.field=`, scoped includes, DSL, `SearchFields`, cursors, and app-spec `stat_card`/`chart` fields) refuses it (`400` at runtime or panic at `Define`).
- In-process Go code (`TypedQuery.Where`) is intentionally **not** blocked so background jobs and read-modify-write paths can query the real value; pass `crud.WithReadHooks(ctx)` whenever in-process reads return rows to end users (see [hooks-and-transactions.md](hooks-and-transactions.md)).
- `Hidden` removes a column from both responses and queries (indistinguishable from a non-existent column).
- **BelongsTo FK writes are scope-checked:** Creating or updating a BelongsTo foreign key referencing a row the caller cannot read under the target's owner/tenant/read scopes returns `404` (same as reading that row), unless exempted by `owner.AllowCrossOwner`, `CrossOwnerRead`, `tenant.AllowCrossTenant`, `crud.WithServerWrites`, or unchanged on update.

### Widget signal exposure

Widget `/state` endpoints are unauthenticated and process-global by default—only expose public display data unless gated. Set `Definition.RequireSession` (any valid browser session, anti-recon) or `Definition.RequireAuthenticated` (signed-in user). Both fail closed if no host predicate is installed, and SSR-inlined chrome obeys the same gate (`widget.GateSatisfied`).

### OpenAPI note for auth endpoints

`App.OpenAPISpec(title)` (`framework/openapi.EntityOpenAPIFor`) documents entity CRUD, bulk/export/pick routes, and declared custom endpoints using your app's `APIPrefix` and `JSONCase`. Plugin-registered `battery/auth` routes (`/auth/*`) are not yet included in the generated OpenAPI spec; if your gateway needs them in OpenAPI, merge a hand-written sibling spec.

### SQLite write availability

SQLite serializes writes; heavy concurrent write traffic can stall non-write requests. Set `MaxOpenConns(1)`, offload heavy writes to a background queue, or switch to Postgres (see [migrations.md](migrations.md)).

## Middleware inventory (`core/middleware`)

- `RequestID()` — generates/echoes `X-Request-ID`.
- `Recovery()` — panic → `500` (`no-store`) or connection close if response already started.
- `Logging()` / `LoggingFn(getLogger)` / `LoggingWithWriter(w)` / `SampledLogging(n, slow)` / `DiscardLogging()` — request logging.
- `SecurityHeaders(SecurityHeadersConfig)` — CSP, HSTS, frame, referrer, permissions headers.
- `CORS(CORSConfig)` — cross-origin headers and preflights.
- `CSRF(CSRFConfig)`, `MaskCSRFToken(token)`, `VerifiedCSRFSubmission(ctx)` — double-submit CSRF protection.
- `Compress()` — gzip compression for text responses.
- `RateLimit(RateLimitConfig)` — per-key token bucket.
- `Timeout(d)` — per-request context deadline.
- `NewMetrics()` / `MetricsMiddleware` / `MetricsHandler` — Prometheus RED metrics.
- `Tracing()` — OpenTelemetry request spans.

## Common mistakes

- **Relaxing CSP globally to fix one third-party asset.** Override only the needed directive (`script-src`, `style-src`) via `WithSecurityHeaders`; never replace `default-src 'self'` with `'unsafe-inline'`.
- **Disabling `Recovery` because handlers "don't panic".** One unhandled panic kills the request without writing a response, hanging the client.
- **Mounting CORS before `RequestID`.** Keep `RequestID` outermost so preflight logs still carry trace IDs.
- **Masking PII in `AfterGet`/`AfterList` without `NoQuery`.** Callers can still leak the real value via filter and sort queries.
