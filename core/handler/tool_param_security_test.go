package handler

import (
	"errors"
	"fmt"
	"testing"
)

func TestToolParamErrorsUseJSONTypes(t *testing.T) {
	for _, test := range []struct {
		value any
		want  string
	}{
		{[]any{"x"}, "an array is not accepted here"},
		{map[string]any{"x": true}, "an object is not accepted here"},
		{nil, "null is not accepted here"},
		{(*string)(nil), "null is not accepted here"},
		{(*int)(nil), "null is not accepted here"},
		{[]string(nil), "null is not accepted here"},
		{[]byte(nil), "null is not accepted here"},
		{map[string]any(nil), "null is not accepted here"},
	} {
		var input struct {
			Query string `query:"q"`
		}
		err := bindToolParams(t.Context(), map[string]any{"q": test.value}, &input)
		var refusal *Error
		if !errors.As(err, &refusal) || refusal.Code != 400 {
			t.Fatalf("refusal = %v; want typed 400", err)
		}
		if detail := fmt.Sprint(refusal.Params["detail"]); detail != test.want {
			t.Errorf("detail = %q; want %q", detail, test.want)
		}
	}
}

func TestToolParamUnknownTypeIsGeneric(t *testing.T) {
	_, err := toolParamString(func() {})
	if err == nil || err.Error() != "the value could not be decoded" {
		t.Fatalf("unknown type refusal = %v", err)
	}
	var input struct{}
	err = bindToolParams(t.Context(), map[string]any{"q": func() {}}, &input)
	var refusal *Error
	if !errors.As(err, &refusal) || fmt.Sprint(refusal.Params["detail"]) != "the value could not be decoded" {
		t.Fatalf("unencodable params refusal = %v", err)
	}
}

// R8-HD-03: the route pattern on the call context decides a path
// parameter's catch-all flag. A {path...} route's tool twin accepts a
// slash-bearing value the route accepts while still refusing dot-dot
// segments, empty segments and control bytes; no pattern (a tool
// registered by hand) and a single-segment wildcard both stay strict.
func TestToolPathParamFollowsRoutePattern(t *testing.T) {
	type input struct {
		Path string `path:"path"`
	}
	ctx := WithRoutePattern(t.Context(), "/files/{path...}")
	for _, v := range []string{"a/b", "docs/report.pdf", "a/b/c"} {
		var in input
		if err := bindToolParams(ctx, map[string]any{"path": v}, &in); err != nil {
			t.Errorf("catch-all route twin refused %q: %v", v, err)
		} else if in.Path != v {
			t.Errorf("bound %q, want %q", in.Path, v)
		}
	}
	for _, v := range []string{"../etc/passwd", "a/../b", "a//b", "/abs", "a\u0000b", "a\nb"} {
		var in input
		if err := bindToolParams(ctx, map[string]any{"path": v}, &in); err == nil {
			t.Errorf("catch-all route twin accepted %q: the route's sanitiser refuses it", v)
		}
	}
	// No route: strict.
	var strict input
	if err := bindToolParams(t.Context(), map[string]any{"path": "a/b"}, &strict); err == nil {
		t.Error("tool without a route accepted a slash in a path parameter")
	}
	// A single-segment wildcard route: strict too.
	var single input
	if err := bindToolParams(WithRoutePattern(t.Context(), "/files/{name}"), map[string]any{"path": "a/b"}, &single); err == nil {
		t.Error("single-segment route twin accepted a slash in a path parameter")
	}
}
