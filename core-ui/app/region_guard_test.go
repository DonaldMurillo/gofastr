package app_test

// PROTOTYPE (spike/layout-resolve): unit tests for region guards —
// outlet/area/fill policies that run in the policy phase, before any
// Load, and apply per region when it resolves.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// stubComp2 is a FRESH-instance alt factory target (decide.RenderAlt
// wants a new component per call; stubComp is value-shaped and shared).
type stubComp2 struct{ html string }

func (s *stubComp2) Render() render.HTML { return render.Text(s.html) }

// loadCounter (screen flavour) comes from parts_test.go: Load counts
// into a *int and renders [label].

// guardedShell is labShell with policies on the aside outlet and the
// crumbs area, so a test drives them through the spec. The handles
// come back with the layout.
func guardedShell(outletPolicy, areaPolicy app.Policy) (*app.Layout, *labOutlets) {
	o := &labOutlets{
		Toolbar: app.NewOutlet("toolbar"),
		Aside:   app.NewOutlet("aside", app.OutletOptions{Default: &fillComp{label: "HELP"}, Policy: outletPolicy}),
	}
	return app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{o.Toolbar, o.Aside},
		Areas:   []app.AreaSpec{{Name: "crumbs", Policy: areaPolicy}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.Text("HEADER "),
			l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
				return render.Text("crumbs:" + m.Path())
			}),
			l.Place(o.Toolbar),
			l.Primary(),
			l.Place(o.Aside),
		)
	}), o
}

// TestRegionGuardRunsBeforeLoad pins the phase: a region guard runs in
// the POLICY phase, before any Load — a Redirect decision moves the
// whole page with the screen's Load never executing (the counter stays
// zero), and the fill's loader is likewise untouched.
func TestRegionGuardRunsBeforeLoad(t *testing.T) {
	var screenLoads, fillLoads int
	a := app.NewApp("t")
	shell, outlets := guardedShell(app.PolicyFunc(func(ctx context.Context) app.Decision {
		return decide.Redirect("/login")
	}), nil)
	a.SetDefaultLayout(shell)
	scr := app.NewScreen("/g", &loadCounter{label: "S", loads: &screenLoads}).
		Fill(outlets.Aside, &loadCounter{label: "F", loads: &fillLoads})
	a.RegisterScreen(scr, nil)

	res, err := a.RenderPageResult(context.Background(), "/g")
	if err != nil {
		t.Fatal(err)
	}
	if res.Kind != app.DecisionRedirect || res.URL != "/login" {
		t.Fatalf("the region guard's Redirect must move the page: %+v", res)
	}
	if screenLoads != 0 {
		t.Fatalf("screen Load ran %d times under a redirecting region guard, want 0", screenLoads)
	}
	if fillLoads != 0 {
		t.Fatalf("fill Load ran %d times under a redirecting region guard, want 0", fillLoads)
	}
}

// TestRegionGuardRendersAltAndBlocks pins the per-region outcomes:
// Block renders the outlet's fallback (its Default, else nothing)
// while the page serves on; RenderAlt replaces the resolved fill with
// the alt component (it outranks the screen's own fill); an area's
// Block renders it empty and its RenderAlt replaces the fn's output.
func TestRegionGuardRendersAltAndBlocks(t *testing.T) {
	t.Run("outlet-block-renders-the-fallback", func(t *testing.T) {
		a := app.NewApp("t")
		shell, outlets := guardedShell(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Block(403, "archived")
		}), nil)
		a.SetDefaultLayout(shell)
		var fillRuns int
		scr := app.NewScreen("/b", &stubComp{html: "B"}).
			Fill(outlets.Aside, &loadCounter{label: "F", loads: &fillRuns})
		a.RegisterScreen(scr, nil)
		res, err := a.RenderPageResult(context.Background(), "/b")
		if err != nil {
			t.Fatal(err)
		}
		s := string(res.HTML)
		if !strings.Contains(s, "[HELP]") {
			t.Errorf("a blocked outlet renders its Default: %s", s)
		}
		if !strings.Contains(s, "B") {
			t.Errorf("the screen still renders: %s", s)
		}
		if fillRuns != 0 {
			t.Fatalf("a blocked region loads no candidate, got %d runs", fillRuns)
		}
	})
	t.Run("fill-guard-alt-replaces-the-fill", func(t *testing.T) {
		a := app.NewApp("t")
		shell, outlets := labShell()
		a.SetDefaultLayout(shell)
		var fillRuns int
		scr := app.NewScreen("/a", &stubComp{html: "A"}).
			Fill(outlets.Toolbar, &loadCounter{label: "F", loads: &fillRuns},
				app.FillPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
					return decide.RenderAlt(func() component.Component { return &stubComp2{html: "[ARCHIVED]"} })
				})))
		a.RegisterScreen(scr, nil)
		res, err := a.RenderPageResult(context.Background(), "/a")
		if err != nil {
			t.Fatal(err)
		}
		s := string(res.HTML)
		if !strings.Contains(s, "[ARCHIVED]") {
			t.Errorf("the guard's alt must render in the outlet: %s", s)
		}
		if fillRuns != 0 {
			t.Fatalf("the alt outranks the fill's own candidates, got %d fill loads", fillRuns)
		}
	})
	t.Run("area-block-and-alt", func(t *testing.T) {
		build := func(p app.Policy) string {
			a := app.NewApp("t")
			shell, _ := guardedShell(nil, p)
			a.SetDefaultLayout(shell)
			a.RegisterScreen(app.NewScreen("/c", &stubComp{html: "C"}), nil)
			res, err := a.RenderPageResult(context.Background(), "/c")
			if err != nil {
				t.Fatal(err)
			}
			return string(res.HTML)
		}
		if s := build(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Block(403, "quiet")
		})); strings.Contains(s, "crumbs:/c") {
			t.Errorf("a blocked area renders nothing: %s", s)
		}
		if s := build(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.RenderAlt(func() component.Component { return &stubComp2{html: "ALT-TRAIL"} })
		})); !strings.Contains(s, "ALT-TRAIL") || strings.Contains(s, "crumbs:/c") {
			t.Errorf("the area's alt replaces its fn: %s", s)
		}
	})
	t.Run("part-request-guard-outcomes", func(t *testing.T) {
		// A DEFERRED outlet (the only kind a part request serves):
		// a region Redirect is the reset; Block answers the fallback
		// HTML as an ordinary 200-body region answer.
		build := func(p app.Policy) *app.App {
			a := app.NewApp("t")
			toolbar := app.NewOutlet("toolbar")
			aside := app.NewOutlet("aside", app.OutletOptions{
				Default:  &fillComp{label: "HELP"},
				Deferred: true,
				Policy:   p,
			})
			shell := app.NewLayout("shell", app.LayoutSpec{
				Outlets: []*app.Outlet{toolbar, aside},
			}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
				return render.Join(l.Place(toolbar), l.Primary(), l.Place(aside))
			})
			a.SetDefaultLayout(shell)
			var loads int
			a.RegisterScreen(app.NewScreen("/p", &stubComp{html: "P"}).
				Fill(aside, &loadCounter{label: "F", loads: &loads}), nil)
			return a
		}
		fill, outcome := build(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Redirect("/login")
		})).RenderPartResult(context.Background(), "/p", "l:shell#aside")
		if outcome != app.PartReset {
			t.Fatalf("a region Redirect in a part request is the reset, got %d", outcome)
		}
		if fill.HTML != "" {
			t.Fatalf("the reset carries no body, got %q", fill.HTML)
		}
		fill, outcome = build(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Block(403, "archived")
		})).RenderPartResult(context.Background(), "/p", "l:shell#aside")
		if outcome != app.PartApplied {
			t.Fatalf("a blocked region answers its fallback as the part body, got %d", outcome)
		}
		if !strings.Contains(string(fill.HTML), "[HELP]") {
			t.Errorf("the part's fallback is the outlet Default: %s", fill.HTML)
		}
	})
	t.Run("guards-read-resolvers-in-the-policy-phase", func(t *testing.T) {
		// A region guard reading a resolver that fails is a whole-page
		// outcome (the policy phase), not a contained one.
		a := app.NewApp("t")
		team := app.NewKey[string]("team")
		shell, _ := guardedShell(app.PolicyFunc(func(ctx context.Context) app.Decision {
			if _, err := team.Get(ctx); err != nil {
				return decide.Block(500, "unreachable")
			}
			return decide.Allow()
		}), nil)
		a.SetDefaultLayout(shell)
		scr := app.NewScreen("/r", &stubComp{html: "R"})
		scr.Resolve(team.From(func(ctx context.Context) (string, error) { return "", app.ErrNotFound }))
		a.RegisterScreen(scr, nil)
		_, err := a.RenderPageResult(context.Background(), "/r")
		var pe *app.PageError
		if err == nil || !errors.As(err, &pe) || !pe.NotFound {
			t.Fatalf("a guard's resolver failure is the page outcome, got %v", err)
		}
	})
}
