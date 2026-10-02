package app_test

// The 404 outlet, end to end (docs/DESIGN-layout-outlets.md "404
// outlet", Decided 5): an outlet declared FallbackNotFound that no
// candidate fills makes the render outcome not-found — the primary
// becomes the not-found body through the screen pipeline, every other
// outlet renders its Default or nothing, the title is the not-found
// body's, and the host reads RenderResult.NotFoundOutlet to answer 404.
// ErrNoFill declines are the only thing that decides it; a contained
// failure never does.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// nfShell is the canonical 404-outlet shell: a `gone` outlet with
// FallbackNotFound (no Default — NewLayout refuses the pair) and an
// `aside` outlet whose Default renders [HELP]. The gone handle comes
// back with the layout for the declining-fill tests.
func nfShell() (*app.Layout, *app.Outlet) {
	gone := app.NewOutlet("gone", app.OutletOptions{Fallback: app.FallbackNotFound})
	aside := app.NewOutlet("aside", app.OutletOptions{Default: &fillComp{label: "HELP"}})
	return app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{gone, aside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.Text("HEADER "),
			l.Place(gone),
			l.Primary(),
			l.Place(aside),
		)
	}), gone
}

var errNFBoom = errors.New("nf: fill exploded")

// TestNotFoundOutletOutcome pins the full-page outcome: the status
// flag, the not-found body in the primary, the NotFound title, the
// other outlets rebuilt from their Defaults, the screen's own content
// and title gone. The ErrNoFill decline and the plain absence of a
// fill both decide it; a contained failure (variant B) never does.
func TestNotFoundOutletOutcome(t *testing.T) {
	t.Run("unfilled outlet is the not-found page", func(t *testing.T) {
		a := app.NewApp("t")
		shell, _ := nfShell()
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/x", &stubComp{html: "SCREEN-BODY"}).WithTitle("Screen title"), nil)

		res, err := a.RenderPageResult(context.Background(), "/x")
		if err != nil {
			t.Fatal(err)
		}
		if !res.NotFoundOutlet {
			t.Fatal("the unfilled FallbackNotFound outlet must set the not-found outcome")
		}
		s := string(res.HTML)
		if !strings.Contains(s, "404: Page not found") {
			t.Errorf("the primary must be the not-found body:\n%s", s)
		}
		if strings.Contains(s, "SCREEN-BODY") {
			t.Errorf("the screen's own content must not ship:\n%s", s)
		}
		if !strings.Contains(s, "[HELP]") {
			t.Errorf("every other outlet renders its Default:\n%s", s)
		}
		if !strings.Contains(s, `<div data-fui-outlet="l:shell#gone"></div>`) {
			t.Errorf("the 404 outlet itself renders empty:\n%s", s)
		}
		if !strings.Contains(s, "<title>Page not found — t</title>") {
			t.Errorf("the title is the not-found body's, not the screen's:\n%s", s)
		}
		if res.Title != "Page not found" {
			t.Errorf("res.Title = %q, want the not-found title", res.Title)
		}
	})

	t.Run("a declining fill decides it too", func(t *testing.T) {
		a := app.NewApp("t")
		shell, gone := nfShell()
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/x", &stubComp{html: "SCREEN-BODY"}).
			Fill(gone, &fillComp{label: "DECLINED", loadErr: app.ErrNoFill}), nil)

		res, err := a.RenderPageResult(context.Background(), "/x")
		if err != nil {
			t.Fatal(err)
		}
		if !res.NotFoundOutlet || !strings.Contains(string(res.HTML), "404: Page not found") {
			t.Errorf("an ErrNoFill decline must produce the not-found outcome:\n%s", res.HTML)
		}
	})

	t.Run("a contained failure is not a decline", func(t *testing.T) {
		a := app.NewApp("t")
		shell, gone := nfShell()
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/x", &stubComp{html: "SCREEN-BODY"}).
			Fill(gone, &fillComp{label: "BROKEN", loadErr: errNFBoom}), nil)

		res, err := a.RenderPageResult(context.Background(), "/x")
		if err != nil {
			t.Fatal(err)
		}
		if res.NotFoundOutlet {
			t.Error("a contained fill failure must never turn the page into a 404")
		}
		if !strings.Contains(string(res.HTML), "SCREEN-BODY") {
			t.Errorf("the page keeps the screen's content under containment:\n%s", res.HTML)
		}
	})
}

// TestValidateRefusesNotFoundWithDefault pins the mount validation:
// FallbackNotFound beside a Default is refused, naming the layout and
// the outlet.
func TestValidateRefusesNotFoundWithDefault(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("FallbackNotFound beside a Default must panic at mount")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "shell") || !strings.Contains(msg, "gone") {
			t.Fatalf("panic must name the layout and the outlet, got: %v", r)
		}
	}()
	app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{app.NewOutlet("gone", app.OutletOptions{
			Default:  &fillComp{label: "D"},
			Fallback: app.FallbackNotFound,
		})},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })

	t.Fatal("unreachable")
}

// TestOutlet404PartRequestIsWholePageReset pins the part lane: a part
// whose own resolution is the 404-outlet outcome is a WHOLE-PAGE
// outcome (the page answers the not-found page, so the part applies
// nothing) — the 409 reset. A part whose outlet resolves stays applied.
func TestOutlet404PartRequestIsWholePageReset(t *testing.T) {
	a := app.NewApp("t")
	slow := app.NewOutlet("slow", app.OutletOptions{
		Deferred: true,
		Fallback: app.FallbackNotFound,
		Loading:  &app.Loading{Show: &fillComp{label: "LOADING"}},
	})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{slow},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(l.Place(slow), l.Primary())
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/gone", &stubComp{html: "GONE"}), nil)
	a.RegisterScreen(app.NewScreen("/ok", &stubComp{html: "OK"}).
		Fill(slow, &fillComp{label: "SLOW"}), nil)

	if _, outcome := a.RenderPartResult(context.Background(), "/gone", "l:shell#slow"); outcome != app.PartReset {
		t.Errorf("part for a 404-outlet route = %v, want PartReset", outcome)
	}
	fill, outcome := a.RenderPartResult(context.Background(), "/ok", "l:shell#slow")
	if outcome != app.PartApplied {
		t.Errorf("part for the filled outlet = %v, want PartApplied", outcome)
	}
	if !strings.Contains(string(fill.HTML), "[SLOW]") {
		t.Errorf("the applied part carries the fill, got %q", fill.HTML)
	}
}
