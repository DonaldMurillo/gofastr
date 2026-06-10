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
mgr.RegisterRoutes(app.Router)
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
| `SessionPendingMarker` | `CorePlugin` | Set `Session.PendingTwoFactor` after login for users who have 2FA enabled. |
| `TwoFactorChecker` | `CorePlugin` | Plugin-side signal: this user has 2FA enabled. `TwoFAPlugin` implements it. Custom plugins (WebAuthn, SMS) can implement it too. |

The `EntityUserStore` and `EntitySessionStore` provided in this
package implement every relevant interface; if you start from
`EntityUserStore` you get the full feature matrix.

## HTML form support

`/auth/login`, `/auth/register`, and `/auth/logout` accept both JSON
and HTML-form bodies. The handler branches on `Content-Type`:

| Request                                       | Response                                              |
|-----------------------------------------------|--------------------------------------------------------|
| `Content-Type: application/json`              | `200 OK` JSON body with `{user, token}`                |
| `application/x-www-form-urlencoded` (HTML)    | `303 See Other` with `Location` to `?next=` or `/`     |
| `multipart/form-data`                         | Same as form-urlencoded                                |

Form-flow responses set the session cookie before redirecting, so the
runtime's [form interceptor](../../core-ui/ARCHITECTURE.md#forms)
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

**X-Forwarded-For is not trusted by default.** Set
`RateLimiterConfig.TrustForwardedFor = true` only when your service
runs behind a reverse proxy that strips client-supplied XFF headers
and rewrites it from the real source IP. Without that posture, an
attacker rotates the header per request and bypasses every per-IP
limit.

## The 2FA flow

```
POST /auth/login            → 200, Set-Cookie session (PendingTwoFactor=true)
GET  /auth/me               → 403 "two-factor verification required"
POST /auth/2fa/challenge    → 200, server clears PendingTwoFactor + sets TwoFactorVerified
GET  /auth/me               → 200
```

The login response always succeeds — clients can't tell whether 2FA
is required by the status code alone. They notice when a follow-up
endpoint returns 403, then drive the user through `/auth/2fa/challenge`
with the TOTP code or a backup code.

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
