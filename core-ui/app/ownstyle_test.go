package app_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Owned sheets register at init, like the generated files that build
// them. ownedHandle is the shape `gofastr gen styles` writes: a struct
// embedding *ownstyle.Sheet, so OwnedSheet is promoted onto it.
type ownedHandle struct{ *ownstyle.Sheet }

var (
	boardStyle  = ownedHandle{ownstyle.Must("apptest-board", ownstyle.KindScoped, `.col { gap: 1px; }`)}
	reviewStyle = ownstyle.Must("apptest-review", ownstyle.KindScoped, `:scope { gap: 1px; }`)
	appStyle    = ownstyle.Must("app", ownstyle.KindApp, `.x { gap: 1px; }`)
)

func renderPath(t *testing.T, a *app.App, path string) string {
	t.Helper()
	res, err := a.RenderPageResult(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	return string(res.HTML)
}

func TestLayoutStyleStampsLayoutRoot(t *testing.T) {
	a := app.NewApp("t")
	l := app.NewLayout("review", app.LayoutSpec{Style: reviewStyle}, func(_ context.Context, t *app.LayoutTree) render.HTML {
		return t.Primary()
	})
	a.RegisterScreen(app.NewScreen("/r", &stubComp{html: "R"}), l)
	got := renderPath(t, a, "/r")
	if !regexp.MustCompile(`<div class="layout-review"[^>]* data-cui-scope="apptest-review"`).MatchString(got) {
		t.Fatalf("the layout root must carry the layout's scope:\n%s", got)
	}
}

func TestScreenStyleWrapsContentInOneDiv(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/b", &stubComp{html: "BOARD"}).WithStyle(boardStyle), nil)
	got := renderPath(t, a, "/b")
	if !strings.Contains(got, `<div data-cui-scope="apptest-board">BOARD</div>`) {
		t.Fatalf("a non-article screen's content must sit in one div carrying its scope:\n%s", got)
	}
	if n := strings.Count(got, "data-cui-scope="); n != 1 {
		t.Fatalf("want exactly one scope marker (the wrapper, never the primary cell), got %d:\n%s", n, got)
	}
	if regexp.MustCompile(`<(main|div class="layout-content")[^>]*data-cui-scope`).MatchString(got) {
		t.Fatalf("the primary cell persists across navigations and must never be the screen's scope root:\n%s", got)
	}
}

func TestScreenStyleGoesOnTheArticle(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	story := app.NewScreen("/a", &stubComp{html: "STORY"}).WithStyle(boardStyle)
	app.AsArticle()(story)
	a.RegisterScreen(story, nil)
	got := renderPath(t, a, "/a")
	if !strings.Contains(got, `<article data-cui-scope="apptest-board">STORY</article>`) {
		t.Fatalf("an article screen's scope goes on its <article>, with no extra div:\n%s", got)
	}
}

func TestScreenWithoutStyleIsNotWrapped(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/p", &stubComp{html: "PLAIN"}), nil)
	if got := renderPath(t, a, "/p"); strings.Contains(got, "data-cui-scope") || strings.Contains(got, "<div>PLAIN</div>") {
		t.Fatalf("a screen with no style renders exactly as before:\n%s", got)
	}
}

// A screen's style never reaches its fills: they render in the
// layout's outlets, outside the screen's wrapper.
func TestFillsSitOutsideTheScreenWrapper(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/f", &stubComp{html: "MAIN"}).
		WithStyle(boardStyle).
		Fill(outlets.Toolbar, &fillComp{label: "TOOLS"}), nil)
	got := renderPath(t, a, "/f")
	wrapper := regexp.MustCompile(`<div data-cui-scope="apptest-board">(.*?)</div>`).FindStringSubmatch(got)
	if wrapper == nil {
		t.Fatalf("no screen wrapper:\n%s", got)
	}
	if wrapper[1] != "MAIN" {
		t.Fatalf("the wrapper must hold the screen's content only, got %q", wrapper[1])
	}
	if !strings.Contains(got, "[TOOLS]") {
		t.Fatalf("the fill must render:\n%s", got)
	}
}

func TestOwnerKindChecksPanic(t *testing.T) {
	build := func(_ context.Context, t *app.LayoutTree) render.HTML { return t.Primary() }
	cases := []struct {
		name, want string
		fn         func()
	}{
		{"layout given the app style", `app: layout "x" Style: the app style covers every page`,
			func() { app.NewLayout("x", app.LayoutSpec{Style: appStyle}, build) }},
		{"screen given the app style", `app: screen "/s" WithStyle: the app style covers every page`,
			func() { app.NewScreen("/s", &stubComp{}).WithStyle(appStyle) }},
		{"app given a scoped style", `app: App.WithStyle: "apptest-review" is a scoped style`,
			func() { app.NewApp("t").WithStyle(reviewStyle) }},
		{"screen given nil", `app: screen "/s" WithStyle: nil style`,
			func() { app.NewScreen("/s", &stubComp{}).WithStyle(nil) }},
		{"app given a handle holding no sheet", `app: App.WithStyle: the style handle holds no sheet`,
			func() { app.NewApp("t").WithStyle(ownedHandle{}) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := panicOf(c.fn); !strings.HasPrefix(got, c.want) {
				t.Fatalf("panic = %q, want prefix %q", got, c.want)
			}
		})
	}
	if got := panicOf(func() { app.NewApp("t").WithStyle(appStyle) }); got != "" {
		t.Fatalf("the app style on the app must not panic: %s", got)
	}
}
