package app_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layouts is what the host walks to collect transition CSS: every
// layout a page can render in, each once, sorted by name.
func TestAppLayoutsEachOnceByName(t *testing.T) {
	build := func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	shell := app.NewLayout("shell", app.LayoutSpec{}, build)
	items := app.NewLayout("items", app.LayoutSpec{}, build)
	print := app.NewLayout("print", app.LayoutSpec{}, build)
	unused := app.NewLayout("unused", app.LayoutSpec{}, build)
	_ = unused

	a := app.NewApp("layouts")
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "home"}), nil)
	a.RegisterScreen(app.NewScreen("/print", &stubComp{html: "print"}), print)
	for _, prefix := range []string{"/a", "/b"} {
		g := app.NewScreenGroup(prefix, items)
		g.Screen(app.NewScreen(prefix+"/list", &stubComp{html: "item"}), nil)
		a.Router.ScreenGroup(g)
	}

	var got []string
	for _, l := range a.Layouts() {
		got = append(got, l.Name)
	}
	want := []string{"items", "print", "shell"}
	if len(got) != len(want) {
		t.Fatalf("Layouts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Layouts() = %v, want %v", got, want)
		}
	}
}

// LayoutsVersion moves on every registration, so the host can notice
// routes added after it froze app.css.
func TestAppLayoutsVersionMovesOnRegistration(t *testing.T) {
	a := app.NewApp("gen")
	v0 := a.LayoutsVersion()
	a.RegisterScreen(app.NewScreen("/", &stubComp{html: "home"}), nil)
	v1 := a.LayoutsVersion()
	a.SetDefaultLayout(app.NewLayout("shell", app.LayoutSpec{}, func(_ context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }))
	if v1 == v0 || a.LayoutsVersion() == v1 {
		t.Fatalf("version did not move: %d, %d, %d", v0, v1, a.LayoutsVersion())
	}
}
