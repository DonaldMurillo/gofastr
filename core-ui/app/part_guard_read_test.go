package app_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// guardReadApp mounts /r under a deferred outlet whose guard reads the
// lazy resolver row, which fails with ErrNotFound, and still allows.
// chain, when set, is /r's screen policy. It returns the screen Load
// count and the guard's run count.
func guardReadApp(chain app.Policy) (*app.App, *int, *int) {
	var loads, guardRuns int
	a := app.NewApp("t")
	row := app.NewKey[string]("row")
	aside := app.NewOutlet("aside", app.OutletOptions{
		Default:  &fillComp{label: "HELP"},
		Deferred: true,
		Policy: app.PolicyFunc(func(ctx context.Context) app.Decision {
			guardRuns++
			_, _ = row.Get(ctx)
			return app.Decision{Kind: app.DecisionAllow}
		}),
	})
	shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{aside}},
		func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), l.Place(aside))
		})
	a.SetDefaultLayout(shell)
	scr := app.NewScreen("/r", &loadCounter{label: "R", loads: &loads}).
		Resolve(row.From(func(context.Context) (string, error) { return "", app.ErrNotFound }))
	if chain != nil {
		scr = scr.WithPolicy(chain)
	}
	a.RegisterScreen(scr, nil)
	return a, &loads, &guardRuns
}

// A region guard reads a row that is missing and still allows: the page
// answers 404, so a part request for the same URL is the reset, never
// one side of that disagreement. The part checked resolvers only before
// the guards ran.
func TestPartResetsOnGuardReadFail(t *testing.T) {
	a, _, _ := guardReadApp(nil)
	if _, err := a.RenderPageResult(context.Background(), "/r"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("render err = %v, want ErrNotFound (the premise)", err)
	}
	if fill, outcome := a.RenderPartResult(context.Background(), "/r", "l:shell#aside"); outcome != app.PartReset {
		t.Errorf("part = %q outcome %d, want PartReset", fill.HTML, outcome)
	}
}

// The page and the bare partial answer a guard's failed read from the
// policy phase, before Load touches the DB.
func TestGuardReadFailSkipsLoad(t *testing.T) {
	a, loads, _ := guardReadApp(nil)
	if _, err := a.RenderPageResult(context.Background(), "/r"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("page err = %v, want ErrNotFound", err)
	}
	if _, err := a.RenderPartialResult(context.Background(), "/r"); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("partial err = %v, want ErrNotFound", err)
	}
	if *loads != 0 {
		t.Errorf("Load ran %d times, want 0", *loads)
	}
}

// The per-URL llm.md answers a guard's missing row as the page does:
// no such page.
func TestLLMMDGuardReadIsNoPage(t *testing.T) {
	a, _, _ := guardReadApp(nil)
	if res, ok := app.ScreenLLMMDForPath(context.Background(), a, "/r"); ok {
		t.Errorf("llm.md served for a 404 page:\n%s", res.MD)
	}
}

// A screen chain that blocks or redirects ends the policy phase: the
// region guards never run, so a guard's read cannot turn the chain's
// answer into a 404.
func TestChainRefusalSkipsRegionGuards(t *testing.T) {
	for _, d := range []app.Decision{
		{Kind: app.DecisionBlock, Status: http.StatusForbidden},
		{Kind: app.DecisionRedirect, URL: "/login"},
	} {
		a, _, guardRuns := guardReadApp(app.PolicyFunc(func(context.Context) app.Decision { return d }))
		res, err := a.RenderPageResult(context.Background(), "/r")
		if err != nil || res.Kind != d.Kind {
			t.Errorf("render = %+v err %v, want %+v", res, err, d)
		}
		if *guardRuns != 0 {
			t.Errorf("%+v: guard ran %d times, want 0", d, *guardRuns)
		}
	}
}
