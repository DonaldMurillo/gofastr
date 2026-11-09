package embed

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// A surface may render a dynamic screen: its frame URL names the concrete
// path, and uihost renders it once it resolves to the surface's screen.
func TestEmbedAcceptsDynamicRoutes(t *testing.T) {
	for _, route := range []string{"/reports/:id", "/reports/:id:int", "/teams/{team}/summary", "/catalog/{slug:string}", "/files/{p...}"} {
		t.Run(route, func(t *testing.T) {
			host, err := New(Config{
				Surfaces: []Surface{{
					Name:    "customer-summary",
					Screen:  testScreen{route},
					Origins: []string{"https://customer.example"},
				}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err != nil {
				t.Fatalf("New refused dynamic screen route %q: %v", route, err)
			}
			s, _ := host.Lookup("customer-summary")
			if !s.Dynamic() {
				t.Errorf("surface on %q reports Dynamic() = false", route)
			}
		})
	}
	for _, route := range []string{"/reports/monthly", "/teams/overview/", "/catalog/customer-summary"} {
		t.Run(route, func(t *testing.T) {
			host, err := New(Config{
				Surfaces:  []Surface{{Name: "summary", Screen: testScreen{route}, Origins: []string{"https://customer.example"}}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err != nil {
				t.Fatalf("New refused static screen route: %v", err)
			}
			if s, _ := host.Lookup("summary"); s.Dynamic() {
				t.Errorf("surface on %q reports Dynamic() = true", route)
			}
		})
	}
}

// A param segment matches any segment, so a route whose params could
// spell a reserved prefix covers it, and boot refuses it the way it
// refuses a static route under one.
func TestDynamicRouteCannotCoverReserved(t *testing.T) {
	for _, route := range []string{"/:section/summary", "/{section}", "/{rest...}", "/auth/:id"} {
		t.Run(route, func(t *testing.T) {
			_, err := New(Config{
				Surfaces:  []Surface{{Name: "summary", Screen: testScreen{route}, Origins: []string{"https://customer.example"}}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err == nil || !strings.Contains(err.Error(), "framework mounts itself") {
				t.Fatalf("New(%q) = %v, want the reserved-prefix refusal", route, err)
			}
		})
	}
	host, err := New(Config{
		Surfaces:  []Surface{{Name: "summary", Screen: testScreen{"/reports/:id"}, Origins: []string{"https://customer.example"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AddReservedPrefixes("/reports/admin"); err == nil {
		t.Error("AddReservedPrefixes(/reports/admin) accepted a surface on /reports/:id, which reaches it")
	}
	// "admin" is no int, so a /reports/:id:int surface never reaches it.
	host, err = New(Config{
		Surfaces:  []Surface{{Name: "summary", Screen: testScreen{"/reports/:id:int"}, Origins: []string{"https://customer.example"}}},
		BurnStore: NewMemoryBurnStore(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.AddReservedPrefixes("/reports/admin"); err != nil {
		t.Errorf("AddReservedPrefixes(/reports/admin) beside /reports/:id:int: %v, want accepted", err)
	}
}

// A grant for a dynamic surface reaches every path its pattern matches,
// and what lies beneath one, segment by segment: a param matches one
// non-empty segment, a catch-all one or more.
func TestDynamicSurfaceReach(t *testing.T) {
	cases := map[string]struct{ in, out []string }{
		"/reports/:id": {
			in: []string{"/reports/42", "/reports/42/export", "/reports/abc"},
			// MayReach cleans first: "/reports//export" is "/reports/export",
			// an id the pattern matches. routeWithin's own empty-segment
			// refusal is TestRouteWithinRefusesEmptySegment's.
			out: []string{"/reports", "/reports/", "/reports-x/42", "/other/42"},
		},
		"/teams/{team}/summary": {
			in:  []string{"/teams/red/summary", "/teams/red/summary/rows"},
			out: []string{"/teams/red", "/teams/red/other", "/teams//summary"},
		},
		// A constraint narrows reach as it narrows the route: a value the
		// surface's route refuses is another route's path.
		"/reports/:id:int": {
			in:  []string{"/reports/42", "/reports/42/export"},
			out: []string{"/reports/abc", "/reports/abc/export", "/reports/4a"},
		},
		"/teams/{team:uuid}": {
			in:  []string{"/teams/0f8fad5b-d9cb-469f-a165-70867728950e"},
			out: []string{"/teams/red", "/teams/red/x"},
		},
		"/files/{p...}": {
			in:  []string{"/files/a", "/files/a/b/c"},
			out: []string{"/files", "/files/", "/filesx/a"},
		},
	}
	for route, c := range cases {
		host, err := New(Config{
			Surfaces:  []Surface{{Name: "s", Screen: testScreen{route}, Origins: []string{"https://customer.example"}}},
			BurnStore: NewMemoryBurnStore(),
		})
		if err != nil {
			t.Fatalf("New(%q): %v", route, err)
		}
		s, _ := host.Lookup("s")
		for _, p := range c.in {
			if !s.MayReach(p) {
				t.Errorf("surface %q: MayReach(%q) = false, want true", route, p)
			}
		}
		for _, p := range c.out {
			if s.MayReach(p) {
				t.Errorf("surface %q: MayReach(%q) = true, want false", route, p)
			}
		}
	}
}

// A param segment matches one non-empty segment, a catch-all a non-empty
// rest: an unclean path does not satisfy either with an empty one.
func TestRouteWithinRefusesEmptySegment(t *testing.T) {
	for pattern, p := range map[string]string{
		"/reports/:id":        "/reports//export",
		"/teams/{team}/x":     "/teams//x",
		"/files/{p...}":       "/files/",
		"/reports/:id/export": "/reports/42",
	} {
		if routeWithin(pattern, p) {
			t.Errorf("routeWithin(%q, %q) = true, want false", pattern, p)
		}
	}
}

// uihost serves a dynamic surface's shell at /__gofastr/embed/<name>/<path>
// and its content at /__gofastr/embed/<name>/content, so no surface may
// render the app path "/content". One whose first segment admits
// "content" also admits "mcp", which is reserved, so New refuses it; the
// ones New accepts refuse "content".
func TestNoSurfaceRendersSlashContent(t *testing.T) {
	for _, route := range []string{"/:slug", "/:slug:alpha", "/:c:alnum", "/{p...}", "/:p*", "/:id:int", "/:id:uuid"} {
		host, err := New(Config{
			Surfaces:  []Surface{{Name: "s", Screen: testScreen{route}, Origins: []string{"https://customer.example"}}},
			BurnStore: NewMemoryBurnStore(),
		})
		if err != nil {
			continue
		}
		s, _ := host.Lookup("s")
		if routeWithin(s.Path(), "/content") {
			t.Errorf("New(%q) accepted a surface that renders /content, the content endpoint's path", route)
		}
	}
}

// routesOverlap is symmetric: either side may be the pattern, and a
// constraint keeps a literal it refuses out of the overlap.
func TestRoutesOverlapEitherWay(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"/reports/:id:int", "/reports/admin", false},
		{"/reports/admin", "/reports/:id:int", false},
		{"/reports/:id:int", "/reports/7", true},
		{"/reports/7", "/reports/:id:int", true},
		{"/reports/:id", "/reports/admin", true},
		{"/reports/:id:int", "/reports/:slug:alpha", true},
	} {
		if got := routesOverlap(c.a, c.b); got != c.want {
			t.Errorf("routesOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// The router decides what is dynamic: only a whole ":name" or "{name}"
// segment captures. A literal brace or colon inside a segment is a
// static path the router matches exactly, and embed must treat it so.
func TestEmbedAcceptsLiteralBraceRoutes(t *testing.T) {
	for _, route := range []string{"/reports/rev{1}", "/reports/{}", "/reports/a:b"} {
		t.Run(route, func(t *testing.T) {
			application := app.NewApp("literal")
			screen := app.NewScreen(route, app.NewContextComponent(func(context.Context) render.HTML { return "static" }))
			application.RegisterScreen(screen, nil)
			got, params, ok := application.Router.Resolve(route)
			if !ok || got != screen || len(params) != 0 {
				t.Fatalf("router does not treat %q as static: ok=%v params=%v", route, ok, params)
			}
			if _, _, ok := application.Router.Resolve("/reports/other"); ok {
				t.Fatalf("router matched /reports/other against %q", route)
			}
			host, err := New(Config{
				Surfaces:  []Surface{{Name: "literal", Screen: screen, Origins: []string{"https://customer.example"}}},
				BurnStore: NewMemoryBurnStore(),
			})
			if err != nil {
				t.Fatalf("New refused static screen route: %v", err)
			}
			if s, _ := host.Lookup("literal"); s.Dynamic() || s.MayReach("/reports/other") {
				t.Errorf("literal route %q treated as a pattern: Dynamic=%v", route, s.Dynamic())
			}
		})
	}
}
