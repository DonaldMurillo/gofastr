package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

// An option with no value prop submits its text, as an HTML option with
// no value attribute does; value="" submitted the empty string and
// blanked the field on save. An explicit empty value stays the
// placeholder idiom.
func TestOptionWithoutValueSubmitsText(t *testing.T) {
	for _, tc := range []struct {
		props map[string]any
		want  string
	}{
		{map[string]any{"text": "Open"}, `value="Open"`},
		{map[string]any{"text": "Open", "value": nil}, `value="Open"`},
		{map[string]any{"text": "Pick one", "value": ""}, `value=""`},
		{map[string]any{"text": "Done", "value": "done"}, `value="done"`},
	} {
		got := string(RenderNode(node.Node{Kind: "option", Props: tc.props}))
		if !strings.Contains(got, tc.want) {
			t.Errorf("props %v: want %s in %q", tc.props, tc.want, got)
		}
	}
}

// The table family renders through the html builders, so th gets its
// scope default and every element keeps the role a cards collapse
// relies on.
func TestTableFamilyKeepsHTMLDefaults(t *testing.T) {
	cell := func(kind string, props map[string]any) string {
		return string(RenderNode(node.Node{Kind: kind, Props: props}))
	}
	for _, tc := range []struct {
		kind  string
		props map[string]any
		want  []string
	}{
		{"th", map[string]any{"text": "Amount"}, []string{`scope="col"`, `role="columnheader"`, `>Amount</th>`}},
		{"th", map[string]any{"text": "Row", "scope": "row"}, []string{`scope="row"`, `role="rowheader"`}},
		{"th", map[string]any{"text": "Span", "colspan": 2}, []string{`colspan="2"`}},
		{"td", map[string]any{"text": "1", "colspan": 3}, []string{`role="cell"`, `colspan="3"`, `>1</td>`}},
		{"table", map[string]any{"id": "t"}, []string{`role="table"`, `id="t"`}},
		{"thead", nil, []string{`role="rowgroup"`}},
		{"tbody", nil, []string{`role="rowgroup"`}},
		{"tr", nil, []string{`role="row"`}},
	} {
		got := cell(tc.kind, tc.props)
		for _, w := range tc.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s %v: want %s in %q", tc.kind, tc.props, w, got)
			}
		}
	}
}
