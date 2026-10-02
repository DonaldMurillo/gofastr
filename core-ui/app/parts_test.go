package app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/store"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The parallel-parts fixtures (spike/layout-parts): one shell whose
// aside outlet DEFERS (with loading content) and whose toolbar outlet
// does not, one screen filling both, and counters on every loader.

type loadCounter struct {
	label   string
	loads   *int
	decline bool
}

func (c *loadCounter) Load(ctx context.Context) error {
	*c.loads++
	if c.decline {
		return app.ErrNoFill
	}
	return nil
}

func (c *loadCounter) Render() render.HTML { return render.Text("[" + c.label + "]") }

func partsApp(t *testing.T) (*app.App, *app.Layout, *int, *int, *int) {
	t.Helper()
	var screenLoads, toolbarLoads, asideLoads int
	a := app.NewApp("t")
	toolbar := app.NewOutlet("toolbar")
	aside := app.NewOutlet("aside", app.OutletOptions{
		Default:  &fillComp{label: "HELP"},
		Deferred: true,
		Loading:  &app.Loading{Show: app.LoadingComponent(render.Text("LOADING-ASIDE"))},
	})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{toolbar, aside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Place(toolbar), l.Primary(), l.Place(aside))
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &loadCounter{label: "SCREEN", loads: &screenLoads}).
		Fill(toolbar, &loadCounter{label: "TOOLBAR", loads: &toolbarLoads}).
		Fill(aside, &loadCounter{label: "ASIDE", loads: &asideLoads}), nil)
	a.RegisterScreen(app.NewScreen("/other", &loadCounter{label: "OTHER", loads: &screenLoads}).
		Fill(toolbar, &loadCounter{label: "TOOLBAR2", loads: &toolbarLoads}).
		Fill(aside, &loadCounter{label: "ASIDE2", loads: &asideLoads}), nil)
	return a, shell, &screenLoads, &toolbarLoads, &asideLoads
}

// TestPageRequestSkipsDeferredLoaders: under X-Gofastr-Defer the
// deferred outlet's loader NEVER runs and its loading content travels
// in its place (kept layers as an envelope fill, so the client paints
// the placeholder at the commit); the ordinary outlet's loader still
// runs.
func TestPageRequestSkipsDeferredLoaders(t *testing.T) {
	a, _, screenLoads, toolbarLoads, asideLoads := partsApp(t)
	res, err := a.RenderPartialFromResultDefer(store.WithValues(context.Background()), "/other", "/")
	if err != nil {
		t.Fatal(err)
	}
	if *toolbarLoads != 1 {
		t.Errorf("toolbar loads = %d, want 1 (an ordinary outlet still loads with the page)", *toolbarLoads)
	}
	if *asideLoads != 0 {
		t.Errorf("deferred aside loads = %d, want 0 (the page request skips deferred loaders; the part owns the fill)", *asideLoads)
	}
	if *screenLoads != 1 {
		t.Errorf("screen loads = %d, want 1 (the destination's own Load runs on the page request)", *screenLoads)
	}
	var aside string
	for _, f := range res.Fills {
		if f.Addr == "l:shell#aside" {
			aside = string(f.HTML)
		}
	}
	if !strings.Contains(aside, "LOADING-ASIDE") {
		t.Errorf("deferred region's envelope fill = %q, want the loading content in its place", aside)
	}
}

// TestFirstLoadCarriesDeferredRegions: a first load (whole document,
// no defer) waits for every fill — the deferred outlet's own content
// renders inline in its cell, so nothing depends on JavaScript.
func TestFirstLoadCarriesDeferredRegions(t *testing.T) {
	a, _, _, _, asideLoads := partsApp(t)
	res, err := a.RenderPageResult(store.WithValues(context.Background()), "/")
	if err != nil {
		t.Fatal(err)
	}
	if *asideLoads != 1 {
		t.Errorf("deferred aside loads on a first load = %d, want 1 (the document waits for every fill)", *asideLoads)
	}
	cell := outletCell(t, string(res.HTML), "l:shell#aside")
	if !strings.Contains(cell, "[ASIDE]") {
		t.Errorf("aside cell on a first load = %q, want the fill's own content inline", cell)
	}
	if strings.Contains(string(res.HTML), "LOADING-ASIDE</div>") {
		t.Errorf("a first load must not leave a deferred region on its loading content")
	}
}

// TestDeferredOutletWhoseScreenFillDeclines: a deferred outlet's screen
// fill can DECLINE at request time (ErrNoFill), which is exactly why
// deferral belongs to the outlet: the manifest lists the address
// anyway, and the part falls to the next candidate (the Default).
func TestDeferredOutletWhoseScreenFillDeclines(t *testing.T) {
	var screenLoads, asideLoads int
	a := app.NewApp("t")
	asideHandle := app.NewOutlet("aside", app.OutletOptions{
		Default:  &fillComp{label: "HELP"},
		Deferred: true,
	})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{asideHandle},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Primary(), l.Place(asideHandle))
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &loadCounter{label: "SCREEN", loads: &screenLoads}).
		Fill(asideHandle, &loadCounter{label: "DECLINED", loads: &asideLoads, decline: true}), nil)

	for _, e := range a.Routes() {
		if e.Path == "/" {
			if len(e.Deferred) != 1 || e.Deferred[0] != "l:shell#aside" {
				t.Fatalf("route manifest deferred = %v, want [l:shell#aside] (deferral is the outlet's, whatever the fill decides)", e.Deferred)
			}
		}
	}
	fill, outcome := a.RenderPartResult(store.WithValues(context.Background()), "/", "l:shell#aside")
	if outcome != app.PartApplied {
		t.Fatalf("part outcome = %v, want PartApplied (a decline is not a disagreement)", outcome)
	}
	if !strings.Contains(string(fill.HTML), "[HELP]") {
		t.Errorf("declined part fill = %q, want the Default [HELP] to win", fill.HTML)
	}
}

// TestPartRequestRunsOnlyItsFill: a part resolves the policy phase and
// runs ONLY the winning fill's loader — no screen Load, no other
// fills, no area builds. Counters on every loader prove it.
func TestPartRequestRunsOnlyItsFill(t *testing.T) {
	a, _, screenLoads, toolbarLoads, asideLoads := partsApp(t)
	ctx := store.WithValues(context.Background())
	fill, outcome := a.RenderPartResult(ctx, "/", "l:shell#aside")
	if outcome != app.PartApplied {
		t.Fatalf("part outcome = %v, want PartApplied", outcome)
	}
	if !strings.Contains(string(fill.HTML), "[ASIDE]") {
		t.Fatalf("part fill = %q, want [ASIDE]", fill.HTML)
	}
	if *screenLoads != 0 {
		t.Errorf("screen loads = %d, want 0 (a part never runs the screen's Load)", *screenLoads)
	}
	if *toolbarLoads != 0 {
		t.Errorf("toolbar loads = %d, want 0 (a part runs no other fill)", *toolbarLoads)
	}
	if *asideLoads != 1 {
		t.Errorf("aside loads = %d, want exactly 1 (only the winning fill's loader)", *asideLoads)
	}
	// A part for an address that is not a deferred outlet of the route
	// is a whole-page disagreement.
	if _, outcome := a.RenderPartResult(ctx, "/", "l:shell#toolbar"); outcome != app.PartReset {
		t.Errorf("toolbar part outcome = %v, want PartReset (the toolbar does not defer)", outcome)
	}
	if _, outcome := a.RenderPartResult(ctx, "/nope", "l:shell#aside"); outcome != app.PartReset {
		t.Errorf("unknown-route part outcome = %v, want PartReset", outcome)
	}
	if _, outcome := a.RenderPartResult(ctx, "/", "forged#addr"); outcome != app.PartReset {
		t.Errorf("forged-address part outcome = %v, want PartReset", outcome)
	}
}

// TestPartPolicyRedirectIsReset: a screen guard that redirects makes
// the part a whole-page outcome — the page answer carries the
// redirect, the part must not apply anything.
func TestPartPolicyRedirectIsReset(t *testing.T) {
	a := app.NewApp("t")
	asideHandle := app.NewOutlet("aside", app.OutletOptions{Deferred: true})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{asideHandle},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Primary(), l.Place(asideHandle))
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(asideHandle, &fillComp{label: "ASIDE"}), nil)
	a.RegisterScreen(app.NewScreen("/guard", &stubComp{html: "GUARD"}).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return app.Decision{Kind: app.DecisionRedirect, URL: "/"}
		})).
		Fill(asideHandle, &fillComp{label: "NEVER"}), nil)

	if _, outcome := a.RenderPartResult(store.WithValues(context.Background()), "/guard", "l:shell#aside"); outcome != app.PartReset {
		t.Errorf("guard part outcome = %v, want PartReset (a Redirect policy is a whole-page outcome)", outcome)
	}
}

// TestRoutesOmitDeferredWhenEmpty: routes without deferred outlets
// omit the field (the manifest travels with every document).
func TestRoutesOmitDeferredWhenEmpty(t *testing.T) {
	a, _, _, _, _ := partsApp(t)
	for _, e := range a.Routes() {
		if e.Path == "/" && len(e.Deferred) != 1 {
			t.Errorf("route / deferred = %v, want the aside address", e.Deferred)
		}
	}
	plain := app.NewApp("p")
	shell := app.NewLayout("shell", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	plain.SetDefaultLayout(shell)
	plain.RegisterScreen(app.NewScreen("/", &stubComp{html: "X"}), nil)
	for _, e := range plain.Routes() {
		if e.Deferred != nil {
			t.Errorf("route %s deferred = %v, want nil (omitted from the manifest)", e.Path, e.Deferred)
		}
	}
}
