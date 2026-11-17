package uihost

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type transitionStub struct{}

func (transitionStub) Render() render.HTML { return render.Text("page") }

// A layout's typed transition is its own component sheet, loaded only
// by the pages that render the layer: app.css never carries it, the
// layer root names the sheet, the sheet holds each rule once however
// many groups share the layout, and a page outside the layer does not
// name it.
func TestLayoutTransitionsLoadWithTheLayer(t *testing.T) {
	build := func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	items := app.NewLayout("items", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 220*time.Millisecond)},
	}, build)
	sheet := items.TransitionSheet()
	if sheet == "" {
		t.Fatal("a layout declaring a transition has no transition sheet")
	}
	site := app.NewApp("transitions")
	site.SetDefaultLayout(app.NewLayout("shell", app.LayoutSpec{}, build))
	site.RegisterScreen(app.NewScreen("/", transitionStub{}), nil)
	for _, prefix := range []string{"/a", "/b"} {
		g := app.NewScreenGroup(prefix, items)
		g.Screen(app.NewScreen(prefix+"/list", transitionStub{}), nil)
		site.Router.ScreenGroup(g)
	}
	ds := New(site)

	rule := minify.ServedCSS("::view-transition-new(vt-items-primary) { animation: vt-items-primary-in 220ms var(--easing-ease-in-out, ease) both; }")
	if css, _ := ds.appCSSCached(); strings.Contains(css, "vt-items-primary") {
		t.Errorf("app.css carries the items transition, which every page would then pay for:\n%s", css)
	}
	if n := strings.Count(ds.ComponentCSSFiles()["/__gofastr/comp/"+sheet+".css"], rule); n != 1 {
		t.Errorf("the transition sheet holds the items rule %d times, want 1", n)
	}
	marker := `data-cui-comp="` + sheet + `"`
	page := func(path string) string {
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	if body := page("/a/list"); !strings.Contains(body, marker) {
		t.Errorf("the items layer root does not name its transition sheet (%s)", marker)
	}
	if body := page("/"); strings.Contains(body, sheet) {
		t.Error("a page outside the items layer names its transition sheet")
	}
}

// A layout built after the component catalog froze cannot register its
// transition sheet, so the host names it once instead of failing.
func TestLateTransitionLayoutWarns(t *testing.T) {
	registry.IsolateForTest(t)
	build := func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	site := app.NewApp("late")
	site.RegisterScreen(app.NewScreen("/", transitionStub{}), nil)
	ds := New(site)
	before, _ := ds.appCSSCached()
	registry.Freeze()

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	late := app.NewLayout("late", app.LayoutSpec{
		Primary: app.PrimaryConfig{Transition: app.Slide(app.Right, 200*time.Millisecond)},
	}, build)
	if late.TransitionSheet() != "" {
		t.Fatal("a layout built after the freeze registered a transition sheet")
	}
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
