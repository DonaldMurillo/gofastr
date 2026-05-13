package registry

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

type stubComponent struct {
	html string
}

func (s *stubComponent) Render() render.HTML { return render.HTML(s.html) }

func TestRegisterAndLookup(t *testing.T) {
	reset()
	fn := func(t style.Theme) string { return ".x { color: red }" }
	h := RegisterStyle("modal", fn)
	if h.Name() != "modal" {
		t.Fatalf("Name=%q want modal", h.Name())
	}
	got, ok := Lookup("modal")
	if !ok {
		t.Fatal("Lookup miss")
	}
	if got != h.Entry() {
		t.Fatal("Lookup returned different entry")
	}
}

func TestIdempotentRegistration(t *testing.T) {
	reset()
	fn := func(t style.Theme) string { return "" }
	a := RegisterStyle("a", fn)
	b := RegisterStyle("a", fn)
	if a.Entry() != b.Entry() {
		t.Fatal("expected same Entry on identical re-registration")
	}
}

func TestConflictingRegistrationPanics(t *testing.T) {
	reset()
	fn1 := func(t style.Theme) string { return "x" }
	fn2 := func(t style.Theme) string { return "y" }
	RegisterStyle("a", fn1)
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on conflicting re-registration")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "duplicate name \"a\"") {
			t.Errorf("panic message: %v", r)
		}
		// The panic must name file:line of both call sites so the
		// dev knows which Register to rename. Raw uintptrs are
		// useless. We check for "registry_test.go:" in the message
		// — it must point back to this very test file twice.
		if strings.Count(msg, "registry_test.go:") < 2 {
			t.Errorf("panic message must include file:line for both styleFns, got:\n%s", msg)
		}
	}()
	RegisterStyle("a", fn2)
}

// TestStyleRenderNilPanicHelpful checks Style.Render(nil) panics with
// a message that points the developer at WrapHTML rather than a raw
// nil-pointer dereference.
func TestStyleRenderNilPanicHelpful(t *testing.T) {
	reset()
	s := RegisterStyle("nilc", func(t style.Theme) string { return "" })
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on Render(nil)")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "Render(nil)") || !strings.Contains(msg, "WrapHTML") {
			t.Errorf("panic must mention Render(nil) and suggest WrapHTML; got %v", r)
		}
	}()
	s.Render(nil)
}

func TestRenderInjectsMarker(t *testing.T) {
	reset()
	h := RegisterStyle("modal", func(t style.Theme) string { return "" })
	got := h.Render(&stubComponent{html: `<div class="x">hi</div>`})
	if !strings.Contains(string(got), `data-fui-comp="modal"`) {
		t.Errorf("marker not injected: %s", got)
	}
}

func TestRenderRejectsBareText(t *testing.T) {
	reset()
	h := RegisterStyle("toast", func(t style.Theme) string { return "" })
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on bare-text component output")
		}
	}()
	h.Render(&stubComponent{html: "plain text"})
}

func TestRenderRejectsMultiSibling(t *testing.T) {
	// Multiple siblings inject into the first; the doc says authors
	// must wrap. The injector currently does inject into the first
	// tag; we don't reject this here because the runtime scan still
	// works. But if we *did* want to reject, this is where the test
	// would assert. Document the current behavior:
	reset()
	h := RegisterStyle("multi", func(t style.Theme) string { return "" })
	got := h.Render(&stubComponent{html: `<a></a><b></b>`})
	if !strings.Contains(string(got), `<a data-fui-comp="multi">`) {
		t.Errorf("first tag must carry marker: %s", got)
	}
}

func TestScanFindsAllMarkers(t *testing.T) {
	html := `<div data-fui-comp="modal"><span data-fui-comp="badge"></span><p data-fui-comp="modal"></p></div>`
	got := Scan(html)
	want := []string{"badge", "modal"}
	if len(got) != len(want) {
		t.Fatalf("Scan got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Scan[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestScanIgnoresOtherAttrs(t *testing.T) {
	html := `<div data-fui-rpc="/x" data-other="modal" class="modal"></div>`
	got := Scan(html)
	if len(got) != 0 {
		t.Errorf("Scan should ignore non-comp attrs: %v", got)
	}
}

func TestScanRequiresAttributeBoundary(t *testing.T) {
	// xdata-fui-comp="y" must NOT match — the anchor requires a
	// preceding whitespace or `/`, so an attribute-name prefix
	// like xdata-fui-comp doesn't masquerade as the marker.
	htmlBad := `<div xdata-fui-comp="masquerade"></div>`
	got := Scan(htmlBad)
	if len(got) != 0 {
		t.Errorf("Scan must not match unanchored attribute name, got %v", got)
	}
	// A legitimate marker still hits.
	htmlGood := `<div data-fui-comp="real"></div>`
	got = Scan(htmlGood)
	if len(got) != 1 || got[0] != "real" {
		t.Errorf("Scan must match anchored marker, got %v", got)
	}
}
// Note: free-text occurrences inside <pre>/<code> can still match
// (the regex can't distinguish "inside an open tag" from "inside
// text content"). Harmless in practice because componentCSSTags
// filters every name through registry.Lookup before emitting a
// <link>, and the runtime's client-side scan uses
// querySelectorAll('[data-fui-comp]') which is DOM-attribute-only.

func TestEagerNamesOnlyLoadAlways(t *testing.T) {
	reset()
	RegisterStyle("auto", func(t style.Theme) string { return "" })
	RegisterStyle("prewarm", func(t style.Theme) string { return "" }, WithLoad(LoadPrewarm))
	RegisterStyle("always-a", func(t style.Theme) string { return "" }, WithLoad(LoadAlways))
	RegisterStyle("always-b", func(t style.Theme) string { return "" }, WithLoad(LoadAlways))
	got := EagerNames()
	want := []string{"always-a", "always-b"}
	if len(got) != len(want) {
		t.Fatalf("EagerNames got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("EagerNames[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestCSSAndVersionCacheStable(t *testing.T) {
	reset()
	calls := 0
	fn := func(t style.Theme) string {
		calls++
		return ".x { color: red }"
	}
	h := RegisterStyle("c", fn)
	theme := style.DefaultTheme()
	css1 := h.Entry().CSSFor(theme)
	css2 := h.Entry().CSSFor(theme)
	if css1 != css2 {
		t.Error("CSS changed between calls")
	}
	if calls != 1 {
		t.Errorf("StyleFn invoked %d times, want 1 (cache miss only)", calls)
	}
	v1 := h.Entry().VersionFor(theme)
	v2 := h.Entry().VersionFor(theme)
	if v1 == "" || v1 != v2 {
		t.Errorf("Version unstable: %q vs %q", v1, v2)
	}
}

// Catch panic with string message — RegisterStyle panics with
// fmt.Sprintf, so the value is a string.
func init() {
	// Ensure component package is reachable for type checks.
	var _ component.Component = (*stubComponent)(nil)
}
