package mcp

import "testing"

// Arguments with two faults are refused naming the same one on every call:
// validateObject ranged the argument and property maps, so the refusal
// named whichever key the iteration reached first.
func TestToolArgRefusalIsDeterministic(t *testing.T) {
	cases := map[string]struct {
		schema map[string]any
		args   map[string]any
	}{
		"unknown arguments": {
			schema: map[string]any{"type": "object", "additionalProperties": false,
				"properties": map[string]any{"a": map[string]any{"type": "string"}}},
			args: map[string]any{"x1": 1, "x2": 2, "x3": 3, "x4": 4},
		},
		"invalid properties": {
			schema: map[string]any{"type": "object", "properties": map[string]any{
				"p1": map[string]any{"type": "string"}, "p2": map[string]any{"type": "string"},
				"p3": map[string]any{"type": "string"}, "p4": map[string]any{"type": "string"},
			}},
			args: map[string]any{"p1": 1, "p2": 2, "p3": 3, "p4": 4},
		},
		"extras against a subschema": {
			schema: map[string]any{"type": "object",
				"additionalProperties": map[string]any{"type": "string"}},
			args: map[string]any{"e1": 1, "e2": 2, "e3": 3, "e4": 4},
		},
	}
	for name, c := range cases {
		first := validateToolArgs(c.schema, c.args)
		if first == nil {
			t.Fatalf("%s: accepted", name)
		}
		for range 64 {
			if got := validateToolArgs(c.schema, c.args); got == nil || got.Error() != first.Error() {
				t.Fatalf("%s: refusal changed between calls: %q then %v", name, first, got)
			}
		}
	}
}
