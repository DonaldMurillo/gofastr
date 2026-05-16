// Package auth is GoFastr's authentication subsystem.
//
// Auth in GoFastr is composed from an [AuthManager] plus a set of plugins.
// The manager holds shared state ([UserStore], [SessionStore], JWT config,
// rate-limit config) and orchestrates a plugin lifecycle; the plugins
// register routes, middleware, and storage extensions for individual
// authentication methods (email/password, OAuth, magic-link, TOTP 2FA,
// account linking, email verification, password reset).
//
// # Quick start
//
//	mgr := auth.New(auth.AuthConfig{
//	    JWTSecret:  "your-secret-here",
//	    UserStore:  myUserStore,            // implements auth.UserStore
//	    // SessionStore left nil → in-memory default (single-instance only).
//	})
//	mgr.Use(auth.NewCorePlugin())           // /auth/login, /auth/register, /auth/me, /auth/logout
//	mgr.Use(auth.NewMagicLinkPlugin(auth.MagicLinkConfig{
//	    BaseURL:     "https://app.example.com",
//	    EmailSender: mySender,              // implements auth.MagicLinkEmailSender
//	}))
//	mgr.Use(auth.NewTwoFAPlugin(auth.TwoFAConfig{}))
//	mgr.Use(auth.NewAccountsPlugin())       // /auth/accounts, /auth/unlink/{provider}
//
//	if err := mgr.Init(nil); err != nil {
//	    return err
//	}
//	mgr.RegisterRoutes(myRouter)
//
// # Defaults & posture
//
// Cookies default secure-by-default: [AuthConfig] resolves to
// SessionCookie="__Host-session", SessionSecure=true. For local HTTP
// development, set [AuthConfig.DevMode]=true to get plain
// SessionCookie="session_id" and Secure=false.
//
// Per-IP rate limiting is opt-in via [AuthConfig.LoginRateLimit] and the
// per-plugin RateLimit fields. Per-account limiting is opt-in via
// [AuthConfig.LoginRateLimitPerAccount]. Neither is on by default — set
// them in production.
//
// X-Forwarded-For is NOT trusted by default. Set
// [RateLimiterConfig.TrustForwardedFor]=true ONLY when you sit behind a
// proxy that strips client-supplied XFF.
//
// # Extension points
//
// Most plugin features are gated on optional interfaces the host's
// [UserStore] or [SessionStore] may implement:
//
//   - [OAuthLinker]:     bind OAuth identities to local users by
//     (provider, providerID). Without it, OAuth callback falls back to
//     email-only matching (insecure for non-verifying providers).
//   - [AccountLister] / [AccountUnlinker]: required by [AccountsPlugin]
//     for the /auth/accounts and /auth/unlink endpoints.
//   - [PasswordChecker]: lets [AccountsPlugin] refuse "unlink the last
//     credential" correctly (otherwise it falls back to "must have at
//     least one OAuth account remaining").
//   - [EmailVerifier]:    required by [EmailVerificationPlugin].
//   - [PasswordSetter]:   required by [PasswordResetPlugin].
//   - [SessionTwoFAMarker] / [SessionPendingMarker]: required by
//     [TwoFAPlugin] for default-deny 2FA enforcement.
//   - [TwoFactorChecker]: plugins implementing this signal to
//     [CorePlugin] that a user has 2FA enabled and a fresh session
//     should be minted with [Session.PendingTwoFactor]=true.
//
// Stores that DON'T implement an optional interface fall back to a
// safe-but-reduced behavior (or, where fail-closed is the only option,
// the feature returns 500 — see each plugin's docs).
//
// # Storage
//
// [MemorySessionStore], [MemoryMagicLinkTokenStore], and [MemoryTwoFAStore]
// are in-memory implementations suitable for single-instance deployments
// and tests. There is no in-memory UserStore — bring your own (typically
// [EntityUserStore], which adapts a database table via the framework's
// entity system) or write a thin map-backed one for tests. For production
// multi-instance deployments, supply [EntityUserStore] + [EntitySessionStore]
// (backed by SQLite or PostgreSQL).
//
// # Sentinel errors
//
// Handlers branch on these typed errors rather than string matching:
//
//   - [ErrUserNotFound]:       UserStore.FindBy* returned no row.
//   - [ErrEmailTaken]:         CreateUser hit a unique constraint on email.
//   - [ErrUnauthorized]:       JWT verification failed.
//   - [ErrForbidden]:          authenticated but not authorized.
//   - [ErrSessionNotFound]:    SessionStore.Get returned no row or expired.
package auth
