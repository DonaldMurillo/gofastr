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
