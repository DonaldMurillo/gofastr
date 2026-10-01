package uihost

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// routeTitleComp binds route.title in its markup — the page a seed
// must still serve route.* for.
type routeTitleComp struct{}

func (c *routeTitleComp) Render() render.HTML { return c.RenderCtx(context.Background()) }

func (c *routeTitleComp) RenderCtx(ctx context.Context) render.HTML {
	return app.Route.Title.Bind(ctx, "h1", nil)
}

// seedIsland returns the page's #gofastr-signals block (the full-page
// seed the host renders into <head>).
func seedIsland(t *testing.T, srv *httptest.Server, path string) string {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	page := string(body)
	i := strings.Index(page, `id="gofastr-signals"`)
	if i < 0 {
		return ""
	}
	block := page[i:]
	return block[:strings.Index(block, "</script>")]
}

// TestRouteSeedOptIn pins the Opt-in decision's route.* arm
// (docs/DESIGN-layout-outlets.md "### Opt-in"): route.* is seeded
// only when the render read a route slice or the chain has a RouteArea.
//
//   - a plain page (no route bindings, no areas) ships NO route.*;
//   - a page that binds route.* in markup must still get it;
//   - a chain with a RouteArea seeds the family.
func TestRouteSeedOptIn(t *testing.T) {
	a := app.NewApp("RouteSeedGate")
	a.RegisterScreen(app.NewScreen("/", &plainSiteComp{marker: "HOME"}).WithTitle("Home"), nil)
	a.RegisterScreen(app.NewScreen("/bound", &routeTitleComp{}).WithTitle("Bound"), nil)
	shell := app.NewLayout("shell", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "crumbs"}},
	}, func(_ context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.RouteArea("crumbs", func(_ context.Context, _ app.Match) render.HTML {
				return render.Raw("CRUMBS")
			}),
			l.Primary(),
		)
	})
	g := app.NewScreenGroup("/area", shell)
	g.Screen(app.NewScreen("/area/x", &plainSiteComp{marker: "AREA"}).WithTitle("X"), nil)
	a.Router.ScreenGroup(g)

	host := New(a)
	srv := httptest.NewServer(host)
	t.Cleanup(srv.Close)

	if block := seedIsland(t, srv, "/"); strings.Contains(block, `"route.`) {
		t.Errorf("plain page's seed carries route.* (%s): a page that reads no route slice ships none", block)
	}
	block := seedIsland(t, srv, "/bound")
	if !strings.Contains(block, `"route.title"`) {
		t.Errorf("page binding route.title must still be seeded it: %s", block)
	}
	if block := seedIsland(t, srv, "/area/x"); !strings.Contains(block, `"route.`) {
		t.Errorf("a chain with a RouteArea must seed route.*: %s", block)
	}
}
