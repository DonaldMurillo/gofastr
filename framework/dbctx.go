package framework

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/router"
)

type dbCtxKey struct{}

// WithDBContext returns ctx carrying db; a nil db returns ctx unchanged.
func WithDBContext(ctx context.Context, db *sql.DB) context.Context {
	if db == nil {
		return ctx
	}
	return context.WithValue(ctx, dbCtxKey{}, db)
}

// DBFromContext returns the *sql.DB on ctx, and false when there is none.
func DBFromContext(ctx context.Context) (*sql.DB, bool) {
	db, ok := ctx.Value(dbCtxKey{}).(*sql.DB)
	return db, ok && db != nil
}

// DBContextMiddleware puts the App's DB on every request context. The
// default middleware chain installs it; it passes through without a DB.
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
