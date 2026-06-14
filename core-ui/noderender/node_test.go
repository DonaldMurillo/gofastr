package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

func TestRenderNodeBasicElements(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "div", Props: map[string]any{"class": "card"}, Children: []node.Node{
		{Kind: "heading", Props: map[string]any{"level": 2, "text": "Title"}},
		{Kind: "paragraph", Props: map[string]any{"text": "body"}},
	}}))
	for _, want := range []string{`class="card"`, "<h2", "Title", "<p", "body"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestRenderNodeUnknownKindLeavesTrace(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "mystery_kind"}))
	if !strings.Contains(got, "mystery_kind") {
		t.Errorf("unknown kind should leave a trace: %q", got)
	}
}

func TestRenderNodeDropsDangerousAttrs(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "div", Props: map[string]any{
		"style":   "color:red",
		"onclick": "alert(1)",
		"data-ok": "yes",
	}}))
	if strings.Contains(got, "style=") || strings.Contains(got, "onclick") {
		t.Errorf("dangerous attrs leaked: %q", got)
	}
	if !strings.Contains(got, "data-ok") {
		t.Errorf("safe data attr dropped: %q", got)
	}
}

func TestRenderNodeEscapesText(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "paragraph", Props: map[string]any{"text": "<script>x</script>"}}))
	if strings.Contains(got, "<script>") {
		t.Errorf("text not escaped: %q", got)
	}
}
