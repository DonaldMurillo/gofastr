package framework

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// dbCtxKey is the context key under which the App stashes its *sql.DB for
// screen / handler retrieval. Unexported so the only way in is
// WithDBContext and the only way out is DBFromContext.
type dbCtxKey struct{}

// WithDBContext returns a derived context carrying db. The App stamps this
// onto every request context (see App.DBContextMiddleware) so a screen's
// RenderCtx(ctx) / Load(ctx) can reach the same *sql.DB the framework holds
// without a package-level global handle.
func WithDBContext(ctx context.Context, db *sql.DB) context.Context {
	if db == nil {
		return ctx
	}
	return context.WithValue(ctx, dbCtxKey{}, db)
}

// DBFromContext returns the *sql.DB stamped onto ctx by an App with a DB
// configured. The second return value is false when no DB is present —
// e.g. a UI-only app, or a context that never passed through the App's
// request chain.
//
// This is the package-portable alternative to the package-level handle
// idiom: a shared screen can call framework.DBFromContext(ctx) instead of
// closing over a global *sql.DB captured at main() time.
func DBFromContext(ctx context.Context) (*sql.DB, bool) {
	db, ok := ctx.Value(dbCtxKey{}).(*sql.DB)
	return db, ok && db != nil
}

// DBContextMiddleware returns middleware that stamps the App's *sql.DB onto
// every request context so downstream handlers and screens can retrieve it
// via DBFromContext. When the App has no DB, the returned middleware is a
// pass-through.
//
// The App installs this automatically as part of the default middleware
// chain when a DB is configured, so most apps never call this directly.
// It's exposed for apps that opt out of the default chain
// (WithoutDefaultMiddleware) and want to wire DB-in-context into their own
// chain.
func (a *App) DBContextMiddleware() router.Middleware {
	db := a.DB
	return func(next http.Handler) http.Handler {
		if db == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithDBContext(r.Context(), db)))
		})
	}
}
