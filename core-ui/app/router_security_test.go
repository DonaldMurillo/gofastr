package app

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestResolveRejectsEmptyParamSegs pins that a dynamic route never binds
// an EMPTY path segment as a param value. matchDynamic splits the trimmed
// path and binds whatever text sits in a param position, so the bare root
// "/" resolves against a "/:slug" registration with slug="" (Trim leaves
// one empty segment) and an interior double slash ("/files//edit") binds
// an empty mid-param; the screen then runs Load with an empty key its
// author never declared reachable. The catch-all branch already requires
// at least one remainder segment ("/docs" must not match "/docs/{p...}");
// the fixed-length branch must hold the same line. Property: a param
// segment binds a non-empty path segment or the route does not match.
// Surfaces: bare root vs single-param route, interior empty segment vs
// multi-segment route, all-empty path.
func TestResolveRejectsEmptyParamSegs(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/:slug", &stubComponent{html: render.Raw("S")}), nil)
	r.Screen(NewScreen("/files/:name/edit", &stubComponent{html: render.Raw("F")}), nil)

	cases := []struct {
		name, path string
	}{
		{"root binds single-param route", "/"},
		{"interior empty segment", "/files//edit"},
		{"all empty segments", "//"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, params, ok := r.Resolve(tc.path)
			if !ok {
				return // route refused the path: property holds
			}
			for _, v := range params {
				if v == "" {
					t.Errorf("SECURITY: [route-empty-param] Resolve(%q) matched with an empty param value (%v) — the route serves a path shape its author never declared reachable", tc.path, params)
					return
				}
			}
		})
	}
}
