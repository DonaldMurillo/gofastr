package uihost

import (
	"net/http"
	"strings"

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
				if m, ok := ds.App.Router.MatchFor(routePath(r)); ok {
					r = r.WithContext(app.WithMatch(r.Context(), m))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// routePath is the request's path in the routing form Router.Resolve
// matches (app.RoutePath), so a key value holding "/" keeps its segment.
func routePath(r *http.Request) string { return app.RoutePath(r.URL.EscapedPath()) }

// fromPath reads X-Gofastr-From in the same routing form. The runtime
// sends location.pathname, percent-encoded, so an origin compared raw
// against the decoded target never shares a {param} group's layer. A
// query or fragment the header carries passes through unchanged.
func fromPath(r *http.Request) string {
	from := r.Header.Get("X-Gofastr-From")
	if i := strings.IndexAny(from, "?#"); i >= 0 {
		return app.RoutePath(from[:i]) + from[i:]
	}
	return app.RoutePath(from)
}

// HasScreen reports whether a screen route serves path, by the same
// resolution RouteMatchMiddleware uses. known is false when the host has
// no app router to ask. entityui calls it, through the framework's
// entityui host, to warn when a list's row links lead to no screen.
// path is escaped, as a link writes it.
func (ds *UIHost) HasScreen(path string) (has, known bool) {
	if ds.App == nil || ds.App.Router == nil {
		return false, false
	}
	_, ok := ds.App.Router.MatchFor(app.RoutePath(path))
	return ok, true
}
