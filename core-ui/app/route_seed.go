package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
)

// Route addresses the route.* signal family (see core-ui/store).
var Route = store.Route

func seedRouteValues(ctx context.Context, path, pattern, title string, params map[string]string, query map[string][]string) {
	store.SeedRoute(ctx, decodeRouteSeg(path), pattern, title, params, store.SeedRouteQueryFrom(query))
}

func chainHasArea(chain []LayoutLayer) bool {
	for _, l := range chain {
		if l.Layout != nil && l.Layout.spec != nil && len(l.Layout.spec.Areas) > 0 {
			return true
		}
	}
	return false
}

func markChainArea(ctx context.Context, chain []LayoutLayer) {
	if chainHasArea(chain) {
		store.MarkRouteArea(ctx)
	}
}

func requestQuery(ctx context.Context) map[string][]string {
	if r := RequestFromContext(ctx); r != nil {
		return r.URL.Query()
	}
	return nil
}
