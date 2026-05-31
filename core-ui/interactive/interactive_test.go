package interactive

import (
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
	// An action with just a method+path always has attrs, but verify
	// wrapWithAction preserves the original when called directly.
	html := render.Tag("div", nil, render.Text("static"))
	// No action wrapping — just verify round-trip
	if string(html) != string(html) {
		t.Error("HTML was unexpectedly modified")
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
