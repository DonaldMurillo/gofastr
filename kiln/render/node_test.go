package render_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/render"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestRenderNodeDiv(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "div",
		Props: map[string]any{"id": "main", "class": "container"},
	})
	want := `<div id="main"></div>`
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderNodeHeading(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "heading",
		Props: map[string]any{"level": float64(2), "text": "Hello"},
	})
	if !strings.HasPrefix(string(got), "<h2") || !strings.Contains(string(got), "Hello") {
		t.Errorf("heading render = %q", got)
	}
}

func TestRenderNodeText(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "text",
		Props: map[string]any{"value": "<script>alert(1)</script>"},
	})
	if strings.Contains(string(got), "<script>") {
		t.Errorf("text must escape: %q", got)
	}
	if !strings.Contains(string(got), "&lt;script&gt;") {
		t.Errorf("text not escaped: %q", got)
	}
}

func TestRenderNodeButton(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "button",
		Props: map[string]any{"label": "Save", "id": "save-btn"},
	})
	if !strings.Contains(string(got), `<button`) || !strings.Contains(string(got), "Save") {
		t.Errorf("button render = %q", got)
	}
}

func TestRenderNodeLink(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "link",
		Props: map[string]any{"href": "/posts", "text": "Posts"},
	})
	if !strings.Contains(string(got), `<a `) || !strings.Contains(string(got), `href="/posts"`) {
		t.Errorf("link render = %q", got)
	}
}

func TestRenderNodeImage(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "image",
		Props: map[string]any{"src": "/cat.png", "alt": "a cat"},
	})
	if !strings.Contains(string(got), `<img`) || !strings.Contains(string(got), `src="/cat.png"`) {
		t.Errorf("image render = %q", got)
	}
}

func TestRenderNodeListUnordered(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind: "list",
		Children: []world.Node{
			{Kind: "text", Props: map[string]any{"value": "a"}},
			{Kind: "text", Props: map[string]any{"value": "b"}},
		},
	})
	s := string(got)
	if !strings.HasPrefix(s, "<ul") {
		t.Errorf("list should default to <ul>: %q", s)
	}
	if strings.Count(s, "<li") != 2 {
		t.Errorf("expected 2 <li…>: %q", s)
	}
}

func TestRenderNodeListOrdered(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "list",
		Props: map[string]any{"ordered": true},
		Children: []world.Node{
			{Kind: "text", Props: map[string]any{"value": "a"}},
		},
	})
	if !strings.HasPrefix(string(got), "<ol") {
		t.Errorf("ordered list should be <ol>: %q", got)
	}
}

func TestRenderNodeChildren(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind: "div",
		Children: []world.Node{
			{Kind: "heading", Props: map[string]any{"level": float64(1), "text": "Title"}},
			{Kind: "paragraph", Children: []world.Node{
				{Kind: "text", Props: map[string]any{"value": "body"}},
			}},
		},
	})
	s := string(got)
	if !strings.Contains(s, "<h1") || !strings.Contains(s, "Title") {
		t.Errorf("missing heading: %q", s)
	}
	if !strings.Contains(s, "<p>") || !strings.Contains(s, "body") {
		t.Errorf("missing paragraph: %q", s)
	}
}

func TestRenderNodeUnknownKind(t *testing.T) {
	got := render.RenderNode(world.Node{Kind: "unknown_kind"})
	if !strings.Contains(string(got), "unknown_kind") {
		t.Errorf("unknown kind should leave a trace: %q", got)
	}
}

func TestRenderNodeAttrsEscape(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "div",
		Props: map[string]any{"class": `evil"onclick="alert(1)`},
	})
	if strings.Contains(string(got), `onclick="alert(1)`) {
		t.Errorf("attrs not escaped: %q", got)
	}
}

// Strict CSP rejects inline style="…" and on*="…" handlers. The
// renderer drops those props server-side so a legacy journal cannot
// poison the page. Typed design-system kinds own styling.
func TestRenderNodeDropsDangerousAttrs(t *testing.T) {
	cases := []struct {
		name string
		prop string
		val  any
	}{
		{"style", "style", "color:red;background:#000"},
		{"onclick", "onclick", "alert(1)"},
		{"onerror", "onerror", "alert(2)"},
		{"onmouseover", "onmouseover", "evil()"},
		{"srcdoc", "srcdoc", "<script>alert(3)</script>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := render.RenderNode(world.Node{
				Kind:  "section",
				Props: map[string]any{"id": "x", tc.prop: tc.val},
			})
			s := string(got)
			if strings.Contains(s, tc.prop+"=") {
				t.Errorf("dangerous attr %q leaked into output: %s", tc.prop, s)
			}
			// id should still pass through.
			if !strings.Contains(s, `id="x"`) {
				t.Errorf("safe attr id was unexpectedly dropped: %s", s)
			}
		})
	}
}

// Agent-authored class names would create a second styling surface. Kiln
// strips them from legacy worlds; current protocol calls reject them.
func TestRenderNodeDropsClassProp(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "section",
		Props: map[string]any{"class": "kiln-section kiln-section-soft"},
	})
	if strings.Contains(string(got), `kiln-section`) {
		t.Errorf("class prop should be design-system-owned: %q", got)
	}
}

func TestRenderNodeUsesDesignSystemCard(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind: "card", Props: map[string]any{"heading": "Current", "variant": "outlined"},
		Children: []world.Node{{Kind: "paragraph", Props: map[string]any{"text": "Built from framework/ui"}}},
	})
	s := string(got)
	if !strings.Contains(s, `data-fui-comp="ui-card"`) || !strings.Contains(s, "Built from framework/ui") {
		t.Fatalf("card did not use current design-system component: %q", s)
	}
}

// TestRawNodeDoesNotEmitUnescapedHTML asserts a `raw` node in untrusted IR
// (an agent-authored world.Node tree carries arbitrary Kind values, with no
// whitelist) cannot inject live <script>. The strict CSP blocks inline
// script in the browser, but the IR must not be a raw-HTML sink either.
// (finding k-raw-1)
func TestRawNodeDoesNotEmitUnescapedHTML(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "raw",
		Props: map[string]any{"value": "<script>alert(1)</script>"},
	})
	if strings.Contains(string(got), "<script>alert(1)</script>") {
		t.Errorf("raw node leaked unescaped HTML: %q", got)
	}
}

// Design-system kinds must render wherever they appear, including inside
// semantic leaf containers — the leaf branch must not hand the whole subtree
// to core noderender, whose vocabulary has no typed kinds.
func TestRenderNodeCardInsideDiv(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind: "div",
		Children: []world.Node{
			{Kind: "card", Props: map[string]any{"heading": "Revenue"}},
		},
	})
	s := string(got)
	if !strings.Contains(s, `data-fui-comp="ui-card"`) || !strings.Contains(s, "Revenue") {
		t.Fatalf("card inside div vanished: %q", s)
	}
	if strings.Contains(s, "unknown kind") {
		t.Fatalf("leaf subtree fell through to core noderender: %q", s)
	}
}

func TestRenderNodeLeafChildrenRenderOnce(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind: "div",
		Children: []world.Node{
			{Kind: "paragraph", Props: map[string]any{"text": "once"}},
		},
	})
	if n := strings.Count(string(got), "once"); n != 1 {
		t.Fatalf("leaf children rendered %d times, want 1: %q", n, got)
	}
}

func TestRenderNodeDropsNestedClassProp(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind: "div",
		Children: []world.Node{
			{Kind: "div", Props: map[string]any{"class": "kiln-rogue"}},
		},
	})
	if strings.Contains(string(got), "kiln-rogue") {
		t.Errorf("nested leaf kept agent-authored class: %q", got)
	}
}
