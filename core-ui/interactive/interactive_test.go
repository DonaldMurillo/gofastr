package interactive

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestOnClickEmitsRPCAttributes(t *testing.T) {
	btn := render.Tag("button", map[string]string{"class": "like-btn"}, render.Text("Like"))
	result := OnClick(btn, Post("/api/like"))
	s := string(result)

	if !strings.Contains(s, `data-cui-rpc="/api/like"`) {
		t.Errorf("missing data-cui-rpc attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-method="POST"`) {
		t.Errorf("missing data-cui-rpc-method attr: %s", s)
	}
	// Original content preserved
	if !strings.Contains(s, "Like") {
		t.Errorf("original text lost: %s", s)
	}
	if !strings.Contains(s, `class="like-btn"`) {
		t.Errorf("original class lost: %s", s)
	}
}

func TestOnClickWithSignal(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Vote"))
	result := OnClick(btn, Post("/api/vote").OnSuccess(SetSignal("count")))
	s := string(result)

	if !strings.Contains(s, `data-cui-rpc-signal="count"`) {
		t.Errorf("missing signal attr: %s", s)
	}
}

func TestOnClickChainsMultipleEffects(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Save"))
	result := OnClick(btn, Post("/api/save").OnSuccess(
		SetSignal("result"),
		CloseWidget(),
	))
	s := string(result)

	if !strings.Contains(s, `data-cui-rpc-signal="result"`) {
		t.Errorf("missing signal attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-close="true"`) {
		t.Errorf("missing close attr: %s", s)
	}
}

func TestOnClickOpenWidget(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Edit"))
	result := OnClick(btn, Post("/api/edit").OnSuccess(OpenWidget("edit-drawer")))
	s := string(result)

	if !strings.Contains(s, `data-cui-rpc-open="edit-drawer"`) {
		t.Errorf("missing rpc-open attr: %s", s)
	}
}

func TestOnSubmitForm(t *testing.T) {
	form := render.Tag("form", nil,
		render.Tag("input", map[string]string{"name": "title"}),
		render.Tag("button", nil, render.Text("Submit")),
	)
	result := OnSubmit(form, Post("/api/posts").OnSuccess(
		SetSignal("post-result"),
		CloseWidget(),
		ResetForm(),
	))
	s := string(result)

	if !strings.Contains(s, `data-cui-rpc="/api/posts"`) {
		t.Errorf("missing rpc attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-signal="post-result"`) {
		t.Errorf("missing signal attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-close="true"`) {
		t.Errorf("missing close attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-reset="true"`) {
		t.Errorf("missing reset attr: %s", s)
	}
}

func TestNavigate(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Go"))
	result := OnClick(btn, Post("/api/action").OnSuccess(Navigate("/dashboard")))
	s := string(result)

	if !strings.Contains(s, `data-cui-rpc-navigate="/dashboard"`) {
		t.Errorf("missing navigate attr: %s", s)
	}
}

func TestHTTPMethods(t *testing.T) {
	tests := []struct {
		action Action
		method string
	}{
		{Get("/api/x"), "GET"},
		{Post("/api/x"), "POST"},
		{Put("/api/x"), "PUT"},
		{Delete("/api/x"), "DELETE"},
		{Patch("/api/x"), "PATCH"},
	}
	for _, tt := range tests {
		attrs := tt.action.attrs()
		if attrs["data-cui-rpc-method"] != tt.method {
			t.Errorf("%s: got method %q, want %q", tt.method, attrs["data-cui-rpc-method"], tt.method)
		}
	}
}

func TestEmptyActionNoModification(t *testing.T) {
	html := render.Tag("div", nil, render.Text("static"))
	result := wrapWithAction(html, Action{})
	if string(result) != string(html) {
		t.Errorf("empty action should not modify HTML:\n got: %s\nwant: %s", result, html)
	}
}

func TestAttrSafety(t *testing.T) {
	// Verify that render.Attr is used (drops unsafe keys)
	// by checking that our data-cui-* keys pass through.
	btn := render.Tag("button", nil, render.Text("Safe"))
	result := OnClick(btn, Post("/api/x").OnSuccess(SetSignal("sig1")))
	s := string(result)

	// data-cui-rpc contains a path with no injection
	if !strings.Contains(s, `data-cui-rpc="/api/x"`) {
		t.Errorf("safe attr dropped: %s", s)
	}
}

func TestWrapEmptyHTML(t *testing.T) {
	result := wrapWithAction(render.HTML(""), Post("/api/x"))
	if string(result) != "" {
		t.Errorf("empty input should return empty, got: %s", result)
	}
}

func TestWrapTextNodeOnly(t *testing.T) {
	result := wrapWithAction(render.Text("hello"), Post("/api/x"))
	if string(result) != "hello" {
		t.Errorf("text node should be unchanged, got: %s", result)
	}
}

func TestWrapGTInAttributeValue(t *testing.T) {
	// render.Tag HTML-escapes the > to &gt; inside attribute values,
	// but the scanner must still correctly find the real tag-closing '>'.
	btn := render.Tag("button", map[string]string{"title": "1>2"}, render.Text("Click"))
	result := wrapWithAction(btn, Post("/api/t"))
	s := string(result)

	// The title attribute must survive intact (HTML-escaped).
	if !strings.Contains(s, `title="1&gt;2"`) {
		t.Errorf("attribute with > broken: %s", s)
	}
	// RPC attributes must appear after the full opening tag.
	if !strings.Contains(s, `data-cui-rpc="/api/t"`) {
		t.Errorf("rpc attr missing: %s", s)
	}
	// Original text preserved.
	if !strings.Contains(s, "Click") {
		t.Errorf("original text lost: %s", s)
	}
}

func TestWrapRawHTMLGTInAttrValue(t *testing.T) {
	// Raw HTML with a literal > inside a quoted attribute, this is the
	// actual bug scenario: the old strings.Index found the > inside the
	// quoted title value instead of the real tag close.
	raw := render.HTML(`<button title="1>2">Click</button>`)
	result := wrapWithAction(raw, Post("/api/raw"))
	s := string(result)

	// RPC attributes must appear before the real '>' that closes the tag,
	// not before the '>' inside the title value. Use Contains because
	// map iteration order is non-deterministic, so the two data-cui-*
	// attributes can appear in either order.
	if !strings.Contains(s, `data-cui-rpc="/api/raw"`) {
		t.Errorf("missing data-cui-rpc attribute:\n got: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-method="POST"`) {
		t.Errorf("missing data-cui-rpc-method attribute:\n got: %s", s)
	}
	if !strings.Contains(s, `title="1>2"`) {
		t.Error(`want title="1>2" preserved in output`)
	}
	if !strings.Contains(s, ">Click</button>") {
		t.Error("want button body preserved")
	}
}

func TestInteractiveAttributesReachRootAfterComments(t *testing.T) {
	const button = render.HTML(`<!-- note --><button title="1>2">Save</button>`)
	if got := OnClick(button, Post("/api/save")); got != `<!-- note --><button title="1>2" data-cui-rpc="/api/save" data-cui-rpc-method="POST">Save</button>` {
		t.Errorf("OnClick = %s", got)
	}
	const input = render.HTML(`<!-- note --><input />`)
	if got := SetLocal(input, "filter", `a"&b`); got != render.HTML(`<!-- note --><input `+render.Attr("data-cui-signal-set", `filter:a"&b`)+` />`) {
		t.Errorf("SetLocal = %s", got)
	}
	if got := Dropdown(button, input); !strings.Contains(string(got), `<!-- note --><input data-cui-dropdown-panel="" hidden="" />`) {
		t.Errorf("Dropdown panel = %s", got)
	}
	if got := registry.AppendAttributes(input, map[string]string{"oninput": "ignored"}); got != input {
		t.Errorf("unsafe attribute was not dropped: %s", got)
	}
}

func TestOnSuccessDerivationsKeepIndependentEffects(t *testing.T) {
	base := Post("/api/save").OnSuccess(OpenWidget("saved"))
	base.effects = append(make([]Effect, 0, 2), base.effects...)
	first := base.OnSuccess(SetSignal("first"))
	second := base.OnSuccess(SetSignal("second"))
	if got := first.Attrs()["data-cui-rpc-signal"]; got != "first" {
		t.Errorf("first derivative writes %q, want first", got)
	}
	if got := second.Attrs()["data-cui-rpc-signal"]; got != "second" {
		t.Errorf("second derivative writes %q, want second", got)
	}
	if _, ok := base.Attrs()["data-cui-rpc-signal"]; ok {
		t.Error("deriving actions changed the base")
	}
}

func TestWrapLeadingWhitespace(t *testing.T) {
	html := render.HTML("  <button>x</button>")
	result := wrapWithAction(html, Post("/api/x"))
	s := string(result)

	if !strings.HasPrefix(s, "  <button") {
		t.Errorf("leading whitespace lost: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc="/api/x"`) {
		t.Errorf("rpc attr missing: %s", s)
	}
}

func TestPathValidation(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for path without leading /")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "must start with '/'") {
			t.Errorf("panic message wrong: %s", msg)
		}
	}()
	Post("no-slash")
}

func TestSetSignalValidation(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for signal name with quote")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "must not contain") {
			t.Errorf("panic message wrong: %s", msg)
		}
	}()
	SetSignal(`bad"name`)
}

// ─── Client-side signal mutation tests ────────────────────────────────

func TestSetLocal(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Set"))
	result := SetLocal(btn, "tab", "settings")
	if !strings.Contains(string(result), `data-cui-signal-set="tab:settings"`) {
		t.Fatalf("SetLocal missing attribute: %s", result)
	}
	if strings.Contains(string(result), "data-cui-rpc") {
		t.Fatal("SetLocal should not emit data-cui-rpc")
	}
}

func TestIncLocal(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("+"))
	result := IncLocal(btn, "count", 1)
	if !strings.Contains(string(result), `data-cui-signal-inc="count"`) {
		t.Fatalf("IncLocal(delta=1) missing attribute: %s", result)
	}
}

func TestIncLocalWithDelta(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("-"))
	result := IncLocal(btn, "count", -1)
	if !strings.Contains(string(result), `data-cui-signal-inc="count:-1"`) {
		t.Fatalf("IncLocal(delta=-1) missing attribute: %s", result)
	}
}

func TestToggleLocal(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Toggle"))
	result := ToggleLocal(btn, "dark-mode")
	if !strings.Contains(string(result), `data-cui-signal-toggle="dark-mode"`) {
		t.Fatalf("ToggleLocal missing attribute: %s", result)
	}
	if strings.Contains(string(result), "data-cui-rpc") {
		t.Fatal("ToggleLocal should not emit data-cui-rpc")
	}
}

func TestSetLocalOnEmptyHTML(t *testing.T) {
	result := SetLocal(render.HTML(""), "x", "y")
	if result != "" {
		t.Fatalf("empty HTML should stay empty: %q", result)
	}
}

// assertSignalWrap checks an interactive wrapper: it must carry its
// signal attribute, keep the wrapped content, and never emit an RPC
// attribute (these wrappers are purely client-side).
func assertSignalWrap(t *testing.T, name, attr, content string, got render.HTML) {
	t.Helper()
	s := string(got)
	if !strings.Contains(s, attr) {
		t.Fatalf("%s missing attribute: %s", name, s)
	}
	if !strings.Contains(s, content) {
		t.Fatalf("%s lost original content", name)
	}
	if strings.Contains(s, "data-cui-rpc") {
		t.Fatalf("%s should not emit data-cui-rpc", name)
	}
}

func TestEditToggle(t *testing.T) {
	span := render.Tag("span", nil, render.Text("Click to edit"))
	assertSignalWrap(t, "EditToggle", `data-cui-signal-toggle="editing"`, "Click to edit", EditToggle(span, "editing"))
}

func TestCancelEdit(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Cancel"))
	assertSignalWrap(t, "CancelEdit", `data-cui-signal-set="editing:false"`, "Cancel", CancelEdit(btn, "editing"))
}

// ─── LiveSearch tests ─────────────────────────────────────────────────

// ─── Reveal tests ────────────────────────────────────────────────────

func TestRevealInjectsAttr(t *testing.T) {
	div := render.Tag("div", nil, render.Text("hello"))
	result := Reveal(div, "fade-up")
	s := string(result)
	if !strings.Contains(s, `data-cui-reveal="fade-up"`) {
		t.Fatalf("missing data-cui-reveal attr: %s", s)
	}
}

func TestRevealDefaultAnimation(t *testing.T) {
	div := render.Tag("div", nil, render.Text("hello"))
	result := Reveal(div, "")
	s := string(result)
	if !strings.Contains(s, `data-cui-reveal="fade-in"`) {
		t.Fatalf("expected default fade-in animation: %s", s)
	}
}

func TestRevealPreservesContent(t *testing.T) {
	div := render.Tag("div", map[string]string{"class": "card"}, render.Text("Content"))
	result := Reveal(div, "slide-left")
	s := string(result)
	if !strings.Contains(s, "Content") {
		t.Fatalf("original content lost: %s", s)
	}
	if !strings.Contains(s, `class="card"`) {
		t.Fatalf("original class lost: %s", s)
	}
}

// TestRevealStampsCompMarkerAndCSS guards the framework fix: Reveal must
// stamp the data-cui-comp marker so the host loads the registered CSS, and
// that CSS must actually style the hidden/revealed states (without it the
// reveal.js classes are inert and the animation does nothing).
func TestRevealStampsCompMarkerAndCSS(t *testing.T) {
	s := string(Reveal(render.Tag("div", nil, render.Text("x")), "fade-up"))
	if !strings.Contains(s, `data-cui-comp="cui-reveal"`) {
		t.Fatalf("Reveal must stamp data-cui-comp=\"cui-reveal\" so the CSS loads: %s", s)
	}
	css := revealStyle.Entry().CSSFor(style.Theme{})
	for _, sel := range []string{
		`[data-cui-reveal].cui-hidden{opacity:0}`,
		`.cui-revealed{opacity:1`,
		`prefers-reduced-motion`,
	} {
		if !strings.Contains(css, sel) {
			t.Errorf("reveal CSS missing %q:\n%s", sel, css)
		}
	}
}

func TestRevealLoadsCSSForStyledRoot(t *testing.T) {
	const html = render.HTML(`<section data-cui-comp="existing-card">Card</section>`)
	got := Reveal(html, "fade-up")
	if !strings.Contains(string(got), `<section data-cui-comp="existing-card" data-cui-reveal="fade-up"`) {
		t.Fatalf("the styled section must still be the reveal target: %s", got)
	}
	names := registry.Scan(string(got))
	if !slices.Contains(names, "existing-card") || !slices.Contains(names, "cui-reveal") {
		t.Errorf("styles discovered for the section = %v, want the original and reveal sheets", names)
	}
	css := revealStyle.Entry().CSSFor(style.Theme{})
	if !strings.Contains(css, `[data-cui-scope="cui-reveal"])[data-cui-reveal].cui-hidden{opacity:0}`) {
		t.Errorf("reveal CSS does not target the scoped behavior marker: %s", css)
	}

	const owned = render.HTML(`<section data-cui-comp="existing-card" data-cui-scope="existing-owner">Card</section>`)
	wrapped := Reveal(owned, "fade-up")
	if !strings.Contains(string(wrapped), `<div data-cui-reveal="fade-up" data-cui-comp="cui-reveal">`+string(owned)+`</div>`) {
		t.Errorf("Reveal must preserve both markers and animate the surrounding element: %s", wrapped)
	}
}

// TestDropdownStampsCompMarkerAndCSS guards that Dropdown renders as a
// real floating menu: the wrap carries the comp marker and the CSS
// positions the panel as an absolute, shadowed surface (without it the
// panel is a flat, full-width, unstyled strip).
func TestDropdownStampsCompMarkerAndCSS(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Tag("a", map[string]string{"href": "#"}, render.Text("Edit")))
	s := string(Dropdown(trigger, panel))
	if !strings.Contains(s, `data-cui-comp="cui-dropdown"`) {
		t.Fatalf("Dropdown must stamp the comp marker so its CSS loads: %s", s)
	}
	css := dropdownStyle.Entry().CSSFor(style.Theme{})
	for _, sel := range []string{
		`[data-cui-comp="cui-dropdown"]{position:relative`,
		`[data-cui-dropdown-panel]{position:absolute`,
		`box-shadow:`,
	} {
		if !strings.Contains(css, sel) {
			t.Errorf("dropdown CSS missing %q:\n%s", sel, css)
		}
	}
}

func TestLiveSearchInjectsTriggerAttr(t *testing.T) {
	form := render.Tag("form", nil,
		render.Tag("input", map[string]string{"name": "q", "type": "text"}),
	)
	result := LiveSearch(form, Post("/api/search").OnSuccess(SetSignal("results")), 300)
	s := string(result)
	if !strings.Contains(s, `data-cui-rpc-trigger="input"`) {
		t.Fatalf("missing data-cui-rpc-trigger attr: %s", s)
	}
}

func TestLiveSearchInjectsRPCAttrs(t *testing.T) {
	form := render.Tag("form", nil,
		render.Tag("input", map[string]string{"name": "q", "type": "text"}),
	)
	result := LiveSearch(form, Post("/api/search").OnSuccess(SetSignal("results")), 300)
	s := string(result)
	if !strings.Contains(s, `data-cui-rpc="/api/search"`) {
		t.Fatalf("missing data-cui-rpc attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-method="POST"`) {
		t.Fatalf("missing data-cui-rpc-method attr: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-signal="results"`) {
		t.Fatalf("missing data-cui-rpc-signal attr: %s", s)
	}
}

func TestLiveSearchDebounce(t *testing.T) {
	cases := []struct {
		name   string
		action Action
		input  int
		want   string
	}{
		{"default 250ms", Post("/api/search"), 0, `data-cui-rpc-debounce-ms="250"`},
		{"custom 500ms", Get("/api/search"), 500, `data-cui-rpc-debounce-ms="500"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			form := render.Tag("form", nil,
				render.Tag("input", map[string]string{"name": "q", "type": "text"}),
			)
			s := string(LiveSearch(form, c.action, c.input))
			if !strings.Contains(s, c.want) {
				t.Fatalf("expected debounce attr %s: %s", c.want, s)
			}
			// old wrong name must NOT appear
			if strings.Contains(s, `data-cui-rpc-debounce="`) {
				t.Fatalf("emitted stale data-cui-rpc-debounce (without -ms suffix): %s", s)
			}
		})
	}
}

// TestLiveSearchAttrMatchesRuntime pins that the attr LiveSearch emits
// matches exactly what runtime.js reads (data-cui-rpc-debounce-ms).
func TestLiveSearchAttrMatchesRuntime(t *testing.T) {
	form := render.Tag("form", nil, render.Tag("input", nil))
	s := string(LiveSearch(form, Post("/api/q"), 400))
	// Runtime reads data-cui-rpc-debounce-ms (confirmed in runtime.js line ~448).
	if !strings.Contains(s, `data-cui-rpc-debounce-ms="400"`) {
		t.Fatalf("LiveSearch must emit data-cui-rpc-debounce-ms to match runtime reader: %s", s)
	}
}

// ─── OptimisticUpdate tests ────────────────────────────────────────────

func TestOptimisticUpdateRendersComponentAttrs(t *testing.T) {
	result := OptimisticUpdate(
		Post("/api/like/42"),
		render.HTML("♡ Like"),
		render.HTML("♥ Liked"),
	)
	s := string(result)
	// Must have the component marker the runtime scans for.
	if !strings.Contains(s, `data-hui-action=""`) {
		t.Fatalf("missing data-cui-comp attr: %s", s)
	}
	// Must start in idle state.
	if !strings.Contains(s, `data-state="idle"`) {
		t.Fatalf("missing data-state=idle: %s", s)
	}
	// Must have the endpoint.
	if !strings.Contains(s, `data-hui-action-endpoint="/api/like/42"`) {
		t.Fatalf("missing endpoint attr: %s", s)
	}
	if !strings.Contains(s, `data-hui-action-failed="Could not save. Try again."`) ||
		!strings.Contains(s, `<span data-hui-action-status="" role="status"></span>`) {
		t.Fatalf("missing failure announcement: %s", s)
	}
	// POST is the default, method attr should NOT be emitted.
	if strings.Contains(s, `data-hui-action-method`) {
		t.Fatalf("POST default should not emit method attr: %s", s)
	}
}

func TestOptimisticUpdateNonPostEmitsMethod(t *testing.T) {
	result := OptimisticUpdate(
		Delete("/api/like/42"),
		render.HTML("♡ Like"),
		render.HTML("♥ Liked"),
	)
	s := string(result)
	if !strings.Contains(s, `data-hui-action-method="DELETE"`) {
		t.Fatalf("non-POST should emit method attr: %s", s)
	}
}

func TestOptimisticUpdateContainsBothVisualStates(t *testing.T) {
	result := OptimisticUpdate(
		Post("/api/like/42"),
		render.HTML(`<span class="icon">♡</span> Like`),
		render.HTML(`<span class="icon">♥</span> Liked`),
	)
	s := string(result)
	// Idle state wrapper.
	if !strings.Contains(s, `data-hui-action-idle`) {
		t.Fatalf("missing idle wrapper: %s", s)
	}
	if !strings.Contains(s, `♡`) {
		t.Fatalf("missing idle content: %s", s)
	}
	// Success state wrapper with hidden attribute.
	if !strings.Contains(s, `data-hui-action-done`) {
		t.Fatalf("missing success wrapper: %s", s)
	}
	if !strings.Contains(s, `hidden`) {
		t.Fatalf("success wrapper should start hidden: %s", s)
	}
	if !strings.Contains(s, `♥`) {
		t.Fatalf("missing success content: %s", s)
	}
}

func TestOptimisticUpdateRendersButton(t *testing.T) {
	result := OptimisticUpdate(
		Post("/api/star/99"),
		render.Text("Star"),
		render.Text("Starred"),
	)
	s := string(result)
	if !strings.HasPrefix(s, "<button") {
		t.Fatalf("should be a button element: %s", s)
	}
	if !strings.Contains(s, "</button>") {
		t.Fatalf("button should be properly closed: %s", s)
	}
}

func TestOptimisticUpdateEndpointFromAction(t *testing.T) {
	for _, method := range []Action{Get("/a"), Put("/b"), Patch("/c"), Delete("/d")} {
		result := OptimisticUpdate(method, render.HTML("idle"), render.HTML("done"))
		s := string(result)
		if !strings.Contains(s, fmt.Sprintf(`data-hui-action-endpoint="%s"`, method.path)) {
			t.Fatalf("missing endpoint for %s %s: %s", method.method, method.path, s)
		}
	}
}

// ─── AnimateOnSignal tests ────────────────────────────────────────────────

func TestAnimateOnSignalInjectsAttrs(t *testing.T) {
	div := render.Tag("div", nil, render.Text("panel"))
	result := AnimateOnSignal(div, "open", "fui-slide-down")
	s := string(result)
	if !strings.Contains(s, `data-cui-animate-signal="open"`) {
		t.Fatalf("missing data-cui-animate-signal: %s", s)
	}
	if !strings.Contains(s, `data-cui-animate-class="fui-slide-down"`) {
		t.Fatalf("missing data-cui-animate-class: %s", s)
	}
}

func TestAnimateOnSignalPreservesContent(t *testing.T) {
	div := render.Tag("div", nil, render.Text("inner content"))
	result := AnimateOnSignal(div, "visible", "fui-fade")
	s := string(result)
	if !strings.Contains(s, "inner content") {
		t.Fatalf("original content lost: %s", s)
	}
}

func TestAnimateOnSignalValidation(t *testing.T) {
	div := render.Tag("div", nil, render.Text("x"))
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty signal name")
		}
	}()
	AnimateOnSignal(div, "", "fui-slide")
}

func TestAnimateOnSignalEmptyClassPanics(t *testing.T) {
	div := render.Tag("div", nil, render.Text("x"))
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for empty css class")
		}
	}()
	AnimateOnSignal(div, "sig", "")
}

// ─── Dropdown tests ────────────────────────────────────────────────────

func TestDropdownTriggerAttrs(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Text("Content"))
	result := Dropdown(trigger, panel)
	s := string(result)
	for _, attr := range []string{
		`data-cui-dropdown`,
		`aria-expanded="false"`,
		`aria-haspopup="true"`,
	} {
		if !strings.Contains(s, attr) {
			t.Errorf("dropdown trigger missing attr %q in:\n%s", attr, s)
		}
	}
}

func TestDropdownWrapsBoth(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Text("Content"))
	result := Dropdown(trigger, panel)
	s := string(result)
	if !strings.Contains(s, `data-cui-dropdown-wrap`) {
		t.Errorf("dropdown missing wrapper data-cui-dropdown-wrap in:\n%s", s)
	}
	if !strings.Contains(s, "Menu") {
		t.Errorf("dropdown missing trigger content in:\n%s", s)
	}
	if !strings.Contains(s, "Content") {
		t.Errorf("dropdown missing panel content in:\n%s", s)
	}
}

// ─── D4: Typed interactive effects ──────────────────────────────────────────
// Failing tests that pin the exact runtime-read attribute name for each effect.

func TestWithConfirmEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Delete"))
	result := OnClick(btn, Delete("/api/item/1").WithConfirm("Sure?"))
	s := string(result)
	if !strings.Contains(s, `data-cui-confirm="Sure?"`) {
		t.Fatalf("WithConfirm must emit data-cui-confirm attr: %s", s)
	}
	if strings.Contains(s, "data-cui-rpc-confirm") {
		t.Fatalf("WithConfirm must NOT emit data-cui-rpc-confirm (wrong prefix): %s", s)
	}
}

func TestOnErrorToastEmitsAttr(t *testing.T) {
	attrs := Delete("/api/item/1").OnErrorToast("Could not delete.").Attrs()
	if got := attrs["data-cui-rpc-error-toast"]; got != "Could not delete." {
		t.Fatalf("OnErrorToast attr = %q, want the title: %v", got, attrs)
	}
	if _, ok := Delete("/api/item/1").Attrs()["data-cui-rpc-error-toast"]; ok {
		t.Fatal("an action without OnErrorToast must not carry the hook")
	}
}

func TestOnSuccessToastEmitsAttr(t *testing.T) {
	attrs := Put("/api/item/1").
		OnSuccessToast("Saved").
		OnSuccess(Navigate("/items/1")).Attrs()
	if got := attrs["data-cui-rpc-success-toast"]; got != "Saved" {
		t.Fatalf("OnSuccessToast attr = %q, want the title: %v", got, attrs)
	}
	if _, ok := Put("/api/item/1").Attrs()["data-cui-rpc-success-toast"]; ok {
		t.Fatal("an action without OnSuccessToast must not carry the hook")
	}
}

// The toast's action rides as JSON: its label and the next action's RPC
// attributes. A confirm on the next action, no label, or no success
// toast to ride is refused.
func TestSuccessToastActionEmitsAttr(t *testing.T) {
	attrs := Delete("/api/item/1").OnSuccessToast("Deleted").
		OnSuccessToastAction("Undo", Post("/api/item/1/_restore").WithBody(`{}`).
			OnSuccess(SetSignal("item")).OnSuccessToast("Restored")).Attrs()
	var got ToastAction
	if err := json.Unmarshal([]byte(attrs["data-cui-rpc-success-action"]), &got); err != nil {
		t.Fatalf("success action is not JSON: %v (%v)", err, attrs)
	}
	want := map[string]string{"data-cui-rpc": "/api/item/1/_restore", "data-cui-rpc-method": "POST",
		"data-cui-rpc-body": "{}", "data-cui-rpc-success-toast": "Restored", "data-cui-rpc-signal": "item"}
	if got.Label != "Undo" || !maps.Equal(got.Attrs, want) {
		t.Fatalf("success action = %+v, want Undo over %v", got, want)
	}
	if _, ok := Delete("/api/item/1").Attrs()["data-cui-rpc-success-action"]; ok {
		t.Fatal("an action without OnSuccessToastAction must not carry the hook")
	}
	for name, f := range map[string]func(){
		"confirm":     func() { Delete("/x").OnSuccessToastAction("Undo", Post("/y").WithConfirm("Sure?")) },
		"no label":    func() { Delete("/x").OnSuccessToastAction("", Post("/y")) },
		"no endpoint": func() { Delete("/x").OnSuccessToastAction("Undo", Action{}) },
		"no toast":    func() { Delete("/x").OnSuccessToastAction("Undo", Post("/y")).Attrs() },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: OnSuccessToastAction did not refuse", name)
				}
			}()
			f()
		}()
	}
}

func TestAfterTextEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Save"))
	result := OnClick(btn, Post("/api/save").OnSuccess(AfterText("Saved ✓")))
	s := string(result)
	if !strings.Contains(s, `data-cui-rpc-after-text="Saved ✓"`) {
		t.Fatalf("AfterText must emit data-cui-rpc-after-text attr: %s", s)
	}
}

func TestAfterDisableEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Submit"))
	result := OnClick(btn, Post("/api/submit").OnSuccess(AfterDisable()))
	s := string(result)
	if !strings.Contains(s, `data-cui-rpc-after-disable`) {
		t.Fatalf("AfterDisable must emit data-cui-rpc-after-disable attr: %s", s)
	}
}

func TestScrollToEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Add"))
	result := OnClick(btn, Post("/api/add").OnSuccess(ScrollTo("#results")))
	s := string(result)
	if !strings.Contains(s, `data-cui-rpc-scroll-to="#results"`) {
		t.Fatalf("ScrollTo must emit data-cui-rpc-scroll-to attr: %s", s)
	}
}

func TestPushStateEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Page 2"))
	result := OnClick(btn, Post("/api/page").OnSuccess(PushState("?p=2")))
	s := string(result)
	if !strings.Contains(s, `data-cui-push-state="?p=2"`) {
		t.Fatalf("PushState must emit data-cui-push-state attr: %s", s)
	}
	if strings.Contains(s, "data-cui-rpc-push-state") {
		t.Fatalf("PushState must NOT emit data-cui-rpc-push-state (wrong name): %s", s)
	}
}

func TestAfterTextAndDisableCombine(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Send"))
	result := OnClick(btn, Post("/api/send").OnSuccess(AfterText("Sent ✓"), AfterDisable()))
	s := string(result)
	if !strings.Contains(s, `data-cui-rpc-after-text="Sent ✓"`) {
		t.Fatalf("missing after-text: %s", s)
	}
	if !strings.Contains(s, `data-cui-rpc-after-disable`) {
		t.Fatalf("missing after-disable: %s", s)
	}
}

func TestDropdownPanelInitiallyHidden(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Text("Content"))
	result := Dropdown(trigger, panel)
	s := string(result)
	// The panel's <div> should have a hidden attribute.
	// Find data-cui-dropdown-panel and check hidden is on same tag.
	idx := strings.Index(s, `data-cui-dropdown-panel`)
	if idx == -1 {
		t.Fatalf("missing data-cui-dropdown-panel")
	}
	// Look backwards for the opening < and forwards for > around this attr.
	tagStart := strings.LastIndex(s[:idx], "<")
	tagEnd := strings.Index(s[idx:], ">")
	if tagStart == -1 || tagEnd == -1 {
		t.Fatalf("cannot locate panel tag boundaries")
	}
	panelTag := s[tagStart : idx+tagEnd+1]
	if !strings.Contains(panelTag, `hidden`) {
		t.Errorf("panel tag should have hidden attr, got:\n%s", panelTag)
	}
}

// Dropdown belongs to the interactive set whose host override surface is the
// legacy --fui-* bridge. Every bridge read must chain to the canonical
// adaptive --color-* token before its literal fallback so the theme reaches
// the panel without host aliases while host --fui-* overrides keep winning
// (framework/ui's TestFuiBridgeChainsToColorTokens is the sibling sweep).
func TestDropdownFuiBridgeChainsToColorTokens(t *testing.T) {
	css := dropdownCSS(style.Theme{})
	if !strings.Contains(css, "var(--fui-") {
		t.Fatal("dropdown CSS lost its --fui-* bridge reads")
	}
	re := regexp.MustCompile(`var\(--fui-(?:border|foreground|muted-bg|muted|primary|surface)\s*,\s*([^)\s,]+)`)
	for _, m := range re.FindAllStringSubmatch(css, -1) {
		if !strings.HasPrefix(m[1], "var(--color-") {
			t.Errorf("dropdown: %q must chain to a canonical --color-* token before its literal fallback", m[0])
		}
	}
}
