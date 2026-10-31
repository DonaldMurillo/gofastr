package noderender

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

// A textarea's value is its content, not an attribute: rendered as
// value="…" the browser shows an empty box, and submitting the edit form
// blanks the stored field (2026-10-27 K8). The prop spellings are the
// text kind's: value, then text, then content.
func TestTextAreaRendersItsValue(t *testing.T) {
	for _, key := range []string{"value", "text", "content"} {
		got := string(RenderNode(node.Node{Kind: "textarea", Props: map[string]any{
			"name": "bio", key: "line one\n<b>&</b>",
		}}))
		if !strings.Contains(got, ">line one\n&lt;b&gt;&amp;&lt;/b&gt;</textarea>") {
			t.Errorf("%s: content missing or unescaped in %q", key, got)
		}
		if strings.Contains(got, key+"=") {
			t.Errorf("%s: emitted as an attribute in %q", key, got)
		}
	}
	got := string(RenderNode(node.Node{Kind: "textarea", Props: map[string]any{
		"name": "bio", "value": "v", "text": "t", "content": "c",
	}}))
	if !strings.Contains(got, ">v</textarea>") {
		t.Errorf("value should win over text and content: %q", got)
	}
}

// Children stay out: a textarea child kind would close the outer
// element early and turn every later child into live markup.
func TestTextAreaIgnoresChildren(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "textarea", Props: map[string]any{"name": "bio"}, Children: []node.Node{
		{Kind: "textarea", Props: map[string]any{"name": "inner"}},
		{Kind: "button", Props: map[string]any{"label": "after"}},
	}}))
	if strings.Count(got, "<textarea") != 1 || strings.Contains(got, "<button") {
		t.Fatalf("children rendered into the textarea: %q", got)
	}
}
