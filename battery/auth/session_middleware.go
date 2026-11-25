package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mediatype"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/embed"
)

// SessionMiddlewareOption tunes SessionMiddleware.
type SessionMiddlewareOption func(*sessionMiddlewareOpts)

type sessionMiddlewareOpts struct {
	logger *slog.Logger
}

type sessionAssuranceKey struct{}

type sessionAssurance struct {
	manager     *AuthManager
	tenantID    string
	tenantBound bool
	userID      string
	verified    bool
}

// WithSessionLogger sets SessionMiddleware's logger; nil disables logging.
func WithSessionLogger(l *slog.Logger) SessionMiddlewareOption {
	return func(o *sessionMiddlewareOpts) { o.logger = l }
}

// SessionMiddleware puts the user behind the session cookie, if any, in the request context.
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
	anon := func(w http.ResponseWriter, r *http.Request, next http.Handler) {
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

			tokens := sessionCookieCandidates(r, cfg.SessionCookie)
			user, assurance, ok := resolveSessionCandidates(ctx, mgr, tokens, "", log)
			if ok {
				ctx = handler.SetUser(ctx, user)
				ctx = context.WithValue(ctx, sessionAssuranceKey{}, assurance)
				ctx = handler.WithPrincipalCheck(ctx, sessionRecheck(mgr, tokens, assurance, log))
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			if target, ok := sameOriginTarget(r); ok && withheldCookieArrival(r, cfg.SessionCookie) {
				refusalCtx, refused := handler.TrackSessionRefusal(ctx)
				r = r.WithContext(refusalCtx)
				w = &arrivalHopWriter{ResponseWriter: w, r: r, pages: cfg.Pages, target: target, refused: refused}
			}
			anon(w, r, next)
		})
	}
}

func sessionRecheck(mgr *AuthManager, tokens []string, principal sessionAssurance, log *slog.Logger) handler.PrincipalCheck {
	return func(ctx context.Context) (context.Context, bool) {
		lookupContext := ctx
		if principal.tenantBound {
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

const maxSessionCookieCandidates = 8

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

func resolveSessionUser(ctx context.Context, mgr *AuthManager, token string, log *slog.Logger) (User, sessionAssurance, bool) {
	sess, err := mgr.SessionStore().Get(ctx, token)
	if err != nil {
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
	if sess.PendingTwoFactor {
		if log != nil {
			log.Debug("session: pending 2FA, request degraded to anonymous",
				"user_id", sess.UserID)
		}
		return nil, sessionAssurance{}, false
	}
	store := mgr.UserStore()
	if store == nil {
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
	assurance := sessionAssurance{manager: mgr, userID: sess.UserID, verified: sess.TwoFactorVerified, tenantBound: !mgr.globalSessionLookup()}
	if assurance.tenantBound {
		assurance.tenantID = handler.GetTenantID(ctx)
	}
	return user, assurance, true
}

// RequireSession refuses a request with no session-loaded user (an API-token caller included) with 401.
func RequireSession(opts ...RequireSessionOption) middleware.Middleware {
	o := requireSessionOptions{}
	for _, fn := range opts {
		fn(&o)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u := GetCurrentUser(r.Context()); u != nil {
				if _, tokenAuth := TokenScopes(r.Context()); tokenAuth {
					handler.WriteError(w, r, handler.Fail(http.StatusUnauthorized, reasonSessionRequired))
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if o.failHTMLPath != "" && mediatype.AcceptsAny(r.Header.Get("Accept"), "text/html", "application/xhtml+xml") {
				handler.NoteSessionRefusal(r.Context())
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

// WithRedirectOnFail sends refused HTML requests to path with a 303 instead of a 401.
func WithRedirectOnFail(path string) RequireSessionOption {
	return func(o *requireSessionOptions) { o.failHTMLPath = path }
}
