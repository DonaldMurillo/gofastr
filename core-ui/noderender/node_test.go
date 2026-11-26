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

func TestButtonNodeWithoutLabelDegrades(t *testing.T) {
	// `label` comes from Kiln IR props with no default. html.Button's
	// empty-label panic is a fine contract for hand-written Go, but a
	// labelless button NODE must degrade instead of crashing every
	// render of the page (recover middleware is opt-in).
	got := string(RenderNode(node.Node{Kind: "button"}))
	if !strings.Contains(got, "<button") {
		t.Fatalf("labelless button node did not render a button: %q", got)
	}
	if !strings.Contains(got, `aria-label="button"`) {
		t.Errorf("degraded button must carry a placeholder aria-label: %q", got)
	}
	// A caller-supplied aria-label must win over the placeholder.
	got = string(RenderNode(node.Node{Kind: "button", Props: map[string]any{
		"aria-label": "Close",
	}}))
	if !strings.Contains(got, `aria-label="Close"`) {
		t.Errorf("explicit aria-label lost: %q", got)
	}
}

func TestRenderNodeEscapesText(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "paragraph", Props: map[string]any{"text": "<script>x</script>"}}))
	if strings.Contains(got, "<script>") {
		t.Errorf("text not escaped: %q", got)
	}
}

func TestFalseBooleanAttributesAreAbsent(t *testing.T) {
	for _, cfg := range []struct{ kind, attr string }{
		{"select", "multiple"}, {"input", "checked"}, {"button", "disabled"},
	} {
		got := string(RenderNode(node.Node{Kind: cfg.kind, Props: map[string]any{cfg.attr: false}}))
		if strings.Contains(got, " "+cfg.attr+"=") {
			t.Errorf("%s activated %s=false: %s", cfg.kind, cfg.attr, got)
		}
	}
	got := string(RenderNode(node.Node{Kind: "input", Props: map[string]any{"checked": true, "aria-hidden": false}}))
	if !strings.Contains(got, " checked=") || !strings.Contains(got, `aria-hidden="false"`) {
		t.Fatalf("true and ARIA boolean state lost: %s", got)
	}
}

func TestTableCellsKeepSpans(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "td", Props: map[string]any{"colspan": 2, "rowspan": 3, "headers": "heading"}}))
	for _, want := range []string{`colspan="2"`, `rowspan="3"`, `headers="heading"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s: %s", want, got)
		}
	}
}

func TestLandmarkUsesAriaLabelledByWithoutFallback(t *testing.T) {
	got := string(RenderNode(node.Node{Kind: "section", Props: map[string]any{"aria-labelledby": "heading"}}))
	if !strings.Contains(got, `aria-labelledby="heading"`) || strings.Contains(got, " aria-label=") {
		t.Fatalf("unexpected landmark accessible name: %s", got)
	}
}
