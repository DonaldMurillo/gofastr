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
		// useless. We check for "registry_test.go:" in the message,
		// it must point back to this very test file twice.
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
	// xdata-fui-comp="y" must NOT match, the anchor requires a
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

func TestIsolateForTest_HidesExistingEntriesAndRestores(t *testing.T) {
	reset()
	RegisterStyle("pollute-auto", func(t style.Theme) string { return "" })
	RegisterStyle("pollute-eager", func(t style.Theme) string { return "" }, WithLoad(LoadAlways))

	t.Run("isolated", func(t *testing.T) {
		IsolateForTest(t)
		if got := All(); len(got) != 0 {
			t.Fatalf("isolated registry must be empty, got %v", names(got))
		}
		if got := EagerNames(); len(got) != 0 {
			t.Fatalf("isolated EagerNames must be empty, got %v", got)
		}
	})

	// The subtest has returned: its Cleanup must have restored the
	// pre-isolation registry exactly, eager set included.
	if _, ok := Lookup("pollute-auto"); !ok {
		t.Fatal("IsolateForTest must restore pre-existing entries on cleanup")
	}
	got := EagerNames()
	if len(got) != 1 || got[0] != "pollute-eager" {
		t.Fatalf("restored EagerNames = %v, want [pollute-eager]", got)
	}
}

// TestIsolateForTest_PremiseUnderPollution is the #331 shape: the
// process already carries LoadAlways registrations from some linked
// package's init (simulated here with framework/ui's exact names). An
// isolated test that registers a single component must see an eager
// set of exactly its own — the premise the SSR host's
// single-direct-link and eager-link emission branches stand on.
func TestIsolateForTest_PremiseUnderPollution(t *testing.T) {
	reset()
	for _, n := range []string{"ui-button", "ui-page-header", "ui-sidebar"} {
		RegisterStyle(n, func(t style.Theme) string { return "" }, WithLoad(LoadAlways))
	}
	// Precondition: the pollution this test exists to run under is
	// really in the registry. Without this, a broken setup passes the
	// isolated assertions vacuously on an empty registry.
	if got := EagerNames(); len(got) != 3 {
		t.Fatalf("precondition: eager pollution = %v, want the three simulated ui-* LoadAlways entries", got)
	}

	t.Run("single component under pollution", func(t *testing.T) {
		IsolateForTest(t)
		RegisterStyle("single", func(t style.Theme) string { return "" })
		if got := EagerNames(); len(got) != 0 {
			t.Fatalf("eager set under isolation = %v, want empty (LoadAuto style only)", got)
		}
	})

	t.Run("one eager component under pollution", func(t *testing.T) {
		IsolateForTest(t)
		RegisterStyle("always", func(t style.Theme) string { return "" }, WithLoad(LoadAlways))
		got := EagerNames()
		if len(got) != 1 || got[0] != "always" {
			t.Fatalf("eager set under isolation = %v, want [always]", got)
		}
	})
}

func TestIsolateForTest_RegistrationsDoNotLeak(t *testing.T) {
	reset()
	RegisterStyle("survivor", func(t style.Theme) string { return "" })
	t.Run("isolated", func(t *testing.T) {
		IsolateForTest(t)
		RegisterStyle("test-local", func(t style.Theme) string { return "" }, WithLoad(LoadAlways))
		if _, ok := Lookup("test-local"); !ok {
			t.Fatal("style registered during isolation must be visible to the test")
		}
	})
	if _, ok := Lookup("test-local"); ok {
		t.Fatal("styles registered during isolation must be dropped on restore, not leak into the process registry")
	}
	if _, ok := Lookup("survivor"); !ok {
		t.Fatal("restore must return to the pre-isolation registry, not an empty one")
	}
}

// names flattens entries for failure messages.
func names(es []*Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
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

// Catch panic with string message, RegisterStyle panics with
// fmt.Sprintf, so the value is a string.
func init() {
	// Ensure component package is reachable for type checks.
	var _ component.Component = (*stubComponent)(nil)
}
