package uihost

import (
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// RouteMatchMiddleware stores the request's [app.Match], when its path names a
// screen, on the context for the middleware registered after it. It authorizes
// nothing; register it before the guards that read route parameters.
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

func routePath(r *http.Request) string { return app.RoutePath(r.URL.EscapedPath()) }

func fromPath(r *http.Request) string {
	from := r.Header.Get("X-Gofastr-From")
	if i := strings.IndexAny(from, "?#"); i >= 0 {
		return app.RoutePath(from[:i]) + from[i:]
	}
	return app.RoutePath(from)
}

// HasScreen reports whether a screen route serves the escaped path; known is
// false when the host has no app router.
func (ds *UIHost) HasScreen(path string) (has, known bool) {
	if ds.App == nil || ds.App.Router == nil {
		return false, false
	}
	_, ok := ds.App.Router.MatchFor(app.RoutePath(path))
	return ok, true
}
