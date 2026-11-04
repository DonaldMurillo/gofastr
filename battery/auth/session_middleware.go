package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// SessionMiddlewareOption tunes SessionMiddleware. Today only logging
// is configurable; future options can land here without breaking the
// SessionMiddleware(mgr) call shape.
type SessionMiddlewareOption func(*sessionMiddlewareOpts)

type sessionMiddlewareOpts struct {
	logger *slog.Logger
}

type sessionAssuranceKey struct{}

type sessionAssurance struct {
	manager  *AuthManager
	tenantID string
	userID   string
	verified bool
}

// WithSessionLogger overrides the logger SessionMiddleware uses for
// observability (store errors, misconfiguration warnings). When unset,
// slog.Default() is used. Passing nil disables all logging.
func WithSessionLogger(l *slog.Logger) SessionMiddlewareOption {
	return func(o *sessionMiddlewareOpts) { o.logger = l }
}

// SessionMiddleware returns middleware that loads the user identified by
// the session cookie (set by /auth/login; register no longer auto-logs-in
// since the 2026-09-04 anti-enumeration change) into the request context.
//
// Behaviour:
//   - No cookie → request proceeds anonymously (user not set in ctx).
//   - Invalid/expired/pending-2FA session → request proceeds anonymously.
//   - Valid session → user is loaded from UserStore.FindByID and stashed
//     in ctx via handler.SetUser. GetCurrentUser(ctx) then returns the
//     User; CRUD owner-scoping picks it up automatically.
//   - The resolved user's 2FA assurance records whether any of their live,
//     non-pending cookie sessions on this request completed 2FA, bound to
//     this manager and user. TenantScopedSessions also binds it to the
//     tenant under which the session was resolved.
//     Clearing or replacing the user also clears or replaces that assurance.
//   - Verified embed grants and calls admitted by App.InProcessHandler
//     preserve their trusted context user instead of clearing it.
//     Marked calls also preserve the outer request's recorded assurance;
//     it is a snapshot, not an expiry or revocation re-check.
//
// This is the missing counterpart to RequireAuth, which is JWT-only. Host
// apps with HTML/form auth want SessionMiddleware mounted on the routes
// that need to know "who is logged in", typically the whole app router.
//
//	app.Use(auth.SessionMiddleware(mgr))
//
// To gate routes behind a session, follow with RequireSession.
//
// Pass WithSessionLogger to receive structured logs about store errors
// (which otherwise look identical to "user logged out" in production).
func SessionMiddleware(mgr *AuthManager, opts ...SessionMiddlewareOption) middleware.Middleware {
	if mgr == nil {
		panic("auth.SessionMiddleware: mgr is nil")
	}
	if mgr.UserStore() == nil {
		panic("auth.SessionMiddleware: AuthManager has no UserStore: set AuthConfig.UserStore before constructing the middleware. Without it, every request would log a WARN and proceed anonymously, making the middleware useless.")
	}
	mgr.markResolving()
	o := sessionMiddlewareOpts{logger: slog.Default()}
	for _, fn := range opts {
		fn(&o)
	}
	log := o.logger
	// anon is the common fall-through for every "no valid user this
	// time" branch. It MUST clear any pre-existing user on ctx that
	// an outer SessionMiddleware (or other auth source) installed,
	// otherwise an inner per-tenant middleware silently carries the
	// outer tenant's identity into its handler, and OwnerField CRUD
	// queries scope cross-tenant. See TestSessionMiddleware_ClearsUserOn*.
	anon := func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		// ...but NOT an embed grant's subject. An embed request has no session
		// cookie by construction, framework/embed deletes it, and a browser
		// would not send one cross-site anyway, so this branch is the one every
		// embed request takes. Clearing here erased the identity the grant just
		// established, which made the documented wiring
		//
		//	fwApp.Use(embeds.Middleware())   // outermost
		//	fwApp.Use(auth.SessionMiddleware(mgr))
		//
		// produce an anonymous handler: owner-scoped CRUD, policies, tenant
		// resolution and audit all saw nobody. The reason anon exists, an outer
		// SessionMiddleware leaking one tenant's user into an inner one, does
		// not apply to a credential this middleware never issued and cannot
		// verify.
		if _, embedded := embed.GrantFromContext(r.Context()); embedded || framework.IsInProcessRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		ctx := handler.SetUser(r.Context(), nil)
		ctx = context.WithValue(ctx, sessionAssuranceKey{}, false)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			cfg := mgr.Config()

			// Try EVERY cookie carrying the session name, not only the first.
			// A jar can hold several: a stale cookie from an old deployment at
			// a more specific Path, or another localhost port's cookie
			// (localhost cookies ignore ports). Browsers send the most
			// path-specific first and r.Cookie returns only that one, so a
			// dead cookie would shadow a live session and login would
			// silently fail while a valid cookie sits one position later.
			tokens := sessionCookieCandidates(r, cfg.SessionCookie)
			user, assurance, ok := resolveSessionCandidates(ctx, mgr, tokens, "", log)
			if ok {
				ctx = handler.SetUser(ctx, user)
				ctx = context.WithValue(ctx, sessionAssuranceKey{}, assurance)
				ctx = handler.WithPrincipalCheck(ctx, sessionRecheck(mgr, tokens, assurance, log))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			anon(w, r, next)
		})
	}
}

// sessionRecheck re-resolves the original candidate session tokens for a
// request that stays open (an _events stream). It keeps the established user
// and partitioned lookup tenant; downstream checks derive the authorization
// tenant. It recomputes assurance across live, non-pending sessions; no remaining
// session or a deleted user ends the stream. See handler.WithPrincipalCheck.
func sessionRecheck(mgr *AuthManager, tokens []string, principal sessionAssurance, log *slog.Logger) handler.PrincipalCheck {
	return func(ctx context.Context) (context.Context, bool) {
		lookupContext := ctx
		if mgr.Config().TenantScopedSessions {
			lookupContext = handler.SetTenantID(lookupContext, principal.tenantID)
		}
		user, assurance, ok := resolveSessionCandidates(lookupContext, mgr, tokens, principal.userID, log)
		if !ok {
			return ctx, false
		}
		ctx = handler.SetUser(ctx, user)
		return context.WithValue(ctx, sessionAssuranceKey{}, assurance), true
	}
}

func resolveSessionCandidates(ctx context.Context, mgr *AuthManager, tokens []string, userID string, log *slog.Logger) (User, sessionAssurance, bool) {
	var user User
	var assurance sessionAssurance
	for _, token := range tokens {
		if user != nil {
			session, err := mgr.SessionStore().Get(ctx, token)
			if err == nil && session != nil && session.UserID == assurance.userID && !session.PendingTwoFactor {
				assurance.verified = assurance.verified || session.TwoFactorVerified
			}
			continue
		}
		candidate, candidateAssurance, ok := resolveSessionUser(ctx, mgr, token, log)
		if !ok || userID != "" && candidateAssurance.userID != userID {
			continue
		}
		user = candidate
		assurance = candidateAssurance
	}
	return user, assurance, user != nil
}

// maxSessionCookieCandidates bounds the same-name session cookies one
// request is judged on. A browser holds two or three at most (a parent
// domain's and the host's, or a stale Path); net/http parses up to 3000,
// and each candidate costs a session-store query (two on logout), so an
// anonymous request with 2999 copies drove 2999 queries on every route.
const maxSessionCookieCandidates = 8

// sessionCookieCandidates returns the distinct values of the cookies named
// name, in the order the client sent them, at most
// maxSessionCookieCandidates of them.
func sessionCookieCandidates(r *http.Request, name string) []string {
	var out []string
	for _, c := range r.CookiesNamed(name) {
		if c.Value == "" || slices.Contains(out, c.Value) {
			continue
		}
		out = append(out, c.Value)
		if len(out) == maxSessionCookieCandidates {
			break
		}
	}
	return out
}

// resolveSessionUser resolves one session token to its user: session lookup,
// pending-2FA refusal, user lookup. Logging matches the middleware's original
// per-step diagnostics.
func resolveSessionUser(ctx context.Context, mgr *AuthManager, token string, log *slog.Logger) (User, sessionAssurance, bool) {
	sess, err := mgr.SessionStore().Get(ctx, token)
	if err != nil {
		// Distinguish "session expired / not in store" (debug, normal
		// anonymous transition) from "store outage" (warn, operator needs
		// to see). The framework's ErrSessionNotFound is the legit not-found
		// sentinel; anything else is suspicious.
		if log != nil {
			if errors.Is(err, ErrSessionNotFound) {
				log.Debug("session: not found in store",
					"err", err.Error())
			} else {
				log.Warn("session: store lookup failed: request degraded to anonymous",
					"err", err.Error())
			}
		}
		return nil, sessionAssurance{}, false
	}
	if sess == nil {
		if log != nil {
			log.Debug("session: store returned nil session for cookie")
		}
		return nil, sessionAssurance{}, false
	}
	// Pending-2FA sessions are NOT loaded into context, they're
	// only usable for /auth/2fa/challenge.
	if sess.PendingTwoFactor {
		if log != nil {
			log.Debug("session: pending 2FA, request degraded to anonymous",
				"user_id", sess.UserID)
		}
		return nil, sessionAssurance{}, false
	}
	store := mgr.UserStore()
	if store == nil {
		// This is a host misconfiguration, battery/auth was wired
		// without a UserStore. WARN every time so operators see
		// the line; we don't panic because that would take the
		// app down for a recoverable mistake.
		if log != nil {
			log.Warn("session: UserStore not configured on AuthManager: session cookie can't be resolved",
				"recommendation", "set AuthConfig.UserStore at init")
		}
		return nil, sessionAssurance{}, false
	}
	user, err := store.FindByID(ctx, sess.UserID)
	if err != nil {
		if log != nil {
			if errors.Is(err, ErrUserNotFound) {
				log.Debug("session: user not found for session",
					"user_id", sess.UserID)
			} else {
				log.Warn("session: user-store lookup failed: request degraded to anonymous",
					"user_id", sess.UserID, "err", err.Error())
			}
		}
		return nil, sessionAssurance{}, false
	}
	if user == nil {
		if log != nil {
			log.Debug("session: user-store returned nil for session",
				"user_id", sess.UserID)
		}
		return nil, sessionAssurance{}, false
	}
	assurance := sessionAssurance{manager: mgr, userID: sess.UserID, verified: sess.TwoFactorVerified}
	if mgr.Config().TenantScopedSessions {
		assurance.tenantID = handler.GetTenantID(ctx)
	}
	return user, assurance, true
}

// RequireSession returns middleware that rejects requests without a
// valid session-cookie-loaded user. Pair with SessionMiddleware upstream.
// On calls admitted by App.InProcessHandler it trusts a non-nil server-set
// context user without re-reading a session or requiring 2FA assurance.
//
// The gate is session-only by contract: a caller authenticated by a
// scoped API token (TokenMiddleware sets the ctx user the same way a
// session does, plus TokenScopes) is refused with 401, so a leaked
// gfsk_ token cannot ride this gate past its scope leash.
//
// By default it returns JSON 401. Use WithRedirectOnFail to redirect
// browser (text/html-accepting) requests to a login page instead.
func RequireSession(opts ...RequireSessionOption) middleware.Middleware {
	o := requireSessionOptions{}
	for _, fn := range opts {
		fn(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u := GetCurrentUser(r.Context()); u != nil {
				// A gate documented "session-cookie-loaded user" only: a
				// caller authenticated by a scoped API token carries
				// TokenScopes in ctx (only TokenMiddleware sets them), and
				// admitting it would let a leaked gfsk_ token escape its
				// scope leash on every SSR screen this gate protects — the
				// exact escape requireSessionUserID refuses on /auth/tokens.
				// A JSON 401, not the browser redirect: a bearer client is
				// not a browser that lost its cookie.
				if _, tokenAuth := TokenScopes(r.Context()); tokenAuth {
					handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, reasonSessionRequired))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if o.failHTMLPath != "" && wantsHTML(r) {
				http.Redirect(w, r, o.failHTMLPath, http.StatusSeeOther)
				return
			}
			handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, reasonLoginRequired))
		})
	}
}

// RequireSessionOption configures RequireSession.
type RequireSessionOption func(*requireSessionOptions)

type requireSessionOptions struct {
	failHTMLPath string
}

// WithRedirectOnFail redirects unauthenticated HTML requests to the
// given path (e.g. "/login") instead of returning JSON 401. JSON
// requests still receive 401, only browsers get the redirect.
func WithRedirectOnFail(path string) RequireSessionOption {
	return func(o *requireSessionOptions) { o.failHTMLPath = path }
}

// wantsHTML reports whether the request prefers an HTML response.
// Browsers send Accept headers containing "text/html"; programmatic
// callers usually don't.
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	for part := range strings.SplitSeq(accept, ",") {
		part = strings.TrimSpace(part)
		if i := strings.IndexByte(part, ';'); i >= 0 {
			part = part[:i]
		}
		if part == "text/html" || part == "application/xhtml+xml" {
			return true
		}
	}
	return false
}
