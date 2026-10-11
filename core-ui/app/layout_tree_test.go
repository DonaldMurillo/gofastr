package app_test

// PROTOTYPE (spike/layout-proto): unit tests for tree layouts and
// outlet fills (docs/DESIGN-layout-outlets.md).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// fillComp renders its label and records its Load call. Load returning
// app.ErrNoFill declines the candidate.
type fillComp struct {
	label   string
	loaded  *[]string
	loadErr error
}

func (f *fillComp) Load(ctx context.Context) error {
	if f.loaded != nil {
		*f.loaded = append(*f.loaded, f.label)
	}
	return f.loadErr
}

func (f *fillComp) Render() render.HTML { return render.Text("[" + f.label + "]") }

// labOutlets holds the canonical test shell's outlet handles.
type labOutlets struct{ Toolbar, Aside *app.Outlet }

// labShell builds the canonical test shell: a static header, a crumbs
// route area, a toolbar outlet (FallbackNothing) and an aside outlet
// whose Default renders [HELP]. The handles come back with the layout
// so the caller's fills name the same typed values.
func labShell() (*app.Layout, *labOutlets) {
	o := &labOutlets{
		Toolbar: app.NewOutlet("toolbar"),
		Aside:   app.NewOutlet("aside", app.OutletOptions{Default: &fillComp{label: "HELP"}}),
	}
	return app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{o.Toolbar, o.Aside},
		Areas:   []app.AreaSpec{{Name: "crumbs"}},
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

func TestFillResolutionOrderScreenGroupDefault(t *testing.T) {
	build := func(screenFill *fillComp) string {
		a := app.NewApp("t")
		shell, outlets := labShell()
		a.SetDefaultLayout(shell)
		g := app.NewScreenGroup("/inbox", nil)
		g.Fill(outlets.Toolbar, &fillComp{label: "GROUP"})
		inbox := app.NewScreen("/inbox", &stubComp{html: "INBOX"})
		if screenFill != nil {
			inbox.Fill(outlets.Toolbar, screenFill)
		}
		g.Screen(inbox, nil)
		a.Router.ScreenGroup(g)
		res, err := a.RenderPageResult(context.Background(), "/inbox")
		if err != nil {
			t.Fatal(err)
		}
		return string(res.HTML)
	}

	// Screen fill wins over the group fill.
	if s := build(&fillComp{label: "SCREEN"}); !strings.Contains(s, "[SCREEN]") || strings.Contains(s, "[GROUP]") {
		t.Errorf("screen fill must beat the group fill: %s", s)
	}
	// Without a screen fill the group fill resolves inside the group.
	if s := build(nil); !strings.Contains(s, "[GROUP]") {
		t.Errorf("group fill must resolve for a group screen: %s", s)
	}
	// Neither screen nor group fills: the toolbar falls to its
	// FallbackNothing (empty), the aside to its Default ([HELP]).
	if s := build(nil); !strings.Contains(s, "[HELP]") {
		t.Errorf("aside must fall to its Default: %s", s)
	}
}

func TestFillFallsThroughErrNoFillToDefault(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	var loaded []string
	screen := app.NewScreen("/", &stubComp{html: "HOME"}).
		Fill(outlets.Toolbar, &fillComp{label: "DECLINED", loadErr: app.ErrNoFill}).
		// Both candidates decline: the aside falls to its Default.
		Fill(outlets.Aside, &fillComp{label: "ALSO_OUT", loaded: &loaded, loadErr: app.ErrNoFill})
	a.RegisterScreen(screen, nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	// The declined toolbar candidate leaves the outlet empty.
	if !strings.Contains(s, `<div data-cui-outlet="l:shell#toolbar"></div>`) {
		t.Errorf("declined toolbar fill must render an empty outlet: %s", s)
	}
	if !strings.Contains(s, "[HELP]") {
		t.Errorf("aside must fall through to its Default: %s", s)
	}
	if len(loaded) != 1 || loaded[0] != "ALSO_OUT" {
		t.Errorf("the aside candidate must have loaded exactly once, got %v", loaded)
	}
}

// TestFillErrorContainedToOutlet (DESIGN "A failing fill is contained
// to its outlet") pins the default build, no switches: a fill's Load
// error, a fill's render panic, and a fill that provides an
// ErrorBoundary fallback each degrade ONLY their outlet — the page
// keeps the screen's status and content, the hostile error text never
// reaches the HTML, and the failure is logged once.
func TestFillErrorContainedToOutlet(t *testing.T) {
	for _, tc := range []struct {
		name string
		fill func() component.Component
		want string
	}{
		{"load error", func() component.Component { return &boomLoad{} }, "[HELP]"},
		{"render panic", func() component.Component { return &boomRender{} }, "[HELP]"},
		{"ErrorBoundary fill", func() component.Component { return &boomBoundary{} }, "[BOUNDARY-FALLBACK]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := app.NewApp("t")
			shell, outlets := labShell()
			a.SetDefaultLayout(shell)
			a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}).
				Fill(outlets.Aside, tc.fill()), nil)
			logs := captureSlog(t)

			res, err := a.RenderPageResult(context.Background(), "/")
			if err != nil {
				t.Fatalf("a failing fill must be contained to its outlet, not fail the page: %v", err)
			}
			html := string(res.HTML)
			if !strings.Contains(html, "HOME") {
				t.Errorf("the page must keep the screen's content: %s", html)
			}
			if res.NotFoundOutlet {
				t.Errorf("a contained failure is not a decline; it must never become the 404-outlet outcome")
			}
			if !strings.Contains(html, tc.want) {
				t.Errorf("the outlet must degrade to %s: %s", tc.want, html)
			}
			if !strings.Contains(logs.String(), "app: fill failed; outlet degraded to fallback") {
				t.Errorf("the contained failure must be logged once, got: %q", logs.String())
			}
			assertNoHostileEcho(t, html, logs.String())
		})
	}
}

func TestTreeLayoutMarkers(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if got := strings.Count(s, "<main"); got != 1 {
		t.Fatalf("want exactly 1 <main>, got %d: %s", got, s)
	}
	if !strings.Contains(s, `<main data-cui-layout-slot="l:shell"`) {
		t.Errorf("primary must be today's content cell: %s", s)
	}
	if !strings.Contains(s, `data-cui-outlet="l:shell#toolbar"`) {
		t.Errorf("toolbar outlet marker missing: %s", s)
	}
	if !strings.Contains(s, `data-cui-outlet="l:shell#aside"`) {
		t.Errorf("aside outlet marker missing: %s", s)
	}
	if !strings.Contains(s, `data-cui-area="l:shell~crumbs"`) {
		t.Errorf("crumbs area marker missing: %s", s)
	}
	if !strings.Contains(s, "crumbs:/") {
		t.Errorf("route area must receive the match path: %s", s)
	}
}

// TestAreaLoadingTemplateRidesBesideCell: an AreaSpec.Loading
// renders its inert template BESIDE the area cell, addressed by the
// area's "~" address (2026-09-26, "Areas take loading content") —
// the same contract an outlet's Loading carries, so the loading
// module finds it by address. An area that declares no Loading
// emits no template (the busy dim only, as before).
func TestAreaLoadingTemplateRidesBesideCell(t *testing.T) {
	withLoading := app.NewLayout("shell", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "crumbs", Loading: &app.Loading{
			Show:  &stubComp{html: "TRAIL-SKELETON"},
			After: 150 * time.Millisecond,
		}}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
				return render.Text("crumbs:" + m.Path())
			}),
			l.Primary(),
		)
	})
	renderPage := func(shell *app.Layout) string {
		a := app.NewApp("t")
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
		res, err := a.RenderPageResult(context.Background(), "/")
		if err != nil {
			t.Fatal(err)
		}
		return string(res.HTML)
	}
	tpl := `<template data-cui-after="150" data-cui-loading="l:shell~crumbs" data-cui-min="0">TRAIL-SKELETON</template>`
	s := renderPage(withLoading)
	if !strings.Contains(s, tpl) {
		t.Errorf("the area's loading template must ride beside the cell, addressed by its ~ address:\n%s", s)
	}
	if i, j := strings.Index(s, `data-cui-area="l:shell~crumbs"`), strings.Index(s, tpl); j < i {
		t.Errorf("the loading template must follow the area cell:\n%s", s)
	}

	// Opt-out: no Loading declared, no template emitted.
	if plain := renderPage(func() *app.Layout { l, _ := labShell(); return l }()); strings.Contains(plain, "data-cui-loading") {
		t.Errorf("an area without a Loading declaration must emit no loading template:\n%s", plain)
	}
}

// TestInlineAreaRendersASpan: an Inline area's cell is a span, so a
// route area can sit inside phrasing content (a count in a nav link);
// an area that is not Inline keeps its div.
func TestInlineAreaRendersASpan(t *testing.T) {
	shell := app.NewLayout("shell", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "count", Inline: true}, {Name: "crumbs"}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.RouteArea("count", func(ctx context.Context, m app.Match) render.HTML { return render.Text("12") }),
			l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML { return render.Text("trail") }),
			l.Primary(),
		)
	})
	a := app.NewApp("t")
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if !strings.Contains(s, `<span data-cui-area="l:shell~count">12</span>`) {
		t.Errorf("an Inline area must render a span cell:\n%s", s)
	}
	if !strings.Contains(s, `<div data-cui-area="l:shell~crumbs">trail</div>`) {
		t.Errorf("an area that is not Inline keeps its div cell:\n%s", s)
	}
}

func TestPartialFromResultExportsKeptLayerFills(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	msg := app.NewScreen("/inbox/1", &stubComp{html: "MSG"}).
		Fill(outlets.Toolbar, &fillComp{label: "ACTIONS"})
	a.RegisterScreen(msg, nil)
	a.RegisterScreen(app.NewScreen("/settings", &stubComp{html: "SET"}), nil)

	// Same chain, one shared layer: the kept layer 0's fills (both
	// outlets + the crumbs area) must be exported; the rendered payload
	// is the bare content below l:shell.
	res, err := a.RenderPartialFromResult(context.Background(), "/inbox/1", "/settings")
	if err != nil {
		t.Fatal(err)
	}
	if res.SwapLayer != "l:shell" {
		t.Fatalf("SwapLayer = %q, want l:shell", res.SwapLayer)
	}
	addrs := map[string]bool{}
	for _, f := range res.Fills {
		addrs[f.Addr] = true
	}
	for _, want := range []string{"l:shell#toolbar", "l:shell#aside", "l:shell~crumbs"} {
		if !addrs[want] {
			t.Errorf("fill %q missing from partial result, got %v", want, addrs)
		}
	}
	if strings.Contains(string(res.HTML), "data-cui-outlet") {
		t.Errorf("bare content partial must carry no outlet markup: %s", res.HTML)
	}
	// The exported area fill carries the DESTINATION path.
	for _, f := range res.Fills {
		if f.Addr == "l:shell~crumbs" && !strings.Contains(string(f.HTML), "crumbs:/inbox/1") {
			t.Errorf("kept layer's area must re-run for the destination, got %q", f.HTML)
		}
	}
}
