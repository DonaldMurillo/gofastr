package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

func TestUnknownKindTraceIsEscaped(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "raw", Props: map[string]any{"value": "<script>x</script>", "html": "<b>x</b>"}}))
	if strings.Contains(got, "<script>") || strings.Contains(got, "<b>") {
		t.Fatalf("a raw kind emitted live markup: %q", got)
	}
	got = string(RenderNode(node.Node{Kind: `x--><script>alert(1)</script>`}))
	if strings.Contains(got, "<script>") || strings.Count(got, "-->") != 1 {
		t.Fatalf("an unknown kind broke out of its comment: %q", got)
	}
}

func TestTextKindEscapes(t *testing.T) {
	for _, prop := range []string{"value", "text", "content"} {
		got := string(RenderNode(node.Node{Kind: "text", Props: map[string]any{prop: "<script>x</script>"}}))
		if strings.Contains(got, "<script>") || !strings.Contains(got, "&lt;script&gt;") {
			t.Errorf("text %s = %q, want it escaped", prop, got)
		}
	}
}

func TestFormDefaultsToJSONEnctype(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "form", Props: map[string]any{"action": "/api/items"}}))
	if !strings.Contains(got, `enctype="application/json"`) || !strings.Contains(got, `method="POST"`) {
		t.Fatalf("form without enctype = %q, want a JSON POST", got)
	}
	got = string(RenderNode(node.Node{Kind: "form", Props: map[string]any{"enctype": "multipart/form-data"}}))
	if strings.Contains(got, "application/json") || !strings.Contains(got, `enctype="multipart/form-data"`) {
		t.Fatalf("explicit enctype lost: %q", got)
	}
}

func TestSectionWithoutLabelIsLabelled(t *testing.T) {
	for _, tc := range []struct {
		props map[string]any
		want  string
	}{
		{nil, `aria-label="section"`},
		{map[string]any{"id": "stats"}, `aria-label="stats"`},
		{map[string]any{"aria-label": "Totals"}, `aria-label="Totals"`},
	} {
		got := string(RenderNode(node.Node{Kind: "section", Props: tc.props}))
		if !strings.Contains(got, tc.want) {
			t.Errorf("section %v = %q, want %s", tc.props, got, tc.want)
		}
	}
}

func TestRenderNodeStripsDataFuiFamily(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "div", Props: map[string]any{
		"data-fui-local-order": "name",
		"DATA-FUI-TOGGLE":      "x",
		"data-testid":          "sentinel",
	}}))
	if !strings.Contains(got, `data-testid="sentinel"`) || strings.Contains(strings.ToLower(got), "data-fui-") {
		t.Fatalf("untrusted render = %q, want the data-fui-* family dropped and the inert marker kept", got)
	}
}

func TestListWrapsBareChildren(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "list", Children: []node.Node{
		{Kind: "text", Props: map[string]any{"value": "bare"}},
		{Kind: "li", Props: map[string]any{"id": "own"}, Children: []node.Node{{Kind: "text", Props: map[string]any{"value": "item"}}}},
	}}))
	if strings.Count(got, "<li") != 2 || !strings.Contains(got, "<ul") {
		t.Fatalf("list = %q, want two items in a ul", got)
	}
}
