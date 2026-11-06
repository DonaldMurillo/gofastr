package uihost

import (
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// The catalog unescapes ?page= once, the way the runtime sends it. The
// embed branch substitutes its grant's surface path, a decoded route, so it
// must escape it first or a percent in the route is decoded twice.
func TestEmbedCatalogKeepsPercentRoutes(t *testing.T) {
	for i, route := range []string{"/embedpct/50%off", "/embedpct/%41"} {
		name := []string{"embedpct-off", "embedpct-hex"}[i]
		widget.Mount(router.New(), &widget.Definition{Name: name, Routes: []widget.RouteMatcher{
			func(p string) bool { return p == route },
		}})
		f := newEmbedFixture(t, func(cfg *fembed.Config) {
			cfg.Surfaces[1].Screen = app.NewScreen(route, &embedSubjectComp{}).WithTitle("Pct")
		})
		grant := f.grantFor(t, "other")
		rec := f.do(t, http.MethodGet, "/__gofastr/widgets", "", func(r *http.Request) {
			r.Header.Set("X-Gofastr-Embed", grant)
		})
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), name) {
			t.Errorf("grant for %s: catalog %d %s, want 200 naming %s", route, rec.Code, rec.Body.String(), name)
		}
	}
}
