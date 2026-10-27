package handler

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// Bind decoded the body with a plain json.Decoder, so a number under a
// map[string]any field reached the handler as 9007199254740992.
func TestBindKeepsBigIntegers(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"meta":{"n":9007199254740993,"f":1.5}}`))
	r.Header.Set("Content-Type", "application/json")
	var in struct {
		Meta map[string]any `json:"meta"`
	}
	if err := Bind(r, &in); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(in.Meta["n"]); got != "9007199254740993" {
		t.Errorf("meta.n = %s, want 9007199254740993", got)
	}
	if got, ok := in.Meta["f"].(float64); !ok || got != 1.5 {
		t.Errorf("meta.f = %#v, want float64 1.5", in.Meta["f"])
	}
}
