package store

import (
	"strings"
	"testing"
)

// TestStoreRefusesRoutePrefix (DESIGN "Route signals"): the route.*
// prefix is reserved for the router's snapshot. A user-declared slice
// under it would race the seeded route values — the declaration's
// default stamping SSR paints while every navigation's seed merge
// overwrites it — so the generic New path panics. The route family's
// own declarations (Route.Path, Route.Param, Route.Query) keep
// working and stay idempotent: route.go's declareRoute owns the
// prefix.
func TestStoreRefusesRoutePrefix(t *testing.T) {
	for _, name := range []string{"route.path", "route.custom", "route.params.id"} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("New(\"\").String(%q) must panic — the full key is under route.*, reserved for the router's snapshot", name)
					return
				}
				msg, ok := r.(string)
				if !ok || !strings.Contains(msg, name) || !strings.Contains(msg, "reserved") {
					t.Errorf("panic must name the slice and say reserved, got: %v", r)
				}
			}()
			_ = New("").String(name, "")
		}()
	}
	// A namespace prefix still lands under route.*: refused too.
	func() {
		defer func() {
			if recover() == nil {
				t.Error(`New("route").String("x", "") must panic — the full key is route.x`)
			}
		}()
		_ = New("route").String("x", "")
	}()

	// The family's own declarations stay idempotent and keep their
	// names (declareRoute owns the prefix).
	if got := Route.Title.Name(); got != "route.title" {
		t.Errorf("Route.Title.Name() = %q, want route.title", got)
	}
	p1, p2 := RouteSlices{}.Param("id"), RouteSlices{}.Param("id")
	if p1 != p2 || p1.Name() != "route.params.id" {
		t.Errorf("Route.Param re-declaration must be idempotent, got %p/%p (%s)", p1, p2, p1.Name())
	}
	q1, q2 := Route.Query("q"), Route.Query("q")
	if q1 != q2 || q1.Name() != "route.query.q" {
		t.Errorf("Route.Query re-declaration must be idempotent, got %p/%p (%s)", q1, q2, q1.Name())
	}

	// A non-route name under the same store is fine.
	if sl := New("t").String("plain", ""); sl.Name() != "t.plain" {
		t.Errorf("plain declaration mangled: %q", sl.Name())
	}
}
