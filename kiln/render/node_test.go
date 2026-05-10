package render_test

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/kiln/render"
	"github.com/gofastr/gofastr/kiln/world"
)

func TestRenderNodeDiv(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "div",
		Props: map[string]any{"id": "main", "class": "container"},
	})
	want := `<div class="container" id="main"></div>`
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
// renderer drops those props server-side so a single bad agent turn
// can't poison the page. The kiln-* utility classes from theme.css
// are the supported alternative.
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

// Class-based styling is the supported path. Verify class survives.
func TestRenderNodeKeepsClassProp(t *testing.T) {
	got := render.RenderNode(world.Node{
		Kind:  "section",
		Props: map[string]any{"class": "kiln-section kiln-section-soft"},
	})
	if !strings.Contains(string(got), `class="kiln-section kiln-section-soft"`) {
		t.Errorf("class prop should pass through: %q", got)
	}
}
