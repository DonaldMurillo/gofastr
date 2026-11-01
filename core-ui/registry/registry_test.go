package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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

// A style registered after the host built its catalog never reaches
// the runtime, so a soft navigation shows its component unstyled.
// Registration past that point fails loudly instead.
func TestRegisterAfterFreezePanics(t *testing.T) {
	IsolateForTest(t)
	fn := func(style.Theme) string { return ".a{}" }
	RegisterStyle("before-freeze", fn)
	Freeze()
	RegisterStyle("before-freeze", fn) // identical re-registration stays a no-op
	defer func() {
		r := recover()
		want := `registry: style "late" registered after the component catalog froze; register styles at package init`
		if r != want {
			t.Fatalf("panic = %v, want %q", r, want)
		}
	}()
	RegisterStyle("late", fn)
}

func TestIsolateForTestStartsUnfrozen(t *testing.T) {
	IsolateForTest(t)
	Freeze()
	t.Run("inner", func(t *testing.T) {
		IsolateForTest(t)
		RegisterStyle("inside", func(style.Theme) string { return "" })
	})
	mu.Lock()
	defer mu.Unlock()
	if !frozen {
		t.Fatal("restoring an inner isolation cleared the outer freeze")
	}
}

// A style name is spliced into every instance's data-cui-comp marker
// and names a /__gofastr/comp/<name>.css path, so it follows the
// behavior-name grammar: a quote would add attributes to every
// instance, a "/" would rewrite the path. This refusal replaced three
// framework/uihost tests that registered "evil/../escape"-shaped names
// to prove the comp-CSS link and handlers refuse them: such an entry
// can no longer exist.
func TestRegisterStyleRefusesBadName(t *testing.T) {
	IsolateForTest(t)
	fn := func(style.Theme) string { return "" }
	for _, name := range []string{
		`x" onclick="alert(1)`, "evil/../escape", "Upper", "9lead", "has space",
		"-lead", strings.Repeat("a", 65),
	} {
		func() {
			defer func() {
				r, _ := recover().(string)
				if !strings.Contains(r, "must match ^[a-z][a-z0-9-]{0,63}$") {
					t.Errorf("RegisterStyle(%q): panic = %q, want the name-grammar refusal", name, r)
				}
			}()
			RegisterStyle(name, fn)
		}()
	}
	RegisterStyle("ui-ok-2", fn) // the grammar's own shape still registers
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
	if !strings.Contains(string(got), `data-cui-comp="modal"`) {
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
	if !strings.Contains(string(got), `<a data-cui-comp="multi">`) {
		t.Errorf("first tag must carry marker: %s", got)
	}
}

func TestScanFindsAllMarkers(t *testing.T) {
	html := `<div data-cui-comp="modal"><span data-cui-comp="badge"></span><p data-cui-comp="modal"></p></div>`
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

// An owned style's root carries data-cui-scope; its sheet loads the
// same way a kit component's does. One element may carry both.
func TestScanReadsScopeMarkers(t *testing.T) {
	html := `<div data-cui-scope="board"><article class="fui-card" data-cui-comp="card" data-cui-scope="review"></article></div>`
	got := strings.Join(Scan(html), ",")
	if got != "board,card,review" {
		t.Fatalf("Scan got %q, want board,card,review", got)
	}
}

func TestScanRefusesUnsafeScopeNames(t *testing.T) {
	for _, html := range []string{
		`<div data-cui-scope="../x"></div>`,
		`<div data-cui-scope="a b"></div>`,
		`<div data-cui-scope="a?b"></div>`,
		`<div data-cui-scope=""></div>`,
		`<div xdata-cui-scope="masquerade"></div>`,
	} {
		if got := Scan(html); len(got) != 0 {
			t.Errorf("Scan(%s) = %v, want none", html, got)
		}
	}
}

func TestScanIgnoresOtherAttrs(t *testing.T) {
	html := `<div data-cui-rpc="/x" data-other="modal" class="modal"></div>`
	got := Scan(html)
	if len(got) != 0 {
		t.Errorf("Scan should ignore non-comp attrs: %v", got)
	}
}

func TestScanRequiresAttributeBoundary(t *testing.T) {
	// xdata-cui-comp="y" must NOT match, the anchor requires a
	// preceding whitespace or `/`, so an attribute-name prefix
	// like xdata-cui-comp doesn't masquerade as the marker.
	htmlBad := `<div xdata-cui-comp="masquerade"></div>`
	got := Scan(htmlBad)
	if len(got) != 0 {
		t.Errorf("Scan must not match unanchored attribute name, got %v", got)
	}
	// A legitimate marker still hits.
	htmlGood := `<div data-cui-comp="real"></div>`
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
// querySelectorAll('[data-cui-comp]') which is DOM-attribute-only.

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

func TestHashVariantsMatchThemeVariants(t *testing.T) {
	reset()
	h := RegisterStyle("c", func(t style.Theme) string { return ".x { color: red }" })
	theme := style.DefaultTheme()
	th := style.ThemeHash(theme)
	if got, want := h.Entry().VersionForHash(theme, th), h.Entry().VersionFor(theme); got != want {
		t.Errorf("VersionForHash = %q, VersionFor = %q", got, want)
	}
	if got, want := h.Entry().CSSForHash(theme, th), h.Entry().CSSFor(theme); got != want {
		t.Errorf("CSSForHash = %q, CSSFor = %q", got, want)
	}
}

// Catch panic with string message, RegisterStyle panics with
// fmt.Sprintf, so the value is a string.
func init() {
	// Ensure component package is reachable for type checks.
	var _ component.Component = (*stubComponent)(nil)
}

// A <template> is inert: the browser never renders its content, and the
// runtime's boot scan (querySelectorAll) does not reach into it. The SSR
// scan must agree, or a dialog or toast kept in a template makes its
// sheet render-blocking on every page that merely carries it.
func TestScanSkipsInertTemplates(t *testing.T) {
	for _, c := range []struct{ name, html, want, held string }{
		{"template content", `<template data-cui-confirm-dialog><dialog data-cui-comp="dlg"></dialog></template><div data-cui-comp="real"></div>`, "real", "dlg"},
		{"marker on the template", `<template data-cui-comp="tpl"></template><i data-cui-comp="real"></i>`, "real", "tpl"},
		{"nested", `<template><template><i data-cui-comp="a"></i></template><i data-cui-comp="b"></i></template><i data-cui-comp="c"></i>`, "c", "a,b"},
		{"unterminated", `<i data-cui-comp="c"></i><template><i data-cui-comp="a"></i>`, "c", "a"},
		{"uppercase", `<TEMPLATE><i data-cui-comp="a"></i></TEMPLATE><i data-cui-comp="c"></i>`, "c", "a"},
		{"declarative shadow root renders", `<div><template shadowrootmode="open"><i data-cui-comp="shadow"></i></template></div>`, "shadow", ""},
		{"shadow root name in uppercase", `<div><template SHADOWROOTMODE=open><i data-cui-comp="shadow"></i></template></div>`, "shadow", ""},
		{"shadowrootmode as a value", `<template title="shadowrootmode"><i data-cui-comp="c"></i></template>`, "", "c"},
		{"shadowrootmode after a value", `<template data-x='a b' shadowrootmode="open"><i data-cui-comp="shadow"></i></template>`, "shadow", ""},
		{"custom element named template-x", `<template-x data-cui-comp="t"></template-x>`, "t", ""},
		{"text mentioning a template", `<p>&lt;template&gt;</p><i data-cui-comp="c"></i>`, "c", ""},
		{"held and rendered", `<template><i data-cui-comp="c"></i><i data-cui-comp="d"></i></template><i data-cui-comp="c"></i>`, "c", "d"},
		{"two templates", `<template><i data-cui-comp="a"></i></template><p></p><template><i data-cui-comp="b"></i></template>`, "", "a,b"},
		{"template in a comment", `<!-- <template> --><i data-cui-comp="c"></i>`, "c", ""},
		{"template in an attribute", `<p title="<template>"></p><i data-cui-comp="c"></i>`, "c", ""},
		{"template in a script", `<script>let t = "<template>";</script><i data-cui-comp="c"></i>`, "c", ""},
		{"template in a textarea", `<TEXTAREA><template></TEXTAREA><i data-cui-comp="c"></i>`, "c", ""},
		{"end tag in a held comment", `<template><!-- </template> --><i data-cui-comp="a"></i></template><i data-cui-comp="c"></i>`, "c", "a"},
		{"unterminated comment", `<i data-cui-comp="c"></i><!-- <template>`, "c", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := strings.Join(Scan(c.html), ","); got != c.want {
				t.Fatalf("Scan = %q, want %q", got, c.want)
			}
			live, held := ScanSplit(c.html)
			if got := strings.Join(live, ","); got != c.want {
				t.Fatalf("ScanSplit live = %q, want %q", got, c.want)
			}
			if got := strings.Join(held, ","); got != c.held {
				t.Fatalf("ScanSplit held = %q, want %q", got, c.held)
			}
		})
	}
}

// The ?v= version addresses the bytes a browser caches as immutable,
// so it is taken over the served (minified) sheet. Taken over the
// source, a minifier change would ship new bytes under an old URL.
func TestVersionCoversServedBytes(t *testing.T) {
	reset()
	h := RegisterStyle("c", func(style.Theme) string { return ".x {\n  color: red;\n}\n/* note */" })
	theme := style.DefaultTheme()
	th := themeHash(theme)
	e := h.Entry()
	served := e.ServedCSSForHash(theme, th)
	sum := sha256.Sum256([]byte(th + "\x00" + served))
	if got, want := e.VersionForHash(theme, th), hex.EncodeToString(sum[:6]); got != want {
		t.Fatalf("version %s, want %s (the served bytes' hash)", got, want)
	}
}

// A behaviour registered after the host built its manifest is missing
// from the behaviours block the kernel scans: it is preloaded and served
// but its marker never loads it. Registration past Freeze fails loudly,
// as a late style does.
func TestRegisterBehaviorAfterFreezePanics(t *testing.T) {
	IsolateForTest(t)
	RegisterBehavior("before-freeze", `(function(){})();`, Markers("[data-bf]"))
	Freeze()
	RegisterBehavior("before-freeze", `(function(){})();`, Markers("[data-bf]")) // identical: no-op
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "registered after the host built its manifest") {
			t.Fatalf("panic = %v, want the late-registration refusal", r)
		}
	}()
	RegisterBehavior("late-behavior", `(function(){})();`, Markers("[data-late]"))
}
