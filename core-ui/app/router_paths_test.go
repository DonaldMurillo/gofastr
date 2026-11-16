package app

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestPathsAreSorted: Paths feeds the route manifest every page embeds
// and the static export's page order, so two builds of one app must
// list the same paths in the same order.
func TestPathsAreSorted(t *testing.T) {
	r := NewRouter()
	comp := &stubComponent{html: render.Raw("<p>x</p>")}
	for _, p := range []string{"/zeta", "/alpha", "/mid", "/b/:id", "/beta", "/a/:slug", "/omega", "/delta"} {
		r.Screen(NewScreen(p, comp), nil)
	}
	want := []string{"/alpha", "/beta", "/delta", "/mid", "/omega", "/zeta", "/b/:id", "/a/:slug"}
	for range 20 {
		if got := r.Paths(); !slices.Equal(got, want) {
			t.Fatalf("Paths = %q, want %q (exact paths sorted, then patterns in registration order)", got, want)
		}
	}
}

func TestEveryPathFindsItsScreenByPattern(t *testing.T) {
	r := NewRouter()
	for _, p := range []string{"/about", "/docs/", "/items/{slug}/", "/teams/:team/", "/files/{rest...}"} {
		r.Screen(NewScreen(p, &matchParamComp{}), nil)
	}
	for _, p := range r.Paths() {
		if s, ok := r.ScreenByPattern(p); !ok || s == nil {
			t.Errorf("ScreenByPattern(%q) found no screen, though Paths lists it", p)
		}
	}
	if _, ok := r.ScreenByPattern("/items/{slug}/"); !ok {
		t.Error("ScreenByPattern missed a pattern spelled as it was registered")
	}
	r.ValidateIntercepts()
}

func TestTrailingSlashOnADynamicPatternIsIgnored(t *testing.T) {
	for _, pair := range [][2]string{{"/items/{id}/", "/items/{id}"}, {"/items/{id}", "/items/:id/"}} {
		application := NewApp("intercept")
		application.Register(pair[0], &ixDetail{}, nil)
		screen := NewScreen("/panel", &ixList{})
		screen.Intercept = &Intercept{From: pair[1], As: ScreenSheet}
		application.Router.Screen(screen, nil)
		application.Router.ValidateIntercepts()
		if _, ok := application.Router.InterceptFor(screen.Path, "/items/7"); !ok {
			t.Errorf("screen %q with intercept origin %q: overlay did not open", pair[0], pair[1])
		}
		if !slices.ContainsFunc(application.Routes(), func(e RouteEntry) bool { return e.Path == "/items/:id" }) {
			t.Errorf("screen %q is missing from the route manifest", pair[0])
		}
	}
}
