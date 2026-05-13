package render

import (
	"net/http"
	"sort"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// middlewareBuilder produces a router.Middleware from declarative config.
type middlewareBuilder func(cfg map[string]any) (router.Middleware, error)

// middlewareCatalog is the closed set of middlewares the world IR can
// reference. Keep small and additive; the agent surfaces unknown names
// as an error rather than silently dropping them.
var middlewareCatalog = map[string]middlewareBuilder{
	"recover": buildRecover,
}

func middlewareNames() []string {
	names := make([]string, 0, len(middlewareCatalog))
	for n := range middlewareCatalog {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func buildRecover(_ map[string]any) (router.Middleware, error) {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}, nil
}
