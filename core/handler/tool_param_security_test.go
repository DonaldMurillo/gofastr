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
		err := bindToolParams(map[string]any{"q": test.value}, &input)
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
	err = bindToolParams(map[string]any{"q": func() {}}, &input)
	var refusal *Error
	if !errors.As(err, &refusal) || fmt.Sprint(refusal.Params["detail"]) != "the value could not be decoded" {
		t.Fatalf("unencodable params refusal = %v", err)
	}
}
