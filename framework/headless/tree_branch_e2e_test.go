package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestTreeBranchEnterFollowsLink(t *testing.T) {
	for _, lazy := range []bool{false, true} {
		node := TreeNode{ID: "docs", Label: "Docs", Href: "/docs", Children: []TreeNode{{ID: "guide", Label: "Guide", Href: "/guide"}}}
		if lazy {
			node.Children = nil
			node.LazyPath = "/__hui/ok"
		}
		tree := Tree(TreeProps{ID: "nav", Label: "Documentation", LazySignalPrefix: "children", Nodes: []TreeNode{node}}, nil)
		server := startBehaviorServer(t, string(tree))
		ctx := behaviorPage(t, server)
		if !pollTrue(ctx, controlsLoadedExpr("headless-tree")) {
			t.Fatal("tree module did not load")
		}
		var followed int
		var expanded bool
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`window.__followed = 0; document.querySelector('#docs a').addEventListener('click', event => { event.preventDefault(); window.__followed++; });`, nil),
			chromedp.Focus(`#docs`, chromedp.ByID),
			chromedp.KeyEvent("\r"),
			chromedp.Evaluate(`window.__followed`, &followed),
			chromedp.Evaluate(`document.getElementById('docs').getAttribute('aria-expanded') === 'true'`, &expanded),
		); err != nil {
			t.Fatal(err)
		}
		if followed != 1 || expanded {
			t.Fatalf("lazy=%v: Enter followed=%d expanded=%v, want 1/false", lazy, followed, expanded)
		}
		if err := chromedp.Run(ctx,
			chromedp.KeyEvent(" "),
			chromedp.Evaluate(`window.__followed`, &followed),
			chromedp.Evaluate(`document.getElementById('docs').getAttribute('aria-expanded') === 'true'`, &expanded),
		); err != nil {
			t.Fatal(err)
		}
		if followed != 1 || !expanded {
			t.Fatalf("lazy=%v: Space followed=%d expanded=%v, want 1/true", lazy, followed, expanded)
		}
	}
}
