# Authentication

GoFastr's authentication lives in `battery/auth`. An `AuthManager` holds shared configuration (user store, session store, JWT secret, rate limits), and opt-in plugins add individual auth methods so unused flows stay out of your binary.

## Quickstart

```go
mgr := auth.New(auth.AuthConfig{
    JWTSecret:    os.Getenv("AUTH_JWT_SECRET"),
    UserStore:    myUserStore,    // required; auth.NewEntityUserStore(db, "users")
    SessionStore: mySessionStore, // required in prod; auth.NewEntitySessionStore(db, "sessions")
    LoginRateLimit: &auth.RateLimiterConfig{
        MaxAttempts:   10,
        Window:        15 * time.Minute,
        BlockDuration: 30 * time.Minute,
    },
})

mgr.Use(auth.NewCorePlugin().WithTwoFactorURL("/login/2fa")) // register first
mgr.Use(auth.NewMagicLinkPlugin(auth.MagicLinkConfig{
    BaseURL:      "https://app.example.com",
    EmailSender:  mySender,
    TwoFactorURL: "/login/2fa",
}))
mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{Store: twofaStore}))
mgr.Use(auth.NewAccountsPlugin())
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

### Production defaults vs. `DevMode`

- **Production (`DevMode: false`, default):**
  - `JWTSecret` is **mandatory**—`Init` fails closed if empty.
  - Durable stores are **mandatory**—in-memory session, 2FA, passkey, and token stores fail `Init` unless `AuthConfig.AllowInMemoryStores: true` is explicitly set.
  - Session cookie defaults to `__Host-session` (`Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, 7-day `SessionTTL`).
- **Development (`DevMode: true`):**
  - Empty `JWTSecret` auto-mints an ephemeral per-process key with a warning.
  - Session cookie defaults to `session_id` (`Secure=false`) so plain-HTTP localhost works.

### Rotating `JWTSecret`

Sign with the new secret while verifying tokens signed by the retiring secret for one `JWTExpiry` window (default 1h):

```go
mgr := auth.New(auth.AuthConfig{
    JWTSecret:          newSecret,
    JWTPreviousSecrets: []string{oldSecret},
})
```

Once the drain window passes, remove `JWTPreviousSecrets` and redeploy.

## Plugins at a glance

All plugins mount under `AuthConfig.BasePath` (default `/auth`). Built-in auth routes return `403` for in-process `App.InProcessHandler()` calls (those carry no browser session) and reserve `BasePath` against embedded surfaces.

| Plugin | Routes | Purpose |
|---|---|---|
| `CorePlugin` | `POST /auth/{login,register,logout,password}`, `GET /auth/me` | Base password login/register/logout, `/me`, and self-service password change. Always register first. Requires `WithTwoFactorURL` when `TwoFAPlugin` is present. Logout sends `Clear-Site-Data: "cache", "storage"` (customize via `WithLogoutClearSiteData`). |
| `MagicLinkPlugin` | `POST /auth/magic-link/send`, `GET/POST /auth/magic-link/verify` | Passwordless email links with a confirmation screen (`POST` redeems, `GET` never does). Auto-creates verified users or claims unverified ones. Requires `EmailSender` (unless `DevMode`) and `TwoFactorURL` next to 2FA. |
| `EmailCodePlugin` | `POST /auth/email-code/{send,verify}` | 8-digit emailed sign-in code bound to an `HttpOnly` challenge cookie in the requesting browser. |
| `OAuth2Plugin` | `GET /auth/oauth/{provider}`, `GET/POST /auth/oauth/{provider}/callback`, `GET /auth/oauth/{provider}/link` | Google, GitHub, Apple, and generic `OIDCProvider`. Requires `OAuthLinker` in prod and `TwoFactorURL` next to 2FA. |
| `TwoFAPlugin` | `POST /auth/2fa/{enroll,verify,challenge,disable,backup-codes}` | TOTP (6 digits, 30s default `Period`) + 8-char backup codes + `RequireTwoFA` middleware. |
| `AccountsPlugin` | `GET /auth/accounts`, `DELETE /auth/unlink/{provider}` | Lists and unlinks OAuth providers; refuses to remove the user's last login method. |
| `EmailVerificationPlugin` | `POST /auth/send-verification`, `GET /auth/verify-email` | Mails a verification token; redemption requires an active session of the requesting account. |
| `PasswordResetPlugin` | `POST /auth/forgot-password`, `POST /auth/reset-password` | Anti-enumeration async mail queue (200 for any address). Reset revokes all sessions, passkeys, OAuth links/tokens, API tokens, magic links, and email codes, then emails a security notice. |
| `PasskeyPlugin` | `POST /auth/passkey/{login,register,reauth}/options`, `POST /auth/passkey/{login,register,rename,delete,reauth}`, `GET /auth/passkey/list` | WebAuthn passkeys, conditional autofill, silent upgrade, and step-up reauth. |
| `TokensPlugin` | `POST/GET /auth/tokens`, `DELETE /auth/tokens/{id}` | Self-service scoped personal API tokens (`gfsk_...`) for logged-in browser sessions. |

Link-minting plugins (`MagicLinkPlugin`, `EmailVerificationPlugin`, `PasswordResetPlugin`) validate `BaseURL` at `Init` (must be absolute `http`/`https` or empty). Their `BodyTemplate` callbacks and `EmailSender.Send` receive the requester's locale context.

## Stores, entities & passwords

`auth.UserStore` (`FindByEmail`, `FindByID`, `CreateUser`, `UpdateRoles`) is the only required interface; return `auth.ErrUserNotFound` and `auth.ErrEmailTaken` for miss/conflict so DB transport errors fail loudly.

Use the built-in SQL stores—they implement the full optional extension matrix (`OAuthLinker`, `OAuthEnrichedLinker`, `OAuthProfileRefresher`, `OAuthUserCreator`, `AccountLister`, `AccountUnlinker`, `AtomicUnlinker`, `PasswordChecker`, `PasswordSetter`, `PasswordSwapper`, `PasswordClearer`, `EmailVerifier`, `EmailVerifiedChecker`, `NameStore`, `UserLister`):

```go
app.Entity("users",    auth.UserEntityConfig())    // CRUD=false, MCP=false
app.Entity("sessions", auth.SessionEntityConfig()) // CRUD=false, MCP=false
mgr.SetUserStore(auth.NewEntityUserStore(db, "users"))
mgr.SetSessionStore(auth.NewEntitySessionStore(db, "sessions"))
```

- **Auth entities are private by default:** `*EntityConfig()` keeps user/session tables off auto-CRUD and MCP. Soft-deleting a user (`Scope.SoftDelete`) immediately signs out all of their sessions, JWTs, and API tokens.
- **Durable 2FA store:** Seal TOTP secrets at rest with AES-GCM via `auth.NewEntityTwoFAStore(db, "auth_twofa", auth.EntityTwoFAStoreConfig{EncryptionKey: key, PreviousEncryptionKeys: oldKeys})`. Retired keys auto-re-seal on read.
- **Default roles:** New accounts receive `AuthConfig.DefaultRoles` (default `["user"]`). Client-supplied `roles` fields on registration are ignored.
- **Listing users:** Use `mgr.ListUsers(ctx, auth.ListUsersOptions{Limit: 50, Offset: 0})` from trusted admin code instead of raw SQL; returns `id`/`email`/`roles` (never `password_hash`).
- **Data erasure:** `AuthManager.Init` binds `App.EraseUserData` to your configured user, session, OAuth, passkey, token, and rate-limit tables.
- **DB vs. wire JSON casing:** Auth columns use `snake_case` in SQL; auto-CRUD converts to `camelCase` JSON by default (override via `AppConfig.JSONCase`).
- **Bootstrap admin seeds:** When creating a user programmatically (e.g., via `App.WithSeed`), call `MarkEmailVerified` immediately if your store implements `auth.EmailVerifier` so a future magic link doesn't claim and clear the seeded password.

### Password hashing & password changes

- `auth.DefaultHasher` defaults to `auth.BcryptHasher{}`. Switch to `auth.Argon2Hasher{}` (64 MiB, t=3, p=2) before `Init`; `CheckPassword` checks the `$argon2id$` prefix so legacy bcrypt rows continue working.
- `ValidatePasswordStrength` enforces at least `RecommendedMinPasswordBytes` (8) characters (up to 128 bytes).
- `POST /auth/password` (`{"current_password", "password"}`, optional `"confirm_password"`) requires an interactive non-pending cookie session (never API tokens or embed grants). On success it rotates the caller's session cookie, revokes all other sessions, purges reset/magic-link tokens, and invalidates older JWTs (JWTs embed a tag derived from the password hash). API tokens stay active.

### Canonical emails

All auth flows canonicalize emails via `auth.CanonicalEmail` (strip invisible/bidi/C1 controls, Unicode NFC normalize, trim, lowercase) and reject NULs, invalid UTF-8, or non-`local@domain` strings with `400 invalid_email`. Override via `AuthConfig.CanonicalizeEmail` (must be idempotent, verified at `Init`) and call `mgr.CanonicalEmail(addr)` in host code.

## HTML forms & password reset

`/auth/login`, `/auth/register`, and `/auth/logout` branch on `Content-Type`:
- `application/json`: JSON response (`200` with `{user, token}` on login; `202 {"accepted": true}` on register—uniform for free or taken emails, never auto-logs in, burns the same hash work either way; optional `AuthConfig.RegisterEmailSender` notifies existing holders async).
- `application/x-www-form-urlencoded` or `multipart/form-data` (1 MiB cap, no file parts): `303` redirect to `?next=` (validated same-origin path starting with `/`) or `/`. Failed form posts redirect `303` back to same-origin `Referer` or configured fallbacks (`auth.SetDefaultLoginErrorPath("/login")`, `auth.SetDefaultRegisterErrorPath("/register")`) with `?error=<code>`.

Duplicate or case-folded JSON/form keys (and unknown JSON keys) are refused with `400`.

Build login forms with `ui.Form`, `ui.Control` (`AutoComplete: "username"`, `NoSpellCheck: true`), and `ui.PasswordInput` (`Autocomplete: "current-password"` or `"new-password"`).

### Password reset from HTML forms

| Route | Fields | Form response |
|---|---|---|
| `POST /auth/forgot-password` | `email`, `next` | `303` to `next` (or `/`) for **every** address |
| `POST /auth/reset-password` | `token`, `password`, `next` | `303` to `next` (or `/`) on success; `303` back with `?error=` on failure (query stripped so token isn't echoed) |

Set `PasswordResetConfig.LinkPath` (same-origin path, e.g., `"/reset-password"`) to point emailed links at your reset screen (`?token=...` is appended).

## Session middleware, SSR policies & BFF posture

Mount `auth.SessionMiddleware(mgr)` globally to resolve the session cookie into `auth.GetCurrentUser(ctx)` and populate `framework/owner` automatically (note: `battery/auth` registers a process-global owner extractor in `init()`; override with `owner.SetExtractor` after import if needed). Invalid, expired, or pending-2FA cookies clear any outer context user and 2FA assurance.

- **Route middleware:** `auth.RequireSession()` (or `auth.RequireSession(auth.WithRedirectOnFail("/login"))`) requires a browser session (refuses scoped API tokens). `auth.RequireAuth(mgr.JWT())` accepts Bearer JWTs (re-resolved against `UserStore` on every request so deletions and role downgrades take effect immediately).
- **Custom tenant-partitioned stores:** Resolve the tenant before `SessionMiddleware`, or set `AuthConfig.GlobalSessionLookup: true` if your custom stores ignore tenant context (the built-in `EntityUserStore` + `EntitySessionStore` / `MemorySessionStore` are recognized as global automatically).

### A link from another site into a guarded page

Because session cookies use `SameSite=Strict`, top-level links from email or chat omit the cookie. When a cross-site `GET`/`HEAD` document navigation arrives with no session cookie and would hit a `401` or a login redirect, `SessionMiddleware` responds with a `200` (`Cache-Control: no-store`) same-origin hop page (`Refresh: 0;url=<path>` + Continue link, customizable via `AuthConfig.Pages.Hop`). The browser reloads same-site with the cookie attached. Non-navigation requests (`fetch`, islands, `POST`, `curl`) are untouched.

Built-in gates (`RequireSession`, `SessionPolicy`, `RolePolicy`, `battery/admin`) mark login redirects automatically. Custom redirect guards should call `handler.NoteSessionRefusal(ctx)` before redirecting:

<!-- gofastr:compile
import "net/http"
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "github.com/DonaldMurillo/gofastr/core/handler"
-->
```go
func guard(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if auth.GetCurrentUser(r.Context()) == nil {
            handler.NoteSessionRefusal(r.Context())
            http.Redirect(w, r, "/login", http.StatusSeeOther)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

<!-- gofastr:compile
import "context"
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "github.com/DonaldMurillo/gofastr/core-ui/app"
import "github.com/DonaldMurillo/gofastr/core-ui/app/decide"
import "github.com/DonaldMurillo/gofastr/core/handler"
-->
```go
// Attach with screen.WithPolicy(app.PolicyFunc(membersOnly)).
func membersOnly(ctx context.Context) app.Decision {
    if _, ok := auth.SessionFrom(ctx); ok {
        return decide.Allow()
    }
    handler.NoteSessionRefusal(ctx)
    return decide.Redirect("/login")
}
```

### Per-SSR-screen policies

Gate `uihost` screens before `Load()` runs using `auth.SessionPolicy` (defaults to `Redirect("/login?next=...")`) and `auth.RolePolicy` (defaults to `Block(403)`, also consults any `access.Decider`):

```go
dash := app.NewScreenGroup("/dashboard", dashLayout, auth.SessionPolicy())
dash.Screen(app.NewScreen("billing", &Billing{}).
    WithPolicy(auth.RolePolicy(auth.Roles("admin"))), nil)

// Serve Landing to anonymous visitors and Dashboard to signed-in users on "/":
application.RegisterScreen(
    app.NewScreen("/", &Dashboard{}).WithPolicy(
        auth.SessionPolicy(auth.WithRenderAlt(func() component.Component { return &Landing{} })),
    ),
    nil,
)
```

Options are last-write-wins (`WithRedirect`, `WithBlock`, `WithBlockReason`, `WithRenderAlt`). Suppress `?next=` with `auth.NoNext()`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/auth"
-->
```go
auth.SessionPolicy(auth.WithRedirect("/marketing", auth.NoNext()))
```

Inside a component's `RenderCtx`, call `auth.SessionFrom(ctx)` for conditional UI elements.

### CSRF & Browser-Backend (BFF) posture

Mount CSRF middleware and embed `auth.CSRFInputHTML(r)` (or `ui.Form`, which stamps it automatically):

```go
app.Use(auth.CSRF(auth.WithCSRFSecret(secret), auth.WithCSRFAuthRoutes(mgr)))
```

Bearer tokens (`Authorization: Bearer`, `X-API-Key`), verified embed exchanges, and `App.InProcessHandler()` calls skip CSRF; Basic/Digest/Negotiate do not.

**Login/register/logout carry their own cross-site guard.** Because login CSRF requires no pre-existing cookie, those endpoints (and bodyless 2FA endpoints `/2fa/{enroll,disable,backup-codes}`) refuse cross-site POSTs for all preflight-free content types (`application/x-www-form-urlencoded`, `multipart/form-data`, `text/plain`, and missing `Content-Type`) whenever `Origin` or `Sec-Fetch-Site: cross-site` indicates another site. JSON POSTs stay exempt because browsers preflight cross-site JSON.

For same-backend browser apps behind HTTPS, use the all-in-one BFF preset instead of wiring session, CSRF, and origin checks separately:

```go
app := framework.NewApp(
    framework.WithAPIPrefix("/api"),
    auth.WithBFFPosture(mgr, auth.BFFPostureConfig{
        AllowedOrigins: []string{"https://app.example.com"},
    }),
)
app.RegisterBattery(mgr)
```

`WithBFFPosture` forces cookie-only login (omits JWT from login JSON), enforces `Secure` session and CSRF cookies (so run behind HTTPS even if `DevMode` is true), mounts `SessionMiddleware` + CSRF, rejects non-allowlisted origins and Bearer JWTs under `APIPrefix`, and still permits `gfsk_` API tokens (customize via `BFFPostureConfig.TokenPrefix`).

`AllowedOrigins` requires exact `http://` or `https://` origins (no wildcards). When `APIPrefix` is unset, CRUD mounts at the root and the origin guard covers the whole app—include the app's own origin in `AllowedOrigins` for Origin-carrying browser requests. `framework/ui.SignOut` stays compatible: the logout path and form-POST OAuth callbacks are exempt from the generic CSRF token check (they verify origin themselves).

## Service accounts & scoped API tokens

Non-human clients (CI, scripts, customer CLIs) authenticate with `gfsk_`-prefixed API tokens (`gfsk_` + 40 random hex chars; only `sha256` + display prefix are stored, plaintext shown once). Tokens belong to a human user or a programmatic-only **service account** (`auth.NewServiceAccount`):

```go
ts, _ := auth.NewSQLAPITokenStore(db)
ss, _ := auth.NewSQLServiceAccountStore(db)

// Service account (no login path; manage from server code, disable via SetDisabled):
sa := auth.NewServiceAccount("ci-runner", []string{"deploy", "reader"})
_ = ss.Create(ctx, sa)

plaintext, rec, err := auth.IssueToken(ctx, ts, auth.TokenSpec{
    Name:      "ci-deploy",
    OwnerKind: "service", // "user" | "service"
    OwnerID:   sa.ID,
    Scopes:    []string{"posts:read", "role:deploy"},
    TTL:       30 * 24 * time.Hour, // 0 = no expiry
})

// Middleware wiring alongside SessionMiddleware:
app.Use(auth.SessionMiddleware(mgr))
app.Use(auth.TokenMiddleware(mgr.UserStore(), ss, ts, auth.WithTokenAudit(auditSink)))
app.Use(auth.RequireAPIScopes("/api")) // enforces <resource>:read / <resource>:write across /api
```

- **Scopes narrow capabilities:** Scopes follow `resource:verb` with `*` wildcards (`posts:*`, `*:read`, `*:*`; max 32 scopes). Sessions and JWTs pass `auth.RequireScope` / `auth.RequireAPIScopes` unscoped; API tokens must hold the required scope on the route, on any `?include=` / `?rel.field=` / cascade target, and `"role:<name>"` for role gates (`RequireRole`, `MCPRole`). Without `RequireAPIScopes` or `RequireScope`, token scopes are advisory only.
- **Self-service (`TokensPlugin`):** `POST/GET /auth/tokens` and `DELETE /auth/tokens/{id}` require an interactive browser session (and completed 2FA if enrolled)—an API token or embed grant cannot mint or list tokens (`401`). Omitted `ttl_seconds` mints a non-expiring token.
- **Generated clients:** Set `client.Token = plaintext`; Bearer requests skip CSRF automatically.

## The 2FA flow

```
POST /auth/login         → 200, Set-Cookie (PendingTwoFactor=true), {"two_factor_required": true} (no JWT)
GET  /auth/me            → 403 "two-factor verification required"
POST /auth/2fa/challenge → 200, Set-Cookie: fresh TwoFactorVerified session (pending session deleted)
GET  /auth/me            → 200
```

- HTML form logins redirect `303` to `WithTwoFactorURL`; render the 6-digit TOTP input with `ui.OneTimeCode` (or `ui.Control` for 8-char backup codes) posting `code` + `next` to `/auth/2fa/challenge`.
- **Every login path must mint via `AuthManager.MintSession(ctx, userID, ttl)`** so pending 2FA is enforced uniformly and fails closed (`ErrTwoFactorEnforcement`) if 2FA state cannot be read.
- **Positive step-up:** Self-service 2FA routes (`enroll`, `verify`, `disable`, `backup-codes`) and `RequireTwoFA()` verify `Session.TwoFactorVerified` (so a session minted *before* 2FA enrollment cannot disable 2FA). `RequireTwoFA` also binds to the same user's stepped-up cookie session on network requests (or outer recorded assurance on marked in-process calls).
- **Challenge limit:** `TwoFAConfig.ChallengeAttemptsPerSession` (default 10) deletes the pending session after too many failed guesses across any IP.

## Emailed sign-in codes

`EmailCodePlugin` signs users in with an 8-digit code emailed to their address—ideal on mobile or native apps where magic links open the wrong browser:

```go
tokens, _ := auth.NewSQLMagicLinkTokenStore(db)
mgr.Use(auth.NewEmailCodePlugin(auth.EmailCodeConfig{
    EmailSender:  mailer, // SendSignInCode(ctx, email, code string) error
    TokenStore:   tokens,
    OnSuccessURL: "/app",
    TwoFactorURL: "/login/2fa",
}))
```

- `POST /auth/email-code/send` (`email`) sets an `HttpOnly`, `SameSite=Strict` challenge cookie (`__Host-gofastr_email_code`) and returns uniform success.
- `POST /auth/email-code/verify` (`code`, ignoring spaces/hyphens) only succeeds in the browser holding that cookie. Capped at 5 guesses per challenge, `AddressGuessLimit` (default 10/hr) per address, and `CodeTTL` (default 10m). Codes are derived via HMAC from `JWTSecret` (nothing reusable stored). Account auto-creation, unverified-account claiming, and 2FA work identically to magic links.

## Magic links, unverified-account claiming & custom pages

### Following a magic link does not sign you in; confirming does

`GET /auth/magic-link/verify?token=...` renders a confirmation page ("Continue to sign in as …?") and redeems nothing—blocking mail scanners and login-CSRF links. A same-origin `POST` from that page redeems the token and redirects `302` to `OnSuccessURL` (or `TwoFactorURL` when 2FA is enabled).

### Proving the mailbox claims an unverified account

When a magic link, emailed code, or completed password reset proves ownership of an **unverified** account (blocking pre-registration squatting), GoFastr claims the account before minting a session: it revokes all sessions, removes all OAuth links and stored provider tokens, clears the squatter's password (for magic link/code), deletes all passkeys, revokes all API tokens, revokes sessions a second time, and finally marks `email_verified = true` (retaining any enrolled 2FA). Already-verified accounts are never claimed.

### Styling confirmation & hop pages

Pass `authui.Pages(host)` from `battery/auth/authui` to render confirmation and same-site hop pages with your app's `ui.AuthCard` and theme:

```go
mgr := auth.New(auth.AuthConfig{
    Pages: authui.Pages(host),
})
```

To draw them some other way, implement `auth.Pages`. Its two methods get the response writer, the request and the page data; the battery has already set `Cache-Control: no-store` and `Referrer-Policy: no-referrer`:

```go
type myPages struct{ host *uihost.UIHost }

func (p myPages) MagicLinkConfirm(w http.ResponseWriter, r *http.Request, d auth.ConfirmPageData) {
    p.host.RenderScreen(w, r, confirmScreen{d: d}, uihost.ScreenResponse{})
}

func (p myPages) Hop(w http.ResponseWriter, r *http.Request, d auth.HopPageData) {
    p.host.RenderScreen(w, r, hopScreen{d: d}, uihost.ScreenResponse{})
}

func (s confirmScreen) RenderCtx(ctx context.Context) render.HTML {
    return ui.AuthCard(ui.AuthCardConfig{
        Title: s.d.Words.Title,
        Body: ui.Form(ui.FormConfig{
            Action:      s.d.Action,
            SubmitLabel: s.d.Words.Submit,
        },
            // Words.Ask is escaped already. Any other dynamic value (d.Email, d.Token)
            // must go through an escaping builder, never raw into render.HTML.
            html.Paragraph(html.TextConfig{}, render.HTML(s.d.Words.Ask)),
            html.Paragraph(html.TextConfig{}, render.Text(s.d.Words.Warning)),
            html.Input(html.InputConfig{Type: "hidden", Name: "token", Value: s.d.Token}),
            // CSRFField is required under auth.CSRF() / WithBFFPosture.
            render.HTML(s.d.CSRFField)),
    })
}
```

## Passkeys (WebAuthn)

`PasskeyPlugin` implements stdlib WebAuthn (ES256, EdDSA, RS256 2048–8192 bits, attestation `none`, clone-counter detection, immutable backup-eligibility check) with lazy-loaded browser modules (`passkey` for login/autofill, `passkey-manage` for settings/step-up):

```go
passkeys, _ := auth.NewSQLPasskeyStore(db)
challenges, _ := auth.NewSQLMagicLinkTokenStore(db)

mgr.Use(auth.NewPasskeyPlugin(auth.PasskeyConfig{
    RPID:           "example.com",
    Origins:        []string{"https://example.com"},
    Store:          passkeys,
    ChallengeStore: challenges,
    OnSuccessURL:   "/app",
    TwoFactorURL:   "/login/2fa", // required next to 2FA
    NoticeSender:   mailer,       // emails verified owner on add (or set NoAddedNotice: true)
}))
```

Key options: `RPName`, `ChallengeTTL` (5m), `MaxPerUser` (20, enforced atomically), `ReauthTTL` (5m), `ReauthMinAge` (1h), `RateLimit` (30/min).

- **Buttons & attributes:** Add `ExtraAttrs` from `PasskeyLoginAttrs(base)`, `PasskeyRegisterAttrs(base)`, `PasskeyRenameAttrs(base, id)`, `PasskeyDeleteAttrs(base, id)`, `PasskeyReauthAttrs(base)`, `PasskeyNameAttrs()`, `PasskeyListAttrs(base)` (syncs removals to password managers via the WebAuthn Signal API), or `PasskeyUpgradeAttrs(base)` (silent conditional create after password login). Buttons render `hidden` and appear only when WebAuthn is available; include `<meta name="csrf-token">` behind `auth.CSRF`.
- **Autofill sign-in:** Combine `auth.PasskeyAutocomplete` (`"username webauthn"`) and `auth.PasskeyAutofillAttrs`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "github.com/DonaldMurillo/gofastr/framework/headless"
import "github.com/DonaldMurillo/gofastr/core/render"
import "github.com/DonaldMurillo/gofastr/framework/ui"
stmt: _ = field
-->
```go
field := func(c headless.FieldControl) render.HTML {
	return ui.Control(ui.ControlConfig{
		Field: c, Type: "email", Name: "email",
		AutoComplete: auth.PasskeyAutocomplete,
		ExtraAttrs:   auth.PasskeyAutofillAttrs("/auth"),
	})
}
```

- **Step-up confirmation:** Put `PasskeyReauthAttrs(base)` on a form button; the module writes a single-use proof into `auth.PasskeyReauthField` (`passkey_reauth`), and your handler redeems it:

<!-- gofastr:compile
import "net/http"
import "github.com/DonaldMurillo/gofastr/battery/auth"
var passkeys *auth.PasskeyPlugin
var w http.ResponseWriter
var r *http.Request
-->
```go
if err := passkeys.RedeemReauthProof(r, r.FormValue(auth.PasskeyReauthField)); err != nil {
	http.Error(w, "Confirm with your passkey first.", http.StatusForbidden)
	return
}
```

Only passkeys registered *before* the current session began and at least `ReauthMinAge` old qualify for step-up. Once an account has a qualifying passkey, adding or deleting passkeys requires a step-up proof too. Password resets delete all passkeys; password changes keep them.

## OAuth 2.0, OIDC & Sign in with Apple

### Identity linking & security guarantees

- **Durable link required:** `OAuth2Plugin` binds `(provider, provider_id)` via `OAuthLinker` (`EntityUserStore` creates `<users>_oauth_links`) and fails closed at `Init` without it (unless `DevMode` or `AllowInMemoryStores`).
- **Browser-bound state + PKCE:** Starting a flow sets an `HttpOnly`, `SameSite=Lax` state cookie (`__Host-gofastr_oauth_state` in prod) matched at callback, with single-use nonces and S256 PKCE enabled by default (`DisablePKCE` opts out). `StateSecret` must be ≥16 bytes (panics otherwise). Provider keys must match `[A-Za-z0-9_-]{1,64}`.
- **Callback resolution:**
  1. Existing `(provider, provider_id)` link → sign in (and refresh profile).
  2. No link, verified email matches existing account → `409` if the account has a password or an unverified address (prevents takeover; user must log in and link via `GET /auth/oauth/{provider}/link`); auto-links only onto a passwordless account whose email is already verified.
  3. Otherwise (including any unverified email) → create a new passwordless account and link it. Empty provider emails answer `400`.
- **Authenticated link & unlink:** `GET /auth/oauth/{provider}/link` binds a provider to the signed-in session (using a same-site hop if the callback arrives cross-site without the `SameSite=Strict` cookie). `DELETE /auth/unlink/{provider}` refuses (`409`) to remove the user's last login method and deletes any stored provider tokens.

### OAuth token store & refresh

Opt in to storing and refreshing provider access/refresh tokens (sealed with AES-GCM; `EncryptionKey` required):

```go
tokStore, _ := auth.NewSQLOAuthTokenStore(db, auth.SQLOAuthTokenStoreConfig{
    EncryptionKey: []byte(os.Getenv("OAUTH_TOKEN_KEY")),
})
oauth := auth.NewOAuth2Plugin(auth.OAuth2Config{
    Providers:   map[string]auth.OAuth2Provider{"google": google},
    StateSecret: os.Getenv("OAUTH_STATE_SECRET"),
    TokenStore:  tokStore,
})

// Always pass the authenticated session userID, never request input:
accessToken, err := oauth.ValidOAuthToken(ctx, "google", userID)
```

`ValidOAuthToken` refreshes transparently within ~60s of expiry (`GoogleProvider` and `GitHubProvider` implement `OAuthTokenRefresher`), serializing refreshes both in-process and across replicas via a row lease (`OAuthTokenLeaser`) so rotating refresh tokens are never double-spent. Prefer `oauth.ValidOAuthToken` / `oauth.RefreshOAuthToken` (keyed by registry name) over the package-level helpers when registering custom OIDC names.

### OIDC (Keycloak, Okta, Entra ID, Authentik, etc.)

<!-- gofastr:compile
stmt: _ = err
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "os"
stmt: _ = oauth
-->
```go
provider, err := auth.NewOIDCProvider(auth.OIDCConfig{
    Issuer:       "https://keycloak.example/realms/myrealm",
    ClientID:     "my-client",
    ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
    RedirectURL:  "https://app.example.com/auth/oauth/keycloak/callback",
    ProviderName: "keycloak",
})

oauth := auth.NewOAuth2Plugin(auth.OAuth2Config{
    Providers:   map[string]auth.OAuth2Provider{"keycloak": provider},
    StateSecret: os.Getenv("OAUTH_STATE_SECRET"),
})
```

Discovery is cached lazily, enforces exact `issuer` matching and HTTPS endpoints (loopback `http://` only for local dev), disables redirects on all IdP fetches, pins `alg` to `RS256`/`ES256` (RSA 2048–8192 bits), validates `iss`/`aud`/`azp`/`exp`/`nbf`/`iat`/`sub`, and maps claims via `OIDCClaimsMapping` (remapped email claims only count as verified if they match the verified `email` claim). Set `TokenAuthMethod` (`OIDCAuthClientSecretPost` or `OIDCAuthClientSecretBasic`) only if discovery misreports it.

**Multi-tenant Microsoft Entra ID:** Set `Tenants` (explicit tenant IDs or `"*"`) when using `/organizations/v2.0` or `/common/v2.0`. Accounts are keyed by `<tid>/<sub>`, never auto-link by email, and only mark email verified when the signed `id_token` carries both `xms_edov: true` and `email_verified: true`:

<!-- gofastr:compile
stmt: _ = err
stmt: _ = microsoft
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "os"
-->
```go
microsoft, err := auth.NewOIDCProvider(auth.OIDCConfig{
    Issuer:       "https://login.microsoftonline.com/organizations/v2.0",
    ClientID:     os.Getenv("MS_CLIENT_ID"),
    ClientSecret: os.Getenv("MS_CLIENT_SECRET"),
    RedirectURL:  "https://app.example.com/auth/oauth/microsoft/callback",
    ProviderName: "microsoft",
    Tenants:      []string{"72f988bf-86f1-41af-91ab-2d7cd011db47"}, // or "*"
})
```

### Sign in with Apple

<!-- gofastr:compile
stmt: _ = err
import "github.com/DonaldMurillo/gofastr/battery/auth"
import "os"
stmt: _ = oauth
-->
```go
provider, err := auth.NewAppleProvider(auth.AppleConfig{
    ClientID:    "com.example.web", // Services ID
    TeamID:      "ABCDE12345",
    KeyID:       "FGHIJ67890",
    PrivateKey:  []byte(os.Getenv("APPLE_PRIVATE_KEY")), // P-256 PKCS #8 .p8
    RedirectURL: "https://app.example.com/auth/oauth/apple/callback",
})

oauth := auth.NewOAuth2Plugin(auth.OAuth2Config{
    Providers:   map[string]auth.OAuth2Provider{"apple": provider},
    StateSecret: os.Getenv("OAUTH_STATE_SECRET"),
})
```

`AppleProvider` signs a fresh 5-minute ES256 client-secret JWT on each exchange and handles Apple's cross-site `form_post` callback via a same-origin hop to the `GET` callback (automatically exempted by `WithBFFPosture` and `WithCSRFAuthRoutes(mgr)`).

## Rate limiting & security audit trail

All auth endpoints ship with built-in per-IP rate-limit floors (plus per-account login limits and a shared per-address mail cooldown `MailAddressRateLimit` across magic-link and password-reset endpoints). `X-Forwarded-For` is ignored unless `TrustForwardedFor: true` (and `TrustedProxies` for multi-hop proxies) is set.

Limits are in-memory per replica by default. Share them across replicas with a SQL store (fails closed on store errors):

```go
shared := auth.NewSQLRateLimitStore(db, "auth_rate_limits")
auth.AuthConfig{
    LoginRateLimit:           &auth.RateLimiterConfig{Store: shared},
    LoginRateLimitPerAccount: &auth.RateLimiterConfig{Store: shared},
}
```

Note: keep `MailAddressRateLimit` in memory unless a fleet-wide cap matters more than preventing concurrent bursts from temporarily locking out a target address.

Wire security audit logging (`login.succeeded`, `2fa.enrolled`, `password.reset_requested`, `oauth.linked`, `token.created`, `roles.updated`, etc.—never logging raw credentials) in one line:

```go
sink, _ := auth.NewSQLAuditSink(db, "")
mgr := auth.New(auth.AuthConfig{AuditSink: sink})
```

See [audit-log.md](audit-log.md#auth-security-events) for the full event taxonomy.

## Common mistakes

- **Replacing the token store on only one plugin.** `MagicLinkPlugin`, `EmailVerificationPlugin`, and `PasswordResetPlugin` each default to an independent in-memory store (which fails `Init` in production). Wire a shared durable store (e.g., `NewSQLMagicLinkTokenStore`) on every link/code plugin you use.
- **Forgetting `DevMode: true` over plain HTTP.** Production defaults to `__Host-session` (`Secure=true`), which browsers reject over plain HTTP—login returns 200, then `/auth/me` returns 401.
- **Leaving `EmailSender` nil in production.** Magic-link and email-verification return 503; password-reset still returns 200 to prevent enumeration, silently dropping reset emails. Never set `DevMode: true` in production to work around it.
- **Trusting `X-Forwarded-For` without a controlled proxy.** Clients can spoof headers and bypass every per-IP rate limit.
- **Treating `/auth/login` 200 as fully authenticated.** Users with 2FA enabled hold a `PendingTwoFactor` session until `/auth/2fa/challenge` succeeds.
- **Losing or leaking the 2FA encryption key.** `NewEntityTwoFAStore` seals TOTP secrets with AES-GCM under `EncryptionKey`. Unsealed legacy plaintext rows only verify if encoded as **unpadded uppercase base32**; Lowercase-base32 seeds and other legacy encodings fail closed. Migrate offline by decoding with the original encoding, re-encoding as unpadded uppercase base32, and rewriting through the store (if the original encoding is unknown, require re-enrollment). Rotate keys with `PreviousEncryptionKeys` and store keys outside the database.
