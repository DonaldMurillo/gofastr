package auth

import (
	"context"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// sessionCtxKey carries the resolved *Session on the request context.
type sessionCtxKey struct{}

// SessionMiddleware reads the session cookie, looks the session up in store,
// and (on hit) attaches it to the request context. Requests without a valid
// cookie pass through unchanged — use RequireSession to enforce presence.
//
// Pair with auth.RequireRole / your-own predicate to gate specific routes.
func SessionMiddleware(store SessionStore, cookieName string) middleware.Middleware {
	if cookieName == "" {
		cookieName = "session_id"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie(cookieName)
			if err == nil && c.Value != "" {
				if sess, err := store.Get(r.Context(), c.Value); err == nil {
					ctx := context.WithValue(r.Context(), sessionCtxKey{}, sess)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireSession is middleware that fails with 401 unless the request
// carries a valid session (as installed by SessionMiddleware).
func RequireSession() middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := SessionFromContext(r.Context()); !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SessionFromContext returns the active session if one is attached.
func SessionFromContext(ctx context.Context) (*Session, bool) {
	sess, ok := ctx.Value(sessionCtxKey{}).(*Session)
	return sess, ok
}
