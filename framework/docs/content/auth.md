# Authentication

GoFastr's auth subsystem lives in `battery/auth`. It is built on a small
`AuthManager` plus a set of plugins. The manager owns shared state
(user store, session store, JWT settings, rate-limit config); plugins
own individual authentication methods. Every plugin is opt-in — a
service that only needs OAuth and 2FA never compiles the
password-reset code in.

The lower-level primitives in `framework/auth` (argon2id, the typed
`Guard`, dialect detection, the `TokenStore`) are dependencies of the
plugins, not a parallel API surface. Apps wire `battery/auth`.

## Quickstart

```go
mgr := auth.New(auth.AuthConfig{
    JWTSecret:    "your-jwt-secret-here",
    UserStore:    myUserStore,        // implements auth.UserStore
    SessionStore: mySessionStore,     // optional; in-memory default
    LoginRateLimit: &auth.RateLimiterConfig{
        MaxAttempts:   10,
        Window:        15 * time.Minute,
        BlockDuration: 30 * time.Minute,
    },
})
mgr.Use(auth.NewCorePlugin())                               // /auth/{login,register,me,logout}
mgr.Use(auth.NewMagicLinkPlugin(auth.MagicLinkConfig{
    BaseURL:     "https://app.example.com",
    EmailSender: mySender,
}))
mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{}))
mgr.Use(auth.NewAccountsPlugin())                           // /auth/accounts, /auth/unlink/{provider}
mgr.Use(auth.NewEmailVerificationPlugin(auth.EmailVerificationConfig{
    BaseURL:     "https://app.example.com",
    EmailSender: mySender,
}))
mgr.Use(auth.NewPasswordResetPlugin(auth.PasswordResetConfig{
    BaseURL:     "https://app.example.com",
    EmailSender: mySender,
}))

if err := mgr.Init(nil); err != nil {
    return err
}
mgr.RegisterRoutes(app.Router())
```

`AuthConfig.defaults()` runs automatically inside `New`. In production
posture (`DevMode` left false) it picks the `__Host-session` cookie
name, sets `Secure=true`, and a seven-day session TTL.

When `DevMode: true` and `JWTSecret` is empty, `New` mints a random
per-process secret and logs a WARN. That lets demo/boilerplate apps
skip the "change-me" literal — sessions invalidate on restart, which
is the right trade for dev.

In production mode (`DevMode: false`) `JWTSecret` is **mandatory**:
`Init` fails closed with
`auth: production mode requires AuthConfig.JWTSecret — set it from
your secret store, or set DevMode: true for local development`, and
`App.Start` refuses to boot. There is no warn-and-continue path — an
empty HMAC key would make every JWT forgeable.

## The plugins

| Plugin | Routes | Notes |
|---|---|---|
| `CorePlugin` | `POST /auth/{login,register,logout}`, `GET /auth/me` | The base. Always register first. Mints a `PendingTwoFactor` session if any registered plugin reports the user has 2FA enabled. |
| `MagicLinkPlugin` | `POST /auth/magic-link/send`, `GET /auth/magic-link/verify` | Passwordless email-link sign-in. Auto-creates users on first verify. Refuses to operate without `EmailSender` unless `DevMode` is explicitly set. |
| `OAuth2Plugin` | `GET /auth/oauth/{provider}`, `GET /auth/oauth/{provider}/callback` | OAuth 2.0 (Google + GitHub built in). Binds identity by `(provider, providerID)` when the store implements `OAuthLinker`; refuses silent linking on email collision with an existing local account. |
| `TwoFAPlugin` | `POST /auth/2fa/{enroll,verify,challenge,disable}`, `GET /auth/2fa/backup-codes` | TOTP + backup codes. Provides `RequireTwoFA` middleware; CorePlugin checks `HasTwoFactorEnabled` at login to set `Session.PendingTwoFactor`. |
| `AccountsPlugin` | `GET /auth/accounts`, `DELETE /auth/unlink/{provider}` | List and unlink linked OAuth identities. Refuses to unlink the user's last login method (checks `HasPassword` + remaining linked accounts). |
| `EmailVerificationPlugin` | `POST /auth/send-verification`, `GET /auth/verify-email` | Issues a token, redeems it, calls `MarkEmailVerified` on the store. |
| `PasswordResetPlugin` | `POST /auth/forgot-password`, `POST /auth/reset-password` | Forgot-password always returns 200 (no enumeration). Calls `SetPassword` on the store. |
| `TokensPlugin` | `POST/GET /auth/tokens`, `DELETE /auth/tokens/{id}` | Self-service scoped API tokens (PATs) for logged-in users. Owner forced from the session; plaintext shown once. See [Service accounts & API tokens](#service-accounts--scoped-api-tokens). |

Each plugin's `RegisterRoutes` mounts under `AuthConfig.BasePath`
(default `/auth`).

## What the host app implements

`auth.UserStore` is the only required interface. It maps email/ID to
`auth.User` and creates new users:

```go
type UserStore interface {
    FindByEmail(ctx, email) (User, hashedPassword string, error)
    FindByID(ctx, id) (User, error)
    CreateUser(ctx, email, hashedPassword, roles) (User, error)
}
```

Return `auth.ErrUserNotFound` from the `FindBy*` methods when no row
matches. Return `auth.ErrEmailTaken` from `CreateUser` on a unique-
violation. Any other error is treated as a transport failure and
propagated — plugins refuse to silently auto-create users when they
can't tell "not found" from "DB unreachable".

`auth.EntityUserStore` is a ready-made implementation that adapts a
database table (SQLite or PostgreSQL) through the framework's entity
system. Use it unless you have a reason not to.

## Optional extension interfaces

Plugins enable extra behaviour when the host's store implements an
optional interface. Stores that don't, fall back to a documented
safe-but-reduced path.

| Interface | Used by | Effect when implemented |
|---|---|---|
| `OAuthLinker` | `OAuth2Plugin` | Bind identity to `(provider, providerID)` instead of email. Refuse silent linking on email collision. |
| `OAuthEnrichedLinker` | `OAuth2Plugin` | Persist profile fields (name, avatar, email) so `AccountsPlugin` can return them in `/auth/accounts`. |
| `OAuthUserCreator` | `OAuth2Plugin`, `MagicLinkPlugin` | Record at creation time that the user has no password. Lets `PasswordChecker.HasPassword` return false correctly. |
| `OAuthTokenRefresher` | `RefreshOAuthToken` / `ValidOAuthToken` | Exchange a stored refresh token for a fresh access token. Implemented by `GoogleProvider` and `GitHubProvider`. See "OAuth token store + refresh". |
| `AccountLister` | `AccountsPlugin` | Power `GET /auth/accounts`. Required. |
| `AccountUnlinker` | `AccountsPlugin` | Power `DELETE /auth/unlink/{provider}`. Required. |
| `PasswordChecker` | `AccountsPlugin` | Refuse unlink-of-last-credential correctly. Without this, the unlink check falls back to "must leave at least one linked OAuth account remaining" — fine when the user has linked accounts, less accurate when they only have a password. |
| `EmailVerifier` | `EmailVerificationPlugin` | Set the `email_verified` flag. Required. |
| `PasswordSetter` | `PasswordResetPlugin` | Persist the new bcrypt hash. Required. |
| `SessionTwoFAMarker` | `TwoFAPlugin` | Mark a session as having completed the second factor. Required for `RequireTwoFA` to ever pass — stores that omit it fail closed. |
| `SessionPendingMarker` | `CorePlugin` | Set `Session.PendingTwoFactor` after login for users who have 2FA enabled. **Fail-closed:** if any registered `TwoFactorChecker` reports a user enrolled and the store omits this interface (or the mark call errors), login is rejected and the session destroyed — a custom store cannot silently downgrade 2FA accounts to password-only auth. |
| `TwoFactorChecker` | `CorePlugin` | Plugin-side signal: this user has 2FA enabled. `TwoFAPlugin` implements it. Custom plugins (WebAuthn, SMS) can implement it too. |
| `UserLister` | Host code (`AuthManager.ListUsers`) | Enumerate accounts for a back-office. Returns `ErrListUsersUnsupported` when absent, so a missing implementation fails loudly instead of returning an empty list. See [Listing users](#listing-users). |

The `EntityUserStore`, `EntitySessionStore`, and `EntityTwoFAStore`
provided in this package implement every relevant interface; if you
start from `EntityUserStore` you get the full feature matrix.

The default stores are **in-memory**: fine for dev and tests, a trap
in production — sessions vanish on restart and never resolve on a
second replica, and in-memory 2FA enrollment reverts accounts to
password-only auth after a restart. Production mode enforces this at
Init: the in-memory session store logs a WARN; the in-memory 2FA
store **fails Init** — a silently-expiring security control is not
warning-grade. Wire the durable store:

```go
mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{
    Store: auth.NewEntityTwoFAStore(db, "auth_twofa"), // plugin creates the table
}))
```

or set `AuthConfig.AllowInMemoryStores: true` to acknowledge a
deliberate single-node deployment (the 2FA refusal downgrades to a
WARN). See [Horizontal scaling](scaling.md).

## Default roles for new accounts

Every newly created account — register, magic-link auto-create, and
OAuth auto-create — is stamped with `AuthConfig.DefaultRoles`. The
default is `["user"]`.

```go
mgr := auth.New(auth.AuthConfig{
    DefaultRoles: []string{"member"}, // everyone starts as "member", not "user"
    // …
})
```

These are **operator configuration, never request data.** The
registration and auto-create flows are anonymous, so honoring a
client-supplied `roles` field would let anyone self-promote to any
role. The handlers read the value through `mgr.DefaultRoles()` and
ignore any `roles` key on the incoming request — role elevation is a
separate admin-gated flow.

## HTML form support

`/auth/login`, `/auth/register`, and `/auth/logout` accept both JSON
and HTML-form bodies. The handler branches on `Content-Type`:

| Request                                       | Response                                              |
|-----------------------------------------------|--------------------------------------------------------|
| `Content-Type: application/json`              | `200 OK` JSON body with `{user, token}`                |
| `application/x-www-form-urlencoded` (HTML)    | `303 See Other` with `Location` to `?next=` or `/`     |
| `multipart/form-data`                         | Same as form-urlencoded                                |

Form-flow responses set the session cookie before redirecting, so the
runtime's [form interceptor](runtime-contract.md#forms)
follows the `Location` header and lands the user on the next page.

Open-redirect protection: the `?next=` (query or form) override is
honored only for same-origin paths starting with `/` — `//evil.example`
and full URLs are rejected, falling back to `/`.

Wire a plain HTML login form like this:

```html
<form action="/auth/login" method="POST" enctype="application/x-www-form-urlencoded">
  <input name="email" type="email" required>
  <input name="password" type="password" required>
  <input name="next" type="hidden" value="/dashboard">
  <button type="submit">Log in</button>
</form>
```

No client-side JavaScript needed beyond the framework runtime.

### Owner extractor — global state and its limit

`battery/auth.init()` installs a global owner extractor in
`framework/owner` so any entity with `OwnerField` set in the process
scopes by the current `auth.GetCurrentUser(ctx)`. **The extractor is
process-global** — one extractor per process, last-import wins. Apps
that need different extractors per `framework.App` instance (e.g. a
single process hosting two unrelated apps) can't have them today.

If you need a different identity source, call
`owner.SetExtractor(yourFunc)` AFTER `battery/auth` is imported (the
last call wins). Document this clearly in your app — the import-order
coupling is subtle.

**Safety**: when an entity has `OwnerField` set and the extractor
can't produce an owner id for the request (no auth, anonymous request,
extractor disabled), CRUD refuses the request with `401 Unauthorized`.
There is no fail-open path: setting `OwnerField` makes the entity
unconditionally require an authenticated owner.

## Session middleware (cookie → ctx user)

`battery/auth.SessionMiddleware(mgr)` reads the session cookie, looks
up the user, and stashes them in the request context via
`handler.SetUser`. After it runs, `auth.GetCurrentUser(ctx)` returns
the live `User`, and any entity with `OwnerField` set automatically
scopes per-user.

```go
app.Use(auth.SessionMiddleware(mgr))
```

Pair with `auth.RequireSession()` (or
`auth.RequireSession(auth.WithRedirectOnFail("/login"))` for browser
flows) on any route that needs a logged-in user.

`RequireAuth` is the JWT-Bearer-only equivalent and is unchanged.

## Service accounts & scoped API tokens

Non-human identities — CI runners, background workers, internal scripts —
authenticate with **API tokens** (`gfsk_`-prefixed PATs) instead of session
cookies or JWTs. Tokens are issued to a human user or to a **service
account**: a non-interactive identity that holds roles like a user but has
no mailbox and cannot log in. Both populate request context the same way
sessions do, so owner-scoping and `access.Can` work unchanged.

### Token format

- Plaintext: `gfsk_` + 40 lowercase hex chars (20 bytes from `crypto/rand`).
  The `gfsk_` prefix makes leaked tokens greppable by secret scanners the
  way `ghp_` / `xoxb-` are.
- At rest: only `sha256(full-plaintext)` is stored, plus a display
  `Prefix` (first 12 chars) so listings can identify a token without
  revealing it.
- The plaintext is returned **exactly once**, from `IssueToken` or
  `POST /auth/tokens`. It is never persisted, logged, or placed in any
  error string or audit event.

### Issuing

```go
ts, _ := auth.NewSQLAPITokenStore(db)
ss, _ := auth.NewSQLServiceAccountStore(db)

// Programmatic: mint a token for a user or service account.
plaintext, rec, err := auth.IssueToken(ctx, ts, auth.TokenSpec{
    Name:      "ci-deploy",           // required
    OwnerKind: "user",                // "user" | "service"
    OwnerID:   currentUser.GetID(),
    Scopes:    []string{"posts:read", "deploys:run"},
    TTL:       30 * 24 * time.Hour,   // 0 = no expiry
})
// Store `plaintext` in your secret manager NOW — it cannot be retrieved again.
_ = rec // {ID, Name, Prefix, Scopes, ExpiresAt, …}
```

Validation: Name/OwnerKind/OwnerID required; OwnerKind ∈ {user, service};
scopes match `^[a-z0-9_-]+:[a-z0-9_*-]+$`; max 32 scopes.

### Middleware wiring

Mount `TokenMiddleware` **alongside** `SessionMiddleware`. It only
intercepts `Authorization: Bearer gfsk_…` credentials; non-`gfsk_` bearers
(JWTs) and header-less requests pass through untouched for the session/JWT
middleware to handle. A `gfsk_` credential that fails validation (unknown,
revoked, expired, disabled owner) clears any prior ctx identity and
proceeds anonymous — it never falls back to an outer identity.

```go
app.Use(auth.SessionMiddleware(mgr))
app.Use(auth.TokenMiddleware(mgr.UserStore(), svcAccounts, apiTokens,
    auth.WithTokenAudit(auditSink)))
```

`TokenMiddleware` has no `AuthManager`, so audit is wired with
`WithTokenAudit(sink)`; a nil sink disables auditing and never panics.

On the consumer side, the generated typed client
([entity declarations](entity-declarations.md#code-generation))
carries the token for you: set `client.Token` to a plaintext PAT and every
request sends `Authorization: Bearer gfsk_…`. Bearer requests skip CSRF by
design, so scripts and CLIs need no cookie or CSRF-token handling — the
customer flow is: log in to the app's web UI, mint a scoped token via
`POST /auth/tokens` (TokensPlugin), paste it into the tool once. The
generated customer CLI ([Ship your API as a CLI](app-cli.md)) packages
exactly this flow as its `login` command.

### Scopes

Sessions and JWTs are **unscoped** — a logged-in user carries their full
capability. API tokens are **additionally restricted** by their scope list:

- exact: `"posts:read"` grants `"posts:read"`
- wildcard verb: `"posts:*"` grants any `"posts:…"`
- wildcard resource: `"*:read"` grants `read` on every resource
- global: `"*:*"` grants everything
- empty scopes: the token authenticates but `RequireScope` always 403s

`RequireScope` only gates the routes you mount it on, and sessions pass
it unscoped — mount it on the machine-facing routes you actually want
scope-limited, and don't rely on it as a blanket gate for human traffic.

```go
// In a handler: token requests are scope-checked; sessions pass.
if !auth.HasScope(ctx, "posts:read") { /* 403 */ }

// As route middleware: 403s token-authenticated requests lacking the scope;
// non-token (session/JWT) requests pass unscoped.
r.With(auth.RequireScope("posts:write")).Post("/posts", handler)
```

For the auto-CRUD tree there is a blanket gate: `RequireAPIScopes(prefix)`
derives the required scope from the route itself — the first path segment
after the prefix is the resource, GET/HEAD need `<resource>:read`,
everything else `<resource>:write` — so one mount makes every minted scope
real across `/api`:

```go
app.Use(auth.TokenMiddleware(users, accounts, tokens))
app.Use(auth.RequireAPIScopes("/api")) // ["customers:*"] token ⇒ 403 off /api/invoices
```

Without it (or per-route `RequireScope`), a token's scope list is
**advisory only** — the token still authenticates as its owner everywhere.
Session/JWT callers and paths outside the prefix are untouched.

`auth.TokenScopes(ctx)` returns `(scopes, true)` only for
token-authenticated requests; `(nil, false)` for sessions/JWT.

**The token-management endpoints are session-only.** `POST/GET/DELETE
/auth/tokens` require an interactive session — a request authenticated by
an API token is rejected with 401, even under the recommended global
`TokenMiddleware` wiring. Otherwise a leaked scoped (or empty-scoped)
token could mint a `*:*` token for its owner and escape its own scope
leash, or list/revoke the owner's other tokens. Token holders manage
their tokens by logging in, not with the token itself.

### Service accounts (programmatic-only)

A service account authenticates **only** via tokens — there is no login
path. Create one in Go; its roles flow to `RequireRole` / `access.Can`
through the `User` interface:

```go
sa := auth.NewServiceAccount("ci-runner", []string{"deploy", "reader"})
ss.Create(ctx, sa)
// then auth.IssueToken with OwnerKind: "service", OwnerID: sa.ID
```

Service-account management has **no HTTP surface in v1** — create and
disable them from trusted server code (`SetDisabled`).

### Management endpoints (`TokensPlugin`)

Self-service token management for logged-in users:

| Endpoint | Effect |
|---|---|
| `POST {base}/tokens` | Create a token for the **caller** (session user). Body `{name, scopes, ttl_seconds}`. Owner is forced from the session — `owner_kind`/`owner_id` in the body are ignored. Returns the plaintext once. |
| `GET {base}/tokens` | List the caller's tokens (prefix only — never the plaintext or hash). |
| `DELETE {base}/tokens/{id}` | Revoke one of the caller's tokens. A foreign id is a 404 (owner-scoped). |

```go
mgr.Use(auth.NewTokensPlugin(apiTokens))
```

### Audit events

| Kind | When | Meta |
|---|---|---|
| `token.created` | `POST /auth/tokens` succeeds | `token` (prefix), `name` |
| `token.revoked` | `DELETE /auth/tokens/{id}` succeeds | `token_id` |
| `token.auth_failed` | a `gfsk_` credential fails in `TokenMiddleware` | `reason` ∈ unknown\|revoked\|expired\|owner_missing\|owner_disabled, `token` (prefix) |

Only the token **prefix** ever appears in an audit event — never the
credential itself.

## Per-SSR-screen policies (`auth.SessionPolicy`, `auth.RolePolicy`)

For apps built with the `framework/uihost` SSR stack, gating happens
at the **screen** layer, not the router middleware layer. Attach an
`app.Policy` to a `Screen` or `ScreenGroup` and the dispatcher
evaluates it before `Load()` runs:

```go
import "github.com/DonaldMurillo/gofastr/core-ui/app"

// Public marketing pages — no policy, no gate.
application.RegisterScreen(app.NewScreen("/",      &HomeScreen{}),  nil)
application.RegisterScreen(app.NewScreen("/about", &AboutScreen{}), nil)

// Gated dashboard group — every screen inherits SessionPolicy.
dash := app.NewScreenGroup("/dashboard", dashLayout, auth.SessionPolicy())
dash.Screen(app.NewScreen("home",    &Home{}),    nil)
dash.Screen(app.NewScreen("billing", &Billing{}).
    WithPolicy(auth.RolePolicy(auth.Roles("admin"))), nil)
application.Router.ScreenGroup(dash)

// Same-URL marketing/dashboard duo via RenderAlt — factory, NOT
// singleton, so each request gets a fresh instance (no cross-user
// data leak on shared alt state).
application.RegisterScreen(
    app.NewScreen("/", &Dashboard{}).
        WithPolicy(auth.SessionPolicy(auth.WithRenderAlt(
            func() component.Component { return &Landing{} },
        ))),
    nil,
)
```

The dispatcher resolves each request as one of four outcomes:

| Decision     | What happens                                                    |
|--------------|-----------------------------------------------------------------|
| Allow        | Normal Load + Render.                                           |
| Redirect     | 303 to `WithRedirect(url)`, default `/login?next=<path>`.        |
| RenderAlt    | The alt component takes the screen's place; its Load runs.      |
| Block        | HTTP status from `WithBlock(status, msg)`, default 401/403.     |

### Option precedence — last-write-wins per call, alt > redirect > block on fail

Each `With*` option overwrites the others' fields on the way in. If
you chain `auth.WithRedirect("/x").WithBlock(403)` the Block wins —
the second option clears the redirect URL. There is no "compose two
failure outcomes" — pick one per policy.

When more than one applies (e.g. through some custom option-builder),
`failureDecision` resolves them in order: `RenderAlt` first if its
factory is set; otherwise `Redirect` if a URL is set; otherwise
`Block` (default 401 for SessionPolicy, 403 for RolePolicy).

Defaults applied when no failure option is passed:

| Policy           | Default failure outcome                            |
|------------------|----------------------------------------------------|
| `SessionPolicy`  | `Redirect("/login")` with `?next=<request-path>`   |
| `RolePolicy`     | `Block(403, "forbidden")`                          |

To suppress the auto-`?next=` on a redirect (e.g. anon "/" →
"/marketing"), pass `auth.NoNext()`:

```go
auth.SessionPolicy(auth.WithRedirect("/marketing", auth.NoNext()))
```

`RenderAlt` takes a factory, not an instance — the framework calls it
once per request so the alt component cannot leak data across users:

```go
auth.SessionPolicy(auth.WithRenderAlt(
    func() component.Component { return &Landing{} },
))
```

Inside a `RenderCtx`, call `auth.SessionFrom(ctx)` for in-component
gating (sidebar nav, conditional CTAs) — no policy machinery needed
for a per-widget branch:

```go
func (s *Header) RenderCtx(ctx context.Context) render.HTML {
    if sess, ok := auth.SessionFrom(ctx); ok {
        return AuthedHeader(sess)
    }
    return AnonHeader()
}
```

Pair with `SessionMiddleware` upstream so the policy sees the loaded
user. JSON/API routes still use `RequireSession` middleware as before
— policies are for the SSR page layer specifically.

## Auth entities are private by default

The user / session tables back the auth subsystem — exposing them via
auto-CRUD would leak password hashes and session tokens. Use the
pre-built configs:

```go
app.Entity("users",    auth.UserEntityConfig())     // CRUD=false, MCP=false
app.Entity("sessions", auth.SessionEntityConfig())  // CRUD=false, MCP=false
mgr.SetUserStore(auth.NewEntityUserStore(db, "users"))
mgr.SetSessionStore(auth.NewEntitySessionStore(db, "sessions"))
```

`auth.UserEntityFields()` and `auth.SessionEntityFields()` are still
exported for hosts that want to assemble their own config — but the
`*EntityConfig()` helpers are the safe default.

## Durable store schema and PostgreSQL first boot

`EntityUserStore`, `EntitySessionStore`, and `EntityTwoFAStore` create
their tables when `AuthManager.Init` runs. On PostgreSQL their boolean
columns use the native `BOOLEAN` type, matching the Go `bool` values
bound by the stores. This includes `auth_users.password_set`, the
session 2FA flags, and the durable 2FA flags.

Generated blueprint apps register a configured bootstrap admin through
`App.WithSeed`. The hook runs after auto-migration and before the server
accepts traffic. A missing `ADMIN_SEED_PASSWORD`, a lookup error other
than `auth.ErrUserNotFound`, password hashing failure, or insert failure
aborts startup instead of leaving a fresh app with an unusable admin
login. The seed is idempotent: an existing account is left unchanged.

The auth stores also convert legacy PostgreSQL `INTEGER` boolean columns
(`0`/`1`) to `BOOLEAN` during `EnsureSchema`, preserving the value and
setting a boolean `FALSE` default. This self-heals tables created by
older GoFastr versions. Back up production data before the first boot
after upgrading.

## Listing users

Back-offices that need to enumerate accounts (an admin user list, a
back-office screen) should call `mgr.ListUsers` — the supported
replacement for raw SQL against `auth_users`. It pages through the
store in a stable email-ordered sequence and returns only
`id`/`email`/`roles`, never `password_hash`.

```go
users, total, err := mgr.ListUsers(ctx, auth.ListUsersOptions{
    Limit:  50, // <=0 → 50, capped at 500
    Offset: 0,  // <0 → 0
})
```

`total` is the full row count (independent of the page), so a UI can
render "showing 1–50 of 832". There is **no HTTP route** — call it
from trusted server code (an admin handler you mount yourself), not
the auth plugin surface.

`ListUsers` type-asserts the configured `UserStore` for the optional
`UserLister` interface. `EntityUserStore` implements it; a custom
store that does not gets `auth.ErrListUsersUnsupported` — a loud
failure rather than a silently empty list, so a deployment that forgot
a listable store is told explicitly.

## CSRF protection

For form-submit flows, mount the CSRF middleware globally and embed
the hidden field helper in every form:

```go
app.Use(auth.CSRF(auth.WithCSRFSecret(secret)))
```

```html
<form action="/save" method="POST" enctype="application/x-www-form-urlencoded">
  {{ csrfField .Request }}
  <input name="title">
</form>
```

Where `csrfField` is a template helper bound to
`auth.CSRFInputHTML(r)`. The middleware accepts the token either as a
hidden `_csrf` field (HTML forms) or as the `X-CSRF-Token` header (XHR /
fetch flows that don't go through a form).

Bearer-token requests (`Authorization: Bearer …`, `X-API-Key: …`) are
skipped — they don't ride on cookies and aren't subject to CSRF.

The CSRF cookie is `Secure`/`__Host-`-prefixed whenever the request is
HTTPS — including behind a TLS-terminating proxy that sets
`X-Forwarded-Proto: https` (the app itself sees plain HTTP there).

**Login/register/logout carry their own cross-site guard.** Those
endpoints can't rely on the CSRF cookie — a login CSRF needs no
pre-existing cookie, so an attacker's page could silently log a victim
into an attacker-controlled account. The core plugin refuses a **form**
(`application/x-www-form-urlencoded` / `multipart`) POST whose `Origin`
(or `Sec-Fetch-Site: cross-site`) says it came from another site. JSON
posts are exempt (a cross-site JSON POST needs a CORS preflight these
routes never answer), and requests with no `Origin` (curl, native apps)
pass. This is on by default; no configuration required.

## Naming conventions — DB columns vs. wire JSON

Mixing DB-column casing with wire-JSON casing trips up most first-time
users. The rule:

| Layer | Convention | Where set |
|---|---|---|
| DB column names | `snake_case` (e.g. `password_hash`, `user_id`) | Entity declarations + `UserEntityFields()` |
| JSON wire format | `camelCase` by default (e.g. `passwordHash`, `userId`) | `EntityConfig.JSONCase` or `AppConfig.JSONCase` — defaults to camelCase |

The framework automatically converts snake_case DB columns to
camelCase JSON keys at the response layer (via `crud.JSONCase`). You
do NOT need to match auth's snake_case column names in your own
entities — define your columns however you like at the DB layer and
the wire format stays consistent.

```go
// Both of these expose the SAME wire format ({"userId":"...","notes":"..."}):
app.Entity("logs", entity.EntityConfig{
    Fields: []schema.Field{
        {Name: "user_id", Type: schema.String}, // snake
        {Name: "notes",   Type: schema.String},
    },
    OwnerField: "user_id",
})
// Inside JSON payloads (POST body, response): {"userId": "...", "notes": "..."}
```

If you genuinely need snake_case on the wire (matching a Python or Rails
client's expectations), set `AppConfig.JSONCase = "snake_case"`.

## Cookie defaults

`AuthConfig.defaults()` produces two postures:

- **Production** (default): `SessionCookie = "__Host-session"`,
  `SessionSecure = true`. The `__Host-` prefix forces the browser to
  reject the cookie unless `Path=/`, `Secure`, and no `Domain` are set
  — this blocks sibling-subdomain cookie injection.
- **Dev** (`DevMode: true`): `SessionCookie = "session_id"`,
  `SessionSecure = false`. Use only over plain HTTP in local
  development.

## Rate limiting

Three independent surfaces:

```go
auth.AuthConfig{
    LoginRateLimit:           &auth.RateLimiterConfig{...}, // per-IP on /auth/login
    LoginRateLimitPerAccount: &auth.RateLimiterConfig{...}, // per-email on /auth/login
    RegisterRateLimit:        &auth.RateLimiterConfig{...}, // per-IP on /auth/register
}
auth.MagicLinkConfig{ RateLimit: &auth.RateLimiterConfig{...} } // per-IP on /auth/magic-link/send
auth.TwoFAConfig{ RateLimit: &auth.RateLimiterConfig{...} }     // per-IP on /auth/2fa/{verify,challenge}
auth.PasswordResetConfig{ RateLimit: &auth.RateLimiterConfig{...} } // per-IP on forgot/reset
auth.EmailVerificationConfig{ RateLimit: &auth.RateLimiterConfig{...} } // per-IP on send-verification
```

Per-IP + per-account on login is the recommended posture in production
— per-IP alone is bypassed by an attacker rotating through a botnet;
per-account alone is bypassed by spreading load across many target
accounts.

Login (per-IP + per-account) **and** register (per-IP) carry defaults
even when you set nothing — credential stuffing and account-table
flooding are network attacks, not config-mode ones. Pass a config with a
large `MaxAttempts` to loosen, not to leave them off.

**DevMode relaxes the per-IP login limiter.** Local screenshot /
verification tooling that hammers `/auth/login` from one IP (localhost)
would otherwise trip the per-IP flood throttle and get locked out. When
`DevMode: true`, the framework sets `RateLimiterConfig.DevMode` on the
per-IP login limiter, which short-circuits it (every attempt admitted).
This is a dev-only affordance — production (`DevMode: false`) is
unchanged and fail-closed. The **per-account** login limiter is
deliberately NOT relaxed in dev: it guards brute-force even there, so an
attacker who pivots IPs is still throttled on the email key. Set
`RateLimiterConfig.DevMode` explicitly on any other limiter you want
relaxed in dev.

**X-Forwarded-For is not trusted by default.** Set
`RateLimiterConfig.TrustForwardedFor = true` only when your service
runs behind a reverse proxy that strips client-supplied XFF headers
and rewrites it from the real source IP. Without that posture, an
attacker rotates the header per request and bypasses every per-IP
limit.

**Limits are per-process by default.** At N replicas the brute-force
budget multiplies by N and a block on one replica doesn't hold on the
others. Share the ledger through the database:

```go
shared := auth.NewSQLRateLimitStore(db, "auth_rate_limits") // creates its tables itself

auth.AuthConfig{
    LoginRateLimit:           &auth.RateLimiterConfig{Store: shared},
    LoginRateLimitPerAccount: &auth.RateLimiterConfig{Store: shared},
}
auth.TwoFAConfig{ RateLimit: &auth.RateLimiterConfig{Store: shared} }
```

One store instance can back every limiter: keys are namespaced by
`RateLimiterConfig.Scope`, which each built-in surface defaults to a
distinct value (`login_ip`, `login_account`, `register`, `twofa`, …).
A store error **fails closed** (denies with a short Retry-After) — an
attacker must never lift the limit by degrading its backend. See
[Horizontal scaling](scaling.md).

## Security audit trail

Security-sensitive auth events — login success/failure, the 2FA lifecycle,
password resets, OAuth links, magic-link issuance — are emitted to an
`AuditSink` on `AuthConfig` and land in the same `audit_log` table as the
CRUD hooks. One line of wiring:

```go
sink, _ := auth.NewSQLAuditSink(db, "")
mgr := auth.New(auth.AuthConfig{ AuditSink: sink, … })
```

Events use a closed vocabulary (e.g. `login.succeeded`, `2fa.enrolled`,
`password.reset_requested`) and never carry credentials — see the
[audit log](audit-log.md#auth-security-events) page for the full taxonomy
and the redaction posture. A nil sink disables auditing entirely.

## The 2FA flow

```
POST /auth/login            → 200, Set-Cookie session (PendingTwoFactor=true)
GET  /auth/me               → 403 "two-factor verification required"
POST /auth/2fa/challenge    → 200, server clears PendingTwoFactor + sets TwoFactorVerified
GET  /auth/me               → 200
```

The login response still returns 200 — clients can't tell whether 2FA
is required by the status code alone. A pending JSON login carries
`"two_factor_required": true` and **omits the JWT `token` field** (a
stateless JWT issued before the challenge would bypass the second
factor on every JWT-authenticated route). Clients either read the flag
or notice a follow-up endpoint returning 403, then drive the user
through `/auth/2fa/challenge` with the TOTP code or a backup code.

If the session store cannot record the pending state — it doesn't
implement `SessionPendingMarker`, the mark call fails, or the 2FA
state lookup itself errors — login **fails closed** with a 500 and the
just-minted session is destroyed. A degraded 2FA backend never means
"2FA is off".

`TwoFAPlugin.RequireTwoFA()` returns a middleware you can install on
any route that needs step-up authentication. The middleware is a
no-op for users who haven't enrolled in 2FA — only enrolled users are
gated.

## Account linking

```
GET    /auth/accounts            → list of linked OAuth providers + profile
DELETE /auth/unlink/{provider}   → remove a link
GET    /auth/oauth/{provider}    → initiate link/sign-in
GET    /auth/oauth/{provider}/cb → callback, binds (provider, providerID)
```

Unlink refuses (`409`) when the requested unlink would leave the user
with no remaining login method. The check considers both linked OAuth
accounts and whether the user has set a real password.

## OAuth token store + refresh

A provider access token is short-lived (Google's is ~1h). Without a
durable store, the provider's refresh token is discarded at login and
any call made on the user's behalf fails once the access token expires,
with no recovery. The `OAuthTokenStore` makes that recoverable. It is
**opt-in** — OAuth login behaves exactly as before when no store is
configured.

```go
tokStore, _ := auth.NewSQLOAuthTokenStore(db, auth.SQLOAuthTokenStoreConfig{
    EncryptionKey: []byte(os.Getenv("OAUTH_TOKEN_KEY")), // seals tokens at rest
})

oauth := auth.NewOAuth2Plugin(auth.OAuth2Config{
    Providers:   map[string]auth.OAuth2Provider{"google": google},
    StateSecret: os.Getenv("OAUTH_STATE_SECRET"),
    TokenStore:  tokStore, // persist {user, provider, access, refresh, expiry} at login
})
```

With a store wired in, the callback handler persists the access and
refresh tokens (upsert per `(user_id, provider)`). Both token columns are
sealed with AES-GCM before they touch the database — a raw table dump does
not surface live secrets. `EncryptionKey` is **required and non-empty**:
stored refresh tokens are password-equivalent, so `NewSQLOAuthTokenStore`
fails closed rather than sealing them with a default key. Source it from a
secret manager, not source code.

> **Pass the authenticated user's id.** `RefreshOAuthToken` / `ValidOAuthToken`
> take a `userID` — it must be the resolved session principal, never a
> request-supplied value, or it is an IDOR onto another user's tokens.

Making a call on the user's behalf:

```go
// Returns a currently-valid access token, refreshing transparently when
// the stored one is expired or within ~60s of expiry.
accessToken, err := auth.ValidOAuthToken(ctx, tokStore, google, userID)

// Or force a refresh and get the full updated record:
rec, err := auth.RefreshOAuthToken(ctx, tokStore, google, userID)
```

Refresh is concrete per provider — there is no generic provider registry.
`GoogleProvider` and `GitHubProvider` implement `OAuthTokenRefresher`
(`RefreshToken(ctx, refreshToken)`), POSTing a `refresh_token` grant to the
provider's token endpoint. Providers commonly omit the refresh token on a
refresh grant (Google does), so the stored refresh token is retained.
`GoogleProvider.AuthURL` now requests `access_type=offline` so Google
actually issues a refresh token.

`RefreshOAuthToken` errors when no refresh token is stored — the user must
re-authenticate. **Security-sensitive surface:** route changes here through
the auth audit gate before merge.

## OIDC (any compliant IdP)

`OIDCProvider` adapts any OpenID Connect-compliant identity provider
(Keycloak, Authentik, Authelia, Zitadel, Entra ID, Okta) to the same
`OAuth2Provider` interface as the built-in Google/GitHub providers — one
config block, discovered endpoints, JWKS-verified id_tokens.

```go
provider, err := auth.NewOIDCProvider(auth.OIDCConfig{
    Issuer:       "https://keycloak.example/realms/myrealm",
    ClientID:     "my-client",
    ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
    RedirectURL:  "https://app.example.com/auth/oauth/keycloak/callback",
    ProviderName: "keycloak", // Name() and OAuth2UserInfo.Provider
    // Scopes defaults to ["openid","email","profile"]; set to override.
    // JWKSCacheTTL defaults to 1h.
})

oauth := auth.NewOAuth2Plugin(auth.OAuth2Config{
    Providers:   map[string]auth.OAuth2Provider{"keycloak": provider},
    StateSecret: os.Getenv("OAUTH_STATE_SECRET"),
})
```

Discovery (`<issuer>/.well-known/openid-configuration`) runs lazily on
first use and is cached for the life of the process; a restart picks up
IdP endpoint moves. The document's `issuer` MUST match the configured
`Issuer` exactly (OIDC §4.3 — issuer-spoofing guard). `Issuer` must be
an `https://` URL; `http://` is accepted only for `localhost`/`127.0.0.1`
(local IdPs and tests).

**What gets verified** before `ExchangeCode` returns:

- the id_token signature against the IdP's JWKS;
- `alg` is pinned to **RS256 or ES256** — `none`, `HS256`, and case
  variants are rejected before any key lookup (alg-confusion defense);
- the signing key's `kty`/`crv` matches the `alg` (no RSA-vs-EC
  confusion); RSA moduli below 2048 bits and off-curve EC points are
  rejected at JWKS parse time;
- `iss` equals `Issuer`; `aud` contains `ClientID`; a present `azp` must
  equal `ClientID` (and a multi-audience token must carry one);
- `exp` (60s skew), `nbf` (tokens not yet valid are rejected), and `iat`
  (tokens issued in the future are rejected);
- a non-empty `sub`.

No `nonce` is sent on the authorize request: this is the
confidential-client authorization-code flow — the code is single-use and
exchanged server-to-server with the secret, and the plugin's HMAC state
token already binds the callback. A nonce only matters for the
implicit/hybrid flow.

No **PKCE** `code_challenge` is sent. PKCE's payload is protecting a
*public* client that has no secret; the flow here is confidential — the
single-use code is exchanged server-to-server under the client secret,
which is the actual protection on the code→token step, and the HMAC state
token binds the callback. A verifier derived from that same secret (or
from the public state) would add no defense a client-secret holder doesn't
already have, so it is deliberately omitted. Supporting public clients
(SPA/mobile) would need genuine PKCE — a random per-request verifier bound
via a cookie or store — and is out of scope for the confidential provider.

**Claims mapping.** `OIDCClaimsMapping` overrides which claim supplies
each field (defaults `sub`, `email`, `name`, `picture`) for IdPs that use
`preferred_username` or `upn`:

```go
Claims: auth.OIDCClaimsMapping{EmailClaim: "upn", NameClaim: "preferred_username"},
```

If the mapped email is empty and the IdP exposes a `userinfo_endpoint`,
it is fetched with the bearer token and only the missing fields are
merged in; the userinfo `sub` MUST match the id_token `sub` (OIDC §5.3.2).

**Separation from app JWTs.** This is a distinct verifier from the
HS256-only JWT verifier in `token.go`. Application-issued session JWTs
stay HS256-only by design; only third-party id_tokens are validated here,
against the IdP's published asymmetric keys. The two never share a code
path.

## Threat model assumptions

- The application sits behind a TLS terminator that rewrites
  `r.RemoteAddr` to the real client IP. Client-supplied XFF is not
  trusted; if you need it, set `TrustForwardedFor` explicitly.
- Cookies are scoped to a single origin. The `__Host-` prefix
  enforces this on the browser side. Cross-subdomain attacker?
  Blocked by the prefix.
- The session store is trusted. A compromise of the session table is
  game over — sessions are bearer tokens by design.
- The `EmailSender` is reliable. Plugins that need email return 503
  if no sender is configured and `DevMode` is off — they refuse to
  silently log live tokens to stdout in production.
- The `crypto/rand` source is available. If it fails, the process
  panics (entropy starvation makes the rest of the system unsound).

## Common mistakes

- **Wiring a custom token store for one plugin only.** The magic-link,
  email-verification, and password-reset plugins each construct their
  own `MemoryMagicLinkTokenStore`. If you replace one with a Redis
  store, replace all three — they share the same shape but not the
  same instance.
- **Forgetting `DevMode` over plain HTTP.** Without it, browsers
  refuse to accept the production `__Host-session` cookie over an
  insecure connection, and the user appears never to log in. The
  symptom is "login returns 200 but `/auth/me` returns 401".
- **Leaving `EmailSender` nil in production.** Magic-link,
  verification, and reset plugins all fail closed (503) in that case.
  Don't set `DevMode=true` as a workaround — that logs live tokens.
- **Trusting X-Forwarded-For without a proxy.** Per the docs above:
  default is off, and turning it on without a stripping proxy
  defeats every per-IP rate limit.
- **Treating `/auth/login` success as "fully authenticated".** A 2FA-
  enrolled user has a `PendingTwoFactor` session until they complete
  `/auth/2fa/challenge`. Don't read user PII from a session that's
  still pending.
- **Storing TOTP secrets cleartext.** The `User.TwoFactorSecret`
  column is plaintext base32 at the framework layer — operators are
  responsible for column-level or disk-level encryption. A DB leak
  with cleartext TOTP secrets is a full second-factor bypass.
