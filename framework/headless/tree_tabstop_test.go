package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A tree is one tab stop: Tab from the focused treeitem leaves the
// tree (APG tree view). Each leaf's <a href> inside role=treeitem was
// a tab stop of its own, so Tab walked into every row's link.
func TestE2E_TreeIsOneTabStop(t *testing.T) {
	tree := Tree(TreeProps{ID: "nav", Label: "Files", Nodes: []TreeNode{
		{ID: "a", Label: "A", Href: "/a"}, {ID: "b", Label: "B", Href: "/b"},
	}}, nil)
	b := startBehaviorServer(t, string(tree)+`<button id="after">after</button>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-tree")) {
		t.Fatal("module never loaded")
	}
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#a`, chromedp.ByID),
		chromedp.KeyEvent("\t"),
		chromedp.Evaluate(`document.activeElement.id || document.activeElement.outerHTML.slice(0, 60)`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got != "after" {
		t.Fatalf("Tab from the focused treeitem landed on %q, want the control after the tree", got)
	}
	// Enter on the treeitem still follows its link (the module clicks
	// it), so the link out of the tab order is not a dead link.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#a a').addEventListener('click', (e) => { e.preventDefault(); window.__followed = true; })`, nil),
		chromedp.Focus(`#a`, chromedp.ByID),
		chromedp.KeyEvent("\r"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `window.__followed === true`) {
		t.Fatal("Enter on the treeitem did not follow its link")
	}
}
