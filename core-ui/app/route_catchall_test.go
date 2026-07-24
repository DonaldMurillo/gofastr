package app

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// catchScreen records every SetParams it receives so tests can assert on
// the joined catch-all remainder.
type catchScreen struct{ got map[string]string }

func (s *catchScreen) SetParams(p map[string]string) { s.got = p }
func (s *catchScreen) Render() render.HTML           { return render.HTML("<p>catch</p>") }

// --- normalizeRoutePath: {path...} and :path* both canonicalize ---

func TestNormalizeCatchAllBrace(t *testing.T) {
	if got := normalizeRoutePath("/docs/{path...}"); got != "/docs/:path*" {
		t.Errorf("brace catch-all: got %q, want /docs/:path*", got)
	}
}

func TestNormalizeCatchAllColonUnchanged(t *testing.T) {
	if got := normalizeRoutePath("/docs/:path*"); got != "/docs/:path*" {
		t.Errorf("colon catch-all should pass through: got %q", got)
	}
}

// --- Resolve: catch-all joins the remainder with "/" ---

func TestResolveCatchAllSingleSegment(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/docs/{path...}", &catchScreen{}), nil)
	_, params, ok := r.Resolve("/docs/getting-started")
	if !ok {
		t.Fatal("expected /docs/getting-started to match /docs/{path...}")
	}
	if params["path"] != "getting-started" {
		t.Errorf("path param: got %q, want getting-started", params["path"])
	}
}

func TestResolveCatchAllMultiSegment(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/docs/{path...}", &catchScreen{}), nil)
	_, params, ok := r.Resolve("/docs/a/b/c")
	if !ok {
		t.Fatal("expected /docs/a/b/c to match")
	}
	if params["path"] != "a/b/c" {
		t.Errorf("joined remainder: got %q, want a/b/c", params["path"])
	}
}

// A catch-all needs at least one remainder segment — /docs and /docs/ do
// NOT match (documented behavior; keeps it simple).
func TestResolveCatchAllNeedsRemainder(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/docs/{path...}", &catchScreen{}), nil)
	for _, p := range []string{"/docs", "/docs/"} {
		if _, _, ok := r.Resolve(p); ok {
			t.Errorf("%q must not match a catch-all (needs >=1 remainder)", p)
		}
	}
}

// Catch-all with a param prefix: /shop/:cat/{rest...}
func TestResolveCatchAllWithParamPrefix(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/shop/:cat/{rest...}", &catchScreen{}), nil)
	_, params, ok := r.Resolve("/shop/books/a/b")
	if !ok {
		t.Fatal("expected match")
	}
	if params["cat"] != "books" {
		t.Errorf("cat: got %q", params["cat"])
	}
	if params["rest"] != "a/b" {
		t.Errorf("rest: got %q, want a/b", params["rest"])
	}
}

// Registration order decides precedence — no specificity ranking. A
// fixed-length dynamic registered BEFORE a catch-all wins for the
// segment count they share; the catch-all only takes longer paths.
func TestResolveFixedLengthBeatsCatchAllByOrder(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/a/{x}", &paramComp{}), nil)      // fixed 2-seg, first
	r.Screen(NewScreen("/a/{y...}", &catchScreen{}), nil) // catch-all, second

	// /a/b matches the fixed route (registered first).
	s, params, ok := r.Resolve("/a/b")
	if !ok {
		t.Fatal("expected /a/b to match")
	}
	if _, isCatch := s.Component.(*catchScreen); isCatch {
		t.Errorf("/a/b matched catch-all; fixed-length route should win by registration order")
	}
	if params["x"] != "b" {
		t.Errorf("fixed param x: got %q", params["x"])
	}
	// /a/b/c only the catch-all can take.
	_, params2, ok2 := r.Resolve("/a/b/c")
	if !ok2 || params2["y"] != "b/c" {
		t.Errorf("/a/b/c should match catch-all with y=b/c, got ok=%v params=%v", ok2, params2)
	}
}

// Reverse order: catch-all registered first swallows the 2-segment path
// too (first-match-wins) — order matters, by design.
func TestResolveCatchAllFirstWinsSharedCount(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/a/{y...}", &catchScreen{}), nil) // catch-all first
	r.Screen(NewScreen("/a/{x}", &paramComp{}), nil)      // fixed second

	s, _, ok := r.Resolve("/a/b")
	if !ok {
		t.Fatal("expected match")
	}
	if _, isCatch := s.Component.(*catchScreen); !isCatch {
		t.Errorf("catch-all registered first should win /a/b (first-match-wins)")
	}
}

// Exact-map registration always wins, regardless of a catch-all that
// could also swallow the path.
func TestResolveExactMapBeatsCatchAll(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/docs/index", &paramComp{}), nil)
	r.Screen(NewScreen("/docs/{path...}", &catchScreen{}), nil)
	s, _, ok := r.Resolve("/docs/index")
	if !ok {
		t.Fatal("expected match")
	}
	if _, isCatch := s.Component.(*catchScreen); isCatch {
		t.Errorf("exact-map /docs/index should beat the catch-all")
	}
}

// --- Registration panics ---

// A catch-all that is not the final segment must panic at registration.
func TestRegisterCatchAllNotFinalPanics(t *testing.T) {
	r := NewRouter()
	msg := panicMsg(t, func() {
		r.Screen(NewScreen("/docs/{p...}/tail", &catchScreen{}), nil)
	})
	if !strings.Contains(msg, "/docs/:p*/tail") || !strings.Contains(msg, "catch-all") {
		t.Errorf("panic must name the path and the catch-all problem, got: %s", msg)
	}
}

// Catch-all value is the raw path remainder (no URL-decoding surprises),
// consistent with how single-segment params are passed through verbatim.
func TestResolveCatchAllRawNoDecode(t *testing.T) {
	r := NewRouter()
	r.Screen(NewScreen("/docs/{path...}", &catchScreen{}), nil)
	_, params, ok := r.Resolve("/docs/a%2Fb")
	if !ok {
		t.Fatal("expected match")
	}
	if params["path"] != "a%2Fb" {
		t.Errorf("catch-all must pass raw (no decode): got %q", params["path"])
	}
}
