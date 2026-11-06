package noderender

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/chromedp/chromedp"
)

func TestLinksRequireAccessibleContent(t *testing.T) {
	for _, test := range []struct {
		name string
		tree node.Node
		link bool
	}{
		{name: "empty link", tree: node.Node{Kind: "link", Props: map[string]any{"href": "/target"}}},
		{name: "empty anchor", tree: node.Node{Kind: "a", Props: map[string]any{"href": "/target"}}},
		{name: "whitespace", tree: node.Node{Kind: "link", Props: map[string]any{"href": "/target", "text": " \t\n"}}},
		{name: "label", tree: node.Node{Kind: "link", Props: map[string]any{"href": "/target", "aria-label": "Open target"}}, link: true},
		{name: "text", tree: node.Node{Kind: "link", Props: map[string]any{"href": "/target", "text": "Open target"}}, link: true},
		{name: "child", tree: node.Node{Kind: "link", Props: map[string]any{"href": "/target"}, Children: []node.Node{{Kind: "strong", Props: map[string]any{"text": "Open target"}}}}, link: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := nodeBrowser(t, test.tree)
			var linked bool
			if err := chromedp.Run(ctx, chromedp.Evaluate(`!!document.querySelector('a[href="/target"]')`, &linked)); err != nil {
				t.Fatal(err)
			}
			if linked != test.link {
				t.Fatalf("interactive link = %v, want %v", linked, test.link)
			}
		})
	}
}
