package app_test

// PROTOTYPE (spike/layout-resolve): unit tests for transitions picked
// from the route resolution — the mount refusals, the per-render pick
// on the placed cell, and the page answer's pick.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// pickShell builds a shell whose primary declares a keyed set picked
// by TransitionFor.
func pickShell(set map[string]app.Transition, tf func(ctx context.Context) string) *app.Layout {
	return app.NewLayout("shell", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transitions: set, TransitionFor: tf},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
}

// TestTransitionNamesRefused pins the mount checks: a direction name
// (forward/back/reload) in a keyed set is refused, setting the static
// Transition beside the keyed form is refused, and TransitionFor with
// an empty set has nothing to pick.
func TestTransitionNamesRefused(t *testing.T) {
	mustPanic := func(t *testing.T, what string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Errorf("%s must be refused at mount", what)
			}
		}()
		fn()
	}
	for _, name := range []string{"forward", "back", "reload"} {
		mustPanic(t, "direction name "+name, func() {
			pickShell(map[string]app.Transition{name: app.Crossfade(time.Millisecond)},
				func(ctx context.Context) string { return name })
		})
	}
	mustPanic(t, "both forms set", func() {
		app.NewLayout("l", app.LayoutSpec{
			Primary: app.PrimaryConfig{
				Transition:  app.Crossfade(time.Millisecond),
				Transitions: map[string]app.Transition{"fade": app.Crossfade(time.Millisecond)},
				TransitionFor: func(ctx context.Context) string {
					return "fade"
				},
			},
		}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	})
	mustPanic(t, "TransitionFor without a set", func() {
		pickShell(nil, func(ctx context.Context) string { return "x" })
	})
}

// TestTransitionPickedPerRender pins the pick on the placed cell: the
// primary carries the picked entry's generated name (keyed by the
// pick), an unknown pick renders no name, and the CSS covers every
// entry in sorted key order whatever a render picks.
func TestTransitionPickedPerRender(t *testing.T) {
	set := map[string]app.Transition{
		"slide":  app.Slide(app.Right, 120*time.Millisecond),
		"fade":   app.Crossfade(90 * time.Millisecond),
		"softer": app.FadeThrough(90 * time.Millisecond),
	}
	render := func(pick string) string {
		a := app.NewApp("t")
		shell := pickShell(set, func(ctx context.Context) string { return pick })
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
		res, err := a.RenderPageResult(context.Background(), "/")
		if err != nil {
			t.Fatal(err)
		}
		return string(res.HTML)
	}
	if s := render("slide"); !strings.Contains(s, `data-fui-vt="vt-shell-primary-slide"`) {
		t.Errorf("the picked entry's keyed name must mark the cell: %s", s)
	}
	if s := render("fade"); !strings.Contains(s, `data-fui-vt="vt-shell-primary-fade"`) {
		t.Errorf("another pick carries its own keyed name: %s", s)
	}
	if s := render("nosuch"); strings.Contains(s, "data-fui-vt=") {
		t.Errorf("an unknown pick transitions nothing: %s", s)
	}
	// The stylesheet is pick-independent and sorted: slide < softer is
	// the alphabetical order of the generated rule sequence.
	css := pickShell(set, nil).TransitionCSS()
	if css == "" {
		t.Fatal("TransitionCSS must cover the keyed set")
	}
	if i, j := strings.Index(css, "vt-shell-primary-slide"), strings.Index(css, "vt-shell-primary-softer"); i < 0 || j < 0 || i > j {
		t.Errorf("CSS generation must be sorted by key (slide before softer):\n%s", css)
	}
	if !strings.Contains(css, "vt-shell-primary-fade") {
		t.Errorf("CSS covers every declared entry:\n%s", css)
	}
}

// TestTransitionPickCarriedOnPartialAnswer pins the wire: a partial's
// answer carries the picked name in X-Gofastr-Transition and the
// document declares its vocabulary (data-fui-vt-kinds, sorted) both on
// <html> at first paint and on the swapped payload's root layer.
func TestTransitionPickCarriedOnPartialAnswer(t *testing.T) {
	a := app.NewApp("t")
	shell := pickShell(map[string]app.Transition{
		"slide": app.Slide(app.Right, 120*time.Millisecond),
		"fade":  app.Crossfade(90 * time.Millisecond),
	}, func(ctx context.Context) string {
		m, _ := app.MatchFromContext(ctx)
		if strings.HasSuffix(m.Path(), "b") {
			return "fade"
		}
		return "slide"
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/a", &stubComp{html: "A"}), nil)
	a.RegisterScreen(app.NewScreen("/b", &stubComp{html: "B"}), nil)

	res, err := a.RenderPageResult(context.Background(), "/a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.HTML), `data-fui-vt-kinds="fade slide"`) {
		t.Errorf("the document declares its sorted vocabulary on <html>: %s", res.HTML)
	}

	pres, err := a.RenderPartialFromResult(context.Background(), "/b", "/a")
	if err != nil {
		t.Fatal(err)
	}
	if pres.Transition != "fade" {
		t.Fatalf("the partial carries the DESTINATION's pick, got %q", pres.Transition)
	}
	// The vocabulary's carrier is the doc shell: <html> at first paint
	// (asserted above) and the root layer of any payload that RENDERS
	// one. This deepest-swap payload is bare content (the whole chain
	// is shared, nothing re-renders), so the live document keeps the
	// kinds its kept shell declared — nothing to re-assert here.
}

// TestTransitionInnermostLayerWins pins the override rule when several
// chain layers declare a keyed primary set: the innermost layer's pick
// is the page's.
func TestTransitionInnermostLayerWins(t *testing.T) {
	a := app.NewApp("t")
	outer := pickShell(map[string]app.Transition{
		"slide": app.Slide(app.Right, 120*time.Millisecond),
	}, func(ctx context.Context) string { return "slide" })
	inner := pickShell(map[string]app.Transition{
		"fade": app.Crossfade(90 * time.Millisecond),
	}, func(ctx context.Context) string { return "fade" })
	a.SetDefaultLayout(outer)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "ROOT"}), nil)
	g := app.NewScreenGroup("/in", inner)
	g.Screen(app.NewScreen("/in", &stubComp{html: "IN"}), nil)
	a.Router.ScreenGroup(g)
	pres, err := a.RenderPartialFromResult(context.Background(), "/in", "/")
	if err != nil {
		t.Fatal(err)
	}
	if pres.Transition != "fade" {
		t.Fatalf("the innermost layer's pick wins, got %q", pres.Transition)
	}
}

// The X-Gofastr-Transition wire test lives in framework/uihost
// (transition_pick_test.go there): it needs the host's partial writer.
