# 014 — Auth Battery

**Phase:** 2 (Batteries) | **Depends on:** 003, 004, 005

## Goal
Pluggable auth. Core interface in `core/`, built-in implementation in `battery/auth/` with sessions, password hashing, OAuth2.

## Deliverables
- [ ] Core auth interface: `GetCurrentUser(ctx)`, `HasRole(ctx, role)`, `IsOwner(ctx, resource)`, `Authenticate(ctx, creds)`, `Authorize(ctx, action, resource)`
- [ ] Session management: secure cookie settings, server-side session store interface, session rotation
- [ ] Password hashing: argon2id (recommended) with configurable params
- [ ] Email/password registration + login routes
- [ ] OAuth2 authorization code grant: Google, GitHub providers built-in
- [ ] Auth middleware: `RequireAuth()`, `RequireRole(role)`, `RequireOwner()`
- [ ] Login/register/logout handlers (mountable on Router)
- [ ] JWT alternative (pluggable token format)
- [ ] Password reset flow (generate token, send email, verify)
- [ ] Context helpers: `SetUser(ctx, user)`, `GetUser(ctx)`

## Acceptance Criteria
- Password hashing takes >100ms (argon2id) — not easily brute-forced
- Sessions use HttpOnly, Secure, SameSite cookies
- OAuth2 flow completes with real Google/GitHub (test with mock)
- RequireAuth middleware returns 401 for unauthenticated requests
