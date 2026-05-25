# battery/auth

Sessions, password handling, CSRF, role checks, user/session stores —
all the auth primitives a host app would otherwise hand-roll.

**Use this when** the prompt mentions: login, signup, session, password,
bcrypt, CSRF, "require admin", "log out everywhere", role check,
"who is the current user".

**Import:** `github.com/DonaldMurillo/gofastr/battery/auth`

## Setup

```go
mgr := auth.New(auth.AuthConfig{
    SessionStore: auth.NewEntitySessionStore(db, "sessions"),
    UserStore:    auth.NewEntityUserStore(db, "users"),
})
app.Router().Use(auth.CSRF(auth.WithCSRFCookieSecure(isProd())))
app.Router().Use(auth.SessionMiddleware(mgr))
```

## Common primitives

| Need | Use this |
|---|---|
| Hash a password | `auth.HashPassword(pw) (string, error)` (bcrypt) |
| Verify a password | `auth.CheckPassword(pw, hash) bool` |
| Add `<input>` for CSRF token in a form | `render.HTML(auth.CSRFInputFromCtx(ctx))` |
| Send `X-CSRF-Token` from JS fetch | Read cookie `auth_csrf` / `__Host-auth_csrf`, send as header |
| Declare a User entity that satisfies auth | `auth.UserEntityFields()` returns the canonical schema fields |

## Hard rules

- **Every `<form method="POST">` MUST include `auth.CSRFInputFromCtx(ctx)`.**
  No exceptions. The CSRF middleware rejects POST/PUT/PATCH/DELETE
  without a matching token cookie + header/form input.
- **Never hand-roll bcrypt / session cookies / user table schema.** The
  battery owns those; rolling your own bypasses the threat model.
- **`auth.UserEntityFields()` returns the canonical field list.** To add
  domain columns (`disabled_at`, `username`, etc.), use the fluent
  builder:
  ```go
  Fields: auth.UserEntityFields().With(
      schema.Field{Name: "username", Type: schema.String, Unique: true},
      schema.Field{Name: "disabled_at", Type: schema.Timestamp},
  )
  ```

## Don't reinvent

- A `users` table with `password_hash` / `email_verified_at` / `roles`
  columns — `UserEntityFields()` ships it.
- A session table + cookie issuance — the SessionStore + middleware
  handle minting, refresh, expiration, and "log out everywhere".
- A login handler — auth-plugin lifecycle composes login/signup/oauth/
  magic-link/2FA. See `framework/docs/content/auth.md`.
