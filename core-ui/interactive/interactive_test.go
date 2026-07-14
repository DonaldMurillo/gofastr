package interactive

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestOnClickEmitsRPCAttributes(t *testing.T) {
	btn := render.Tag("button", map[string]string{"class": "like-btn"}, render.Text("Like"))
	result := OnClick(btn, Post("/api/like"))
	s := string(result)

	if !strings.Contains(s, `data-fui-rpc="/api/like"`) {
		t.Errorf("missing data-fui-rpc attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-method="POST"`) {
		t.Errorf("missing data-fui-rpc-method attr: %s", s)
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

	if !strings.Contains(s, `data-fui-rpc-signal="count"`) {
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

	if !strings.Contains(s, `data-fui-rpc-signal="result"`) {
		t.Errorf("missing signal attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-close="true"`) {
		t.Errorf("missing close attr: %s", s)
	}
}

func TestOnClickOpenWidget(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Edit"))
	result := OnClick(btn, Post("/api/edit").OnSuccess(OpenWidget("edit-drawer")))
	s := string(result)

	if !strings.Contains(s, `data-fui-rpc-open="edit-drawer"`) {
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

	if !strings.Contains(s, `data-fui-rpc="/api/posts"`) {
		t.Errorf("missing rpc attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-signal="post-result"`) {
		t.Errorf("missing signal attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-close="true"`) {
		t.Errorf("missing close attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-reset="true"`) {
		t.Errorf("missing reset attr: %s", s)
	}
}

func TestNavigate(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Go"))
	result := OnClick(btn, Post("/api/action").OnSuccess(Navigate("/dashboard")))
	s := string(result)

	if !strings.Contains(s, `data-fui-rpc-navigate="/dashboard"`) {
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
		if attrs["data-fui-rpc-method"] != tt.method {
			t.Errorf("%s: got method %q, want %q", tt.method, attrs["data-fui-rpc-method"], tt.method)
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
	// by checking that our data-fui-* keys pass through.
	btn := render.Tag("button", nil, render.Text("Safe"))
	result := OnClick(btn, Post("/api/x").OnSuccess(SetSignal("sig1")))
	s := string(result)

	// data-fui-rpc contains a path with no injection
	if !strings.Contains(s, `data-fui-rpc="/api/x"`) {
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
	if !strings.Contains(s, `data-fui-rpc="/api/t"`) {
		t.Errorf("rpc attr missing: %s", s)
	}
	// Original text preserved.
	if !strings.Contains(s, "Click") {
		t.Errorf("original text lost: %s", s)
	}
}

func TestWrapRawHTMLGTInAttrValue(t *testing.T) {
	// Raw HTML with a literal > inside a quoted attribute — this is the
	// actual bug scenario: the old strings.Index found the > inside the
	// quoted title value instead of the real tag close.
	raw := render.HTML(`<button title="1>2">Click</button>`)
	result := wrapWithAction(raw, Post("/api/raw"))
	s := string(result)

	// RPC attributes must appear before the real '>' that closes the tag,
	// not before the '>' inside the title value. Use Contains because
	// map iteration order is non-deterministic, so the two data-fui-*
	// attributes can appear in either order.
	if !strings.Contains(s, `data-fui-rpc="/api/raw"`) {
		t.Errorf("missing data-fui-rpc attribute:\n got: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-method="POST"`) {
		t.Errorf("missing data-fui-rpc-method attribute:\n got: %s", s)
	}
	if !strings.Contains(s, `title="1>2"`) {
		t.Error(`want title="1>2" preserved in output`)
	}
	if !strings.Contains(s, ">Click</button>") {
		t.Error("want button body preserved")
	}
}

func TestWrapLeadingWhitespace(t *testing.T) {
	html := render.HTML("  <button>x</button>")
	result := wrapWithAction(html, Post("/api/x"))
	s := string(result)

	if !strings.HasPrefix(s, "  <button") {
		t.Errorf("leading whitespace lost: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc="/api/x"`) {
		t.Errorf("rpc attr missing: %s", s)
	}
}

func TestWrapNoTag(t *testing.T) {
	result := wrapWithAction(render.HTML("plain text"), Post("/api/x"))
	if string(result) != "plain text" {
		t.Errorf("plain text should be unchanged, got: %s", result)
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
	if !strings.Contains(string(result), `data-fui-signal-set="tab:settings"`) {
		t.Fatalf("SetLocal missing attribute: %s", result)
	}
	if strings.Contains(string(result), "data-fui-rpc") {
		t.Fatal("SetLocal should not emit data-fui-rpc")
	}
}

func TestIncLocal(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("+"))
	result := IncLocal(btn, "count", 1)
	if !strings.Contains(string(result), `data-fui-signal-inc="count"`) {
		t.Fatalf("IncLocal(delta=1) missing attribute: %s", result)
	}
}

func TestIncLocalWithDelta(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("-"))
	result := IncLocal(btn, "count", -1)
	if !strings.Contains(string(result), `data-fui-signal-inc="count:-1"`) {
		t.Fatalf("IncLocal(delta=-1) missing attribute: %s", result)
	}
}

func TestToggleLocal(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Toggle"))
	result := ToggleLocal(btn, "dark-mode")
	if !strings.Contains(string(result), `data-fui-signal-toggle="dark-mode"`) {
		t.Fatalf("ToggleLocal missing attribute: %s", result)
	}
	if strings.Contains(string(result), "data-fui-rpc") {
		t.Fatal("ToggleLocal should not emit data-fui-rpc")
	}
}

func TestSetLocalOnEmptyHTML(t *testing.T) {
	result := SetLocal(render.HTML(""), "x", "y")
	if result != "" {
		t.Fatalf("empty HTML should stay empty: %q", result)
	}
}

func TestEditToggle(t *testing.T) {
	span := render.Tag("span", nil, render.Text("Click to edit"))
	result := EditToggle(span, "editing")
	s := string(result)
	if !strings.Contains(s, `data-fui-signal-toggle="editing"`) {
		t.Fatalf("EditToggle missing attribute: %s", s)
	}
	if !strings.Contains(s, "Click to edit") {
		t.Fatal("EditToggle lost original content")
	}
	if strings.Contains(s, "data-fui-rpc") {
		t.Fatal("EditToggle should not emit data-fui-rpc")
	}
}

func TestCancelEdit(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Cancel"))
	result := CancelEdit(btn, "editing")
	s := string(result)
	if !strings.Contains(s, `data-fui-signal-set="editing:false"`) {
		t.Fatalf("CancelEdit missing attribute: %s", s)
	}
	if !strings.Contains(s, "Cancel") {
		t.Fatal("CancelEdit lost original content")
	}
	if strings.Contains(s, "data-fui-rpc") {
		t.Fatal("CancelEdit should not emit data-fui-rpc")
	}
}

// ─── LiveSearch tests ─────────────────────────────────────────────────

// ─── Reveal tests ────────────────────────────────────────────────────

func TestRevealInjectsAttr(t *testing.T) {
	div := render.Tag("div", nil, render.Text("hello"))
	result := Reveal(div, "fade-up")
	s := string(result)
	if !strings.Contains(s, `data-fui-reveal="fade-up"`) {
		t.Fatalf("missing data-fui-reveal attr: %s", s)
	}
}

func TestRevealDefaultAnimation(t *testing.T) {
	div := render.Tag("div", nil, render.Text("hello"))
	result := Reveal(div, "")
	s := string(result)
	if !strings.Contains(s, `data-fui-reveal="fade-in"`) {
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
// stamp the data-fui-comp marker so the host loads the registered CSS, and
// that CSS must actually style the hidden/revealed states (without it the
// reveal.js classes are inert and the animation does nothing).
func TestRevealStampsCompMarkerAndCSS(t *testing.T) {
	s := string(Reveal(render.Tag("div", nil, render.Text("x")), "fade-up"))
	if !strings.Contains(s, `data-fui-comp="fui-reveal"`) {
		t.Fatalf("Reveal must stamp data-fui-comp=\"fui-reveal\" so the CSS loads: %s", s)
	}
	css := revealStyle.Entry().CSSFor(style.Theme{})
	for _, sel := range []string{
		`[data-fui-comp="fui-reveal"].fui-hidden{opacity:0}`,
		`.fui-revealed{opacity:1`,
		`prefers-reduced-motion`,
	} {
		if !strings.Contains(css, sel) {
			t.Errorf("reveal CSS missing %q:\n%s", sel, css)
		}
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
	if !strings.Contains(s, `data-fui-comp="fui-dropdown"`) {
		t.Fatalf("Dropdown must stamp the comp marker so its CSS loads: %s", s)
	}
	css := dropdownStyle.Entry().CSSFor(style.Theme{})
	for _, sel := range []string{
		`[data-fui-comp="fui-dropdown"]{position:relative`,
		`[data-fui-dropdown-panel]{position:absolute`,
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
	if !strings.Contains(s, `data-fui-rpc-trigger="input"`) {
		t.Fatalf("missing data-fui-rpc-trigger attr: %s", s)
	}
}

func TestLiveSearchInjectsRPCAttrs(t *testing.T) {
	form := render.Tag("form", nil,
		render.Tag("input", map[string]string{"name": "q", "type": "text"}),
	)
	result := LiveSearch(form, Post("/api/search").OnSuccess(SetSignal("results")), 300)
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc="/api/search"`) {
		t.Fatalf("missing data-fui-rpc attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-method="POST"`) {
		t.Fatalf("missing data-fui-rpc-method attr: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-signal="results"`) {
		t.Fatalf("missing data-fui-rpc-signal attr: %s", s)
	}
}

func TestLiveSearchDefaultDebounce(t *testing.T) {
	form := render.Tag("form", nil,
		render.Tag("input", map[string]string{"name": "q", "type": "text"}),
	)
	result := LiveSearch(form, Post("/api/search"), 0)
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc-debounce-ms="300"`) {
		t.Fatalf("expected default 300ms debounce attr data-fui-rpc-debounce-ms: %s", s)
	}
	// old wrong name must NOT appear
	if strings.Contains(s, `data-fui-rpc-debounce="`) {
		t.Fatalf("emitted stale data-fui-rpc-debounce (without -ms suffix): %s", s)
	}
}

func TestLiveSearchCustomDebounce(t *testing.T) {
	form := render.Tag("form", nil,
		render.Tag("input", map[string]string{"name": "q", "type": "text"}),
	)
	result := LiveSearch(form, Get("/api/search"), 500)
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc-debounce-ms="500"`) {
		t.Fatalf("expected custom 500ms attr data-fui-rpc-debounce-ms: %s", s)
	}
	if strings.Contains(s, `data-fui-rpc-debounce="`) {
		t.Fatalf("should not emit stale data-fui-rpc-debounce (without -ms suffix): %s", s)
	}
}

// TestLiveSearchAttrMatchesRuntime pins that the attr LiveSearch emits
// matches exactly what runtime.js reads (data-fui-rpc-debounce-ms).
func TestLiveSearchAttrMatchesRuntime(t *testing.T) {
	form := render.Tag("form", nil, render.Tag("input", nil))
	s := string(LiveSearch(form, Post("/api/q"), 400))
	// Runtime reads data-fui-rpc-debounce-ms (confirmed in runtime.js line ~448).
	if !strings.Contains(s, `data-fui-rpc-debounce-ms="400"`) {
		t.Fatalf("LiveSearch must emit data-fui-rpc-debounce-ms to match runtime reader: %s", s)
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
	if !strings.Contains(s, `data-fui-comp="ui-optimistic-action"`) {
		t.Fatalf("missing data-fui-comp attr: %s", s)
	}
	// Must start in idle state.
	if !strings.Contains(s, `data-state="idle"`) {
		t.Fatalf("missing data-state=idle: %s", s)
	}
	// Must have the endpoint.
	if !strings.Contains(s, `data-fui-optimistic-endpoint="/api/like/42"`) {
		t.Fatalf("missing endpoint attr: %s", s)
	}
	// POST is the default — method attr should NOT be emitted.
	if strings.Contains(s, `data-fui-optimistic-method`) {
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
	if !strings.Contains(s, `data-fui-optimistic-method="DELETE"`) {
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
	if !strings.Contains(s, `data-fui-optimistic-idle`) {
		t.Fatalf("missing idle wrapper: %s", s)
	}
	if !strings.Contains(s, `♡`) {
		t.Fatalf("missing idle content: %s", s)
	}
	// Success state wrapper with hidden attribute.
	if !strings.Contains(s, `data-fui-optimistic-success`) {
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
		if !strings.Contains(s, fmt.Sprintf(`data-fui-optimistic-endpoint="%s"`, method.path)) {
			t.Fatalf("missing endpoint for %s %s: %s", method.method, method.path, s)
		}
	}
}

// ─── AnimateOnSignal tests ────────────────────────────────────────────────

func TestAnimateOnSignalInjectsAttrs(t *testing.T) {
	div := render.Tag("div", nil, render.Text("panel"))
	result := AnimateOnSignal(div, "open", "fui-slide-down")
	s := string(result)
	if !strings.Contains(s, `data-fui-animate-signal="open"`) {
		t.Fatalf("missing data-fui-animate-signal: %s", s)
	}
	if !strings.Contains(s, `data-fui-animate-class="fui-slide-down"`) {
		t.Fatalf("missing data-fui-animate-class: %s", s)
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
		`data-fui-dropdown`,
		`aria-expanded="false"`,
		`aria-haspopup="true"`,
	} {
		if !strings.Contains(s, attr) {
			t.Errorf("dropdown trigger missing attr %q in:\n%s", attr, s)
		}
	}
}

func TestDropdownPanelAttrs(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Text("Content"))
	result := Dropdown(trigger, panel)
	s := string(result)
	if !strings.Contains(s, `data-fui-dropdown-panel`) {
		t.Errorf("dropdown panel missing data-fui-dropdown-panel in:\n%s", s)
	}
}

func TestDropdownWrapsBoth(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Text("Content"))
	result := Dropdown(trigger, panel)
	s := string(result)
	if !strings.Contains(s, `data-fui-dropdown-wrap`) {
		t.Errorf("dropdown missing wrapper data-fui-dropdown-wrap in:\n%s", s)
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

func TestConfirmEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Delete"))
	result := OnClick(btn, Delete("/api/item/1").OnSuccess(Confirm("Sure?")))
	s := string(result)
	if !strings.Contains(s, `data-fui-confirm="Sure?"`) {
		t.Fatalf("Confirm must emit data-fui-confirm attr: %s", s)
	}
	if strings.Contains(s, "data-fui-rpc-confirm") {
		t.Fatalf("Confirm must NOT emit data-fui-rpc-confirm (wrong prefix): %s", s)
	}
}

func TestWithConfirmEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Delete"))
	result := OnClick(btn, Delete("/api/item/1").WithConfirm("Sure?"))
	s := string(result)
	if !strings.Contains(s, `data-fui-confirm="Sure?"`) {
		t.Fatalf("WithConfirm must emit data-fui-confirm attr: %s", s)
	}
	if strings.Contains(s, "data-fui-rpc-confirm") {
		t.Fatalf("WithConfirm must NOT emit data-fui-rpc-confirm (wrong prefix): %s", s)
	}
}

// WithConfirm and the deprecated Confirm effect emit the identical attribute,
// so an app can migrate without any runtime-visible change.
func TestWithConfirmMatchesConfirmEffect(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Delete"))
	viaMethod := string(OnClick(btn, Delete("/api/x").WithConfirm("Gone?")))
	viaEffect := string(OnClick(btn, Delete("/api/x").OnSuccess(Confirm("Gone?"))))
	if !strings.Contains(viaMethod, `data-fui-confirm="Gone?"`) ||
		!strings.Contains(viaEffect, `data-fui-confirm="Gone?"`) {
		t.Fatalf("both spellings must emit the same attr:\n method=%s\n effect=%s", viaMethod, viaEffect)
	}
}

func TestAfterTextEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Save"))
	result := OnClick(btn, Post("/api/save").OnSuccess(AfterText("Saved ✓")))
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc-after-text="Saved ✓"`) {
		t.Fatalf("AfterText must emit data-fui-rpc-after-text attr: %s", s)
	}
}

func TestAfterDisableEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Submit"))
	result := OnClick(btn, Post("/api/submit").OnSuccess(AfterDisable()))
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc-after-disable`) {
		t.Fatalf("AfterDisable must emit data-fui-rpc-after-disable attr: %s", s)
	}
}

func TestScrollToEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Add"))
	result := OnClick(btn, Post("/api/add").OnSuccess(ScrollTo("#results")))
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc-scroll-to="#results"`) {
		t.Fatalf("ScrollTo must emit data-fui-rpc-scroll-to attr: %s", s)
	}
}

func TestPushStateEmitsAttr(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Page 2"))
	result := OnClick(btn, Post("/api/page").OnSuccess(PushState("?p=2")))
	s := string(result)
	if !strings.Contains(s, `data-fui-push-state="?p=2"`) {
		t.Fatalf("PushState must emit data-fui-push-state attr: %s", s)
	}
	if strings.Contains(s, "data-fui-rpc-push-state") {
		t.Fatalf("PushState must NOT emit data-fui-rpc-push-state (wrong name): %s", s)
	}
}

func TestAfterTextAndDisableCombine(t *testing.T) {
	btn := render.Tag("button", nil, render.Text("Send"))
	result := OnClick(btn, Post("/api/send").OnSuccess(AfterText("Sent ✓"), AfterDisable()))
	s := string(result)
	if !strings.Contains(s, `data-fui-rpc-after-text="Sent ✓"`) {
		t.Fatalf("missing after-text: %s", s)
	}
	if !strings.Contains(s, `data-fui-rpc-after-disable`) {
		t.Fatalf("missing after-disable: %s", s)
	}
}

func TestConfirmNoPanicEmptyMessage(t *testing.T) {
	// Empty message is allowed (runtime will show blank confirm dialog).
	btn := render.Tag("button", nil, render.Text("x"))
	result := OnClick(btn, Delete("/api/x").OnSuccess(Confirm("")))
	if !strings.Contains(string(result), `data-fui-confirm=""`) {
		t.Fatalf("empty confirm message should still emit attr: %s", result)
	}
}

func TestDropdownPanelInitiallyHidden(t *testing.T) {
	trigger := render.Tag("button", nil, render.Text("Menu"))
	panel := render.Tag("div", nil, render.Text("Content"))
	result := Dropdown(trigger, panel)
	s := string(result)
	// The panel's <div> should have a hidden attribute.
	// Find data-fui-dropdown-panel and check hidden is on same tag.
	idx := strings.Index(s, `data-fui-dropdown-panel`)
	if idx == -1 {
		t.Fatalf("missing data-fui-dropdown-panel")
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
