package uihost

import (
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// RouteMatchMiddleware returns middleware that resolves every request
// path against the app's screen router and stores the resulting
// [app.Match] on the request context, so middleware registered after
// it can read dynamic route parameters without re-parsing the path:
//
//	fwApp.Use(host.RouteMatchMiddleware()) // before the guards
//	fwApp.Use(auth.Session(...))
//	fwApp.Mount(host)
//
// It only populates; it authorizes nothing, and normal middleware
// ownership of authentication is unchanged. Router.Use runs the first
// registered middleware outermost, so this must be registered before
// the middleware whose guards read the parameters. A request that
// matches no screen carries no Match: guards see ok=false and should
// let the request fall through so unknown routes keep their truthful
// 404.
func (ds *UIHost) RouteMatchMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ds.App != nil && ds.App.Router != nil && r.URL != nil {
				if m, ok := ds.App.Router.MatchFor(r.URL.Path); ok {
					r = r.WithContext(app.WithMatch(r.Context(), m))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
