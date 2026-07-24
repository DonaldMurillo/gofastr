package app

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// --- exact redirects ---

func TestRedirectExactResolves(t *testing.T) {
	a := NewApp("t")
	a.Redirect("/old", "/new")
	target, ok := a.ResolveRedirect("/old")
	if !ok || target != "/new" {
		t.Errorf("ResolveRedirect(/old) = %q %v, want /new true", target, ok)
	}
	if _, ok := a.ResolveRedirect("/unrelated"); ok {
		t.Error("unrelated path should not resolve as a redirect")
	}
}

func TestRedirectExactInRoutes(t *testing.T) {
	a := NewApp("t")
	a.Register("/", &stubComponent{html: render.Raw("<p>home</p>")}, nil)
	a.Redirect("/old", "/new")
	var found *RouteEntry
	entries := a.Routes()
	for i := range entries {
		if entries[i].Path == "/old" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("redirect /old missing from Routes()")
	}
	if found.RedirectTo != "/new" {
		t.Errorf("RedirectTo: got %q, want /new", found.RedirectTo)
	}
}

// --- pattern redirects with param passthrough ---

func TestRedirectPatternParamPassthrough(t *testing.T) {
	a := NewApp("t")
	a.RedirectPattern("/old/{id}", "/new/{id}")
	target, ok := a.ResolveRedirect("/old/42")
	if !ok || target != "/new/42" {
		t.Errorf("pattern redirect: got %q %v, want /new/42 true", target, ok)
	}
}

func TestRedirectPatternCatchAllPassthrough(t *testing.T) {
	a := NewApp("t")
	a.RedirectPattern("/legacy/{path...}", "/docs/{path...}")
	target, ok := a.ResolveRedirect("/legacy/a/b/c")
	if !ok || target != "/docs/a/b/c" {
		t.Errorf("catch-all passthrough: got %q %v, want /docs/a/b/c", target, ok)
	}
}

// --- registration panics ---

func TestRedirectCollisionWithScreenPanics(t *testing.T) {
	a := NewApp("t")
	a.Register("/exists", &stubComponent{html: render.Raw("x")}, nil)
	msg := panicMsg(t, func() { a.Redirect("/exists", "/elsewhere") })
	if !strings.Contains(msg, "collides") {
		t.Errorf("collision panic, got: %s", msg)
	}
}

func TestRedirectDuplicatePanics(t *testing.T) {
	a := NewApp("t")
	a.Redirect("/old", "/new")
	msg := panicMsg(t, func() { a.Redirect("/old", "/other") })
	if !strings.Contains(msg, "duplicate") {
		t.Errorf("duplicate panic, got: %s", msg)
	}
}

func TestRedirectAbsoluteTargetPanics(t *testing.T) {
	a := NewApp("t")
	for _, bad := range []string{"https://evil.com/x", "//evil.com/x", "http://x"} {
		msg := panicMsg(t, func() { a.Redirect("/old", bad) })
		if !strings.Contains(msg, "redirect target") {
			t.Errorf("open-redirect guard for %q did not fire: %s", bad, msg)
		}
	}
}

func TestRedirectPatternUndeclaredParamPanics(t *testing.T) {
	a := NewApp("t")
	msg := panicMsg(t, func() { a.RedirectPattern("/old/{id}", "/new/{other}") })
	if !strings.Contains(msg, "other") || !strings.Contains(msg, "not declared") {
		t.Errorf("undeclared param panic, got: %s", msg)
	}
}

func TestRedirectDynamicFromPanics(t *testing.T) {
	a := NewApp("t")
	msg := panicMsg(t, func() { a.Redirect("/old/{id}", "/new") })
	if !strings.Contains(msg, "RedirectPattern") {
		t.Errorf("dynamic-from-in-Redirect panic, got: %s", msg)
	}
}

func TestScreenOverRedirectPanics(t *testing.T) {
	a := NewApp("t")
	a.Redirect("/taken", "/elsewhere")
	msg := panicMsg(t, func() {
		a.Register("/taken", &stubComponent{html: render.Raw("x")}, nil)
	})
	if !strings.Contains(msg, "collides") {
		t.Errorf("screen-over-redirect panic, got: %s", msg)
	}
}

// --- server 308 on hard GET (through the host) is covered by the
// uihost test; here we assert the resolve contract the host calls. ---

func TestResolveRedirectNoMatchForScreen(t *testing.T) {
	a := NewApp("t")
	a.Register("/page", &stubComponent{html: render.Raw("x")}, nil)
	a.Redirect("/old", "/new")
	if _, ok := a.ResolveRedirect("/page"); ok {
		t.Error("a real screen path must not resolve as a redirect")
	}
}

// --- hardening: chains, cycles, substituted-target safety ---

func TestRedirectChainResolvesToFinal(t *testing.T) {
	a := NewApp("t")
	a.Redirect("/a", "/b")
	a.Redirect("/b", "/c")
	target, ok := a.ResolveRedirect("/a")
	if !ok || target != "/c" {
		t.Errorf("chain /a→/b→/c: got %q %v, want /c true", target, ok)
	}
}

func TestRedirectCycleFailsClosed(t *testing.T) {
	a := NewApp("t")
	a.Redirect("/a", "/b")
	a.Redirect("/b", "/a")
	if target, ok := a.ResolveRedirect("/a"); ok {
		t.Errorf("cycle must not resolve, got %q", target)
	}
}

func TestRedirectBackslashTargetPanics(t *testing.T) {
	a := NewApp("t")
	msg := panicMsg(t, func() { a.Redirect("/old", `/\evil.com`) })
	if !strings.Contains(msg, "backslash") {
		t.Errorf("backslash target panic, got: %s", msg)
	}
}

func TestRedirectSubstitutedUnsafeFailsClosed(t *testing.T) {
	// The registered pattern is safe, but an empty first remainder
	// segment joins into a protocol-relative "//host" target. Must not
	// redirect (fail closed → 404), never emit "//evil.com".
	a := NewApp("t")
	a.RedirectPattern("/old/{p...}", "/{p...}")
	if target, ok := a.ResolveRedirect("/old//evil.com"); ok {
		t.Errorf("unsafe substituted target must fail closed, got %q", target)
	}
	// A normal remainder still passes through.
	target, ok := a.ResolveRedirect("/old/x/y")
	if !ok || target != "/x/y" {
		t.Errorf("safe passthrough: got %q %v, want /x/y true", target, ok)
	}
}

func TestRoutesRedirectOrderDeterministic(t *testing.T) {
	a := NewApp("t")
	a.Register("/", &stubComponent{html: render.Raw("x")}, nil)
	a.Redirect("/z", "/1")
	a.Redirect("/a", "/2")
	a.Redirect("/m", "/3")
	var got []string
	for _, e := range a.Routes() {
		if e.RedirectTo != "" {
			got = append(got, e.Path)
		}
	}
	want := []string{"/a", "/m", "/z"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("redirect entries: got %v, want sorted %v", got, want)
	}
}

func TestRedirectTenHopChainResolves(t *testing.T) {
	a := NewApp("t")
	hops := []string{"/h0", "/h1", "/h2", "/h3", "/h4", "/h5", "/h6", "/h7", "/h8", "/h9", "/final"}
	for i := 0; i < len(hops)-1; i++ {
		a.Redirect(hops[i], hops[i+1])
	}
	target, ok := a.ResolveRedirect("/h0")
	if !ok || target != "/final" {
		t.Errorf("10-edge chain: got %q %v, want /final true", target, ok)
	}
}

func TestRedirectParamRenameStillCollides(t *testing.T) {
	// "/users/:id" and "/users/:slug" match the same URL set — renaming
	// the param must not slip a redirect past the collision guard.
	a := NewApp("t")
	a.Register("/users/{id}", &basicComp{}, nil)
	msg := panicMsg(t, func() { a.RedirectPattern("/users/{slug}", "/new/{slug}") })
	if !strings.Contains(msg, "overlap") {
		t.Errorf("param-renamed redirect over screen must panic, got: %s", msg)
	}
	// And the reverse: screen over a shape-equal redirect.
	b := NewApp("t")
	b.RedirectPattern("/users/{slug}", "/new/{slug}")
	msg = panicMsg(t, func() { b.Register("/users/{id}", &basicComp{}, nil) })
	if !strings.Contains(msg, "overlap") {
		t.Errorf("param-renamed screen over redirect must panic, got: %s", msg)
	}
	// OVERLAPPING shapes collide too (Sol round-2): "/users/{slug}"
	// matches every numeric id "/users/{id:int}" serves, and redirects
	// run before screens — so this must panic, not silently shadow.
	c := NewApp("t")
	c.Register("/users/{id:int}", &basicComp{}, nil)
	msg = panicMsg(t, func() { c.RedirectPattern("/users/{slug}", "/new/{slug}") })
	if !strings.Contains(msg, "overlap") {
		t.Errorf("overlapping redirect over screen must panic, got: %s", msg)
	}
	// Genuinely DISJOINT match sets coexist: int vs alpha.
	d := NewApp("t")
	d.Register("/users/{id:int}", &basicComp{}, nil)
	d.RedirectPattern("/users/{handle:alpha}", "/profiles/{handle}") // must NOT panic
}

func TestRedirectOverlapAndValidation(t *testing.T) {
	// Sol round-2's exact input: an int-constrained redirect over an
	// unconstrained screen would steal every numeric id.
	a := NewApp("t")
	a.Register("/users/{id}", &basicComp{}, nil)
	msg := panicMsg(t, func() { a.RedirectPattern("/users/{n:int}", "/legacy/{n}") })
	if !strings.Contains(msg, "overlap") {
		t.Errorf("int-over-plain overlap must panic, got: %s", msg)
	}
	// A catch-all redirect overlaps a fixed-length dynamic screen.
	b := NewApp("t")
	b.Register("/files/{name}", &basicComp{}, nil)
	msg = panicMsg(t, func() { b.RedirectPattern("/files/{p...}", "/blobs/{p...}") })
	if !strings.Contains(msg, "overlap") {
		t.Errorf("catch-all-over-dynamic overlap must panic, got: %s", msg)
	}
	// RedirectPattern applies the route grammar: unknown constraints and
	// non-final catch-alls are registration errors, not silent no-ops.
	c := NewApp("t")
	msg = panicMsg(t, func() { c.RedirectPattern("/u/{h:string}", "/profiles/{h}") })
	if !strings.Contains(msg, "unknown constraint") {
		t.Errorf("bad constraint in redirect must panic, got: %s", msg)
	}
	msg = panicMsg(t, func() { c.RedirectPattern("/x/{p...}/y", "/z/{p...}") })
	if !strings.Contains(msg, "final segment") {
		t.Errorf("non-final catch-all in redirect must panic, got: %s", msg)
	}
}

// --- round-3 matrix pins (behavior verified by differential testing;
// these keep the tricky regions from regressing) ---

func TestRedirectConstrainedCatchAllPanics(t *testing.T) {
	// Canonical ordering: constraint before the ellipsis.
	a := NewApp("t")
	msg := panicMsg(t, func() { a.RedirectPattern("/f/{p:int...}", "/g/{p...}") })
	if !strings.Contains(msg, "not allowed on a catch-all") {
		t.Errorf("constrained catch-all in redirect must panic, got: %s", msg)
	}
	// Ellipsis-first is malformed syntax and fails with a clear message,
	// not a garbage "p..." param name.
	b := NewApp("t")
	msg = panicMsg(t, func() { b.RedirectPattern("/f/{p...:int}", "/g/{p}") })
	if !strings.Contains(msg, "malformed") {
		t.Errorf("ellipsis-first constrained catch-all must panic as malformed, got: %s", msg)
	}
}

func TestCatchAllVsCatchAllOverlap(t *testing.T) {
	// /docs/{p...} and /docs/sub/{q...} share /docs/sub/x → overlap.
	a := NewApp("t")
	a.Register("/docs/{p...}", &catchAllComp{}, nil)
	msg := panicMsg(t, func() { a.RedirectPattern("/docs/sub/{q...}", "/x/{q...}") })
	if !strings.Contains(msg, "overlap") {
		t.Errorf("nested catch-alls must collide, got: %s", msg)
	}
}

func TestUUIDDisjointConstraintsCoexist(t *testing.T) {
	// uuid requires hyphens; int/alpha/alnum forbid them — disjoint, so
	// a redirect over a uuid-constrained screen with an int pattern is legal.
	a := NewApp("t")
	a.Register("/r/{id:uuid}", &basicComp{}, nil)
	a.RedirectPattern("/r/{n:int}", "/nums/{n}") // must NOT panic
}

func TestRedirectElevenHopChainFailsClosed(t *testing.T) {
	a := NewApp("t")
	hops := []string{"/h0", "/h1", "/h2", "/h3", "/h4", "/h5", "/h6", "/h7", "/h8", "/h9", "/h10", "/final"}
	for i := 0; i < len(hops)-1; i++ {
		a.Redirect(hops[i], hops[i+1])
	}
	if target, ok := a.ResolveRedirect("/h0"); ok {
		t.Errorf("11-edge chain must fail closed, got %q", target)
	}
}

type catchAllComp struct{ p string }

func (c *catchAllComp) SetParams(m map[string]string) { c.p = m["p"] }
func (c *catchAllComp) Render() render.HTML           { return render.Raw("x") }
