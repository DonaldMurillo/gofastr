package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A right-to-left tree indents from the right, so its horizontal arrows
// swap: ArrowLeft opens a branch and goes into it, ArrowRight goes back
// out and closes it, as the menu's submenu keys already do. The tree
// read only the left-to-right meaning (found by core-ui/check's keyguard
// lint).
func TestE2E_TreeArrowsFlipInRTL(t *testing.T) {
	tree := Tree(TreeProps{ID: "rt", Label: "Files", Nodes: []TreeNode{
		{ID: "r-branch", Label: "Branch", Children: []TreeNode{{ID: "r-leaf", Label: "Leaf"}}},
		{ID: "r-other", Label: "Other"},
	}}, nil)
	b := startBehaviorServer(t, `<div dir="rtl">`+string(tree)+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-tree")) {
		t.Fatal("module never loaded")
	}
	state := func() string {
		return evalString(t, ctx, `document.activeElement.id + ':' + document.getElementById('r-branch').getAttribute('aria-expanded')`)
	}
	if err := chromedp.Run(ctx, chromedp.Focus(`#r-branch`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct{ name, key, want string }{
		{"ArrowLeft", kb.ArrowLeft, "r-branch:true"},
		{"ArrowLeft", kb.ArrowLeft, "r-leaf:true"},
		{"ArrowRight", kb.ArrowRight, "r-branch:true"},
		{"ArrowRight", kb.ArrowRight, "r-branch:false"},
	} {
		if err := chromedp.Run(ctx, chromedp.KeyEvent(step.key)); err != nil {
			t.Fatal(err)
		}
		if !pollTrue(ctx, `document.activeElement.id + ':' + document.getElementById('r-branch').getAttribute('aria-expanded') === '`+step.want+`'`) {
			t.Fatalf("after %s in RTL [focus:branch expanded] = %q, want %q", step.name, state(), step.want)
		}
	}
}
