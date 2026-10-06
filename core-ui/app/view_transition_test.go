package app_test

// PROTOTYPE (spike/layout-motion, P11-B): unit tests for the typed
// transition spec (Transition / Anim), the data-cui-vt markers it
// renders, the CSS Layout.TransitionCSS generates, and the root
// presets.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestTypedTransitionMarkersRender(t *testing.T) {
	a := app.NewApp("t")
	toolbar := app.NewOutlet("toolbar", app.OutletOptions{Transition: app.Crossfade(150 * time.Millisecond)})
	aside := app.NewOutlet("aside", app.OutletOptions{Default: &fillComp{label: "HELP"}})
	shell := app.NewLayout("shell", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 220*time.Millisecond)},
		Outlets: []*app.Outlet{toolbar, aside},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.Place(toolbar),
			l.Primary(),
			l.Place(aside),
		)
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	for _, want := range []string{
		`data-cui-vt="vt-shell-primary"`,  // generated from layout+slot
		`data-cui-vt="vt-shell-toolbar"`,  // generated for the outlet
		`data-cui-outlet="l:shell#aside"`, // unnamed outlet carries no marker
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered page missing %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, `data-cui-outlet="l:shell#aside" data-cui-vt`) {
		t.Errorf("unnamed aside outlet must not carry a vt marker:\n%s", s)
	}
}

func TestTypedTransitionRawNameEscapeHatch(t *testing.T) {
	a := app.NewApp("t")
	shell := app.NewLayout("shell", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Transition{Name: "detail"}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return l.Primary()
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(res.HTML); !strings.Contains(s, `data-cui-vt="detail"`) {
		t.Errorf("raw Name must land verbatim in the marker:\n%s", s)
	}
	// Name-only generates exactly the assignment rule; the author's CSS
	// owns the animation.
	css := shell.TransitionCSS()
	if !strings.Contains(css, `[data-cui-vt="detail"] { view-transition-name: detail; }`) {
		t.Errorf("Name-only CSS must assign the name and nothing else:\n%s", css)
	}
	if strings.Contains(css, "@keyframes") {
		t.Errorf("Name-only CSS must generate no keyframes:\n%s", css)
	}
}

func TestTransitionCSSGeneratesSlideAndBackVariant(t *testing.T) {
	l := app.NewLayout("items", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 220*time.Millisecond)},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	css := l.TransitionCSS()
	for _, want := range []string{
		`[data-cui-vt="vt-items-primary"] { view-transition-name: vt-items-primary; }`,
		`::view-transition-new(vt-items-primary) { animation: vt-items-primary-in 220ms var(--easing-ease-in-out, ease) both; }`,
		`@keyframes vt-items-primary-in { 0%, 50% { transform: translateX(var(--spacing-xl, 24px)); opacity: 0; } }`,
		`:root:active-view-transition-type(back) ::view-transition-new(vt-items-primary) { animation-name: vt-items-primary-in-back; }`,
		`@keyframes vt-items-primary-in-back { 0%, 50% { transform: translateX(calc(-1 * var(--spacing-xl, 24px))); opacity: 0; } }`,
		`::view-transition-old(vt-items-primary) { animation: vt-items-primary-out 220ms var(--easing-ease-in-out, ease) both; }`,
		// S2: the legs are sequential — the old snapshot is fully faded
		// by the midpoint, the new one holds opacity 0 until then.
		`@keyframes vt-items-primary-out { 50%, to { opacity: 0; } }`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("generated CSS missing %s:\n%s", want, css)
		}
	}
	// R1: snapshots draw in the top layer unclipped by the slot, so a
	// sliding region must never translate by more than the offset
	// token — a percentage of the region would cross into its
	// neighbours mid-transition. Prove the guard on the translateX
	// ARGUMENT (the keyframe offsets themselves are percentages, as
	// they must be): every argument is the 24px token, never a %.
	for _, line := range strings.Split(css, "\n") {
		parts := strings.Split(line, "translateX(")
		for _, arg := range parts[1:] {
			arg = arg[:strings.Index(arg, ")")]
			if strings.Contains(arg, "%") {
				t.Errorf("slide translate must be the token offset, not a percentage:\n%s", line)
			}
			if !strings.Contains(arg, "--spacing-xl") {
				t.Errorf("slide translate must ride the spacing-xl token:\n%s", line)
			}
		}
	}
	// Left mirrors to the other side.
	l2 := app.NewLayout("items", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Left, 220*time.Millisecond)},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	if css2 := l2.TransitionCSS(); !strings.Contains(css2, "translateX(calc(-1 * var(--spacing-xl, 24px)))") {
		t.Errorf("Slide(Left) must enter from the left:\n%s", css2)
	}
}

func TestTransitionCSSZeroAndOutlet(t *testing.T) {
	empty := app.NewLayout("e", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	if css := empty.TransitionCSS(); css != "" {
		t.Errorf("a spec with no transitions must generate no CSS, got:\n%s", css)
	}
	l := app.NewLayout("shell", app.LayoutSpec{
		Outlets: []*app.Outlet{app.NewOutlet("toolbar", app.OutletOptions{Transition: app.Crossfade(120 * time.Millisecond)})},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	css := l.TransitionCSS()
	if !strings.Contains(css, `[data-cui-vt="vt-shell-toolbar"]`) || !strings.Contains(css, "@keyframes vt-shell-toolbar-in { from { opacity: 0; } }") {
		t.Errorf("outlet transition CSS wrong:\n%s", css)
	}
}

func TestNewLayoutRefusesBadTransitionName(t *testing.T) {
	for _, bad := range []string{"9lives", "two words", "none", "dot.name"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewLayout must panic on Transition.Name %q", bad)
				}
			}()
			app.NewLayout("l", app.LayoutSpec{
				Primary: app.PrimaryConfig{Transition: app.Transition{Name: bad}},
			}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return "" })
		}()
	}
}

func TestViewTransitionPresets(t *testing.T) {
	for _, name := range []string{"fade", "slide", "none"} {
		css, err := app.ViewTransitionPresetCSS(name)
		if err != nil {
			t.Fatalf("preset %s: %v", name, err)
		}
		if !strings.Contains(css, "::view-transition-") {
			t.Errorf("preset %s carries no view-transition rule: %q", name, css)
		}
	}
	if _, err := app.ViewTransitionPresetCSS("slid"); err == nil {
		t.Error("a typo'd preset name must be an error, not a silent no-op")
	}
	css, _ := app.ViewTransitionPresetCSS("slide")
	if !strings.Contains(css, ":active-view-transition-type(back)") {
		t.Error("slide preset must ship a :active-view-transition-type(back) variant")
	}
}

// U2 (tracker round 4): a route area can declare its own transition —
// the area CELL persists across navigations (fills replace its
// innerHTML), so the marker rides the box both trails render into.
func TestAreaTransitionMarkerAndCSS(t *testing.T) {
	a := app.NewApp("t")
	shell := app.NewLayout("shell", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "crumbs", Transition: app.FadeThrough(160 * time.Millisecond)}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			l.RouteArea("crumbs", func(ctx context.Context, m app.Match) render.HTML {
				return render.Text("HOME")
			}),
			l.Primary(),
		)
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	// The marker rides the area cell, beside its address.
	if s := string(res.HTML); !strings.Contains(s, `data-cui-area="l:shell~crumbs" data-cui-vt="vt-shell-crumbs"`) {
		t.Errorf("area cell must carry its generated vt name:\n%s", s)
	}
	css := shell.TransitionCSS()
	for _, want := range []string{
		`[data-cui-vt="vt-shell-crumbs"] { view-transition-name: vt-shell-crumbs; }`,
		// FadeThrough: sequential legs, no nudge (transform must not
		// appear in either keyframe block).
		`@keyframes vt-shell-crumbs-in { 0%, 50% { opacity: 0; } }`,
		`@keyframes vt-shell-crumbs-out { 50%, to { opacity: 0; } }`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("area transition CSS missing %s:\n%s", want, css)
		}
	}
	if strings.Contains(css, "transform") {
		t.Errorf("FadeThrough must not nudge:\n%s", css)
	}
}

func TestNewLayoutRefusesBadAreaTransitionName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewLayout must panic on an area Transition.Name that is not a CSS custom-ident")
		}
	}()
	app.NewLayout("l", app.LayoutSpec{
		Areas: []app.AreaSpec{{Name: "crumbs", Transition: app.Transition{Name: "none"}}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return "" })
}

// U1 (tracker round 4): Narrow makes the primary's name
// breakpoint-conditional — the placed cell above it, the build's
// VTRegion element below (the master-detail collapse: one pane on a
// phone). The cell and the region must carry DIFFERENT when-conditions
// so exactly one owns the name at any width.
func TestNarrowRegionMarkersRender(t *testing.T) {
	a := app.NewApp("t")
	var region render.HTML
	shell := app.NewLayout("project", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Transition{
			Enter:  app.SlideFrom(app.Right, 220*time.Millisecond),
			Exit:   app.Fade(220 * time.Millisecond),
			Narrow: "920px",
		}},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		attrs := l.VTRegion()
		region = html.Div(html.DivConfig{Class: "panes", ExtraAttrs: attrs})
		return render.Join(region, l.Primary())
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	for _, want := range []string{
		// The region carries the name BELOW the breakpoint (attr values
		// arrive HTML-escaped; the browser reads them back unescaped).
		`data-cui-vt="vt-project-primary" data-cui-vt-when="(width &lt; 920px)"`,
		// The placed cell carries it AT and above.
		`data-cui-vt="vt-project-primary" data-cui-vt-when="(width &gt;= 920px)"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("SSR missing %s:\n%s", want, s)
		}
	}
	css := shell.TransitionCSS()
	for _, want := range []string{
		`@media (width >= 920px) { [data-cui-vt="vt-project-primary"][data-cui-vt-when="(width >= 920px)"] { view-transition-name: vt-project-primary; } }`,
		`@media (width < 920px) { [data-cui-vt="vt-project-primary"][data-cui-vt-when="(width < 920px)"] { view-transition-name: vt-project-primary; } }`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("Narrow CSS missing %s:\n%s", want, css)
		}
	}
	// No unconditional assignment: it would name BOTH elements at the
	// matching width and the browser would skip the whole transition.
	if strings.Contains(css, `[data-cui-vt="vt-project-primary"] {`) {
		t.Errorf("Narrow must not emit an unconditional name assignment:\n%s", css)
	}
}

// No Narrow declared: VTRegion is nil and the cell carries no
// when-attribute (the classic unconditional marker).
func TestVTRegionNilWithoutNarrow(t *testing.T) {
	a := app.NewApp("t")
	shell := app.NewLayout("project", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 220*time.Millisecond)},
	}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		if l.VTRegion() != nil {
			t.Error("VTRegion must be nil when the primary declares no Narrow")
		}
		return l.Primary()
	})
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "HOME"}), nil)
	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(res.HTML); strings.Contains(s, "data-cui-vt-when") {
		t.Errorf("an unconditional transition must carry no when-attribute:\n%s", s)
	}
}

func TestNewLayoutRefusesBadNarrowWidth(t *testing.T) {
	for _, bad := range []string{"920", "100%", "wide", "920 PX"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewLayout must panic on Transition.Narrow %q", bad)
				}
			}()
			app.NewLayout("l", app.LayoutSpec{
				Primary: app.PrimaryConfig{Transition: app.Transition{
					Enter:  app.Fade(time.Millisecond),
					Narrow: bad,
				}},
			}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return "" })
		}()
	}
}
