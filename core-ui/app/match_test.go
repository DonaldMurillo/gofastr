package app

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// matchParamComp accepts route params; the router requires ParamSetter
// on dynamic-route components.
type matchParamComp struct{ stubComponent }

func (m *matchParamComp) SetParams(map[string]string) {}

func newMatchRouter() *Router {
	r := NewRouter()
	r.Screen(NewScreen("/session/:sessionId", &matchParamComp{stubComponent{html: render.HTML("<p>session</p>")}}), nil)
	r.Screen(NewScreen("/session/:sessionId/operator", &matchParamComp{stubComponent{html: render.HTML("<p>operator</p>")}}), nil)
	r.Screen(NewScreen("/about", &stubComponent{html: render.HTML("<p>about</p>")}), nil)
	return r
}

func TestMatchForReturnsDynamicParams(t *testing.T) {
	m, ok := newMatchRouter().MatchFor("/session/abc")
	if !ok {
		t.Fatal("expected a match for /session/abc")
	}
	if got := m.Param("sessionId"); got != "abc" {
		t.Errorf("sessionId = %q, want abc", got)
	}
	if got := m.ScreenID(); got != "/session/:sessionId" {
		t.Errorf("ScreenID = %q, want /session/:sessionId", got)
	}
	if got := m.Path(); got != "/session/abc" {
		t.Errorf("Path = %q, want /session/abc", got)
	}
}

// The more specific sibling route wins by registration order; ScreenID
// must name the pattern that actually matched.
func TestMatchForPicksRegisteredRoute(t *testing.T) {
	m, ok := newMatchRouter().MatchFor("/session/abc/operator")
	if !ok {
		t.Fatal("expected a match for /session/abc/operator")
	}
	if got := m.ScreenID(); got != "/session/:sessionId/operator" {
		t.Errorf("ScreenID = %q, want /session/:sessionId/operator", got)
	}
	if got := m.Param("sessionId"); got != "abc" {
		t.Errorf("sessionId = %q, want abc", got)
	}
}

// Router.Resolve trims slashes on both ends before splitting, so a
// trailing slash resolves with the same param value the screen's
// SetParams would see.
func TestMatchForTrailingSlashKeepsParams(t *testing.T) {
	m, ok := newMatchRouter().MatchFor("/session/abc/")
	if !ok {
		t.Fatal("expected a match for /session/abc/")
	}
	if got := m.Param("sessionId"); got != "abc" {
		t.Errorf("sessionId = %q, want abc", got)
	}
}

func TestMatchForStaticRouteHasNoParams(t *testing.T) {
	m, ok := newMatchRouter().MatchFor("/about")
	if !ok {
		t.Fatal("expected a match for /about")
	}
	if got := m.Param("sessionId"); got != "" {
		t.Errorf("Param on a static route = %q, want \"\"", got)
	}
	if got := m.ScreenID(); got != "/about" {
		t.Errorf("ScreenID = %q, want /about", got)
	}
}

func TestMatchForUnknownPathMisses(t *testing.T) {
	m, ok := newMatchRouter().MatchFor("/nope")
	if ok {
		t.Fatalf("unexpected match for /nope: %+v", m)
	}
	if m.ScreenID() != "" || m.Path() != "" || m.Param("x") != "" {
		t.Errorf("miss must return the zero Match, got %+v", m)
	}
}

func TestMatchFromContextWithoutMatch(t *testing.T) {
	m, ok := MatchFromContext(context.Background())
	if ok {
		t.Fatalf("unexpected match from an empty context: %+v", m)
	}
	if m, ok := MatchFromContext(nil); ok || m.ScreenID() != "" || m.Path() != "" || m.Param("x") != "" {
		t.Error("nil context must yield the zero Match, not panic")
	}
}

func TestMatchFromContextRoundTrip(t *testing.T) {
	m, ok := newMatchRouter().MatchFor("/session/abc")
	if !ok {
		t.Fatal("expected a match")
	}
	got, ok := MatchFromContext(WithMatch(context.Background(), m))
	if !ok {
		t.Fatal("match lost on the context round trip")
	}
	if got.Param("sessionId") != "abc" {
		t.Errorf("sessionId = %q, want abc", got.Param("sessionId"))
	}
}

// The Match must own its params: mutating the map the snapshot was
// built from cannot change what a holder already read.
func TestMatchParamsCopiedFromSource(t *testing.T) {
	src := map[string]string{"sessionId": "abc"}
	m := newMatch("/session/:sessionId", "/session/abc", src)
	src["sessionId"] = "tampered"
	if got := m.Param("sessionId"); got != "abc" {
		t.Errorf("Param changed with the source map: %q, want abc", got)
	}
}

// Two resolutions must not share storage either.
func TestMatchForMatchesAreIndependent(t *testing.T) {
	r := newMatchRouter()
	first, ok := r.MatchFor("/session/one")
	if !ok {
		t.Fatal("expected first match")
	}
	second, ok := r.MatchFor("/session/two")
	if !ok {
		t.Fatal("expected second match")
	}
	if got := first.Param("sessionId"); got != "one" {
		t.Errorf("first.Param = %q, want one", got)
	}
	if got := second.Param("sessionId"); got != "two" {
		t.Errorf("second.Param = %q, want two", got)
	}
}
