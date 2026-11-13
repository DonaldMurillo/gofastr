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
