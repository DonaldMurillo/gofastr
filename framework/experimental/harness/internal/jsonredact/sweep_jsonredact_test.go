package jsonredact

import (
	"encoding/json"
	"testing"
)

func TestRedactionWithQuotesStaysValidJSON(t *testing.T) {
	out, ok := Strings([]byte(`{"a":"secret"}`), func(s string) string {
		if s == "secret" {
			return `x" , "y\`
		}
		return s
	})
	if !ok || !json.Valid(out) {
		t.Fatalf("redacted document is not JSON: %s", out)
	}
	var got map[string]string
	if err := json.Unmarshal(out, &got); err != nil || got["a"] != `x" , "y\` {
		t.Fatalf("got %v err=%v", got, err)
	}
}

func TestUnchangedStringsKeepTheirBytes(t *testing.T) {
	in := []byte(`{"a":"A\/b","n":1.50}`)
	out, ok := Strings(in, func(s string) string { return s })
	if !ok || string(out) != string(in) {
		t.Fatalf("got %s, want %s", out, in)
	}
}
