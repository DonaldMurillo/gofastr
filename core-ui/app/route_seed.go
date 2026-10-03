package app

// route state reaches the browser.
//
// Route aliases the store's route.* slice family so layouts bind
// route values directly:
//
//	app.Route.Title.Bind(ctx, "span", nil)
//	app.Route.Param("id").Bind(ctx, "span", nil)
//
// seedRouteValues writes the live snapshot (path, pattern, effective
// title, params, query) into the request value bag right before the
// layouts and fills render, so every Bind stamps the values of the
// route being rendered. The bag is shared with the host's context
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/store"
)

// Route addresses the route.* signal family (see core-ui/store).
var Route = store.Route

// seedRouteValues installs the route snapshot for this render. query
// may be nil. See store.SeedRoute.
//
// The bag writes are cheap and carry no wire cost; what the opt-in
// gates is the seed WRITERS (store.SeedFor / SeedSplit include the
// declared route.* names only when the render read a route slice, the
// chain carries a RouteArea, or a computed names one — store.
// routeSeedDue). markChainArea below feeds the RouteArea arm: an
// area's fn runs on every render its layer survives and its markup
// can bind any route.* slice.
func seedRouteValues(ctx context.Context, path, pattern, title string, params map[string]string, query map[string][]string) {
	store.SeedRoute(ctx, path, pattern, title, params, store.SeedRouteQueryFrom(query))
}

// chainHasArea reports whether any tree layer of the chain declares a
// route area.
func chainHasArea(chain []LayoutLayer) bool {
	for _, l := range chain {
		if l.Layout != nil && l.Layout.spec != nil && len(l.Layout.spec.Areas) > 0 {
			return true
		}
	}
	return false
}

// markChainArea marks the request's bag when the chain carries a
// RouteArea, so the seed writers include the route.* family.
func markChainArea(ctx context.Context, chain []LayoutLayer) {
	if chainHasArea(chain) {
		store.MarkRouteArea(ctx)
	}
}

// requestQuery returns the URL query off the request the render
// serves, nil when the context carries none.
func requestQuery(ctx context.Context) map[string][]string {
	if r := RequestFromContext(ctx); r != nil {
		return r.URL.Query()
	}
	return nil
}
