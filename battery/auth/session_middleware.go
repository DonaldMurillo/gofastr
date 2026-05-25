package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// SessionMiddlewareOption tunes SessionMiddleware. Today only logging
// is configurable; future options can land here without breaking the
// SessionMiddleware(mgr) call shape.
type SessionMiddlewareOption func(*sessionMiddlewareOpts)

type sessionMiddlewareOpts struct {
	logger *slog.Logger
}

// WithSessionLogger overrides the logger SessionMiddleware uses for
// observability (store errors, misconfiguration warnings). When unset,
// slog.Default() is used. Passing nil disables all logging.
func WithSessionLogger(l *slog.Logger) SessionMiddlewareOption {
	return func(o *sessionMiddlewareOpts) { o.logger = l }
}

// SessionMiddleware returns middleware that loads the user identified by
// the session cookie (set by /auth/login or /auth/register) into the
// request context.
//
// Behaviour:
//   - No cookie → request proceeds anonymously (user not set in ctx).
//   - Invalid/expired/pending-2FA session → request proceeds anonymously.
//   - Valid session → user is loaded from UserStore.FindByID and stashed
//     in ctx via handler.SetUser. GetCurrentUser(ctx) then returns the
//     User; CRUD owner-scoping picks it up automatically.
//
// This is the missing counterpart to RequireAuth, which is JWT-only. Host
// apps with HTML/form auth want SessionMiddleware mounted on the routes
// that need to know "who is logged in" — typically the whole app router.
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
		panic("auth.SessionMiddleware: AuthManager has no UserStore — set AuthConfig.UserStore before constructing the middleware. Without it, every request would log a WARN and proceed anonymously, making the middleware useless.")
	}
	o := sessionMiddlewareOpts{logger: slog.Default()}
	for _, fn := range opts {
		fn(&o)
	}
	log := o.logger
	// anon is the common fall-through for every "no valid user this
	// time" branch. It MUST clear any pre-existing user on ctx that
	// an outer SessionMiddleware (or other auth source) installed —
	// otherwise an inner per-tenant middleware silently carries the
	// outer tenant's identity into its handler, and OwnerField CRUD
	// queries scope cross-tenant. See TestSessionMiddleware_ClearsUserOn*.
	anon := func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		ctx := handler.SetUser(r.Context(), nil)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			cfg := mgr.Config()

			cookie, err := r.Cookie(cfg.SessionCookie)
			if err != nil || cookie.Value == "" {
				anon(w, r, next)
				return
			}
			sess, err := mgr.SessionStore().Get(ctx, cookie.Value)
			if err != nil {
				// Distinguish "session expired / not in store" (debug —
				// normal anonymous transition) from "store outage" (warn
				// — operator needs to see). The framework's ErrSessionNotFound
				// is the legit not-found sentinel; anything else is suspicious.
				if log != nil {
					if errors.Is(err, ErrSessionNotFound) {
						log.Debug("session: not found in store",
							"err", err.Error())
					} else {
						log.Warn("session: store lookup failed — request degraded to anonymous",
							"err", err.Error())
					}
				}
				anon(w, r, next)
				return
			}
			if sess == nil {
				if log != nil {
					log.Debug("session: store returned nil session for cookie")
				}
				anon(w, r, next)
				return
			}
			// Pending-2FA sessions are NOT loaded into context — they're
			// only usable for /auth/2fa/challenge.
			if sess.PendingTwoFactor {
				if log != nil {
					log.Debug("session: pending 2FA, request degraded to anonymous",
						"user_id", sess.UserID)
				}
				anon(w, r, next)
				return
			}
			store := mgr.UserStore()
			if store == nil {
				// This is a host misconfiguration — battery/auth was wired
				// without a UserStore. WARN every time so operators see
				// the line; we don't panic because that would take the
				// app down for a recoverable mistake.
				if log != nil {
					log.Warn("session: UserStore not configured on AuthManager — session cookie can't be resolved",
						"recommendation", "set AuthConfig.UserStore at init")
				}
				anon(w, r, next)
				return
			}
			user, err := store.FindByID(ctx, sess.UserID)
			if err != nil {
				if log != nil {
					if errors.Is(err, ErrUserNotFound) {
						log.Debug("session: user not found for session",
							"user_id", sess.UserID)
					} else {
						log.Warn("session: user-store lookup failed — request degraded to anonymous",
							"user_id", sess.UserID, "err", err.Error())
					}
				}
				anon(w, r, next)
				return
			}
			if user == nil {
				if log != nil {
					log.Debug("session: user-store returned nil for session",
						"user_id", sess.UserID)
				}
				anon(w, r, next)
				return
			}

			ctx = handler.SetUser(ctx, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireSession returns middleware that rejects requests without a
// valid session-cookie-loaded user. Pair with SessionMiddleware upstream.
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
			if GetCurrentUser(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			if o.failHTMLPath != "" && wantsHTML(r) {
				http.Redirect(w, r, o.failHTMLPath, http.StatusSeeOther)
				return
			}
			http.Error(w, `{"error":{"code":401,"message":"login required"}}`, http.StatusUnauthorized)
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
// requests still receive 401 — only browsers get the redirect.
func WithRedirectOnFail(path string) RequireSessionOption {
	return func(o *requireSessionOptions) { o.failHTMLPath = path }
}

// wantsHTML reports whether the request prefers an HTML response.
// Browsers send Accept headers containing "text/html"; programmatic
// callers usually don't.
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	for _, part := range strings.Split(accept, ",") {
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
