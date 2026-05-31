package interactive

import (
	"fmt"
	"strings"
	"testing"

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
		render.Tag("input", map[string]string{"name": "title"},),
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
	// not before the '>' inside the title value.
	wantPrefix := `<button title="1>2" data-fui-rpc="/api/raw" data-fui-rpc-method="POST">Click</button>`
	if s != wantPrefix {
		t.Errorf("wrong result:\n got: %s\nwant: %s", s, wantPrefix)
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
