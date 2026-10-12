package ui

import (
	"strings"
	"testing"
)

// FilterRows draws its value inputs outside any FormField, so the input
// itself must name the field sheet that styles .fui-input: on a page with
// no other field (the gallery tile) nothing else would fetch it, and the
// input drew as bare text.
func TestFilterRowsValueInputLoadsFieldSheet(t *testing.T) {
	out := string(FilterRows(FilterRowsConfig{
		ID: "f", FieldName: "f", OpName: "op", ValueName: "v",
		Legend: "Filters", FieldLabel: "Field", OpLabel: "Operator", ValueLabel: "Value",
		Fields:    []SelectOption{{Value: "status", Text: "Status"}},
		Operators: []SelectOption{{Value: "eq", Text: "is"}},
		Rows:      []FilterRow{{Field: "status", Op: "eq", Value: "open"}},
	}))
	i := strings.Index(out, `id="f-1-v"`)
	if i < 0 {
		t.Fatalf("no value input:\n%s", out)
	}
	start := strings.LastIndex(out[:i], "<input")
	tag := out[start : strings.Index(out[start:], ">")+start]
	if !strings.Contains(tag, `data-cui-comp="ui-form-field"`) {
		t.Errorf("the value input does not carry the field sheet's marker: %s", tag)
	}
}
