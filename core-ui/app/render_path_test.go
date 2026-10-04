package app_test

// Render-path unit tests the audit found untested or partial
// (docs/DESIGN-layout-outlets.md test plan):
//
//   - TestHandleResolvesInsideGroup: a group layer's outlet address is
//     "g:<prefix>:<name>#<outlet>", derived from the resolved chain —
//     never serialised as an eager "l:<name>".
//   - TestRenderRawHasRouteState: RenderRaw installs the route match,
//     so a kept layer's RouteArea renders the live path.
//   - TestBuildRunsPerRenderWithLiveCtx: a kept layer's build re-runs
//     on every partial with the LIVE request context (the crumbs area
//     reads the request's user, not a stale closure's).
//   - TestFillResolutionOrder: with NESTED groups the candidates walk
//     innermost → outermost after the screen's own fill.

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestHandleResolvesInsideGroup: the wire address of an outlet a GROUP
// layer owns carries the group's resolved layer key
// ("g:/help:docs#toc"), the address the fills export and the client's
// targeter both use. The mutation it catches: serialising "l:<name>"
// eagerly — the fill would ship under an address no DOM holds.
func TestHandleResolvesInsideGroup(t *testing.T) {
	toc := app.NewOutlet("toc")
	a := app.NewApp("t")
	shell := app.NewLayout("shell", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	docs := app.NewLayout("docs", app.LayoutSpec{
		Outlets: []*app.Outlet{toc},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Place(toc), l.Primary())
	})
	a.SetDefaultLayout(shell)
	g := app.NewScreenGroup("/help", docs)
	g.Screen(app.NewScreen("/help/x", app.NewStaticComponent("X")).
		Fill(toc, app.NewStaticComponent("TOC-FILL")), nil)
	a.Router.ScreenGroup(g)

	res, err := a.RenderPageResult(context.Background(), "/help/x")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if !strings.Contains(s, `data-cui-outlet="g:/help/:docs#toc"`) {
		t.Errorf("a group layer's outlet must carry its group-resolved address:\n%s", s)
	}
	if !strings.Contains(s, "TOC-FILL") {
		t.Errorf("the fill for the group outlet must render:\n%s", s)
	}

	// The partial path: the kept shell exports nothing for the docs
	// layer (it renders), but a sibling INSIDE the group keeps the
	// docs layer and its toc fill must travel under the group address.
	pres, err := a.RenderPartialFromResult(context.Background(), "/help/x", "/help/y")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range pres.Fills {
		if f.Addr != "g:/help/:docs#toc" && !strings.HasPrefix(f.Addr, "l:shell") {
			t.Errorf("unexpected fill address %q (the group outlet must keep its group key on the wire)", f.Addr)
		}
	}
}

// TestRenderRawHasRouteState: RenderRaw installs the route match on
// its context (router.go), so a layout's RouteArea renders the live
// path without any host middleware. The mutation it catches: reverting
// RenderRaw to context.Background() — the area renders pathless.
func TestRenderRawHasRouteState(t *testing.T) {
	a := app.NewApp("t")
	shell := app.NewLayout("shell", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "crumbs"}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
				return render.Text("crumbs:" + m.Path() + "/id=" + m.Param("id"))
			}),
			l.Primary(),
		)
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/x/{id}", &paramEchoComp{}), nil)

	out, err := a.Router.RenderRaw("/x/7")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "crumbs:/x/7/id=7") {
		t.Errorf("RenderRaw's route areas must see the live match (path and params):\n%s", out)
	}
}

// paramEchoComp accepts and renders its route params.
type paramEchoComp struct {
	id string
}

func (p *paramEchoComp) SetParams(m map[string]string) { p.id = m["id"] }
func (p *paramEchoComp) Render() render.HTML           { return render.Text("BODY-" + p.id) }

// ctxUserKey is a request-scoped value only the live context carries.
type ctxUserKey struct{}

// TestBuildRunsPerRenderWithLiveCtx: on a subtree partial the KEPT
// layer's build re-runs in collect mode with the live request context
// — the area fn reads the request's user, so the exported fill carries
// the destination render's value, not a stored closure's. The mutation
// it catches: caching the area fn's output (or running the build with
// a stale ctx).
func TestBuildRunsPerRenderWithLiveCtx(t *testing.T) {
	areaShell := func() (*app.Layout, *labOutlets) {
		o := &labOutlets{
			Toolbar: app.NewOutlet("toolbar"),
			Aside:   app.NewOutlet("aside", app.OutletOptions{Default: app.NewStaticComponent("[HELP]")}),
		}
		return app.NewLayout("shell", app.LayoutSpec{
			Outlets: []*app.Outlet{o.Toolbar, o.Aside},
			Areas:   []app.AreaSpec{{Name: "crumbs"}},
		}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(
				l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
					user, _ := ctx.Value(ctxUserKey{}).(string)
					return render.Text("user:" + user + " path:" + m.Path())
				}),
				l.Place(o.Toolbar),
				l.Primary(),
				l.Place(o.Aside),
			)
		}), o
	}
	a := app.NewApp("t")
	shell, _ := areaShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/a", app.NewStaticComponent("A")).
		WithTitle("Page A"), nil)
	a.RegisterScreen(app.NewScreen("/b", app.NewStaticComponent("B")).
		WithTitle("Page B"), nil)

	ctxA := context.WithValue(context.Background(), ctxUserKey{}, "alice")
	ctxB := context.WithValue(context.Background(), ctxUserKey{}, "bob")

	resA, err := a.RenderPartialFromResult(ctxA, "/a", "/b")
	if err != nil {
		t.Fatal(err)
	}
	resB, err := a.RenderPartialFromResult(ctxB, "/b", "/a")
	if err != nil {
		t.Fatal(err)
	}
	fillA, fillB := "", ""
	for _, f := range resA.Fills {
		if f.Addr == "l:shell~crumbs" {
			fillA = string(f.HTML)
		}
	}
	for _, f := range resB.Fills {
		if f.Addr == "l:shell~crumbs" {
			fillB = string(f.HTML)
		}
	}
	if !strings.Contains(fillA, "user:alice") || !strings.Contains(fillA, "path:/a") {
		t.Errorf("the kept layer's area must run with the live ctx and the destination path, got %q", fillA)
	}
	if !strings.Contains(fillB, "user:bob") || !strings.Contains(fillB, "path:/b") {
		t.Errorf("every render re-runs the build (no stored closure), got %q", fillB)
	}
}

// TestFillResolutionOrder: the nested-group walk. The default shell
// declares aside; group /a fills it OUTER; subgroup /a/b fills it
// INNER; the screen fills it SCREEN. The screen's fill wins, then the
// INNERMOST group's, then the outer group's, then the Default.
func TestFillResolutionOrder(t *testing.T) {
	newApp := func(screenFill, innerFill, outerFill *app.StaticComponent) (*app.App, *app.Outlet) {
		a := app.NewApp("t")
		aside := app.NewOutlet("aside", app.OutletOptions{Default: app.NewStaticComponent("[HELP]")})
		shell := app.NewLayout("shell", app.LayoutSpec{
			Outlets: []*app.Outlet{aside},
		}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Place(aside), l.Primary())
		})
		a.SetDefaultLayout(shell)
		outer := app.NewScreenGroup("/a", nil)
		if outerFill != nil {
			outer.Fill(aside, outerFill)
		}
		inner := outer.SubGroup("/a/b", nil)
		if innerFill != nil {
			inner.Fill(aside, innerFill)
		}
		scr := app.NewScreen("/a/b/1", app.NewStaticComponent("SCREEN"))
		if screenFill != nil {
			scr.Fill(aside, screenFill)
		}
		inner.Screen(scr, nil)
		a.Router.ScreenGroup(outer)
		return a, aside
	}
	render := func(screenFill, innerFill, outerFill *app.StaticComponent) string {
		a, _ := newApp(screenFill, innerFill, outerFill)
		res, err := a.RenderPageResult(context.Background(), "/a/b/1")
		if err != nil {
			t.Fatal(err)
		}
		return string(res.HTML)
	}

	if s := render(app.NewStaticComponent("[SCREEN]"), app.NewStaticComponent("[INNER]"), app.NewStaticComponent("[OUTER]")); !strings.Contains(s, "[SCREEN]") {
		t.Errorf("the screen's fill outranks every group's: %s", s)
	}
	// No screen fill: the INNERMOST group wins.
	s := render(nil, app.NewStaticComponent("[INNER]"), app.NewStaticComponent("[OUTER]"))
	if !strings.Contains(s, "[INNER]") || strings.Contains(s, "[OUTER]") {
		t.Errorf("the innermost group's fill must beat the outer group's: %s", s)
	}
	// No subgroup fill: the outer group's resolves.
	if s := render(nil, nil, app.NewStaticComponent("[OUTER]")); !strings.Contains(s, "[OUTER]") {
		t.Errorf("the outer group's fill must resolve when the inner declares none: %s", s)
	}
	// Nothing fills: the outlet's Default.
	if s := render(nil, nil, nil); !strings.Contains(s, "[HELP]") {
		t.Errorf("the outlet's Default must be the last candidate: %s", s)
	}
}
