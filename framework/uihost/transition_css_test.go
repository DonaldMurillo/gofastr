package uihost

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type transitionStub struct{}

func (transitionStub) Render() render.HTML { return render.Text("page") }

// A layout's typed transition reaches app.css with no WithCustomCSS:
// the host collects every layout's TransitionCSS, each layout once.
func TestAppCSSCollectsLayoutTransitions(t *testing.T) {
	build := func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	items := app.NewLayout("items", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 220*time.Millisecond)},
	}, build)
	site := app.NewApp("transitions")
	site.SetDefaultLayout(app.NewLayout("shell", app.LayoutSpec{}, build))
	site.RegisterScreen(app.NewScreen("/", transitionStub{}), nil)
	for _, prefix := range []string{"/a", "/b"} {
		g := app.NewScreenGroup(prefix, items)
		g.Screen(app.NewScreen(prefix+"/list", transitionStub{}), nil)
		site.Router.ScreenGroup(g)
	}

	css, _ := New(site).appCSSCached()
	rule := "::view-transition-new(vt-items-primary) { animation: vt-items-primary-in 220ms ease both; }"
	if n := strings.Count(css, rule); n != 1 {
		t.Fatalf("app.css holds the items transition rule %d times, want 1:\n%s", n, css)
	}
}

// app.css is frozen at first render. A layout with transitions that
// arrives later cannot ship them, so the host names it once instead of
// recomposing.
func TestLateTransitionLayoutWarns(t *testing.T) {
	build := func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	site := app.NewApp("late")
	site.RegisterScreen(app.NewScreen("/", transitionStub{}), nil)
	ds := New(site)
	before, _ := ds.appCSSCached()

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	late := app.NewLayout("late", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 200*time.Millisecond)},
	}, build)
	site.RegisterScreen(app.NewScreen("/late", transitionStub{}), late)
	after, _ := ds.appCSSCached()
	ds.appCSSCached()

	if after != before {
		t.Error("app.css was recomposed after the first render")
	}
	if n := strings.Count(logs.String(), "layout=late"); n != 1 {
		t.Errorf("want one warning naming the late layout, got %d:\n%s", n, logs.String())
	}
}
