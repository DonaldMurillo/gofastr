package app_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// idFill renders one derived element id ("features" through the
// render context's id scope); wait delays its Load so a later outlet's
// goroutine finishes first.
type idFill struct{ wait time.Duration }

func (c *idFill) Load(ctx context.Context) error {
	time.Sleep(c.wait)
	return nil
}

func (c *idFill) Render() render.HTML { return "" }

func (c *idFill) RenderCtx(ctx context.Context) render.HTML {
	return render.Raw(`<h2 id="` + render.UniqueID(ctx, "features") + `"></h2>`)
}

var idAttr = regexp.MustCompile(`id="([^"]*features[^"]*)"`)

func featureIDs(s string) []string {
	var out []string
	for _, m := range idAttr.FindAllStringSubmatch(s, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestSlotIDsSettleInSlotOrder(t *testing.T) {
	a := app.NewApp("t")
	one, two, three := app.NewOutlet("one"), app.NewOutlet("two"), app.NewOutlet("three")
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{one, two, three}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(one), l.Place(two), l.Place(three))
		})
	a.SetDefaultLayout(shell)
	// The first outlet's loader is the slowest: its goroutine finishes
	// last, yet its id is settled first.
	a.RegisterScreen(app.NewScreen("/", &idFill{}).
		Fill(one, &idFill{wait: 30 * time.Millisecond}).
		Fill(two, &idFill{wait: 10 * time.Millisecond}).
		Fill(three, &idFill{}), nil)
	ctx := render.WithIDScope(store.WithValues(context.Background()))
	res, err := a.RenderPageResult(ctx, "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	want := []string{"features", "features-2", "features-3", "features-4"}
	if got := featureIDs(s); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("ids = %v, want %v:\n%s", got, want, s)
	}
	if strings.Contains(s, "gofastr-idref-") {
		t.Errorf("a placeholder survived the settle:\n%s", s)
	}
}

func TestDeferredPartMatchesPageIDs(t *testing.T) {
	a := app.NewApp("t")
	aside := app.NewOutlet("aside", app.OutletOptions{Deferred: true})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &idFill{}).Fill(aside, &idFill{}), nil)

	res, err := a.RenderPageResult(render.WithIDScope(store.WithValues(context.Background())), "/")
	if err != nil {
		t.Fatal(err)
	}
	page := featureIDs(outletCell(t, string(res.HTML), "l:shell#aside"))
	fill, outcome := a.RenderPartResult(render.WithIDScope(store.WithValues(context.Background())), "/", "l:shell#aside")
	if outcome != app.PartApplied {
		t.Fatalf("part outcome = %v", outcome)
	}
	part := featureIDs(string(fill.HTML))
	if len(page) != 1 || len(part) != 1 || page[0] != part[0] {
		t.Errorf("deferred outlet ids: page %v, part %v; the part must carry the page's ids", page, part)
	}
	// The outlet's namespace keeps it apart from the screen's own id.
	if got := featureIDs(string(res.HTML)); strings.Join(got, " ") != "features l-shell-aside--features" {
		t.Errorf("page ids = %v, want [features l-shell-aside--features]", got)
	}
}

func TestKeptAreaMatchesPageIDs(t *testing.T) {
	a := app.NewApp("t")
	site := app.NewLayout("site", app.LayoutSpec{Areas: []app.AreaSpec{{Name: "crumbs"}}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(
				l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
					return (&idFill{}).RenderCtx(ctx)
				}),
				l.Primary(),
			)
		})
	docs := app.NewLayout("docs", app.LayoutSpec{},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join((&idFill{}).RenderCtx(ctx), l.Primary())
		})
	a.SetDefaultLayout(site)
	g := app.NewScreenGroup("/docs", docs)
	g.Screen(app.NewScreen("intro", &idFill{}), nil)
	a.Router.ScreenGroup(g)
	a.Register("/about", &stubComp{html: "ABOUT"}, nil)

	full, err := a.RenderPageResult(render.WithIDScope(store.WithValues(context.Background())), "/docs/intro")
	if err != nil {
		t.Fatal(err)
	}
	part, err := a.RenderPartialFromResult(render.WithIDScope(store.WithValues(context.Background())), "/docs/intro", "/about")
	if err != nil {
		t.Fatal(err)
	}
	var crumbs string
	for _, f := range part.Fills {
		if f.Addr == "l:site~crumbs" {
			crumbs = string(f.HTML)
		}
	}
	// Document order: the site's area, the docs layer, the screen.
	want := []string{"features-3", "features-2", "features"}
	if got := featureIDs(string(full.HTML)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("full page ids %v, want %v", got, want)
	}
	got := append(featureIDs(crumbs), featureIDs(string(part.HTML))...)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("partial ids %v (kept area, then rendered layers), full page %v", got, want)
	}
}

// namedFill renders an explicit id="features".
type namedFill struct{}

func (namedFill) Render() render.HTML { return "" }

func (namedFill) RenderCtx(ctx context.Context) render.HTML {
	render.ReserveID(ctx, "features")
	return render.Raw(`<p id="features"></p>`)
}

func TestPageExplicitIDWins(t *testing.T) {
	a := app.NewApp("t")
	aside := app.NewOutlet("aside")
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a.SetDefaultLayout(shell)
	// The screen derives "features" before the outlet names it.
	a.RegisterScreen(app.NewScreen("/", &idFill{}).Fill(aside, namedFill{}), nil)
	res, err := a.RenderPageResult(render.WithIDScope(store.WithValues(context.Background())), "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if !strings.Contains(s, `<h2 id="features-2">`) || !strings.Contains(s, `<p id="features">`) {
		t.Errorf("the explicit id must keep features and the derived one move:\n%s", s)
	}
}

func TestDeferredOutletNamespacesDiffer(t *testing.T) {
	a := app.NewApp("t")
	names := []string{"aside-a", "aside_a", "Aside", "aside"}
	var outlets []*app.Outlet
	for _, n := range names {
		outlets = append(outlets, app.NewOutlet(n, app.OutletOptions{Deferred: true}))
	}
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: outlets},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			parts := []render.HTML{l.Primary()}
			for _, o := range outlets {
				parts = append(parts, l.Place(o))
			}
			return render.Join(parts...)
		})
	a.SetDefaultLayout(shell)
	s := app.NewScreen("/", &stubComp{html: "home"})
	for _, o := range outlets {
		s = s.Fill(o, &idFill{})
	}
	a.RegisterScreen(s, nil)
	res, err := a.RenderPageResult(render.WithIDScope(store.WithValues(context.Background())), "/")
	if err != nil {
		t.Fatal(err)
	}
	got := featureIDs(string(res.HTML))
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Errorf("two deferred outlets derived %q: %v", id, got)
		}
		seen[id] = true
	}
	if len(got) != len(names) || !seen["l-shell-aside--features"] {
		t.Errorf("ids = %v, want four, one of them l-shell-aside--features", got)
	}
}

var seedPanel = store.New("idseed").String("panel", "")

// idSeedFill seeds HTML holding a derived id into a slice and binds it.
type idSeedFill struct{ ns string }

func (idSeedFill) Render() render.HTML { return "" }

func (f idSeedFill) RenderCtx(ctx context.Context) render.HTML {
	if f.ns != "" {
		ctx = render.WithIDNamespace(ctx, f.ns)
		ids := render.NewRegionBatch(ctx, 1)
		h := idSeedFill{}.RenderCtx(ids.Context(0))
		return ids.Settle(0)(h)
	}
	seedPanel.Seed(ctx, `<h2 id="`+render.UniqueID(ctx, "features")+`"></h2>`)
	return seedPanel.BindHTML(ctx, "div", nil)
}

func TestSeededHTMLCarriesSettledIDs(t *testing.T) {
	for _, ns := range []string{"", "cart"} {
		a := app.NewApp("t")
		a.Register("/", idSeedFill{ns: ns}, nil)
		ctx := render.WithIDScope(store.WithValues(context.Background()))
		res, err := a.RenderPageResult(ctx, "/")
		if err != nil {
			t.Fatal(err)
		}
		v, _ := store.SeedFor(ctx, string(res.HTML))["idseed.panel"].(string)
		want := featureIDs(string(res.HTML))
		if len(want) != 1 || !strings.Contains(v, `id="`+want[0]+`"`) {
			t.Errorf("ns %q: seed = %q, page ids %v", ns, v, want)
		}
	}
}
