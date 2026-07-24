package app

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// --- resolve: constraints match / fall through ---

func TestConstraintIntMatches(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/{id:int}", &paramComp{}), nil)
	if _, params, ok := r.Resolve("/orders/42"); !ok || params["id"] != "42" {
		t.Errorf("int match: ok=%v params=%v", ok, params)
	}
}

func TestConstraintIntNonMatchFallsThrough(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/{id:int}", &paramComp{}), nil)
	// a non-numeric id does not match the int-constrained route → 404
	if _, _, ok := r.Resolve("/orders/abc"); ok {
		t.Error("/orders/abc must not match /orders/{id:int}")
	}
}

func TestConstraintIntFallsThroughToNextRoute(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/{id:int}", &paramComp{}), nil)
	r.Screen(NewScreen("/orders/{slug}", &paramComp{}), nil)
	// "abc" fails int → falls through to the second, unconstrained route.
	s, params, ok := r.Resolve("/orders/abc")
	if !ok {
		t.Fatal("expected /orders/abc to match the fallback route")
	}
	if _, isInt := s.Component.(*paramComp); !isInt {
		t.Error("expected the second route's screen")
	}
	if params["slug"] != "abc" {
		t.Errorf("slug: got %q", params["slug"])
	}
}

func TestConstraintUUIDMatches(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/u/{id:uuid}", &paramComp{}), nil)
	id := "550e8400-e29b-41d4-a716-446655440000"
	if _, params, ok := r.Resolve("/u/" + id); !ok || params["id"] != id {
		t.Errorf("uuid match: ok=%v params=%v", ok, params)
	}
}

func TestConstraintUUIDNonMatch(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/u/{id:uuid}", &paramComp{}), nil)
	for _, bad := range []string{"not-a-uuid", "550e8400-e29b-41d4-a716", "xyz"} {
		if _, _, ok := r.Resolve("/u/" + bad); ok {
			t.Errorf("/u/%s must not match the uuid constraint", bad)
		}
	}
}

// --- registration panics ---

func TestConstraintUnknownPanics(t *testing.T) {
	r := NewRouter()
	msg := panicMsg(t, func() {
		r.Screen(NewScreen("/x/{id:slug}", &paramComp{}), nil)
	})
	if !strings.Contains(msg, "unknown constraint") || !strings.Contains(msg, "slug") {
		t.Errorf("unknown constraint panic, got: %s", msg)
	}
}

func TestConstraintOnCatchAllPanics(t *testing.T) {
	r := NewRouter()
	msg := panicMsg(t, func() {
		r.Screen(NewScreen("/x/{p*:int}", &catchScreen{}), nil)
	})
	if !strings.Contains(msg, "catch-all") {
		t.Errorf("constraint-on-catchall panic, got: %s", msg)
	}
}

// --- name stripping: the constraint suffix never reaches SetParams ---

func TestConstraintNameStripped(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/orders/{id:int}", &paramComp{}), nil)
	_, params, _ := r.Resolve("/orders/7")
	if _, ok := params["id:int"]; ok {
		t.Error("param key must be 'id', not 'id:int'")
	}
	if params["id"] != "7" {
		t.Errorf("id: got %q", params["id"])
	}
}

// SetParams receives the raw matched string (no type conversion).
func TestConstraintSetParamsRawString(t *testing.T) {
	r := NewRouter()
	comp := &paramComp{}
	r.Screen(NewScreen("/n/{x:int}", comp), nil)
	screen, params, _ := r.Resolve("/n/123")
	ps, ok := screen.Component.(ParamSetter)
	if ok {
		ps.SetParams(params)
	}
	if comp.got["x"] != "123" {
		t.Errorf("SetParams raw value: got %v", comp.got)
	}
}

var _ = render.Raw // keep render import meaningful if helper expands

func TestConstraintAlphaAndAlnum(t *testing.T) {
	a := NewApp("t")
	a.Register("/u/{handle:alpha}", &basicComp{}, nil)
	a.Register("/u/{handle:alnum}", &basicComp{}, nil)

	cases := []struct {
		path string
		want bool
	}{
		{"/u/donald", true},   // alpha
		{"/u/donald42", true}, // falls to alnum
		{"/u/don-ald", false}, // hyphen matches neither
		{"/u/don.png", false}, // dot matches neither
		{"/u/42", true},       // alnum
	}
	for _, c := range cases {
		_, _, ok := a.Router.Resolve(c.path)
		if ok != c.want {
			t.Errorf("Resolve(%s) = %v, want %v", c.path, ok, c.want)
		}
	}
}

func TestConstraintStringRejected(t *testing.T) {
	// "string" is deliberately not a constraint — it would be a no-op
	// alias for unconstrained.
	a := NewApp("t")
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for :string constraint")
		}
	}()
	a.Register("/x/{v:string}", &basicComp{}, nil)
}
