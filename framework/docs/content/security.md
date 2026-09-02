# Security defaults

`core/middleware` provides the defensive HTTP primitives the framework
composes by default. Most apps should accept the defaults and override
specific knobs rather than rebuild the chain.

## The default stack

`framework.NewApp` installs this middleware chain on `app.Router` unless
you pass `WithoutDefaultMiddleware()`. (`app.Use(...)` appends your
middleware to the chain. It does not replace or disable the defaults;
use `WithoutDefaultMiddleware()` when you want to build the chain
yourself.)

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/middleware"
import "time"
-->
```go
middleware.Recovery()
middleware.RequestID()
middleware.SecurityHeaders(middleware.SecurityHeadersConfig{})
middleware.Timeout(30 * time.Second)
```

(`WithIdempotency` / `WithI18n` insert their middleware between
`RequestID` and `SecurityHeaders` when configured.)

`Recovery` is outermost so a panic anywhere below it produces a clean
`500`. `RequestID` runs next so every later log line carries the trace
ID. `Timeout` is innermost: a `30s` deadline that cancels the request
context if the handler hangs.

Access logging is deliberately not in the chain: `battery/log` owns
structured access logging when registered, and an app that just wants a
basic line can add `middleware.LoggingFn(app.Logger)` itself; running
both would double-log every request.

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

| Header                    | Default                                                                          |
|---------------------------|----------------------------------------------------------------------------------|
| `Content-Security-Policy` | `default-src 'self'; img-src 'self' data:; object-src 'none'; form-action 'self'; frame-ancestors 'none'; base-uri 'self'` |
| `X-Content-Type-Options`  | `nosniff` (always, not configurable)                                            |
| `Referrer-Policy`         | `no-referrer`                                                                    |
| `X-Frame-Options`         | `DENY`                                                                           |
| `Permissions-Policy`      | `geolocation=(), microphone=(), camera=()`                                       |
| `Strict-Transport-Security` | `max-age=31536000` (1 year), **HTTPS responses only** |

`object-src` and `form-action` are named explicitly because `default-src`
does not cover them: CSP never let `form-action` fall back, and
`object-src`'s fallback was removed in CSP3. A policy of just
`default-src 'self'` therefore leaves an injected `<form>` free to post
the page's data anywhere, and `<object>`/`<embed>` free to execute, so
a custom `ContentSecurityPolicy` should keep both directives.

### Static exports

A static export (the app binary's `--export <dir>` flag) writes the
policy into every page as an in-document
`<meta http-equiv="Content-Security-Policy">` and emits a `_headers`
file for hosts that read one (Netlify, Cloudflare Pages). Response
headers are a server's job, and a static export has no server; without
this the export would be the one deployment target shipping the runtime
with no CSP at all.

**The meta does not carry the clickjacking guard.** Per CSP Level 3
§3.1, a `<meta>`-delivered policy ignores `frame-ancestors` (alongside
`report-uri` and `sandbox`), so the meta enforces the fetch directives
but not `frame-ancestors 'none'`. That directive reaches the browser
only through the `_headers` file, and the export emits no
`X-Frame-Options` header either, so a host that ignores `_headers` (S3,
GitHub Pages) serves the pages with no clickjacking guard at all. On
those hosts, configure the CDN to send `Content-Security-Policy` (or
`X-Frame-Options: DENY`) as a real response header.

**HSTS is on by default.** `Strict-Transport-Security` is emitted with a
one-year `max-age` whenever the request is HTTPS: direct TLS, or a
TLS-terminating proxy that sets `X-Forwarded-Proto: https` (the app
sees plain HTTP there). Plain-HTTP local dev never receives it. Set
`HSTSMaxAge: -1` to disable, a positive value to change the age, or
`HSTSIncludeSub` / `HSTSPreload` to extend it.

The CSP default works with the built-in UI runtime because all CSS and
scripts are served as external resources under `/__gofastr/*`. If you
embed third-party scripts, fonts, or frames you must override
`ContentSecurityPolicy` explicitly; do not relax it with
`'unsafe-inline'` globally.

### Configuring the default chain's headers

The example above constructs the middleware by hand. The default chain
installed by `NewApp` is configurable through `AppConfig.SecurityHeaders`
(or the `framework.WithSecurityHeaders(cfg)` option), so you can relax a
single directive, e.g. allow `style-src 'unsafe-inline'` for a
third-party CSS dependency, without shadowing the whole chain with your
own `SecurityHeaders` middleware:

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

Unset fields keep their built-in defaults (the strict CSP, `Referrer-Policy:
no-referrer`, `X-Frame-Options: DENY`, …), so a partial override never
silently drops a defensive header. The zero value reproduces the original
strict defaults exactly.

## CORS

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

CORS is **not** in the default chain. Add it explicitly if your API
serves browser clients on another origin.

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

Issues a cookie on safe requests; requires the matching header on
mutating requests (`POST`, `PUT`, `PATCH`, `DELETE`).
`SkipBearerAuth()` is the shipped helper that bypasses CSRF on
requests with `Authorization: Bearer …`, appropriate for pure API
deployments where the browser is not involved.

**Always set `SecretKey` explicitly in production.** The middleware
will autogenerate one if omitted, but that key rotates every process
restart, and the auditable signing seam moves into the binary
instead of into your secret store. Source it from your config /
secret manager the same way you'd source `SessionSecret`. With
`SecretKey` set AND `CookieSecure=true`, the cookie also gets the
`__Host-` prefix in production, blocking subdomain cookie-injection
attacks.

On the next safe-method request (GET / HEAD / OPTIONS) the middleware
**self-heals** stale or tampered cookies: it verifies any incoming
cookie against `SecretKey` + `AdditionalKeys` and silently re-mints
one if the signature doesn't validate. This means a process restart
(which rotates an auto-generated key) or a key rotation that drops
the previous secret no longer leaves browsers stranded with a cookie
that's guaranteed to 403 the next POST. To carry tokens across a
planned rotation without bouncing in-flight forms, list the previous
secret(s) in `AdditionalKeys`; drain once the old tokens have
expired.

## Secret rotation

Two app secrets can be rotated without a mass logout: the uihost session
signing key (`GOFASTR_SECRET`) and the auth battery's JWT signing key
(`AuthConfig.JWTSecret`). Both follow the same shape as the CSRF
`AdditionalKeys` idiom above: **sign with the current secret, verify
against the current OR any listed previous secret**, then drop the previous
secret once the drain window (one session/token TTL) has elapsed.

### GOFASTR_SECRET (uihost session tokens)

Set the new secret as the current and move the old one into the previous
list. New session tokens are signed with the current secret; tokens signed
by a previous secret still verify until the window closes.

```go
app := framework.NewApp(
    framework.WithSecretRotation(newSecret, oldSecret),
    // ...rest unchanged
)
```

Equivalent zero-code path via the environment (comma-separated; each entry
must independently meet the 32-char floor):

```sh
GOFASTR_SECRET=new-secret-...
GOFASTR_SECRET_PREVIOUS=old-secret-...
```

An explicit `WithSecretRotation` option wins over `GOFASTR_SECRET_PREVIOUS`.
`WithSecret(secret)`, the no-rotation shorthand, stays unchanged.

### AuthConfig.JWTSecret (auth battery JWTs)

```go
mgr := auth.New(auth.AuthConfig{
    JWTSecret:          newSecret,            // signs new tokens
    JWTPreviousSecrets: []string{oldSecret},  // verify-only during the drain
    // ...rest unchanged
})
```

See [auth](auth.md) for the full procedure. Production mode still requires
a non-empty `JWTSecret`; a previous-only configuration is rejected at Init.

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
})
```

Token-bucket per key. `KeyFunc` defaults to `RemoteAddr` (X-Forwarded-For
is ignored unless `TrustProxyHeaders` + `TrustedProxies` are set). Tune
`Capacity`/`RefillEvery`/`RefillBy` per route by composing two `RateLimit`
middlewares in different `middleware.Chain` calls.

On every response that passes through it (both allowed and 429) the
middleware also emits the IETF-draft budget headers so well-behaved API
clients can self-pace: `RateLimit-Limit` (the configured `Capacity`),
`RateLimit-Remaining` (tokens left after this request), and
`RateLimit-Reset` (whole seconds, rounded up, until the bucket is back at
full capacity). Set `OmitBudgetHeaders: true` to suppress them when the
per-response header cost matters at scale or an upstream cache would shard
by remaining budget; `Retry-After` on the 429 path is unaffected. The auth
battery's own limiter (`battery/auth`) intentionally exposes **only**
`Retry-After` and never the budget headers; a live remaining-attempt count
on login / password-reset endpoints would hand an attacker exact brute-force
pacing information. That limiter is the general-purpose sliding-window limiter
now in `framework/ratelimit`, the same package to reach for when you want "at
most N per period, then lock out" on your own routes. See
[rate-limit.md](rate-limit.md) for the quickstart and the token-bucket-vs-
sliding-window choice.

## Recovery screens and cache policy

When middleware refuses a guarded screen route, it answers with
`uihost.UIHost.RenderScreen` (see [ui-wiring](ui-wiring.md) → "Guards
on dynamic screens"). That helper defaults to
`Cache-Control: private, no-store` on both the full-page and the
client-side-navigation arm, and mints no session cookie. Three rules
keep the failure surfaces safe:

- **A recovery page is per-user.** It was rendered for one caller's
  auth state; a shared cache replaying it to a second visitor leaks
  that state and can pin a stale grant. Don't override the default
  `CacheControl` without a reason you can write down.
- **Don't reveal existence.** Answer an expired session and a missing
  session with the same screen and status, so an unauthorized caller
  can't probe whether a given id is live.
- **Keep statuses truthful.** 401/403 for an authentication failure on
  a route that exists, 410 (or a 404 via the screen's
  `ScreenStatusCode`) for a route that resolved but whose resource is
  gone, and a plain 404 — never a recovery screen — for a path nothing
  matches.

The partial-navigation arm matters as much as the full page: the
runtime keeps the response out of the screen cache on a non-2xx, but
any proxy between client and server obeys the headers, which is what
`no-store` is for.

## OpenAPI coverage for auth endpoints

Auth endpoints registered by `AuthManager.RegisterRoutes` (login,
register, logout, /auth/me, /auth/2fa/*, /auth/oauth/*, magic-link,
verify-email, forgot-password, reset-password, /auth/accounts,
/auth/unlink/{provider}) are **not** currently part of the
auto-generated OpenAPI spec.

`framework/openapi.EntityOpenAPI` walks the entity registry to emit
schemas for entity CRUD routes; the app passes its route predicate so
the served spec never documents CRUD paths registration did not mount
(no DB, or `Exposure.CRUD=false` — declared custom endpoints stay
documented either way). Plugin-registered HTTP handlers go
through `router.Post / router.Get / …` directly and don't carry
schema metadata that the spec generator can consume. There is no
plugin → OpenAPI extension hook today.

Until that hook lands, the auth routes are documented through this
file, the plugin source comments, and integration tests. If your
deployment needs an OpenAPI document that includes the auth routes,
hand-write them into a sibling spec and merge with the generated one
in the gateway / docs pipeline.

## The full inventory

`core/middleware` exports:

- `RequestID()`: generates or echoes `X-Request-ID`.
- `Recovery()`: turns panics into `500` with structured log line.
- `Logging()` / `LoggingFn(getLogger)` / `LoggingWithWriter(io.Writer)`:
  structured request log. `LoggingFn` reads the logger per-request so
  plugins can swap it after the chain is wired.
- `SampledLogging(sampleN, slowThreshold)`: logs 1-in-N requests but
  always logs errors (status ≥ 400) and slow ones (duration >
  `slowThreshold`). Preferred for production paths where the unsampled
  `Logging()` cost dominates the middleware chain.
- `DiscardLogging()`: request-timing wrapper that emits no log lines;
  for high-throughput paths where structured logging is handled by
  an upstream proxy or APM agent.
- `SecurityHeaders(SecurityHeadersConfig)`: defensive headers above.
- `CORS(CORSConfig)`: cross-origin headers + preflight.
- `CSRF(CSRFConfig)`: double-submit cookie pattern.
- `RateLimit(RateLimitConfig)`: token-bucket per key.
- `Timeout(d)`: per-request deadline; cancels context on expiry.
- `NewMetrics()` + `MetricsMiddleware` + `MetricsHandler`: RED metrics.
- `Tracing()`: OpenTelemetry span around each request.

Each has a `*_test.go` you can read for the exact behaviour.

## Availability notes

- **SQLite serialises writes.** Concurrent write load can climb to
  100ms+ p99 latencies and starve out non-write traffic, a soft DoS
  vector for any endpoint that writes. Set `MaxOpenConns(1)` on the
  `*sql.DB`, keep writes off the request path where possible (queue +
  background worker), or run Postgres. Full discussion in
  [migrations](migrations.md) §Concurrency model.

## Local control planes and DNS rebinding

Anything that binds loopback and accepts privileged requests needs a
`Host` check, not just an `Origin` check. Origin alone cannot stop DNS
rebinding: the attacker points their own domain at `127.0.0.1`, so the
browser treats the request as same-origin and `Origin` matches `Host`.
Comparing `Host` against the authority the server expects is what
refuses it, because a rebound request still carries the attacker's name.

This applies to three surfaces the framework ships:

- **`/mcp`**: `core/mcp.Server` refuses a browser `Origin` that is not
  same-origin with the request. Pin the authority with
  `SetAllowedHosts` (or `SetRequireLoopbackHost(true)`) when the
  transport is local; allow tunnels with `SetAllowedOrigins`.
  `gofastr dev` pins to loopback automatically, because dev mode
  implies the mutating control tools plus every entity's write tools
  with no auth in front of them.
- **`gofastr harness`**: the sidecar pins `Host` to the authority it
  bound. Its chat page carries the bearer token in a meta tag, so an
  unpinned `Host` would let a rebound page read the token and then
  drive the agent.
- **`kiln serve`**: pins `Host` to loopback whenever `--addr` bound a
  loopback address. `kiln` also refuses request-borne agent commands
  unless started with `--allow-custom-agent`: that form lets the
  request body choose the argv of a spawned process.

A deliberate non-loopback bind (`--addr 0.0.0.0:…`) skips the pin,
since the framework cannot know the intended public name. That is the
point at which the "unauthenticated" warning in the startup banner is
the contract.

## Widget signal exposure

A widget's `/state` endpoint is **unauthenticated** by default, and
`SignalSource.Read` takes no request context, so a signal value is
process-global, identical for every caller, and cannot be scoped per
user. Treat every signal as world-readable: counts, statuses, and other
non-sensitive display data.

Widgets whose signals should not be public set
`Definition.RequireSession`, which gates `/state` and `/chrome`. The
gate fails closed: if the host installed no session check, a widget
that asked for one serves nothing rather than serving everyone.

## Owner isolation and `CrossOwnerRead`

Entities with `OwnerField` scope every read/write to the requesting
user's rows: the framework refuses anonymous requests (401) and
injects `WHERE <owner_field> = <ctx user id>` into every query so a
user can never see or mutate another user's data. `CrossOwnerRead`
optionally widens this for **reads only**: when the request context
holds the named RBAC permission, List/Get/Count span all owners.
Writes (Create/Update/Delete) stay owner-scoped regardless, and
multi-tenant isolation is preserved: a granted context in tenant A
never sees tenant B rows. The widening is fail-closed: no access policy
in context ⇒ no widening. See
[entity-declarations](entity-declarations.md) → "CrossOwnerRead".

## Masked fields and the query surface

Redacting a field in an `AfterGet` / `AfterList` hook changes what the
caller reads. It does not change what the database filtered and sorted
on, so the stored value stays reachable through the query surface:

```
GET /cards?number_like=4111    → 1 row    every response still
GET /cards?number_like=4112    → 0 rows   reads "****1111"
```

Repeat that a character at a time and the masked value comes back in
full. `?sort=number` leaks relative ordering the same way.

Mark such a column `NoQuery` (`no_query: true` in a declaration). The
field stays in responses, and every wire query surface refuses it: flat
filters, `?sort=` (including alongside `?cursor=`, where the sort is
ignored but still validated), `?where=` predicate trees, nested
`?rel.field=` filters, scoped include filters, and the DSL all return a
400 naming the field. `?q=` search is refused earlier still: listing a
`NoQuery` column in `SearchFields` panics at `Define`, so the app fails
to start rather than serving a searchable mask, as does naming one as a
cursor field.

The in-process Go API is deliberately not gated. `TypedQuery.Where`
accepts a caller-built condition on a `NoQuery` column and returns the
stored value, because read-modify-write, seed lookups, and aggregates
all need the real row; the server cannot tell those apart from a
rendered list. Where rows reach an end user, pass
`crud.WithReadHooks(ctx)` so the same `AfterList`/`AfterGet` chain the
HTTP surface runs applies (see `hooks-and-transactions.md`).

A nested `?rel.field=` filter needs the target entity's schema to run
that check, so an unresolvable target refuses the filter rather than
skipping the check. A relation may legitimately point at a table no
entity registers; the auth battery self-migrates `auth_users`. Trusting
the column name there meant any column of that table could be
predicated on. `?include=` has always refused the same shape.

A nested filter on a target that declares `Scope.OwnerField` or
`Scope.MultiTenant` carries the caller's owner and tenant predicates into the
`EXISTS` subquery. The clause counts rows without selecting them, so without
those predicates it counts every row in the target table and the parent's row
count confirms values in other owners' or tenants' rows one guess at a time.
The predicates come from the same builder the include and eager paths use, so
the three surfaces answer alike. A caller holding a cross-owner or cross-tenant
grant gets no predicate on that axis, since they can already list the target
wholesale; the axes are independent. See [access control](access-control.md).

The two gates part company in process. The **posture** gate, which asks whether
the caller may read the target entity at all, is HTTP-only: it is the baseline
session requirement, and in-process code runs with no session by construction,
so asking it there refuses every nested filter a background job makes and
protects nothing. `ApplyIncludes` draws the line in the same place.

The **scope predicates** are not HTTP-only. Owner, tenant, and read scope
narrow a nested filter on every surface, in-process included, because they are
what stops the `EXISTS` clause counting rows the caller may not see. A host
handler that forwards a caller-influenced relation into `ListAll` or
`CountAll` gets the same narrowing the HTTP path applies.

Server-side code that genuinely means "read across every owner" says so with
[`owner.AllowCrossOwner`](access-control.md) or `tenant.AllowCrossTenant`.
Both are explicit at the call site, both are unreachable from an HTTP route,
and both are already how every other in-process read widens its scope.

`Hidden` and `NoQuery` are enforced when a nested-filter spec is RESOLVED, on
both surfaces, because those describe the data rather than the caller: a masked
column stays masked no matter who asks. That is specific to nested filters —
`TypedQuery.Where` remains deliberately ungated for `NoQuery`, as described
above.

### Foreign keys on writes are not permission-checked

Read paths carry the target entity's posture: an `?include=` or a nested
filter is refused when the caller may not read the related entity. Write
paths do not. A create or update that sets a relation column such as
`order_id` or `author_id` stores whatever id the body supplies, and nothing asks
whether the caller may read the row it names or whether that row is theirs.
A caller can attach their own row to another owner's parent.

Two gaps sat behind that sentence. One is closed:

- **Existence IS checked.** `AutoMigrate` emits a `FOREIGN KEY` clause for
  every declared relation, and both dialects now enforce it. PostgreSQL
  always did. SQLite honours the constraint only when `PRAGMA foreign_keys`
  is on; that pragma is off by default in every driver, so every DSN opened through the
  `sqlite3` driver name defaults to `_pragma=foreign_keys(1)`. An id naming
  no row is rejected on both. (`_pragma=foreign_keys(0)` opts out.)
- **Permission is NOT checked.** Nothing consults the target entity's
  `Exposure.Access` or `Scope` on the write path. A caller who may not read
  a row can still point their own row at it, so long as the id exists.

So a fabricated id now fails; a real id belonging to someone else still
succeeds. An entity whose relation columns must not be retargeted needs a
`BeforeCreate`/`BeforeUpdate` hook that validates them against the caller;
see [hooks and transactions](hooks-and-transactions.md).

Blueprint screens are checked at generate time, because several of them
reach the database without passing through the HTTP filter parser: an
`entity_list` `search:` or `filters:`, a `stat_card` `source.filter` or
summed `source.field`, and a chart's `group_by`. The chart is the
sharpest of these: `group_by` renders each distinct stored value as a
bar or slice LABEL, so a masked column would print in full on a page
whose table shows the mask.

`Hidden` already implies all of this: it removes the column from
responses *and* from every query surface. `NoQuery` is the option for
values the caller must still see in some form.

`NoQuery` does not mask anything on its own; the hook does. Register it
on `AfterGet` **and** `AfterList`, since each response path runs the one
that matches the shape it serves; a to-one `?include=` runs `AfterGet`
because that is what the child's own `GET /child/{id}` runs.

The rejection deliberately names a `NoQuery` field, unlike a `Hidden`
one. A hidden column's existence is itself the secret, so it has to be
indistinguishable from a column that does not exist; a `NoQuery` column
is right there in the response, so a precise error costs nothing and
saves a developer hunting for a typo.

## Default CRUD authentication

Prior to this section's introduction, an entity declaring **neither**
`OwnerField` **nor** `Access` got zero enforcement from auto-CRUD: List,
Get, Create, Update, and Delete were all reachable by an anonymous
caller: an unauthenticated `POST /api/<entity>` returned 201 and
persisted the row. Generated MCP tools inherited the same gap, since
`RegisterEntityMCPTools` dispatches entity tools through the same router
+ middleware chain as REST.

Auto-CRUD is now secure-by-default: `framework/crud`'s `requireScope`
chokepoint requires an authenticated session (`core/handler.GetUser`)
for every operation on an entity that declares none of `OwnerField`,
`Access`, or `Public`. The three opt-outs, in the order they're checked:

1. **`OwnerField` set**: the existing `RequireOwner` gate already
   requires an authenticated owner for every operation; the new
   session gate is redundant there and steps aside.
2. **`Access` declared** (any operation, even a partial block): RBAC
   governs the entity "as today": a blank permission for an operation
   leaves it un-gated by RBAC, and the new session gate does not layer
   an extra requirement on top.
3. **`Public: true`**: a deliberate, full opt-out. Every operation,
   reads and writes, is open to anonymous callers: the framework's
   pre-secure-by-default behaviour for that entity. Meant for content
   that's genuinely public (a contact form, a blog's comments), not as
   a workaround for a 401 during development. An entity that wants
   public reads but gated writes uses `Access` instead (a blank
   `Read` + a real `Create` permission).

The read posture has two questions, and `Public` answers only the first
(may this caller read the entity). **Which rows** they see is
`Exposure.ReadScope`: declared predicates on the entity's own columns,
applied to every read surface (List, Get, count, cursor, stream,
`?include=`, `?rel.field=`, the in-process API, typed queries), lifted for
a caller holding the `Unrestricted` permission, or, when it is empty, for
any signed-in caller. That empty-`Unrestricted` form is the "anonymous
visitors see published rows, signed-in editors see drafts" posture, and it
is deliberately weak: any signed-in user, not an editor role. Writes are
not filtered by it. See
[entity-declarations](entity-declarations.md) → "Row-level read scoping".

Because entity MCP tools dispatch through the router, this gate governs
them automatically; no separate `mcp.WithToolGate`/`auth.MCPUser` wiring
is needed for generated CRUD tools (that machinery remains for *custom*
tools registered directly via `app.MCP.RegisterTool`; `Endpoint.MCPHandler`
twins default to requiring an authenticated caller; see
[agent-ready](agent-ready.md)).

The entity's auto-generated `/{table}/llm.md` runs the **same** `requireScope`
chain as `List`. It documents exactly the entity `List` serves, so it answers
to the same gate: an authenticated caller without the entity's read permission
gets a 403 on the schema, not just on the rows. The field list is a disclosure
in its own right: it names every non-`Hidden` column, its type and its enum
set.

`gofastr generate` prints a warning listing every entity left publicly
readable/writable (`public: true`), so a generated app never has open
entities you didn't get told about. See
[entity-declarations](entity-declarations.md) → "Default CRUD
authentication" for the blueprint YAML shape.

`gofastr audit lint` flags this as rule `unscoped-pii`, and it inspects
Go-declared entities (`app.Entity(...)` with an unscoped PII-shaped field
exposed via auto-CRUD) as well as `gofastr.yml`: the cross-user exposure
is identical either way.

## Common mistakes

- **Relaxing CSP to fix a broken third-party script.** Override only
  the directive you need (`script-src`, `style-src`); never replace
  `default-src 'self'` with `'unsafe-inline'`.
- **Skipping `Recovery` because the app doesn't panic.** It does
  eventually. Without it, a single panic terminates the request handler
  goroutine without writing a response, leaving the client hanging.
- **Composing CORS before `RequestID`.** Preflights still need trace
  IDs; keep `RequestID` first.
