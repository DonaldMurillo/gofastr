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
