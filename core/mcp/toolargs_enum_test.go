package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

// An enum member that holds an object or an array compares its numbers the
// way a scalar member does: {"n":1} off the wire is a json.Number or a
// float64, and Go equality against a declared int(1) refused it.
func TestCompoundEnumComparesNumbers(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{
		"pick": map[string]any{"enum": []any{
			map[string]any{"n": 1, "tags": []any{"a", 2}},
			[]any{1, map[string]any{"m": int64(3)}},
		}},
	}}
	accept := []string{
		`{"n":1,"tags":["a",2]}`,
		`{"n":1.0,"tags":["a",2e0]}`,
		`[1,{"m":3}]`,
	}
	for _, raw := range accept {
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := validateToolArgs(schema, map[string]any{"pick": v}); err != nil {
			t.Errorf("%s refused: %v", raw, err)
		}
		if err := validateToolArgs(schema, map[string]any{"pick": decodeNumbers(t, raw)}); err != nil {
			t.Errorf("%s as json.Number refused: %v", raw, err)
		}
	}
	refuse := []string{
		`{"n":2,"tags":["a",2]}`,
		`{"n":1,"tags":["a",2],"x":0}`,
		`{"n":1}`,
		`{"n":1,"tagz":["a",2]}`,
		`{"n":"1","tags":["a",2]}`,
		`[1]`,
		`[1,{"m":3},4]`,
		`[{"m":3},1]`,
	}
	for _, raw := range refuse {
		if err := validateToolArgs(schema, map[string]any{"pick": decodeNumbers(t, raw)}); err == nil {
			t.Errorf("SECURITY: [tool-args] %s matched an enum it is not in", raw)
		}
	}
	// A typed Go member, named key type included, compares the same way.
	type key string
	typed := map[string]any{"enum": []any{map[key]int{"k": 5}, []string{"x"}}}
	for _, raw := range []string{`{"k":5}`, `["x"]`} {
		if err := validateValue(typed, decodeNumbers(t, raw), "v"); err != nil {
			t.Errorf("%s against a typed member refused: %v", raw, err)
		}
	}
}

func decodeNumbers(t *testing.T, raw string) any {
	t.Helper()
	var v any
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
