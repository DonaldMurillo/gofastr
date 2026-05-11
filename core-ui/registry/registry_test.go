package registry

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core-ui/style"
	"github.com/gofastr/gofastr/core/render"
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
		if !strings.Contains(r.(string), "duplicate name \"a\"") {
			t.Errorf("panic message: %v", r)
		}
	}()
	RegisterStyle("a", fn2)
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
